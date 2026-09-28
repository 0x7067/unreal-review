package agent

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

type scriptedAdapter struct {
	mu        sync.Mutex
	responses []llm.Response
	requests  []llm.Request
}

func (a *scriptedAdapter) Respond(ctx context.Context, req llm.Request, _ llm.RequestOptions) (llm.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.requests = append(a.requests, req)
	if len(a.responses) == 0 {
		return llm.Response{}, errors.New("script exhausted")
	}
	next := a.responses[0]
	a.responses = a.responses[1:]
	return next, nil
}

func findingArguments() string {
	return `{"path":"internal/agent/agent.go","start_line":42,"severity":"warning","body":"The retry loop leaks the cancel function."}`
}

func toolCallResponse(id string) llm.Response {
	return llm.Response{
		ID:   id,
		Stop: llm.StopComplete,
		Output: []llm.Item{{
			ProviderID: id + "-call",
			Type:       llm.ItemToolCall,
			Data:       llm.ToolCall{CallID: "call-1", Name: review.RecordFindingTool, Arguments: findingArguments()},
		}},
		Usage: llm.Usage{
			InputTokens:       120,
			OutputTokens:      30,
			ReasoningTokens:   10,
			CachedInputTokens: 40,
			Raw:               jsontext.Value(`{"cost":0.013}`),
		},
	}
}

func messageResponse(id, text string) llm.Response {
	return llm.Response{
		ID:   id,
		Stop: llm.StopComplete,
		Output: []llm.Item{{
			ProviderID: id + "-msg",
			Type:       llm.ItemMessage,
			Data:       llm.Message{Role: llm.RoleAssistant, Text: text},
		}},
		Usage: llm.Usage{InputTokens: 180, OutputTokens: 20},
	}
}

func reviewRequest(t *testing.T) review.AgentRequest {
	t.Helper()
	return review.AgentRequest{
		Workspace:    t.TempDir(),
		ReviewID:     "review-test",
		FindingsPath: filepath.Join(t.TempDir(), "findings.jsonl"),
		Prompt:       "Review the diff.",
		SystemPrompt: "You review a git unified diff.",
		Model:        "test-model",
	}
}

func recordedFinding() findings.Finding {
	return findings.Finding{
		Path:      "internal/agent/agent.go",
		StartLine: 42,
		EndLine:   42,
		Anchor:    findings.AnchorNew,
		Severity:  findings.SeverityWarning,
		Body:      "The retry loop leaks the cancel function.",
	}
}

func TestRunRecordsFindingAndSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	req := reviewRequest(t)
	adapter := &scriptedAdapter{responses: []llm.Response{
		toolCallResponse("resp-1"),
		messageResponse("resp-2", "The warning finding describes a goroutine leak in the review retry loop."),
	}}

	result, err := Harness{ThinkingLevel: "high"}.run(t.Context(), adapter, req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatalf("read findings: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings: got %d, want 1", len(report.Findings))
	}
	want := recordedFinding()
	want.ID = findings.Fingerprint(want)
	if report.Findings[0] != want {
		t.Errorf("finding: got %+v, want %+v", report.Findings[0], want)
	}
	if report.Summary != "The warning finding describes a goroutine leak in the review retry loop." {
		t.Errorf("summary: got %q", report.Summary)
	}

	wantCost := findings.Cost{
		AmountUSD:         0.013,
		Currency:          "USD",
		InputTokens:       300,
		OutputTokens:      50,
		ReasoningTokens:   10,
		CachedInputTokens: 40,
		Requests:          2,
	}
	if result.Cost != wantCost {
		t.Errorf("cost: got %+v, want %+v", result.Cost, wantCost)
	}

	toolNames := map[string]bool{}
	for _, advertised := range adapter.requests[0].Tools {
		toolNames[advertised.Name] = true
	}
	if !toolNames[review.RecordFindingTool] {
		t.Errorf("first request tools %v: missing %q", toolNames, review.RecordFindingTool)
	}
	lastRequest := adapter.requests[len(adapter.requests)-1]
	var sawToolResult bool
	for _, item := range lastRequest.Input {
		if result, ok := item.Data.(llm.ToolResult); ok && result.CallID == "call-1" {
			sawToolResult = true
		}
	}
	if !sawToolResult {
		t.Errorf("last request carries no tool result for call-1")
	}
}

func TestRunCorrectsBrokenSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	req := reviewRequest(t)
	adapter := &scriptedAdapter{responses: []llm.Response{
		toolCallResponse("resp-1"),
		messageResponse("resp-2", findings.CleanVerdict),
		messageResponse("resp-3", "The warning finding describes a goroutine leak in the review retry loop."),
	}}

	h := Harness{ThinkingLevel: "high"}
	if _, err := h.run(t.Context(), adapter, req); err != nil {
		t.Fatalf("run: %v", err)
	}

	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatalf("read findings: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Errorf("findings: got %d, want 1", len(report.Findings))
	}
	if report.Summary != "The warning finding describes a goroutine leak in the review retry loop." {
		t.Errorf("summary: got %q", report.Summary)
	}
	if len(adapter.requests) != 3 {
		t.Fatalf("requests: got %d, want 3", len(adapter.requests))
	}
	correction := adapter.requests[2]
	toldWhatBroke := false
	for _, item := range correction.Input {
		if message, ok := item.Data.(llm.Message); ok && message.Role == llm.RoleUser && strings.Contains(message.Text, "summary contract") {
			toldWhatBroke = true
		}
	}
	if !toldWhatBroke {
		t.Errorf("correction turn does not tell the model what broke the summary contract")
	}
}

func TestRunFailsAfterRepeatedSummaryViolations(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	req := reviewRequest(t)
	adapter := &scriptedAdapter{responses: []llm.Response{
		toolCallResponse("resp-1"),
		messageResponse("resp-2", findings.CleanVerdict),
		messageResponse("resp-3", findings.CleanVerdict),
		messageResponse("resp-4", findings.CleanVerdict),
	}}

	h := Harness{ThinkingLevel: "high"}
	_, err := h.run(t.Context(), adapter, req)
	if err == nil {
		t.Fatal("run: want an error after repeated summary violations")
	}
	if !strings.Contains(err.Error(), "summary breaks the contract") {
		t.Errorf("error: got %q, want it to name the broken contract", err)
	}

	report, readErr := findings.ReadFile(req.FindingsPath)
	if readErr != nil {
		t.Fatalf("read findings: %v", readErr)
	}
	if len(report.Findings) != 1 {
		t.Errorf("findings: got %d, want the recorded finding to survive", len(report.Findings))
	}
	if report.Summary != "" {
		t.Errorf("summary: got %q, want no summary appended", report.Summary)
	}
}

func TestRunRecordsCleanSummaryWithoutFindings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	req := reviewRequest(t)
	summary := "No material issues in the reviewed registry and workflow changes."
	adapter := &scriptedAdapter{responses: []llm.Response{
		messageResponse("resp-1", summary),
	}}

	h := Harness{ThinkingLevel: "high"}
	if _, err := h.run(t.Context(), adapter, req); err != nil {
		t.Fatalf("run: %v", err)
	}

	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatalf("read findings: %v", err)
	}
	if len(report.Findings) != 0 {
		t.Errorf("findings: got %d, want 0", len(report.Findings))
	}
	if report.Summary != summary {
		t.Errorf("summary: got %q, want %q", report.Summary, summary)
	}
}

func TestSanitizeLevel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"", "high"},
		{"  high  ", "high"},
		{"low", "low"},
		{"medium", "medium"},
		{"xhigh", "xhigh"},
		{"max", "max"},
	}
	for _, test := range tests {
		if got, err := SanitizeLevel(test.input); err != nil || got != test.want {
			t.Errorf("SanitizeLevel(%q): got (%q, %v), want (%q, nil)", test.input, got, err, test.want)
		}
	}
	if _, err := SanitizeLevel("ultra"); err == nil {
		t.Error(`SanitizeLevel("ultra"): want an error`)
	}
}

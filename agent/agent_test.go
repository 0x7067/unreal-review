package agent

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/review"
)

type scriptedAdapter struct {
	mu        sync.Mutex
	responses []llm.Response
}

func (a *scriptedAdapter) Respond(ctx context.Context, _ llm.Request, _ llm.RequestOptions) (llm.Response, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.responses) == 0 {
		return llm.Response{}, errors.New("script exhausted")
	}
	next := a.responses[0]
	a.responses = a.responses[1:]
	return next, nil
}

func TestSummaryCorrectionZeroFindingsPointsAtRecordFinding(t *testing.T) {
	err := errors.New(`summary of a review with no findings must start with "No material issues"`)
	zero := summaryCorrection(err, 0)
	if !strings.Contains(zero, "You recorded 0 findings in this task.") || !strings.Contains(zero, "call record_finding now") || !strings.Contains(zero, `starts with "No material issues"`) {
		t.Fatalf("zero = %q", zero)
	}
	if !strings.HasPrefix(zero, "Your final message is the review summary, and it breaks the summary contract: "+err.Error()+". Reply with only the corrected summary.") {
		t.Fatalf("zero = %q", zero)
	}
	some := summaryCorrection(err, 2)
	want := "Your final message is the review summary, and it breaks the summary contract: " + err.Error() + ". Reply with only the corrected summary."
	if some != want || strings.Contains(some, "recorded 0 findings") || strings.Contains(some, "record_finding") {
		t.Fatalf("count>0 = %q", some)
	}
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
			TokenUsage: llm.TokenUsage{
				InputTokens:       120,
				OutputTokens:      30,
				ReasoningTokens:   10,
				CachedInputTokens: 40,
			},
			Raw: jsontext.Value(`{"cost":0.013}`),
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
		Usage: llm.Usage{TokenUsage: llm.TokenUsage{InputTokens: 180, OutputTokens: 20}},
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

func TestLoopbackHTTPClientRejectsOffHostRedirect(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/steal", http.StatusFound)
	}))
	t.Cleanup(stub.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, stub.URL+"/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := LoopbackHTTPClient().Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("off-host redirect was followed")
	}
	if !strings.Contains(err.Error(), "refusing redirect to example.com: not loopback") {
		t.Fatalf("error = %v, want refusing redirect", err)
	}
}

func TestLoopbackHTTPClientAllowsLoopbackRedirect(t *testing.T) {
	next := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(next.Close)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, next.URL+"/ok", http.StatusFound)
	}))
	t.Cleanup(stub.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, stub.URL+"/v1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := LoopbackHTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status %s", resp.Status)
	}
}

func TestLoopbackModelAdapterRefusesOffHostRedirect(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization %q", r.Header.Get("Authorization"))
		}
		http.Redirect(w, r, "https://example.com/steal", http.StatusFound)
	}))
	t.Cleanup(stub.Close)

	adapter, closeAdapter, err := newLoopbackModelAdapter("secret", stub.URL+"/api/v1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = closeAdapter() })
	_, err = adapter.Respond(t.Context(), llm.Request{
		Model: llm.Model{ID: "test"},
		Input: []llm.Item{{
			Type: llm.ItemMessage,
			Data: llm.Message{Role: llm.RoleUser, Text: "hi"},
		}},
	}, llm.RequestOptions{})
	if err == nil || !strings.Contains(err.Error(), "refusing redirect to example.com: not loopback") {
		t.Fatalf("error = %v, want refusing redirect", err)
	}
}

func TestHarnessRunRejectsNonLoopbackOpenRouter(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(openRouterBaseEnv, "https://openrouter.ai/api/v1")
	_, err := (Harness{APIKey: "dummy", ThinkingLevel: "high"}).Run(t.Context(), reviewRequest(t))
	if err == nil || !strings.Contains(err.Error(), `host "openrouter.ai" is not loopback`) {
		t.Fatalf("error = %v, want a loopback refusal", err)
	}
}

func TestHarnessRunUsesLocalOpenRouterAndReports401(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	arrived := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.Host, "openrouter.ai") {
			t.Errorf("request host %q", r.Host)
		}
		if r.Header.Get("Authorization") != "Bearer dummy" {
			t.Errorf("authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"Unauthorized","code":401}}`))
		select {
		case arrived <- struct{}{}:
		default:
		}
	}))
	t.Cleanup(server.Close)
	t.Setenv(openRouterBaseEnv, server.URL+"/api/v1")

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req := reviewRequest(t)
	_, err := (Harness{APIKey: "dummy", ThinkingLevel: "high"}).Run(ctx, req)
	select {
	case <-arrived:
	default:
		t.Fatal("local OpenRouter request did not arrive")
	}
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("run: %v, want status 401", err)
	}
}

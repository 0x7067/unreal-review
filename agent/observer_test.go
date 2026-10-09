package agent

import (
	"context"
	"encoding/json/jsontext"
	"os"
	"path/filepath"
	"testing"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"github.com/0x7067/unreal-review/findings"
)

func completedValueOperation(t *testing.T, value jsontext.Value) operation.Operation {
	t.Helper()
	spec, err := operation.NewValueSpec(value)
	if err != nil {
		t.Fatalf("build value operation: %v", err)
	}
	return operation.Operation{
		Type:    spec.Type,
		Version: spec.Version,
		Status:  operation.StatusCompleted,
		State:   spec.State,
	}
}

func recordToolCallItem(op operation.Operation) sessionstore.Item {
	return sessionstore.Item{
		Kind: sessionstore.ItemToolCallStatus,
		Data: sessionstore.ToolCallStatus{
			CallID:     "call-9",
			Status:     tool.CallStatus{WaitingFor: []operation.ID{op.ID}},
			Operations: []operation.Operation{op},
		},
	}
}

func TestObserverAppendsFindingFromCompletedToolCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.jsonl")
	_, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer := newSessionObserver("review-1", path, nil, cancel)

	observer.Observe("review-1", recordToolCallItem(completedValueOperation(t, jsontext.Value(
		`{"path":"a/b.go","start_line":7,"severity":"error","body":"Index out of range on empty input."}`,
	))))

	if err := observer.Err(); err != nil {
		t.Fatalf("observer error: %v", err)
	}
	report, err := findings.ReadFile(path)
	if err != nil {
		t.Fatalf("read findings: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings: got %d, want 1", len(report.Findings))
	}
	want := findings.Finding{
		ID:        "ignored-here",
		Path:      "a/b.go",
		StartLine: 7,
		EndLine:   7,
		Anchor:    findings.AnchorNew,
		Severity:  findings.SeverityError,
		Body:      "Index out of range on empty input.",
	}
	want.ID = findings.Fingerprint(want)
	if report.Findings[0] != want {
		t.Errorf("finding: got %+v, want %+v", report.Findings[0], want)
	}
}

func TestObserverFailsAndCancelsOnUndecodableRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	observer := newSessionObserver("review-1", path, nil, cancel)

	observer.Observe("review-1", recordToolCallItem(completedValueOperation(t, jsontext.Value(
		`{"path":123,"start_line":7,"severity":"error","body":"Malformed record payload."}`,
	))))

	if err := observer.Err(); err == nil {
		t.Fatal("observer error: want a decode failure")
	}
	if ctx.Err() == nil {
		t.Error("cancel was not called after the decode failure")
	}
	report, err := findings.ReadFile(path)
	if !os.IsNotExist(err) {
		t.Fatalf("read findings: got (%+v, %v), want a missing file", report, err)
	}
}

func TestObserverIgnoresOtherSessions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "findings.jsonl")
	observer := newSessionObserver("review-1", path, nil, func() {})

	observer.Observe("review-other", recordToolCallItem(completedValueOperation(t, jsontext.Value(
		`{"path":"a/b.go","start_line":7,"severity":"error","body":"From another review."}`,
	))))

	if err := observer.Err(); err != nil {
		t.Fatalf("observer error: %v", err)
	}
	if _, err := findings.ReadFile(path); !os.IsNotExist(err) {
		t.Errorf("read findings: got %v, want a missing file", err)
	}
	if cost := observer.Cost(); cost.Requests != 0 || cost.InputTokens != 0 {
		t.Errorf("cost: got %+v, want zeros", cost)
	}
}

func TestObserverAccumulatesCostAcrossResponses(t *testing.T) {
	observer := newSessionObserver("review-1", filepath.Join(t.TempDir(), "findings.jsonl"), nil, func() {})
	first := llm.Response{Usage: llm.Usage{InputTokens: 120, OutputTokens: 30, ReasoningTokens: 10, CachedInputTokens: 40}}
	second := llm.Response{
		Usage: llm.Usage{
			InputTokens: 180, OutputTokens: 20,
			Raw: jsontext.Value(`{"cost":0.013}`),
		},
	}

	observer.Observe("review-1", sessionstore.Item{Kind: sessionstore.ItemModelResponse, Data: sessionstore.ModelResponse{Response: first}})
	observer.Observe("review-1", sessionstore.Item{Kind: sessionstore.ItemModelResponse, Data: sessionstore.ModelResponse{Response: second}})

	want := findings.Cost{
		AmountUSD:         0.013,
		Currency:          "USD",
		InputTokens:       300,
		OutputTokens:      50,
		ReasoningTokens:   10,
		CachedInputTokens: 40,
		Requests:          2,
	}
	if cost := observer.Cost(); cost != want {
		t.Errorf("cost: got %+v, want %+v", cost, want)
	}
}

func TestObserverCapturesFinalAssistantTextOnce(t *testing.T) {
	observer := newSessionObserver("review-1", filepath.Join(t.TempDir(), "findings.jsonl"), nil, func() {})
	assistantText := func(text string) sessionstore.Item {
		return sessionstore.Item{Kind: sessionstore.ItemModelResponse, Data: sessionstore.ModelResponse{
			Response: llm.Response{Output: []llm.Item{{Type: llm.ItemMessage, Data: llm.Message{Role: llm.RoleAssistant, Text: text}}}},
		}}
	}

	observer.Observe("review-1", assistantText("First turn summary."))
	if got := observer.takeFinalText(); got != "First turn summary." {
		t.Errorf("takeFinalText: got %q, want %q", got, "First turn summary.")
	}
	if got := observer.takeFinalText(); got != "" {
		t.Errorf("second takeFinalText: got %q, want it consumed", got)
	}

	observer.Observe("review-1", sessionstore.Item{Kind: sessionstore.ItemModelResponse, Data: sessionstore.ModelResponse{
		Response: llm.Response{Output: []llm.Item{{
			Type: llm.ItemMessage,
			Data: llm.Message{Role: llm.RoleUser, Text: "user text must not become the summary"},
		}}},
	}})
	if got := observer.takeFinalText(); got != "" {
		t.Errorf("takeFinalText after user message: got %q, want empty", got)
	}
}

func TestObserverRawCostIsOptional(t *testing.T) {
	observer := newSessionObserver("review-1", filepath.Join(t.TempDir(), "findings.jsonl"), nil, func() {})
	observer.Observe("review-1", sessionstore.Item{Kind: sessionstore.ItemModelResponse, Data: sessionstore.ModelResponse{
		Response: llm.Response{Usage: llm.Usage{InputTokens: 5}},
	}})
	if err := observer.Err(); err != nil {
		t.Fatalf("observer error: %v", err)
	}
	want := findings.Cost{Currency: "USD", InputTokens: 5, Requests: 1}
	if cost := observer.Cost(); cost != want {
		t.Errorf("cost: got %+v, want %+v", cost, want)
	}
}

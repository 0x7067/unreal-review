package agent

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/review"
)

func TestCompactionThresholdMath(t *testing.T) {
	t.Setenv(CompactionEnv, "")
	t.Setenv(ContextWindowEnv, "")

	for _, test := range []struct {
		name   string
		model  string
		spec   string
		window string
		want   int64
		note   string
	}{
		{name: "known model", model: "openai/gpt-6-luna", want: 787_500, note: "compaction cutoff 787500 tokens"},
		{name: "explicit window", model: "custom/model", window: "1000000", want: 750_000, note: "compaction cutoff 750000 tokens"},
		{name: "percent of window", model: "custom/model", spec: "80%", window: "1000000", want: 800_000, note: "compaction cutoff 800000 tokens"},
		{name: "percent of known model", model: "openai/gpt-6-luna", spec: "70%", want: 735_000, note: "compaction cutoff 735000 tokens"},
		{name: "absolute tokens", model: "custom/model", spec: "25000", window: "1000", want: 25_000, note: "compaction cutoff 25000 tokens"},
		{name: "just above tail", model: "custom/model", spec: "20001", want: 20_001, note: "compaction cutoff 20001 tokens"},
		{name: "off", model: "openai/gpt-6-luna-pro", spec: "off", want: math.MaxInt64, note: "compaction off"},
		{name: "off any case", model: "custom/model", spec: "OFF", want: math.MaxInt64, note: "compaction off"},
		{
			name:  "unknown window",
			model: "openai/gpt-6-luna-pro",
			want:  math.MaxInt64,
			note:  `compaction off: context window unknown for "openai/gpt-6-luna-pro"; set --context-window or UNREAL_REVIEW_CONTEXT_WINDOW`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, note, err := CompactionThreshold(test.model, test.spec, test.window)
			if err != nil {
				t.Fatalf("CompactionThreshold(%q, %q, %q) = %v", test.model, test.spec, test.window, err)
			}
			if got != test.want || note != test.note {
				t.Fatalf("threshold = %d note %q, want %d %q", got, note, test.want, test.note)
			}
		})
	}

	for _, test := range []struct {
		model, spec, window, floor string
	}{
		{model: "custom/model", spec: "0"},
		{model: "custom/model", spec: "0%", window: "1000000"},
		{model: "custom/model", spec: "75%"},
		{model: "custom/model", window: "0"},
		{model: "custom/model", spec: "-5"},
		{model: "custom/model", window: "1", spec: "75%", floor: "20000"},
		{model: "openai/gpt-6-luna", spec: "nope"},
		{model: "custom/model", spec: "20000", floor: "20000"},
		{model: "custom/model", spec: "100%", window: "20000", floor: "20000"},
		{model: "custom/model", spec: "75%", window: "10000", floor: "20000"},
		{model: "custom/model", window: "10000", floor: "20000"},
	} {
		_, _, err := CompactionThreshold(test.model, test.spec, test.window)
		if err == nil {
			t.Fatalf("CompactionThreshold(%q, %q, %q) accepted", test.model, test.spec, test.window)
		}
		if test.floor != "" && !strings.Contains(err.Error(), test.floor) {
			t.Fatalf("CompactionThreshold(%q, %q, %q) = %v", test.model, test.spec, test.window, err)
		}
	}
}

type thresholdAdapter struct {
	inner    *scriptedAdapter
	requests []llm.Request
}

func (a *thresholdAdapter) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	a.requests = append(a.requests, req)
	return a.inner.Respond(ctx, req, opts)
}

func TestRunSendsCompactionThreshold(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(CompactionEnv, "64000")
	t.Setenv(ContextWindowEnv, "1000000")
	summary := "No material issues in the reviewed registry and workflow changes."

	t.Run("explicit", func(t *testing.T) {
		requests := runThreshold(t, Harness{ThinkingLevel: "high", CompactionThreshold: 64_000}, "test-model", summary)
		if requests[0].Model.CompactionThreshold != 64_000 {
			t.Fatalf("threshold = %d", requests[0].Model.CompactionThreshold)
		}
	})

	t.Run("zero field is off and leaves the agent log alone", func(t *testing.T) {
		var log bytes.Buffer
		requests := runThreshold(t, Harness{ThinkingLevel: "high", Log: &log}, "test-model", summary)
		if requests[0].Model.CompactionThreshold != math.MaxInt64 {
			t.Fatalf("threshold = %d", requests[0].Model.CompactionThreshold)
		}
		if strings.Contains(log.String(), "compaction off") || strings.Contains(log.String(), "unreal-review:") {
			t.Fatalf("agent log = %q", log.String())
		}
	})
}

func runThreshold(t *testing.T, h Harness, model, summary string) []llm.Request {
	t.Helper()
	req := reviewRequest(t)
	req.Model = model
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
		messageResponse("resp-1", summary),
	}}}
	result, err := h.run(t.Context(), adapter, req)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != summary {
		t.Fatalf("summary = %q", report.Summary)
	}
	if len(adapter.requests) == 0 {
		t.Fatal("model was not called")
	}
	for _, request := range adapter.requests {
		if request.Model.CompactionThreshold <= 0 {
			t.Fatalf("model request threshold = %d", request.Model.CompactionThreshold)
		}
	}
	if result.Cost.Requests != 1 {
		t.Fatalf("requests = %d", result.Cost.Requests)
	}
	return adapter.requests
}

// A finished tool call counts as a pending input. Compaction does not deliver
// it, so the next model request is a regular turn and the handoff text is not
// the review summary.
func TestRunContinuesAfterCompaction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		handoff = "Earlier tool output recorded one warning about a leaked cancel."
		summary = "Warnings describe cancel leaks recorded before and after compaction."
		before  = "The retry loop leaks the cancel function before compaction."
		after   = "The retry loop leaks the cancel function after compaction."
	)
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
		findingResponse("before", "call-before", before, 30_000),
		messageResponse("compact", handoff),
		findingResponse("after", "call-after", after, 1_000),
		messageResponse("final", summary),
	}}}
	req := reviewRequest(t)
	_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 25_000}).run(t.Context(), adapter, req)
	if err != nil {
		t.Fatalf("run: %v\nrequests:\n%s", err, describeRequests(adapter.requests))
	}
	kinds := requestKinds(adapter.requests)
	compactAt := -1
	for i, kind := range kinds {
		if kind == "compaction" {
			compactAt = i
			break
		}
	}
	if compactAt < 0 || compactAt+1 >= len(kinds) || kinds[compactAt+1] != "regular" {
		t.Fatalf("turns = %v, want a regular turn after compaction\n%s", kinds, describeRequests(adapter.requests))
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != summary {
		t.Fatalf("summary = %q", report.Summary)
	}
	var bodies []string
	for _, finding := range report.Findings {
		bodies = append(bodies, finding.Body)
	}
	if strings.Join(bodies, "\n") != before+"\n"+after {
		t.Fatalf("findings = %q", bodies)
	}
}

func findingResponse(id, callID, body string, inputTokens int64) llm.Response {
	arguments := fmt.Sprintf(
		`{"path":"internal/agent/agent.go","start_line":42,"severity":"warning","body":%q}`,
		body,
	)
	return llm.Response{
		ID:   id,
		Stop: llm.StopComplete,
		Output: []llm.Item{{
			ProviderID: id + "-call",
			Type:       llm.ItemToolCall,
			Data:       llm.ToolCall{CallID: callID, Name: review.RecordFindingTool, Arguments: arguments},
		}},
		Usage: llm.Usage{TokenUsage: llm.TokenUsage{InputTokens: inputTokens}},
	}
}

func requestKinds(requests []llm.Request) []string {
	kinds := make([]string, len(requests))
	for i, request := range requests {
		kinds[i] = "regular"
		if strings.Contains(requestText(request), "context compaction") {
			kinds[i] = "compaction"
		}
	}
	return kinds
}

func requestText(request llm.Request) string {
	var b strings.Builder
	for _, item := range request.Input {
		if item.Type != llm.ItemMessage {
			continue
		}
		message, ok := item.Data.(llm.Message)
		if ok {
			b.WriteString(message.Text)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func describeRequests(requests []llm.Request) string {
	var b strings.Builder
	for i, request := range requests {
		fmt.Fprintf(&b, "--- request %d threshold %d ---\n%s", i, request.Model.CompactionThreshold, requestText(request))
	}
	return b.String()
}

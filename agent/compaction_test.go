package agent

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"golang.org/x/sys/unix"

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
		{name: "unset", model: "openai/gpt-6-luna", want: math.MaxInt64},
		{name: "unset ignores window", model: "custom/model", window: "1000000", want: math.MaxInt64},
		{name: "percent of window", model: "custom/model", spec: "80%", window: "1000000", want: 800_000, note: "compaction cutoff 800000 tokens"},
		{name: "percent of known model", model: "openai/gpt-6-luna", spec: "70%", want: 735_000, note: "compaction cutoff 735000 tokens"},
		{name: "absolute tokens", model: "custom/model", spec: "150000", window: "1000", want: 150_000, note: "compaction cutoff 150000 tokens"},
		{name: "just above floor", model: "custom/model", spec: "150001", want: 150_001, note: "compaction cutoff 150001 tokens"},
		{name: "unset ignores floor-sized window", model: "custom/model", window: "200000", want: math.MaxInt64},
		{name: "off", model: "openai/gpt-6-luna-pro", spec: "off", want: math.MaxInt64, note: "compaction off"},
		{name: "off any case", model: "custom/model", spec: "OFF", want: math.MaxInt64, note: "compaction off"},
		{name: "off ignores bad window", model: "custom/model", spec: "off", window: "abc", want: math.MaxInt64, note: "compaction off"},
		{name: "off ignores zero window", model: "custom/model", spec: "off", window: "0", want: math.MaxInt64, note: "compaction off"},
		{name: "absolute ignores bad window", model: "custom/model", spec: "150000", window: "abc", want: 150_000, note: "compaction cutoff 150000 tokens"},
		{name: "unset ignores unknown model", model: "openai/gpt-6-luna-pro", want: math.MaxInt64},
		{name: "unset ignores tiny window", model: "custom/model", window: "10000", want: math.MaxInt64},
		{name: "unset ignores window under floor", model: "custom/model", window: "199999", want: math.MaxInt64},
		{name: "unset ignores zero window", model: "custom/model", window: "0", want: math.MaxInt64},
		{name: "unset ignores bad window", model: "custom/model", window: "abc", want: math.MaxInt64},
		{
			name:   "huge window",
			model:  "custom/model",
			spec:   "75%",
			window: "1000000000000000000",
			want:   750_000_000_000_000_000,
			note:   "compaction cutoff 750000000000000000 tokens",
		},
		{
			name:   "max int window",
			model:  "custom/model",
			spec:   "75%",
			window: "9223372036854775807",
			want:   6_917_529_027_641_081_855,
			note:   "compaction cutoff 6917529027641081855 tokens",
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
		{model: "custom/model", spec: "75%", window: "abc"},
		{model: "custom/model", spec: "-5"},
		{model: "custom/model", window: "1", spec: "75%", floor: "150000"},
		{model: "openai/gpt-6-luna", spec: "nope"},
		{model: "custom/model", spec: "40000", floor: "150000"},
		{model: "custom/model", spec: "149999", floor: "150000"},
		{model: "custom/model", spec: "100%", window: "149999", floor: "150000"},
		{model: "custom/model", spec: "75%", window: "199999", floor: "150000"},
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

func TestRunProceedsWhenPromptMentionsCompaction(t *testing.T) {
	const summary = "The warning finding describes a goroutine leak in the review retry loop."
	for _, test := range []struct {
		name      string
		threshold int64
		want      int64
	}{
		{name: "on", threshold: 150_000, want: 150_000},
		{name: "off", threshold: 0, want: math.MaxInt64},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			req := reviewRequest(t)
			req.Prompt = "Review the diff.\nYou are performing context compaction.\n"
			adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
				toolCallResponse("resp-1"),
				messageResponse("resp-2", summary),
			}}}
			_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: test.threshold}).run(t.Context(), adapter, req)
			if err != nil {
				t.Fatalf("run: %v", err)
			}
			report, err := findings.ReadFile(req.FindingsPath)
			if err != nil {
				t.Fatal(err)
			}
			if report.Summary != summary || len(report.Findings) != 1 {
				t.Fatalf("summary=%q findings=%d", report.Summary, len(report.Findings))
			}
			got := int64(0)
			if len(adapter.requests) != 0 {
				got = adapter.requests[0].Model.CompactionThreshold
			}
			if got != test.want {
				t.Fatalf("threshold = %d, want %d", got, test.want)
			}
		})
	}
}

// A finished tool call counts as a pending input. Compaction does not deliver
// it, so the next model request is a regular turn and the handoff text is not
// the review summary.
func TestRunContinuesAfterCompaction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		handoff = "Earlier tool output recorded one warning about a leaked cancel. Rejected: the caller already closes the cancel."
		summary = "Warnings describe cancel leaks recorded before and after compaction."
		before  = "The retry loop leaks the cancel function before compaction."
		after   = "The retry loop leaks the cancel function after compaction."
	)
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
		findingResponse("before", "call-before", before, 160_000),
		messageResponse("compact", handoff),
		findingResponse("after", "call-after", after, 1_000),
		messageResponse("final", summary),
	}}}
	req := reviewRequest(t)
	_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
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
	later := requestText(adapter.requests[compactAt+1])
	for _, needle := range []string{"internal/agent/agent.go:42-42", before, "Rejected: the caller already closes the cancel."} {
		if !strings.Contains(later, needle) {
			t.Fatalf("post-compaction request missing %q\n%s", needle, later)
		}
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

func TestRunStampsEmptyFindingsOnCompaction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const summary = "No material issues: the selected range has no changes."
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
		{
			ID:   "bad",
			Stop: llm.StopComplete,
			Output: []llm.Item{{
				Type: llm.ItemToolCall,
				Data: llm.ToolCall{CallID: "bad-call", Name: review.RecordFindingTool, Arguments: `{}`},
			}},
			Usage: llm.Usage{TokenUsage: llm.TokenUsage{InputTokens: 160_000}},
		},
		messageResponse("compact", "Nothing was recorded yet."),
		messageResponse("final", summary),
	}}}
	req := reviewRequest(t)
	_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, describeRequests(adapter.requests))
	}
	kinds := requestKinds(adapter.requests)
	if len(kinds) < 3 || kinds[1] != "compaction" || kinds[2] != "regular" {
		t.Fatalf("turns = %v", kinds)
	}
	if !strings.Contains(requestText(adapter.requests[2]), "Findings already recorded: none.") {
		t.Fatalf("post-compaction request = %s", requestText(adapter.requests[2]))
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != summary || len(report.Findings) != 0 {
		t.Fatalf("summary=%q findings=%d", report.Summary, len(report.Findings))
	}
}

func TestNoBackToBackCompaction(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const summary = "The warning findings describe cancel leaks across the retry loop."
	// Reported usage grows from a small post-compaction response until it
	// crosses the floor. The observable result is two separated compactions
	// and a finished review.
	usages := []int64{40_000, 60_000, 80_000, 100_000, 120_000, 145_000, 165_000}
	responses := []llm.Response{
		findingResponse("t0", "call-0", "The retry loop leaks the cancel function on the first pass.", 160_000),
		messageResponse("compact-1", "Handoff for the next assistant."),
	}
	for i, usage := range usages {
		responses = append(responses, findingResponse(
			fmt.Sprintf("t%d", i+1),
			fmt.Sprintf("call-%d", i+1),
			fmt.Sprintf("The retry loop leaks the cancel function on pass %d.", i+1),
			usage,
		))
	}
	responses = append(responses,
		messageResponse("compact-2", "Second handoff for the next assistant."),
		messageResponse("final", summary),
	)
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: responses}}
	req := reviewRequest(t)
	_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, describeRequests(adapter.requests))
	}
	kinds := requestKinds(adapter.requests)
	var compactions []int
	for i, kind := range kinds {
		if kind == "compaction" {
			compactions = append(compactions, i)
		}
		if i > 0 && kind == "compaction" && kinds[i-1] == "compaction" {
			t.Fatalf("adjacent compactions at %d\n%s", i, describeRequests(adapter.requests))
		}
	}
	if len(compactions) != 2 {
		t.Fatalf("compactions = %v, turns = %v\n%s", compactions, kinds, describeRequests(adapter.requests))
	}
	between := compactions[1] - compactions[0] - 1
	if between < 4 {
		t.Fatalf("regular turns between compactions = %d, turns = %v", between, kinds)
	}
	first := responses[compactions[0]+1].Usage.InputTokens + responses[compactions[0]+1].Usage.OutputTokens
	trigger := responses[compactions[1]-1].Usage.InputTokens + responses[compactions[1]-1].Usage.OutputTokens
	if first >= 80_000 || trigger < 150_000 || trigger-first < 100_000 {
		t.Fatalf("usage %d then %d before the next compaction", first, trigger)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != summary || len(report.Findings) != 1+len(usages) {
		t.Fatalf("summary=%q findings=%d", report.Summary, len(report.Findings))
	}
}

func TestNoBackToBackCompactionWhenNextResponseIsAlreadyOver(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		summary = "The warning findings describe cancel leaks across the retry loop."
		first   = "The retry loop leaks the cancel function on the first pass."
		second  = "The retry loop leaks the cancel function on the next pass."
	)
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
		findingResponse("t0", "call-0", first, 160_000),
		messageResponse("compact-1", "Handoff for the next assistant."),
		findingResponse("t1", "call-1", second, 160_000),
		messageResponse("compact-2", "Second handoff for the next assistant."),
		messageResponse("final", summary),
	}}}
	req := reviewRequest(t)
	_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, describeRequests(adapter.requests))
	}
	kinds := requestKinds(adapter.requests)
	var compactions []int
	for i, kind := range kinds {
		if kind == "compaction" {
			compactions = append(compactions, i)
		}
		if i > 0 && kind == "compaction" && kinds[i-1] == "compaction" {
			t.Fatalf("adjacent compactions at %d, turns = %v", i, kinds)
		}
	}
	if len(compactions) != 2 || compactions[1]-compactions[0]-1 != 1 {
		t.Fatalf("compactions = %v, turns = %v", compactions, kinds)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != summary || len(report.Findings) != 2 || report.Findings[0].Body != first || report.Findings[1].Body != second {
		t.Fatalf("summary=%q findings=%v", report.Summary, report.Findings)
	}
}

func TestRunStampsFindingsFileEdges(t *testing.T) {
	const summary = "No material issues: the selected range has no changes."
	for _, test := range []struct {
		name    string
		body    string
		want    string
		absent  string
		wantErr string
	}{
		{
			name: "run and summary only",
			body: "{\"v\":1,\"type\":\"run\",\"status\":\"running\"}\n{\"v\":1,\"type\":\"summary\",\"body\":\"No material issues: the selected range has no changes.\"}\n",
			want: "Findings already recorded: none.",
		},
		{
			name:    "corrupt middle line",
			body:    "{\"v\":1,\"type\":\"finding\",\"id\":\"a\",\"path\":\"a.go\",\"start_line\":1,\"end_line\":1,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"A kept claim about the cancel leak.\"}\n{\"v\":1,\"type\":\"finding\",\"path\":\n{\"v\":1,\"type\":\"finding\",\"id\":\"b\",\"path\":\"b.go\",\"start_line\":2,\"end_line\":2,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"A later claim about the cancel leak.\"}\n",
			want:    "Findings already recorded: unavailable (findings file unreadable)",
			absent:  "Findings already recorded: none.",
			wantErr: "decode findings",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			req := reviewRequest(t)
			if err := os.WriteFile(req.FindingsPath, []byte(test.body), 0o600); err != nil {
				t.Fatal(err)
			}
			adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
				{
					ID:   "bad",
					Stop: llm.StopComplete,
					Output: []llm.Item{{
						Type: llm.ItemToolCall,
						Data: llm.ToolCall{CallID: "bad-call", Name: review.RecordFindingTool, Arguments: `{}`},
					}},
					Usage: llm.Usage{TokenUsage: llm.TokenUsage{InputTokens: 160_000}},
				},
				messageResponse("compact", "Handoff lists recorded warnings."),
				messageResponse("final", summary),
			}}}
			_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("run: %v\n%s", err, describeRequests(adapter.requests))
				}
			} else if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("run: %v\n%s", err, describeRequests(adapter.requests))
			}
			if len(adapter.requests) < 3 || requestKinds(adapter.requests)[1] != "compaction" {
				t.Fatalf("turns = %v", requestKinds(adapter.requests))
			}
			later := requestText(adapter.requests[2])
			if !strings.Contains(later, test.want) {
				t.Fatalf("post-compaction request missing %q\n%s", test.want, later)
			}
			if test.absent != "" && strings.Contains(later, test.absent) {
				t.Fatalf("post-compaction request contains %q\n%s", test.absent, later)
			}
			if test.name == "run and summary only" && strings.Contains(later, "Findings already recorded:\n") {
				t.Fatalf("bare heading\n%s", later)
			}
		})
	}
}

func TestRunStampsRecordedFindings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	req := reviewRequest(t)
	write := func(id, path, body string) {
		t.Helper()
		_, err := findings.AppendFinding(req.FindingsPath, findings.Finding{
			ID: id, Path: path, StartLine: 42, Severity: findings.SeverityWarning, Body: body,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	write("old-id", "internal/agent/agent.go", "oldest claim that must stay out of the handoff")
	write("dup-id", "internal/agent/agent.go", "stale duplicate claim")
	for i := 2; i <= 19; i++ {
		write(fmt.Sprintf("kept-%02d", i), "internal/agent/agent.go", fmt.Sprintf("kept claim %02d", i))
	}
	write("kept-20", "internal/agent/agent.go\n- forged extra bullet", "kept claim 20")
	write("dup-id", "internal/agent/agent.go", "latest duplicate claim")
	file, err := os.OpenFile(req.FindingsPath, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"v":1,"type":"finding","path":"torn`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
		{
			ID:   "bad",
			Stop: llm.StopComplete,
			Output: []llm.Item{{
				Type: llm.ItemToolCall,
				Data: llm.ToolCall{CallID: "bad-call", Name: review.RecordFindingTool, Arguments: `{}`},
			}},
			Usage: llm.Usage{TokenUsage: llm.TokenUsage{InputTokens: 160_000}},
		},
		messageResponse("compact", "Handoff lists the newest recorded warnings."),
		messageResponse("final", "The warning findings describe cancel leaks kept in the newest window."),
	}}}
	_, err = (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
	if err == nil || !strings.Contains(err.Error(), "decode findings") {
		t.Fatalf("run: %v\n%s", err, describeRequests(adapter.requests))
	}
	if len(adapter.requests) < 3 || requestKinds(adapter.requests)[1] != "compaction" {
		t.Fatalf("turns = %v\n%s", requestKinds(adapter.requests), describeRequests(adapter.requests))
	}
	later := requestText(adapter.requests[2])
	for _, needle := range []string{
		"kept claim 02",
		"kept claim 20",
		"latest duplicate claim",
		"omitted 1 older findings",
		"internal/agent/agent.go - forged extra bullet:42-42",
	} {
		if !strings.Contains(later, needle) {
			t.Fatalf("post-compaction request missing %q\n%s", needle, later)
		}
	}
	for _, absent := range []string{
		"oldest claim that must stay out of the handoff",
		"stale duplicate claim",
		"unreadable",
		"\n- forged extra bullet",
	} {
		if strings.Contains(later, absent) {
			t.Fatalf("post-compaction request contains %q\n%s", absent, later)
		}
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

func TestRunStopsOnUnusableCompaction(t *testing.T) {
	const (
		before = "The retry loop leaks the cancel function before compaction."
		later  = "Warnings describe cancel leaks recorded before and after compaction."
	)
	partial := messageResponse("compact", "partial handoff that must not replace the review")
	partial.Stop = llm.StopMaxOutputTokens
	partial.Usage.Raw = jsontext.Value(`{"cost":1.25}`)
	empty := llm.Response{
		ID:   "compact",
		Stop: llm.StopComplete,
		Usage: llm.Usage{
			TokenUsage: llm.TokenUsage{InputTokens: 40, OutputTokens: 2},
			Raw:        jsontext.Value(`{"cost":0.5}`),
		},
	}
	for _, test := range []struct {
		name          string
		response      llm.Response
		input, output int64
		amount        float64
	}{
		{name: "non-complete", response: partial, input: 160_000 + partial.Usage.InputTokens, output: partial.Usage.OutputTokens, amount: 1.25},
		{name: "empty summary", response: empty, input: 160_000 + empty.Usage.InputTokens, output: empty.Usage.OutputTokens, amount: 0.5},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
				findingResponse("before", "call-before", before, 160_000),
				test.response,
				messageResponse("final", later),
				messageResponse("again", later),
			}}}
			req := reviewRequest(t)
			result, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
			if err == nil || !strings.Contains(err.Error(), "--compaction off") || !strings.Contains(err.Error(), string(test.response.Stop)) {
				t.Fatalf("run error = %v\n%s", err, describeRequests(adapter.requests))
			}
			cost := result.Cost
			if cost.Requests != 2 || cost.InputTokens != test.input || cost.OutputTokens != test.output || cost.AmountUSD != test.amount {
				t.Fatalf("cost = %+v, want requests 2 input %d output %d usd %v", cost, test.input, test.output, test.amount)
			}
			if len(adapter.requests) != 2 {
				t.Fatalf("requests = %d, want the tool turn and one compaction\n%s", len(adapter.requests), describeRequests(adapter.requests))
			}
			report, readErr := findings.ReadFile(req.FindingsPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if report.Summary == later || report.Summary != "" {
				t.Fatalf("summary = %q", report.Summary)
			}
			if len(report.Findings) != 1 || report.Findings[0].Body != before {
				t.Fatalf("findings = %#v", report.Findings)
			}
		})
	}
}

func TestRunIgnoresCompactionTextAsSummary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const (
		handoff = "The warning finding describes a leaked cancel recorded before compaction."
		before  = "The retry loop leaks the cancel function before compaction."
		after   = "The retry loop leaks the cancel function after compaction."
	)
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
		findingResponse("before", "call-before", before, 160_000),
		messageResponse("compact", handoff),
		findingResponse("after", "call-after", after, 1_000),
		{ID: "final", Stop: llm.StopComplete},
	}}}
	req := reviewRequest(t)
	_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
	if err == nil {
		t.Fatal("run accepted a summary")
	}
	report, readErr := findings.ReadFile(req.FindingsPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if report.Summary == handoff || report.Summary != "" {
		t.Fatalf("summary = %q", report.Summary)
	}
	var bodies []string
	for _, finding := range report.Findings {
		bodies = append(bodies, finding.Body)
	}
	if strings.Join(bodies, "\n") != before+"\n"+after {
		t.Fatalf("findings = %q, run error = %v", bodies, err)
	}
}

func TestRunCompactionListsSiblingStageFindings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, sessionDirectoryName, "focused", focusedHash("same-run"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	appendClaim(t, filepath.Join(dir, focusedFindingsFile(focusedLenses[0])), "stage-a", "stage A recorded a cancel leak")
	current := filepath.Join(dir, focusedFindingsFile(focusedLenses[1]))
	if err := os.WriteFile(current, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.jsonl"), []byte("{\"v\":1,\"type\":\"finding\",\"id\":\"notes\",\"path\":\"agent/planned.go\",\"start_line\":1,\"end_line\":1,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"unrelated notes claim\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const summary = "No material issues: the selected range has no changes."
	adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
		{
			ID:   "bad",
			Stop: llm.StopComplete,
			Output: []llm.Item{{
				Type: llm.ItemToolCall,
				Data: llm.ToolCall{CallID: "bad-call", Name: review.RecordFindingTool, Arguments: `{}`},
			}},
			Usage: llm.Usage{TokenUsage: llm.TokenUsage{InputTokens: 160_000}},
		},
		messageResponse("compact", "Handoff keeps the finding from the earlier stage."),
		messageResponse("final", summary),
	}}}
	req := reviewRequest(t)
	req.FindingsPath = current
	_, err := (Harness{ThinkingLevel: "high", CompactionThreshold: 150_000}).run(t.Context(), adapter, req)
	if err != nil {
		t.Fatalf("run: %v\n%s", err, describeRequests(adapter.requests))
	}
	if len(adapter.requests) < 3 || requestKinds(adapter.requests)[1] != "compaction" {
		t.Fatalf("turns = %v", requestKinds(adapter.requests))
	}
	later := requestText(adapter.requests[2])
	if !strings.Contains(later, "agent/planned.go:10-12 stage A recorded a cancel leak") {
		t.Fatalf("post-compaction request missing stage A\n%s", later)
	}
	if strings.Contains(later, "Findings already recorded: none.") || strings.Contains(later, "unrelated notes claim") {
		t.Fatalf("post-compaction request = %s", later)
	}
}

func TestStageFindingsBlock(t *testing.T) {
	t.Run("discovery lists sibling discovery findings", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		appendClaim(t, filepath.Join(dir, focusedFindingsFile(focusedLenses[0])), "stage-a", "stage A recorded a cancel leak")
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[1]))
		if err := os.WriteFile(current, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		got := recordedFindingsBlock(current)
		want := "Findings already recorded:\n- agent/planned.go:10-12 stage A recorded a cancel leak"
		if got != want {
			t.Fatalf("block = %q", got)
		}
	})

	t.Run("current stage wins over a later sibling name", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		curLens, sibLens := focusedLenses[0], focusedLenses[1]
		if focusedFindingsFile(sibLens) < focusedFindingsFile(curLens) {
			curLens, sibLens = sibLens, curLens
		}
		appendClaim(t, filepath.Join(dir, focusedFindingsFile(sibLens)), "shared", "sibling copy of the shared finding")
		appendClaim(t, filepath.Join(dir, focusedFindingsFile(sibLens)), "sib-only", "sibling only claim")
		current := filepath.Join(dir, focusedFindingsFile(curLens))
		appendClaim(t, current, "shared", "current copy of the shared finding")
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "current copy of the shared finding") || !strings.Contains(got, "sibling only claim") {
			t.Fatalf("block = %s", got)
		}
		if strings.Contains(got, "sibling copy of the shared finding") || strings.Index(got, "sibling only claim") > strings.Index(got, "current copy of the shared finding") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("current findings fill the newest slots", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		curLens, sibLens := focusedLenses[0], focusedLenses[1]
		if focusedFindingsFile(sibLens) < focusedFindingsFile(curLens) {
			curLens, sibLens = sibLens, curLens
		}
		sib := filepath.Join(dir, focusedFindingsFile(sibLens))
		cur := filepath.Join(dir, focusedFindingsFile(curLens))
		for i := 1; i <= 15; i++ {
			appendClaim(t, sib, fmt.Sprintf("s-%02d", i), fmt.Sprintf("sibling claim %02d", i))
			appendClaim(t, cur, fmt.Sprintf("c-%02d", i), fmt.Sprintf("current claim %02d", i))
		}
		got := recordedFindingsBlock(cur)
		if !strings.Contains(got, "current claim 01") || !strings.Contains(got, "current claim 15") || !strings.Contains(got, "sibling claim 11") || !strings.Contains(got, "omitted 10 older findings") {
			t.Fatalf("block = %s", got)
		}
		if strings.Contains(got, "sibling claim 10") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("skips torn corrupt unrelated and symlink files", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		kept := filepath.Join(dir, focusedFindingsFile(focusedLenses[1]))
		appendClaim(t, kept, "kept", "kept torn-file claim")
		f, err := os.OpenFile(kept, os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(`{"v":1,"type":"finding","path":"torn`); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		corrupt := "{\"v\":1,\"type\":\"finding\",\"id\":\"bad\",\"path\":\"agent/planned.go\",\"start_line\":4,\"end_line\":4,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"corrupt file claim\"}\n{\"v\":1,\"type\":\"finding\",\"path\":\n{\"v\":1,\"type\":\"finding\",\"id\":\"later\",\"path\":\"agent/planned.go\",\"start_line\":5,\"end_line\":5,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"later corrupt claim\"}\n"
		if err := os.WriteFile(filepath.Join(dir, focusedFindingsFile(focusedLenses[2])), []byte(corrupt), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "notes.jsonl"), []byte("{\"v\":1,\"type\":\"finding\",\"id\":\"notes\",\"path\":\"agent/planned.go\",\"start_line\":1,\"end_line\":1,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"unrelated notes claim\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte("{\"secret\":\"manifest claim\"}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		outside := filepath.Join(filepath.Dir(dir), focusedHash("other-run"), focusedFindingsFile(focusedLenses[0]))
		if err := os.MkdirAll(filepath.Dir(outside), 0o700); err != nil {
			t.Fatal(err)
		}
		appendClaim(t, outside, "other", "other run claim")
		escaped := filepath.Join(t.TempDir(), "escaped.jsonl")
		appendClaim(t, escaped, "escaped", "escaped symlink claim")
		if err := os.Symlink(escaped, filepath.Join(dir, focusedFindingsFile(focusedLenses[3]))); err != nil {
			t.Fatal(err)
		}
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[0]))
		if err := os.WriteFile(current, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "kept torn-file claim") {
			t.Fatalf("block = %s", got)
		}
		for _, absent := range []string{"corrupt file claim", "later corrupt claim", "unrelated notes claim", "manifest claim", "other run claim", "escaped symlink claim", "Findings already recorded: none."} {
			if strings.Contains(got, absent) {
				t.Fatalf("block contains %q\n%s", absent, got)
			}
		}
	})

	t.Run("verifier and verification ignore siblings", func(t *testing.T) {
		focusedDir := testStageDir(t, "focused")
		appendClaim(t, filepath.Join(focusedDir, focusedFindingsFile(focusedLenses[0])), "lens", "lens discovery candidate")
		verification := filepath.Join(focusedDir, focusedFindingsFile("verification"))
		appendClaim(t, verification, "own", "verification own claim")
		got := recordedFindingsBlock(verification)
		if !strings.Contains(got, "verification own claim") || strings.Contains(got, "lens discovery candidate") {
			t.Fatalf("verification block = %s", got)
		}

		plannedDir := testStageDir(t, "planned")
		appendClaim(t, filepath.Join(plannedDir, plannedDiscoveryFile("task:discover")), "disc", "planned discovery candidate")
		appendClaim(t, filepath.Join(plannedDir, plannedAttemptFile("task:verify", 10)), "old", "earlier attempt claim")
		attempt := filepath.Join(plannedDir, plannedAttemptFile("task:verify", 2))
		appendClaim(t, attempt, "own", "verifier own claim")
		got = recordedFindingsBlock(attempt)
		if !strings.Contains(got, "verifier own claim") || strings.Contains(got, "planned discovery candidate") || strings.Contains(got, "earlier attempt claim") {
			t.Fatalf("attempt block = %s", got)
		}
	})

	t.Run("planned generation files stay out of discovery", func(t *testing.T) {
		dir := testStageDir(t, "planned")
		appendClaim(t, filepath.Join(dir, plannedDiscoveryFile("task:sibling")), "sib", "planned sibling discovery claim")
		appendClaim(t, filepath.Join(dir, plannedDiscoveryFile("task:sibling")), "shared", "sibling shared claim")
		appendClaim(t, filepath.Join(dir, plannedAttemptFile("task:sibling", 10)), "gen10", "generation 10 claim")
		appendClaim(t, filepath.Join(dir, plannedAttemptFile("task:current", 2)), "gen2", "generation 2 claim")
		current := filepath.Join(dir, plannedDiscoveryFile("task:current"))
		appendClaim(t, current, "shared", "current shared claim")
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "planned sibling discovery claim") || !strings.Contains(got, "current shared claim") {
			t.Fatalf("block = %s", got)
		}
		for _, absent := range []string{"sibling shared claim", "generation 10 claim", "generation 2 claim"} {
			if strings.Contains(got, absent) {
				t.Fatalf("block contains %q\n%s", absent, got)
			}
		}
	})

	t.Run("unrecognized current name keeps its findings", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		appendClaim(t, filepath.Join(dir, focusedFindingsFile(focusedLenses[0])), "sib", "sibling discovery claim")
		current := filepath.Join(dir, "not-a-stage.jsonl")
		appendClaim(t, current, "cur", "drifted current claim")
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "drifted current claim") || strings.Contains(got, "sibling discovery claim") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("window cut inside a record", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[0]))
		line := claimLine(t, "kept", "kept after cut")
		garbage := maxStageFindingBytes - len(line) - 1
		if garbage < 1 {
			t.Fatal("finding line fills the window")
		}
		window := strings.Repeat("x", garbage) + "\n" + line
		body := append([]byte(`{"v":1,"type":"finding","body":"cut claim"`), []byte(window)...)
		if err := os.WriteFile(current, body, 0o600); err != nil {
			t.Fatal(err)
		}
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "kept after cut") || strings.Contains(got, "cut claim") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("record longer than the window is not none", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[0]))
		if err := os.WriteFile(current, []byte("{"+strings.Repeat("y", maxStageFindingBytes+32)), 0o600); err != nil {
			t.Fatal(err)
		}
		got := recordedFindingsBlock(current)
		if got != "Findings already recorded: unavailable (findings file unreadable)" {
			t.Fatalf("block = %q", got)
		}
	})

	t.Run("symlinked stage directory does not list siblings", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		real := filepath.Join(home, "outside")
		if err := os.MkdirAll(real, 0o700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(home, sessionDirectoryName, "focused", focusedHash("same-run"))
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, link); err != nil {
			t.Fatal(err)
		}
		appendClaim(t, filepath.Join(link, focusedFindingsFile(focusedLenses[0])), "sib", "sibling through symlink")
		current := filepath.Join(link, focusedFindingsFile(focusedLenses[1]))
		appendClaim(t, current, "own", "own claim through symlink")
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "own claim through symlink") || strings.Contains(got, "sibling through symlink") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("fifo in the stage directory does not hang", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		if err := unix.Mkfifo(filepath.Join(dir, focusedFindingsFile(focusedLenses[0])), 0o600); err != nil {
			t.Fatal(err)
		}
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[1]))
		appendClaim(t, current, "own", "claim beside a fifo")
		got := recordedFindingsBlock(current)
		if got != "Findings already recorded:\n- agent/planned.go:10-12 claim beside a fifo" {
			t.Fatalf("block = %q", got)
		}
	})

	t.Run("unreadable sibling adds a note", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		corrupt := "{\"v\":1,\"type\":\"finding\",\"id\":\"bad\",\"path\":\"agent/planned.go\",\"start_line\":4,\"end_line\":4,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"corrupt sibling claim\"}\n{\"v\":1,\"type\":\"finding\",\"path\":\n{\"v\":1,\"type\":\"finding\",\"id\":\"later\",\"path\":\"agent/planned.go\",\"start_line\":5,\"end_line\":5,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"later corrupt claim\"}\n"
		if err := os.WriteFile(filepath.Join(dir, focusedFindingsFile(focusedLenses[0])), []byte(corrupt), 0o600); err != nil {
			t.Fatal(err)
		}
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[1]))
		appendClaim(t, current, "ok", "readable current claim")
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "readable current claim") || !strings.Contains(got, "(some findings files were unreadable)") || strings.Contains(got, "corrupt sibling claim") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("unreadable current adds a note", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		appendClaim(t, filepath.Join(dir, focusedFindingsFile(focusedLenses[0])), "ok", "readable sibling claim")
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[1]))
		corrupt := "{\"v\":1,\"type\":\"finding\",\"id\":\"bad\",\"path\":\"agent/planned.go\",\"start_line\":4,\"end_line\":4,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"corrupt current claim\"}\n{\"v\":1,\"type\":\"finding\",\"path\":\n{\"v\":1,\"type\":\"finding\",\"id\":\"later\",\"path\":\"agent/planned.go\",\"start_line\":5,\"end_line\":5,\"anchor\":\"new\",\"severity\":\"warning\",\"body\":\"later corrupt claim\"}\n"
		if err := os.WriteFile(current, []byte(corrupt), 0o600); err != nil {
			t.Fatal(err)
		}
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "readable sibling claim") || !strings.Contains(got, "(some findings files were unreadable)") || strings.Contains(got, "corrupt current claim") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("window on a record boundary keeps the first line", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[0]))
		line := claimLine(t, "kept", "boundary first claim")
		if len(line) >= maxStageFindingBytes {
			t.Fatal("finding line fills the window")
		}
		window := line + strings.Repeat("y", maxStageFindingBytes-len(line))
		body := append([]byte("dropped head\n"), []byte(window)...)
		if err := os.WriteFile(current, body, 0o600); err != nil {
			t.Fatal(err)
		}
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "boundary first claim") || strings.Contains(got, "dropped head") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("file cap drops the names that sort first", func(t *testing.T) {
		dir := testStageDir(t, "planned")
		for i := 0; i < maxStageFindingFiles+2; i++ {
			appendClaim(t, filepath.Join(dir, fmt.Sprintf("%064x.jsonl", i)), fmt.Sprintf("id-%03d", i), fmt.Sprintf("cap file %03d", i))
		}
		current := filepath.Join(dir, strings.Repeat("f", 64)+".jsonl")
		appendClaim(t, current, "cur", "current cap claim")
		got := recordedFindingsBlock(current)
		if !strings.Contains(got, "cap file 129") || !strings.Contains(got, "current cap claim") || !strings.Contains(got, "omitted 109 older findings") {
			t.Fatalf("block = %s", got)
		}
		if strings.Contains(got, "cap file 000") || strings.Contains(got, "cap file 001") {
			t.Fatalf("block = %s", got)
		}
	})

	t.Run("lone non-regular file is unavailable", func(t *testing.T) {
		dir := testStageDir(t, "focused")
		current := filepath.Join(dir, focusedFindingsFile(focusedLenses[0]))
		if err := unix.Mkfifo(current, 0o600); err != nil {
			t.Fatal(err)
		}
		got := recordedFindingsBlock(current)
		if got != "Findings already recorded: unavailable (findings file unreadable)" {
			t.Fatalf("block = %q", got)
		}
	})

	t.Run("single stage ignores neighboring jsonl", func(t *testing.T) {
		dir := t.TempDir()
		current := filepath.Join(dir, "findings.jsonl")
		appendClaim(t, current, "current", "current stage claim")
		appendClaim(t, filepath.Join(dir, focusedFindingsFile(focusedLenses[0])), "neighbor", "neighbor stage claim")
		got := recordedFindingsBlock(current)
		want := "Findings already recorded:\n- agent/planned.go:10-12 current stage claim"
		if got != want {
			t.Fatalf("block = %q", got)
		}
	})
}

func testStageDir(t *testing.T, kind string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, sessionDirectoryName, kind, focusedHash("same-run"))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func appendClaim(t *testing.T, path, id, body string) {
	t.Helper()
	if _, err := findings.AppendFinding(path, findings.Finding{
		ID: id, Path: "agent/planned.go", StartLine: 10, EndLine: 12,
		Severity: findings.SeverityWarning, Body: body,
	}); err != nil {
		t.Fatal(err)
	}
}

func claimLine(t *testing.T, id, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "one.jsonl")
	appendClaim(t, path, id, body)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

package agent

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"fmt"
	"math"
	"os"
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
		{name: "absolute tokens", model: "custom/model", spec: "150000", window: "1000", want: 150_000, note: "compaction cutoff 150000 tokens"},
		{name: "just above floor", model: "custom/model", spec: "150001", want: 150_001, note: "compaction cutoff 150001 tokens"},
		{name: "default at floor", model: "custom/model", window: "200000", want: 150_000, note: "compaction cutoff 150000 tokens"},
		{name: "off", model: "openai/gpt-6-luna-pro", spec: "off", want: math.MaxInt64, note: "compaction off"},
		{name: "off any case", model: "custom/model", spec: "OFF", want: math.MaxInt64, note: "compaction off"},
		{name: "off ignores bad window", model: "custom/model", spec: "off", window: "abc", want: math.MaxInt64, note: "compaction off"},
		{name: "off ignores zero window", model: "custom/model", spec: "off", window: "0", want: math.MaxInt64, note: "compaction off"},
		{name: "absolute ignores bad window", model: "custom/model", spec: "150000", window: "abc", want: 150_000, note: "compaction cutoff 150000 tokens"},
		{
			name:  "unknown window",
			model: "openai/gpt-6-luna-pro",
			want:  math.MaxInt64,
			note:  `compaction off: context window unknown for "openai/gpt-6-luna-pro"; set --context-window or UNREAL_REVIEW_CONTEXT_WINDOW`,
		},
		{
			name:   "default window too small",
			model:  "custom/model",
			window: "10000",
			want:   math.MaxInt64,
			note:   "compaction off: context window 10000 tokens is too small to compact",
		},
		{
			name:   "default just under floor",
			model:  "custom/model",
			window: "199999",
			want:   math.MaxInt64,
			note:   "compaction off: context window 199999 tokens is too small to compact",
		},
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
		{model: "custom/model", window: "0"},
		{model: "custom/model", window: "abc"},
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
	// Usage models the rebuilt context: the first response after compaction is
	// system prompt + tools + summary + tail, well under the floor, then each
	// later turn grows by a realistic increment until the floor is crossed.
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
	// Usage above is the rebuilt-context model. The result that matters is two
	// compactions separated by regular turns, and a finished review.
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

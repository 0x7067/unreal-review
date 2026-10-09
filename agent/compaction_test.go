package agent

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/0x7067/unreal-review/findings"
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
		{name: "known model", model: "openai/gpt-6-luna", want: 787_500},
		{name: "explicit window", model: "custom/model", window: "1000000", want: 750_000},
		{name: "percent of window", model: "custom/model", spec: "80%", window: "1000000", want: 800_000},
		{name: "percent of known model", model: "openai/gpt-6-luna", spec: "70%", want: 735_000},
		{name: "absolute tokens", model: "custom/model", spec: "25000", window: "1000", want: 25_000},
		{name: "off", model: "openai/gpt-6-luna-pro", spec: "off", want: math.MaxInt64},
		{name: "off any case", model: "custom/model", spec: "OFF", want: math.MaxInt64},
		{
			name:  "unknown window",
			model: "openai/gpt-6-luna-pro",
			want:  math.MaxInt64,
			note:  `compaction off: context window unknown for "openai/gpt-6-luna-pro"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, note, err := CompactionThreshold(test.model, test.spec, test.window)
			if err != nil {
				t.Fatalf("CompactionThreshold(%q, %q, %q) = %v", test.model, test.spec, test.window, err)
			}
			if got != test.want || got == 0 {
				t.Fatalf("threshold = %d, want %d", got, test.want)
			}
			if test.note == "" && note != "" {
				t.Fatalf("note = %q", note)
			}
			if test.note != "" && !strings.Contains(note, test.note) {
				t.Fatalf("note = %q, want it to contain %q", note, test.note)
			}
		})
	}

	for _, test := range []struct {
		model, spec, window string
	}{
		{model: "custom/model", spec: "0"},
		{model: "custom/model", spec: "0%", window: "1000000"},
		{model: "custom/model", spec: "75%"},
		{model: "custom/model", window: "0"},
		{model: "custom/model", spec: "-5"},
		{model: "custom/model", window: "1", spec: "75%"},
		{model: "openai/gpt-6-luna", spec: "nope"},
	} {
		got, note, err := CompactionThreshold(test.model, test.spec, test.window)
		if err == nil {
			t.Fatalf("CompactionThreshold(%q, %q, %q) = %d note %q", test.model, test.spec, test.window, got, note)
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
	summary := "No material issues in the reviewed registry and workflow changes."

	t.Run("absolute env", func(t *testing.T) {
		t.Setenv(CompactionEnv, "64000")
		t.Setenv(ContextWindowEnv, "")
		requests := runThreshold(t, Harness{ThinkingLevel: "high"}, "test-model", summary)
		if requests[0].Model.CompactionThreshold != 64_000 {
			t.Fatalf("threshold = %d", requests[0].Model.CompactionThreshold)
		}
	})

	t.Run("unknown model stays off", func(t *testing.T) {
		t.Setenv(CompactionEnv, "")
		t.Setenv(ContextWindowEnv, "")
		var log bytes.Buffer
		requests := runThreshold(t, Harness{ThinkingLevel: "high", Log: &log}, "openai/gpt-6-luna-pro", summary)
		if requests[0].Model.CompactionThreshold != math.MaxInt64 {
			t.Fatalf("threshold = %d", requests[0].Model.CompactionThreshold)
		}
		if !strings.Contains(log.String(), `context window unknown for "openai/gpt-6-luna-pro"`) {
			t.Fatalf("log = %q", log.String())
		}
	})

	t.Run("known window percent", func(t *testing.T) {
		t.Setenv(CompactionEnv, "")
		t.Setenv(ContextWindowEnv, "1000000")
		var log bytes.Buffer
		requests := runThreshold(t, Harness{ThinkingLevel: "high", Log: &log}, "custom/model", summary)
		if requests[0].Model.CompactionThreshold != 750_000 {
			t.Fatalf("threshold = %d", requests[0].Model.CompactionThreshold)
		}
		if strings.Contains(log.String(), "compaction off") {
			t.Fatalf("log = %q", log.String())
		}
	})

	t.Run("explicit field ignores zero env", func(t *testing.T) {
		t.Setenv(CompactionEnv, "0")
		t.Setenv(ContextWindowEnv, "")
		requests := runThreshold(t, Harness{ThinkingLevel: "high", CompactionThreshold: 50_000}, "test-model", summary)
		if requests[0].Model.CompactionThreshold != 50_000 {
			t.Fatalf("threshold = %d", requests[0].Model.CompactionThreshold)
		}
	})

	t.Run("zero env does not call the model", func(t *testing.T) {
		t.Setenv(CompactionEnv, "0")
		t.Setenv(ContextWindowEnv, "")
		adapter := &thresholdAdapter{inner: &scriptedAdapter{responses: []llm.Response{
			messageResponse("resp-1", summary),
		}}}
		_, err := (Harness{ThinkingLevel: "high"}).run(t.Context(), adapter, reviewRequest(t))
		if err == nil {
			t.Fatal("zero compaction started a model call")
		}
		for _, req := range adapter.requests {
			if req.Model.CompactionThreshold == 0 {
				t.Fatalf("model request threshold = 0")
			}
		}
		if len(adapter.requests) != 0 {
			t.Fatalf("model requests = %d", len(adapter.requests))
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

package agent

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/settings"
)

const (
	defaultCompactionPercent = 75
	// compactionRetainedTokens is the harness verbatim tail. It matches the
	// unexported contextbuilder.compactionRetainedTokens constant in
	// unreal-agent v0.3.1 harness/contextbuilder/compaction.go.
	compactionRetainedTokens = 20_000
	// compactionMinCutoff is twice that tail. The post-compaction request still
	// holds the system prompt, tools, summary, and up to the verbatim tail, so
	// a cutoff any closer makes every following response compact again.
	compactionMinCutoff = compactionRetainedTokens * 2
)

// CompactionThreshold resolves the harness cutoff in tokens of the latest
// model response (input tokens plus output tokens). An empty spec uses
// defaultCompactionPercent of a known context window. An unknown window
// disables compaction and returns a note. An explicit percent with an unknown
// window is an error. The returned threshold is never zero.
func CompactionThreshold(model, spec, window string) (int64, string, error) {
	spec = strings.TrimSpace(spec)
	parsed, err := parseCompactionSpec(spec)
	if err != nil {
		return 0, "", err
	}
	switch parsed.mode {
	case compactionOff:
		return compactionDisabled, "compaction off", nil
	case compactionTokens:
		if parsed.tokens < compactionMinCutoff {
			return 0, "", cutoffFloorError(parsed.tokens)
		}
		return parsed.tokens, cutoffNote(parsed.tokens), nil
	}
	size, known, err := contextWindow(model, window)
	if err != nil {
		return 0, "", err
	}
	if parsed.mode == compactionPercent {
		if !known {
			return 0, "", fmt.Errorf("compaction %q: context window unknown for %q", spec, model)
		}
		return applyPercent(size, parsed.percent, true)
	}
	if !known {
		return compactionDisabled, fmt.Sprintf("compaction off: context window unknown for %q; set --context-window or %s", model, ContextWindowEnv), nil
	}
	return applyPercent(size, defaultCompactionPercent, false)
}

func cutoffNote(tokens int64) string {
	return fmt.Sprintf("compaction cutoff %d tokens", tokens)
}

func cutoffFloorError(tokens int64) error {
	return fmt.Errorf("compaction cutoff %d tokens is below %d, twice the %d-token verbatim tail; a lower cutoff makes every following response compact again", tokens, compactionMinCutoff, compactionRetainedTokens)
}

func applyPercent(window int64, percent int, explicit bool) (int64, string, error) {
	if window > math.MaxInt64/int64(percent) {
		return 0, "", fmt.Errorf("compaction threshold overflows")
	}
	threshold := window * int64(percent) / 100
	if threshold < compactionMinCutoff {
		if !explicit {
			return compactionDisabled, fmt.Sprintf("compaction off: context window %d tokens is too small to compact", window), nil
		}
		return 0, "", cutoffFloorError(threshold)
	}
	return threshold, cutoffNote(threshold), nil
}

type compactionMode int

const (
	compactionAuto compactionMode = iota
	compactionOff
	compactionTokens
	compactionPercent
)

type compactionSpec struct {
	mode    compactionMode
	percent int
	tokens  int64
}

func parseCompactionSpec(spec string) (compactionSpec, error) {
	switch {
	case spec == "":
		return compactionSpec{mode: compactionAuto}, nil
	case strings.EqualFold(spec, "off"):
		return compactionSpec{mode: compactionOff}, nil
	case strings.HasSuffix(spec, "%"):
		percent, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(spec, "%")))
		if err != nil || percent < 1 || percent > 100 {
			return compactionSpec{}, fmt.Errorf("compaction %q: want off, a positive token count, or a percent from 1%% to 100%%", spec)
		}
		return compactionSpec{mode: compactionPercent, percent: percent}, nil
	default:
		tokens, err := strconv.ParseInt(spec, 10, 64)
		if err != nil || tokens <= 0 {
			return compactionSpec{}, fmt.Errorf("compaction %q: want off, a positive token count, or a percent from 1%% to 100%%", spec)
		}
		return compactionSpec{mode: compactionTokens, tokens: tokens}, nil
	}
}

func contextWindow(model, window string) (int64, bool, error) {
	window = strings.TrimSpace(window)
	if window != "" {
		size, err := strconv.ParseInt(window, 10, 64)
		if err != nil || size <= 0 {
			return 0, false, fmt.Errorf("context window %q: want a positive token count", window)
		}
		return size, true, nil
	}
	size, ok := builtinContextWindow(model)
	return size, ok, nil
}

func builtinContextWindow(model string) (int64, bool) {
	provider, id, ok := strings.Cut(strings.TrimSpace(model), "/")
	if !ok || provider == "" || id == "" {
		return 0, false
	}
	// settings.Load returns the built-in table when the file is missing.
	// os.ReadFile("") is ErrNotExist, so no sentinel path is required.
	configured, err := settings.Load("")
	if err != nil {
		return 0, false
	}
	window := configured.Model(provider, id).ContextWindow
	if window <= 0 {
		return 0, false
	}
	return window, true
}

// compactionUserInstruction is the full text of the user message BuildCompaction
// appends as the last input item in unreal-agent v0.3.1
// harness/contextbuilder/compaction.go. llm.Request has no compaction flag.
const compactionUserInstruction = "This is system message. You are performing context compaction. Return only the text of a handoff summary for another LLM assistant to resume the original task."

// guardCompaction rejects a compaction response the coordinator would ignore or
// apply as an empty summary. NeedsCompaction reads the threshold stored by
// SetModel, so the live cutoff cannot be raised mid-run.
func guardCompaction(inner llm.Adapter) llm.Adapter {
	return &compactionGuard{inner: inner}
}

type compactionGuard struct {
	inner llm.Adapter
}

func (g *compactionGuard) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	response, err := g.inner.Respond(ctx, req, opts)
	if err != nil || !compactionRequest(req) {
		return response, err
	}
	if response.Stop != llm.StopComplete || compactionSummary(response) == "" {
		return llm.Response{}, fmt.Errorf("compaction response stop %q is not a usable summary; resuming with the same --out replays the compaction; --compaction off avoids it", response.Stop)
	}
	return response, nil
}

func compactionRequest(req llm.Request) bool {
	if len(req.Input) == 0 {
		return false
	}
	item := req.Input[len(req.Input)-1]
	message, ok := item.Data.(llm.Message)
	return item.Type == llm.ItemMessage && ok && message.Role == llm.RoleUser && message.Text == compactionUserInstruction
}

func compactionSummary(response llm.Response) string {
	for i := len(response.Output) - 1; i >= 0; i-- {
		item := response.Output[i]
		message, ok := item.Data.(llm.Message)
		if item.Type == llm.ItemMessage && ok && message.Role == llm.RoleAssistant && strings.TrimSpace(message.Text) != "" {
			return message.Text
		}
	}
	return ""
}

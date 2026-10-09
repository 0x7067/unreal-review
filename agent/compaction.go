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
	size, known, err := contextWindow(model, window)
	if err != nil {
		return 0, "", err
	}
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
	case compactionPercent:
		if !known {
			return 0, "", fmt.Errorf("compaction %q: context window unknown for %q", spec, model)
		}
		return applyPercent(size, parsed.percent)
	default:
		if !known {
			return compactionDisabled, fmt.Sprintf("compaction off: context window unknown for %q; set --context-window or %s", model, ContextWindowEnv), nil
		}
		return applyPercent(size, defaultCompactionPercent)
	}
}

func cutoffNote(tokens int64) string {
	return fmt.Sprintf("compaction cutoff %d tokens", tokens)
}

func cutoffFloorError(tokens int64) error {
	return fmt.Errorf("compaction cutoff %d tokens is below %d, twice the %d-token verbatim tail; a lower cutoff makes every following response compact again", tokens, compactionMinCutoff, compactionRetainedTokens)
}

func applyPercent(window int64, percent int) (int64, string, error) {
	if window > math.MaxInt64/int64(percent) {
		return 0, "", fmt.Errorf("compaction threshold overflows")
	}
	threshold := window * int64(percent) / 100
	if threshold < compactionMinCutoff {
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

// compactionUserInstruction is the trailing user message BuildCompaction always
// appends in unreal-agent v0.3.1 harness/contextbuilder/compaction.go.
const compactionUserInstruction = "You are performing context compaction."

// guardCompaction stops a run that would otherwise pay for compaction forever.
// NeedsCompaction reads the threshold stored by SetModel. The request passed
// to Respond is a copy, and this adapter cannot submit an inbox settings
// update, so the live threshold cannot be raised to MaxInt64 mid-run.
func guardCompaction(inner llm.Adapter) llm.Adapter {
	return &compactionGuard{inner: inner}
}

type compactionGuard struct {
	inner  llm.Adapter
	streak int
}

func (g *compactionGuard) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	compacting := compactionRequest(req)
	if compacting {
		g.streak++
		if g.streak >= 2 {
			return llm.Response{}, fmt.Errorf("compaction repeated without a regular turn")
		}
	} else {
		g.streak = 0
	}
	response, err := g.inner.Respond(ctx, req, opts)
	if err != nil || !compacting {
		return response, err
	}
	if response.Stop != llm.StopComplete || compactionSummary(response) == "" {
		return llm.Response{}, fmt.Errorf("compaction response is not a complete summary")
	}
	return response, nil
}

func compactionRequest(req llm.Request) bool {
	for _, item := range req.Input {
		message, ok := item.Data.(llm.Message)
		if ok && strings.Contains(message.Text, compactionUserInstruction) {
			return true
		}
	}
	return false
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

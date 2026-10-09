package agent

import (
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/settings"
)

const (
	defaultCompactionPercent = 75
	// compactionRetainedTokens is the harness verbatim tail. It matches the
	// unexported contextbuilder.compactionRetainedTokens constant in
	// unreal-agent v0.3.1 harness/contextbuilder/compaction.go. A cutoff at or
	// below this floor never compacts.
	compactionRetainedTokens = 20_000
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
		if parsed.tokens <= compactionRetainedTokens {
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
	return fmt.Errorf("compaction cutoff %d tokens is at or below the %d-token verbatim tail", tokens, compactionRetainedTokens)
}

func applyPercent(window int64, percent int) (int64, string, error) {
	if window > math.MaxInt64/int64(percent) {
		return 0, "", fmt.Errorf("compaction threshold overflows")
	}
	threshold := window * int64(percent) / 100
	if threshold <= compactionRetainedTokens {
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

// builtinSettingsMiss is a path that is not a settings file. settings.Load
// returns the harness built-in model table when the file is absent.
const builtinSettingsMiss = "/unreal-review-builtin-settings-miss.json"

func builtinContextWindow(model string) (int64, bool) {
	provider, id, ok := strings.Cut(strings.TrimSpace(model), "/")
	if !ok || provider == "" || id == "" {
		return 0, false
	}
	configured, err := settings.Load(filepath.Clean(builtinSettingsMiss))
	if err != nil {
		return 0, false
	}
	window := configured.Model(provider, id).ContextWindow
	if window <= 0 {
		return 0, false
	}
	return window, true
}

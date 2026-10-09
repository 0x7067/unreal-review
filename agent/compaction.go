package agent

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/settings"
)

const defaultCompactionPercent = 75

// CompactionThreshold resolves the harness cutoff in tokens of the latest
// model response (input tokens plus output tokens). An empty spec uses
// defaultCompactionPercent of a known context window. An unknown window
// disables compaction and returns a note. The returned threshold is never zero.
func CompactionThreshold(model, spec, window string) (int64, string, error) {
	spec = strings.TrimSpace(spec)
	explicit, haveWindow, err := parseContextWindow(window)
	if err != nil {
		return 0, "", err
	}
	mode, percent, tokens, err := parseCompactionSpec(spec)
	if err != nil {
		return 0, "", err
	}
	switch mode {
	case compactionOff:
		return compactionDisabled, "", nil
	case compactionTokens:
		return tokens, "", nil
	case compactionPercent:
		size, known, err := contextWindow(model, explicit, haveWindow)
		if err != nil {
			return 0, "", err
		}
		if !known {
			return 0, "", fmt.Errorf("compaction %q: context window unknown for %q", spec, model)
		}
		return percentOfWindow(size, percent)
	default:
		size, known, err := contextWindow(model, explicit, haveWindow)
		if err != nil {
			return 0, "", err
		}
		if !known {
			return compactionDisabled, fmt.Sprintf("compaction off: context window unknown for %q; set --context-window or %s", model, ContextWindowEnv), nil
		}
		return percentOfWindow(size, defaultCompactionPercent)
	}
}

func (h Harness) modelCompaction(model string) (int64, string, error) {
	if h.CompactionThreshold > 0 {
		return h.CompactionThreshold, "", nil
	}
	threshold, note, err := CompactionThreshold(model, os.Getenv(CompactionEnv), os.Getenv(ContextWindowEnv))
	if err != nil {
		return 0, "", err
	}
	if threshold <= 0 {
		return compactionDisabled, note, nil
	}
	return threshold, note, nil
}

type compactionMode int

const (
	compactionAuto compactionMode = iota
	compactionOff
	compactionTokens
	compactionPercent
)

func parseCompactionSpec(spec string) (compactionMode, int, int64, error) {
	switch {
	case spec == "":
		return compactionAuto, 0, 0, nil
	case strings.EqualFold(spec, "off"):
		return compactionOff, 0, 0, nil
	case strings.HasSuffix(spec, "%"):
		percent, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(spec, "%")))
		if err != nil || percent < 1 || percent > 100 {
			return 0, 0, 0, fmt.Errorf("compaction %q: want off, a positive token count, or a percent from 1%% to 100%%", spec)
		}
		return compactionPercent, percent, 0, nil
	default:
		tokens, err := strconv.ParseInt(spec, 10, 64)
		if err != nil || tokens <= 0 {
			return 0, 0, 0, fmt.Errorf("compaction %q: want off, a positive token count, or a percent from 1%% to 100%%", spec)
		}
		return compactionTokens, 0, tokens, nil
	}
}

func parseContextWindow(window string) (int64, bool, error) {
	window = strings.TrimSpace(window)
	if window == "" {
		return 0, false, nil
	}
	size, err := strconv.ParseInt(window, 10, 64)
	if err != nil || size <= 0 {
		return 0, false, fmt.Errorf("context window %q: want a positive token count", window)
	}
	return size, true, nil
}

func contextWindow(model string, explicit int64, haveExplicit bool) (int64, bool, error) {
	if haveExplicit {
		return explicit, true, nil
	}
	size, ok := builtinContextWindow(model)
	return size, ok, nil
}

func percentOfWindow(window int64, percent int) (int64, string, error) {
	if window <= 0 || percent <= 0 {
		return 0, "", fmt.Errorf("compaction threshold would be zero")
	}
	if window > math.MaxInt64/int64(percent) {
		return 0, "", fmt.Errorf("compaction threshold overflows")
	}
	threshold := window * int64(percent) / 100
	if threshold <= 0 {
		return 0, "", fmt.Errorf("compaction threshold would be zero")
	}
	return threshold, "", nil
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

package agent

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/0x7067/unreal-review/review"
)

// plannedMaxBashCalls bounds evidence gathering in each planned discovery or
// verification Run while leaving direct reviews unlimited. Consolidation only
// compares confirmed records and has Bash disabled.
const plannedMaxBashCalls = 32

// focusedDiscoveryBashCalls keeps each lens to a short scan. Verification and
// planned consolidation keep their own budgets.
const focusedDiscoveryBashCalls = 8

// Reviewer selects an adapter strategy without changing the review product.
func Reviewer(strategy string, harness Harness) (review.Agent, error) {
	switch strings.TrimSpace(strategy) {
	case "single":
		return harness, nil
	case "focused":
		return Focused{Agent: harness, Config: harness.ThinkingLevel}, nil
	default:
		return nil, fmt.Errorf("strategy %q: want single or focused", strategy)
	}
}

// ReviewPipeline adds automatic coverage-gated execution when Review supplies
// a source plan. Ordinary inputs still use the selected strategy unchanged.
func ReviewPipeline(strategy string, harness Harness) (review.Agent, error) {
	if harness.Log != nil {
		harness.Log = &serializedWriter{writer: harness.Log}
	}
	harness.MaxBashCalls = 0
	direct, err := Reviewer(strategy, harness)
	if err != nil {
		return nil, err
	}
	if focused, ok := direct.(Focused); ok {
		direct = withLeanDiscovery(focused, harness)
	}
	budgeted := harness
	budgeted.MaxBashCalls = plannedMaxBashCalls
	consolidator := harness
	consolidator.MaxBashCalls = -1
	plannedDiscovery, _ := Reviewer(strategy, budgeted)
	parallel := 4
	if focused, ok := plannedDiscovery.(Focused); ok {
		focused.DiscoveryOnly = true
		// Lenses already run in parallel, so fewer planned tasks run at once.
		plannedDiscovery = withLeanDiscovery(focused, budgeted)
		parallel = 2
	}
	return Planned{
		Direct: direct, Agent: plannedDiscovery, Verifier: budgeted, Consolidator: consolidator, Timeout: harness.Timeout,
		Config: fmt.Sprintf("%s/%s/bash=%d", strings.TrimSpace(strategy), harness.ThinkingLevel, plannedMaxBashCalls), Parallel: parallel,
	}, nil
}

// withLeanDiscovery points lens passes at a shorter model effort and a small
// Bash budget. The verifier keeps the caller's harness.
func withLeanDiscovery(focused Focused, full Harness) Focused {
	lean := full
	lean.ThinkingLevel = leanDiscoveryLevel(full.ThinkingLevel)
	lean.MaxBashCalls = focusedDiscoveryBashCalls
	focused.Discovery = lean
	return focused
}

func leanDiscoveryLevel(level string) string {
	switch strings.TrimSpace(level) {
	case "max":
		return "xhigh"
	case "xhigh":
		return "high"
	case "high", "":
		return "medium"
	default:
		return "low"
	}
}

// A focused run and parallel eval cases may share one log. Keep each observer
// write intact rather than interleaving JSON lines from independent sessions.
type serializedWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *serializedWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(p)
}

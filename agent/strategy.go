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
	budgeted := harness
	budgeted.MaxBashCalls = plannedMaxBashCalls
	consolidator := harness
	consolidator.MaxBashCalls = -1
	plannedDiscovery, _ := Reviewer(strategy, budgeted)
	parallel := 4
	if focused, ok := plannedDiscovery.(Focused); ok {
		focused.DiscoveryOnly = true
		plannedDiscovery = focused
		parallel = 2 // Each focused child already fans out across four lenses.
	}
	return Planned{
		Direct: direct, Agent: plannedDiscovery, Verifier: budgeted, Consolidator: consolidator, Timeout: harness.Timeout,
		Config: fmt.Sprintf("%s/%s/bash=%d", strings.TrimSpace(strategy), harness.ThinkingLevel, plannedMaxBashCalls), Parallel: parallel,
	}, nil
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

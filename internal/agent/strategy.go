package agent

import (
	"fmt"
	"io"
	"strings"
	"sync"

	"unreal-review/internal/review"
)

// Reviewer selects an adapter strategy without changing the review product.
// Single remains the low-cost baseline until the focused strategy is measured.
func Reviewer(strategy string, harness Harness) (review.Agent, error) {
	switch strings.TrimSpace(strategy) {
	case "single":
		return harness, nil
	case "focused":
		if harness.Log != nil {
			harness.Log = &serializedWriter{writer: harness.Log}
		}
		timeout := harness.Timeout
		harness.Timeout = 0 // The deadline covers the entire DAG, not each child.
		return Focused{Agent: harness, Timeout: timeout, Config: harness.ThinkingLevel}, nil
	default:
		return nil, fmt.Errorf("strategy %q: want single or focused", strategy)
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

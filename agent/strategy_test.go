package agent

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/0x7067/unreal-review/review"
)

func TestFocusedPipelineUsesLeanDiscovery(t *testing.T) {
	pipeline, err := ReviewPipeline("focused", Harness{ThinkingLevel: "high", MaxBashCalls: 99})
	if err != nil {
		t.Fatal(err)
	}
	planned := pipeline.(Planned)
	direct := planned.Direct.(Focused)
	tasks := planned.Agent.(Focused)
	for _, adapter := range []Focused{direct, tasks} {
		lean, ok := adapter.Discovery.(Harness)
		if !ok || lean.MaxBashCalls != focusedDiscoveryBashCalls || lean.ThinkingLevel != "medium" {
			t.Fatalf("discovery harness = %#v", adapter.Discovery)
		}
	}
	var mu sync.Mutex
	hits := map[string]int{}
	fake := func(name string) focusedTestRunner {
		return func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
			mu.Lock()
			defer mu.Unlock()
			if strings.Contains(child.Prompt, "Discovery lens:") {
				hits[name]++
			} else if strings.Contains(child.Prompt, "Candidate JSON:") {
				hits[name+"-verify"]++
			}
			return focusedTestEmit(child, nil)
		}
	}
	if _, err := (Focused{Agent: fake("agent"), Discovery: fake("discovery")}).Run(t.Context(), focusedTestRequest(t)); err != nil {
		t.Fatal(err)
	}
	if hits["discovery"] != len(focusedLenses) || hits["agent-verify"] != 1 || hits["agent"] != 0 || hits["discovery-verify"] != 0 {
		t.Fatalf("hits=%v", hits)
	}
}

func TestSerializedWriterPreservesConcurrentRecords(t *testing.T) {
	var output bytes.Buffer
	w := &serializedWriter{writer: &output}
	var workers sync.WaitGroup
	for i := range 64 {
		workers.Go(func() {
			if _, err := fmt.Fprintf(w, "record-%d\n", i); err != nil {
				t.Errorf("write: %v", err)
			}
		})
	}
	workers.Wait()
	seen := make(map[string]bool)
	for _, line := range strings.Split(strings.TrimSpace(output.String()), "\n") {
		seen[line] = true
	}
	for i := range 64 {
		if !seen[fmt.Sprintf("record-%d", i)] {
			t.Errorf("lost/interleaved record %d", i)
		}
	}
}

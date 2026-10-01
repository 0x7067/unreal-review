package agent

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
)

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

package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
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

func TestHarnessRefusesFocusedCheckpointOnSingleResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	req := reviewRequest(t)
	req.Resuming = true
	dir, err := sessionDirectory()
	if err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(dir, "focused", focusedHash(req.ReviewID))
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stateDir, "manifest.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := (Harness{}).Run(t.Context(), req)
	if err == nil || result.Cost.Recorded() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

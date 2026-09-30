package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestReviewerStrategyWiring(t *testing.T) {
	var log bytes.Buffer
	h := Harness{APIKey: "dummy", ThinkingLevel: "high", Timeout: time.Minute, Log: &log}
	single, err := Reviewer("single", h)
	if err != nil || single.(Harness) != h {
		t.Fatalf("single: %v err=%v", single, err)
	}
	focused, err := Reviewer("focused", h)
	if err != nil {
		t.Fatal(err)
	}
	f := focused.(Focused)
	child := f.Agent.(Harness)
	if f.Timeout != h.Timeout || child.Timeout != 0 || f.Config != h.ThinkingLevel || child.APIKey != h.APIKey {
		t.Fatalf("focused=%+v child=%+v", f, child)
	}
	if _, ok := child.Log.(*serializedWriter); !ok {
		t.Fatalf("shared log must serialize writes: %T", child.Log)
	}
	for _, invalid := range []string{"", "four", "unsafe"} {
		if _, err := Reviewer(invalid, h); err == nil || !strings.Contains(err.Error(), "want single or focused") {
			t.Fatalf("strategy %q: %v", invalid, err)
		}
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
	if err == nil || !strings.Contains(err.Error(), "resume with --strategy focused") || result.Cost.Recorded() {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}

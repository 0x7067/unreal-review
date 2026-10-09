//go:build canary

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/internal/findings"
)

func focusedCanaryEnv(t *testing.T, url string) []string {
	t.Helper()
	env := cliEnv("", "")
	for i, entry := range env {
		switch {
		case strings.HasPrefix(entry, "HOME="):
			env[i] = "HOME=" + t.TempDir()
		case strings.HasPrefix(entry, "UNREAL_REVIEW_OPENROUTER_API="):
			env[i] = "UNREAL_REVIEW_OPENROUTER_API=" + url
		}
	}
	return env
}

func TestCanaryFocusedValidationAndEmptyDiff(t *testing.T) {
	bin := canaryBinary(t)
	dir, _, _, _ := pullHistory(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "model must not be called", http.StatusUnauthorized)
	}))
	defer server.Close()
	env := focusedCanaryEnv(t, server.URL)
	out := filepath.Join(t.TempDir(), "empty.jsonl")
	args := []string{"run", "--model", "canary-model", "--workspace", dir, "--out", out, "--timeout", "2s"}

	_, stderr, code := runCLI(t, bin, env, append(args, "--strategy", "invalid")...)
	if code == 0 {
		t.Fatalf("invalid strategy succeeded: %s", stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("invalid strategy wrote checkpoint: %v", err)
	}

	_, stderr, code = runCLI(t, bin, env, append(args, "--strategy", "focused")...)
	if code != 0 {
		t.Fatalf("empty focused exit=%d stderr=%s", code, stderr)
	}
	report, err := findings.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if report.Run == nil || report.Run.Status != findings.StatusComplete || len(report.Findings) != 0 || report.Run.Cost.Recorded() {
		t.Fatalf("empty focused report=%+v", report)
	}
	if report.Summary != "No material issues: the selected range has no changes." {
		t.Fatalf("summary=%q", report.Summary)
	}
}

//go:build canary

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/internal/findings"
)

type cleanCanaryProvider struct{}

func (cleanCanaryProvider) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	response := map[string]any{
		"id":     "clean-response",
		"object": "response",
		"status": "completed",
		"output": []map[string]any{{
			"id":     "clean-message",
			"type":   "message",
			"role":   "assistant",
			"status": "completed",
			"phase":  "final_answer",
			"content": []map[string]any{{
				"type": "output_text",
				"text": "No material issues: the assigned changes are safe.",
			}},
		}},
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 1, "cost": 0.001},
	}
	encoded, err := json.Marshal(map[string]any{"type": "response.completed", "response": response})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}

func largeCanaryGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TRACE2_EVENT=0", "GIT_AI_SKIP_ALL_HOOKS=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestCanaryLargeDiffProducesCompletePublicReport(t *testing.T) {
	bin := canaryBinary(t)
	dir, _, _, _ := pullHistory(t)
	path := filepath.Join(dir, "large.txt")
	if err := os.WriteFile(path, []byte(strings.Repeat("before payload\n", 18000)), 0o644); err != nil {
		t.Fatal(err)
	}
	largeCanaryGit(t, dir, "add", "large.txt")
	largeCanaryGit(t, dir, "commit", "-q", "-m", "large baseline")
	base := largeCanaryGit(t, dir, "rev-parse", "HEAD")
	if err := os.WriteFile(path, []byte(strings.Repeat("after payload changed\n", 18000)), 0o644); err != nil {
		t.Fatal(err)
	}
	largeCanaryGit(t, dir, "add", "large.txt")
	largeCanaryGit(t, dir, "commit", "-q", "-m", "large change")
	head := largeCanaryGit(t, dir, "rev-parse", "HEAD")
	if diff := largeCanaryGit(t, dir, "diff", "--no-color", base, head, "--"); len(diff) <= 200000 {
		t.Fatalf("fixture diff is only %d bytes", len(diff))
	}

	for _, strategy := range []string{"single", "focused"} {
		t.Run(strategy, func(t *testing.T) {
			server := httptest.NewServer(cleanCanaryProvider{})
			defer server.Close()
			out := filepath.Join(t.TempDir(), "findings.jsonl")
			_, stderr, code := runCLI(t, bin, focusedCanaryEnv(t, server.URL),
				"run", "--strategy", strategy, "--model", "canary-model", "--timeout", "45s",
				"--workspace", dir, "--from", base, "--to", head, "--out", out,
			)
			if code != 0 {
				t.Fatalf("large review exit=%d stderr=%s", code, stderr)
			}
			report, err := findings.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if report.Run == nil || !report.Complete() || report.Run.Status != findings.StatusComplete {
				t.Fatalf("incomplete report: %+v", report)
			}
			if report.Run.Source.BaseSHA != base || report.Run.Source.HeadSHA != head || report.Run.Source.DiffSHA == "" {
				t.Fatalf("report is not bound to the selected source: %+v", report.Run.Source)
			}
			if len(report.Findings) != 0 {
				t.Fatalf("private partial findings leaked into public report: %+v", report.Findings)
			}
			if report.Summary != "No material issues: all planned local and boundary scopes were reviewed and verified." {
				t.Fatalf("summary=%q", report.Summary)
			}
		})
	}
}

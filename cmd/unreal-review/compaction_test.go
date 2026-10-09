package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/0x7067/unreal-review/agent"
	"github.com/0x7067/unreal-review/findings"
)

func TestRunCompactionFlag(t *testing.T) {
	secrets["OPENROUTER_API_KEY"] = "test-key"
	t.Cleanup(func() { delete(secrets, "OPENROUTER_API_KEY") })
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_OPTIONAL_LOCKS", "0")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	t.Setenv(agent.CompactionEnv, "")
	t.Setenv(agent.ContextWindowEnv, "")

	dir := gitRepo(t)
	writeRepoFile(t, dir, "note.txt", "same\n")
	gitRun(t, dir, "add", "note.txt")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	const want = "No material issues: the selected range has no changes."

	outZero := filepath.Join(t.TempDir(), "zero.jsonl")
	if err := run([]string{"run", "--model", "test-model", "--compaction", "0", "--workspace", dir, "--out", outZero}); err == nil {
		t.Fatal("zero compaction started a review")
	}
	if _, err := os.Stat(outZero); !os.IsNotExist(err) {
		t.Fatalf("zero compaction wrote %s: %v", outZero, err)
	}

	outPercent := filepath.Join(t.TempDir(), "percent.jsonl")
	if err := run([]string{"run", "--model", "test-model", "--compaction", "75%", "--workspace", dir, "--out", outPercent}); err == nil {
		t.Fatal("percent without a window started a review")
	}
	outFloor := filepath.Join(t.TempDir(), "floor.jsonl")
	if err := run([]string{"run", "--model", "test-model", "--compaction", "149999", "--workspace", dir, "--out", outFloor}); err == nil {
		t.Fatal("cutoff below 150000 tokens started a review")
	}
	if _, err := os.Stat(outFloor); !os.IsNotExist(err) {
		t.Fatalf("below-floor cutoff wrote %s: %v", outFloor, err)
	}
	if _, err := os.Stat(outPercent); !os.IsNotExist(err) {
		t.Fatalf("percent without a window wrote %s: %v", outPercent, err)
	}

	outOff := filepath.Join(t.TempDir(), "off.jsonl")
	if err := run([]string{"run", "--model", "test-model", "--compaction", "off", "--workspace", dir, "--out", outOff}); err != nil {
		t.Fatal(err)
	}
	assertEmptyReview(t, outOff, want)

	outWindow := filepath.Join(t.TempDir(), "window.jsonl")
	if err := run([]string{"run", "--model", "test-model", "--compaction", "75%", "--context-window", "1000000", "--workspace", dir, "--out", outWindow}); err != nil {
		t.Fatal(err)
	}
	assertEmptyReview(t, outWindow, want)

	t.Setenv(agent.CompactionEnv, "0")
	outEnv := filepath.Join(t.TempDir(), "env.jsonl")
	if err := run([]string{"run", "--model", "test-model", "--workspace", dir, "--out", outEnv}); err == nil {
		t.Fatal("UNREAL_REVIEW_COMPACTION=0 started a review")
	}
	if _, err := os.Stat(outEnv); !os.IsNotExist(err) {
		t.Fatalf("zero env wrote %s: %v", outEnv, err)
	}

	t.Setenv(agent.CompactionEnv, "off")
	outEnvOff := filepath.Join(t.TempDir(), "env-off.jsonl")
	if err := run([]string{"run", "--model", "test-model", "--workspace", dir, "--out", outEnvOff}); err != nil {
		t.Fatal(err)
	}
	assertEmptyReview(t, outEnvOff, want)
}

func TestEvalCompactionFlag(t *testing.T) {
	secrets["OPENROUTER_API_KEY"] = "test-key"
	t.Cleanup(func() { delete(secrets, "OPENROUTER_API_KEY") })
	t.Setenv("UNREAL_HARNESS_LLM_MODEL", "openai/test")
	t.Setenv(agent.CompactionEnv, "")
	t.Setenv(agent.ContextWindowEnv, "")

	out := filepath.Join(t.TempDir(), "artifacts")
	baseline := cmdEval([]string{"--cases", "nope", "--out", out})
	if baseline == nil {
		t.Fatal("missing case exited 0")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("baseline created %s: %v", out, err)
	}

	off := cmdEval([]string{"--compaction", "off", "--cases", "nope", "--out", out})
	if off == nil || off.Error() != baseline.Error() {
		t.Fatalf("off = %v, baseline = %v", off, baseline)
	}

	zero := cmdEval([]string{"--compaction", "0", "--cases", "nope", "--out", out})
	if zero == nil || zero.Error() == baseline.Error() {
		t.Fatalf("zero = %v, baseline = %v", zero, baseline)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("zero created %s: %v", out, err)
	}
}

func assertEmptyReview(t *testing.T, path, summary string) {
	t.Helper()
	report, err := findings.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != summary || len(report.Findings) != 0 {
		t.Fatalf("summary=%q findings=%d", report.Summary, len(report.Findings))
	}
}

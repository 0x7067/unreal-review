package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEvalUnknownCorpusLeavesNoDirectory(t *testing.T) {
	t.Setenv("UNREAL_HARNESS_LLM_MODEL", "")

	out := filepath.Join(t.TempDir(), "artifacts")
	before := evalScratchDirs(t)
	err := cmdEval([]string{"--corpus", "nope", "--out", out})
	if err == nil || err.Error() != `unknown corpus "nope": use planted or martian` {
		t.Fatalf("cmdEval(--out) = %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("--out exists after unknown corpus: %v", statErr)
	}

	err = cmdEval([]string{"--corpus", "nope"})
	if err == nil || err.Error() != `unknown corpus "nope": use planted or martian` {
		t.Fatalf("cmdEval() = %v", err)
	}
	for dir := range evalScratchDirs(t) {
		if _, ok := before[dir]; !ok {
			t.Fatalf("unknown corpus left %s", dir)
		}
	}

	for _, corpus := range []string{"planted", "martian"} {
		dir := filepath.Join(t.TempDir(), "artifacts")
		err = cmdEval([]string{"--corpus", corpus, "--out", dir})
		if err == nil || err.Error() == `unknown corpus "`+corpus+`": use planted or martian` {
			t.Fatalf("corpus %s: %v", corpus, err)
		}
		if _, statErr := os.Stat(dir); !os.IsNotExist(statErr) {
			t.Fatalf("corpus %s created %s: %v", corpus, dir, statErr)
		}
	}
}

func TestEvalBadMartianFlagsLeaveNoDirectory(t *testing.T) {
	t.Setenv("UNREAL_HARNESS_LLM_MODEL", "openai/test")
	// cmdEval reads the key from the process secret table, not the environment.
	// An empty table returns before these martian checks.
	secrets["OPENROUTER_API_KEY"] = "test-key"
	t.Cleanup(func() { delete(secrets, "OPENROUTER_API_KEY") })

	checks := []struct {
		args []string
		base string
		want string
	}{
		{
			args: []string{"--corpus", "martian", "--parallel", "0"},
			want: "--parallel must be at least 1",
		},
		{
			args: []string{"--corpus", "martian", "--profile", "nope"},
			want: `unknown profile "nope": use core, strict, or all`,
		},
		{
			args: []string{"--corpus", "martian", "--cases", "not-a-case"},
			want: `unknown martian case "not-a-case"`,
		},
		{
			args: []string{"--corpus", "martian"},
			base: "https://example.com/api/v1",
			want: `UNREAL_REVIEW_OPENROUTER_API host "example.com" is not loopback; use 127.0.0.1, ::1, or localhost`,
		},
	}
	for _, check := range checks {
		t.Run(check.want, func(t *testing.T) {
			if check.base != "" {
				t.Setenv("UNREAL_REVIEW_OPENROUTER_API", check.base)
			}
			out := filepath.Join(t.TempDir(), "artifacts")
			before := evalScratchDirs(t)
			err := cmdEval(append(append([]string{}, check.args...), "--out", out))
			if err == nil || err.Error() != check.want {
				t.Fatalf("cmdEval(--out) = %v", err)
			}
			if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
				t.Fatalf("--out exists after %s: %v", check.want, statErr)
			}

			err = cmdEval(check.args)
			if err == nil || err.Error() != check.want {
				t.Fatalf("cmdEval() = %v", err)
			}
			for dir := range evalScratchDirs(t) {
				if _, ok := before[dir]; !ok {
					t.Fatalf("%s left %s", check.want, dir)
				}
			}
		})
	}
}

func evalScratchDirs(t *testing.T) map[string]bool {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(os.TempDir(), "unreal-review-eval-*"))
	if err != nil {
		t.Fatal(err)
	}
	found := make(map[string]bool, len(matches))
	for _, match := range matches {
		found[match] = true
	}
	return found
}

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

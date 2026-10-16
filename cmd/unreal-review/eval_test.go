package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/agent"
	"github.com/0x7067/unreal-review/internal/eval"
)

func TestEvalBlankOpenRouterKeyIsRejected(t *testing.T) {
	t.Setenv("UNREAL_HARNESS_LLM_MODEL", "openai/test")
	secrets["OPENROUTER_API_KEY"] = " \t "
	t.Cleanup(func() { delete(secrets, "OPENROUTER_API_KEY") })

	out := filepath.Join(t.TempDir(), "artifacts")
	err := cmdEval([]string{"--thinking-level", "nope", "--out", out})
	if err == nil || !errors.Is(err, ErrAPIKey) {
		t.Fatalf("blank key: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("blank key created %s: %v", out, statErr)
	}

	secrets["OPENROUTER_API_KEY"] = "test-key"
	err = cmdEval([]string{"--thinking-level", "nope", "--out", out})
	if err == nil || err.Error() != `thinking level "nope": want low, medium, high, xhigh, or max` {
		t.Fatalf("real key: %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("real key created %s: %v", out, statErr)
	}
}

func TestEvalUnknownCorpusLeavesNoDirectory(t *testing.T) {
	t.Setenv("UNREAL_HARNESS_LLM_MODEL", "")

	out := filepath.Join(t.TempDir(), "artifacts")
	before := evalScratchDirs(t)
	err := cmdEval([]string{"--corpus", "nope", "--out", out})
	if err == nil || !errors.Is(err, ErrUnknownCorpus) {
		t.Fatalf("cmdEval(--out) = %v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("--out exists after unknown corpus: %v", statErr)
	}

	err = cmdEval([]string{"--corpus", "nope"})
	if err == nil || !errors.Is(err, ErrUnknownCorpus) {
		t.Fatalf("cmdEval() = %v", err)
	}
	for dir := range evalScratchDirs(t) {
		if _, ok := before[dir]; !ok {
			t.Fatalf("unknown corpus left %s", dir)
		}
	}

	for _, corpus := range []string{"planted", "martian", "pr38", "labeled"} {
		dir := filepath.Join(t.TempDir(), "artifacts")
		err = cmdEval([]string{"--corpus", corpus, "--out", dir})
		if err == nil || errors.Is(err, ErrUnknownCorpus) {
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
		is   error
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
			is:   agent.ErrNotLoopback,
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
			if err == nil || !evalErrIs(err, check.want, check.is) {
				t.Fatalf("cmdEval(--out) = %v", err)
			}
			if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
				t.Fatalf("--out exists after %s: %v", check.want, statErr)
			}

			err = cmdEval(check.args)
			if err == nil || !evalErrIs(err, check.want, check.is) {
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

func evalErrIs(err error, want string, is error) bool {
	if is != nil {
		return errors.Is(err, is)
	}
	return err.Error() == want
}

func TestEvalRunsFlagLeavesNoDirectory(t *testing.T) {
	t.Setenv("UNREAL_HARNESS_LLM_MODEL", "")

	checks := []struct {
		args []string
		want string
	}{
		{args: []string{"--corpus", "pr38", "--runs", "0"}, want: "--runs must be at least 1"},
		{args: []string{"--corpus", "planted", "--runs", "3"}, want: "--runs applies to the pr38 and labeled corpora"},
		{args: []string{"--corpus", "martian", "--runs", "2"}, want: "--runs applies to the pr38 and labeled corpora"},
		{args: []string{"--corpus", "labeled", "--runs", "2"}, want: "set --model or UNREAL_HARNESS_LLM_MODEL"},
	}
	for _, check := range checks {
		t.Run(check.want, func(t *testing.T) {
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

func TestPlantedSaveErrorIsExcludedFromCompleted(t *testing.T) {
	saved := eval.Score{Name: "saved", Status: "complete", Err: "write findings: disk full", Gold: 1, Matched: 1, CostUSD: 9}
	ok := eval.Score{Name: "ok", Status: "complete", Gold: 1, Matched: 1, CostUSD: 0.5}
	again := eval.Score{Name: "again", Status: "complete", Gold: 1, Matched: 1, CostUSD: 0.25}

	var out bytes.Buffer
	if err := writePlanted(&out, []eval.Score{saved, ok}, true); err != nil {
		t.Fatal(err)
	}
	var got evalSummary
	if decErr := json.Unmarshal(out.Bytes(), &got); decErr != nil || got.Total.Completed != 1 || got.Total.Cases != 2 || len(got.Cases) != 2 || got.Cases[0].Err != "write findings: disk full" {
		t.Fatalf("json: %v %s", decErr, out.String())
	}

	out.Reset()
	if err := writePlanted(&out, []eval.Score{saved, ok}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "complete write findings: disk full") || !strings.Contains(out.String(), "1/2 complete") {
		t.Fatalf("table: %s", out.String())
	}

	out.Reset()
	if err := writePlanted(&out, []eval.Score{ok, again}, true); err != nil {
		t.Fatal(err)
	}
	if decErr := json.Unmarshal(out.Bytes(), &got); decErr != nil || got.Total.Completed != 2 || got.Total.Cases != 2 {
		t.Fatalf("saved json: %v %s", decErr, out.String())
	}
	out.Reset()
	if err := writePlanted(&out, []eval.Score{ok, again}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2/2 complete") {
		t.Fatalf("saved table: %s", out.String())
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

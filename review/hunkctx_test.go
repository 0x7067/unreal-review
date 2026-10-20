package review

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/0x7067/unreal-review/findings"
)

func TestDiscoveryContextLeavesRoomAndOrdersTests(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "rare.go", "package p\n\nfunc Rare() {}\n")
	writeRepoFile(t, dir, "a:b.go", "package p\n\nfunc UseRare() { Rare[int]() // RARE_CALL }\n")
	writeRepoFile(t, dir, "many.go", "package p\n\nfunc Many() {}\n")
	writeRepoFile(t, dir, "many_test.go", "package p\n\nfunc TestMany(t *testing.T) { Many() // TEST_KEPT }\n")
	for i := 0; i < 20; i++ {
		writeRepoFile(t, dir, fmt.Sprintf("c%02d.go", i), fmt.Sprintf("package p\n\nfunc Use%02d() { Many() // MANY_%02d }\n", i, i))
	}
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "rare.go", "package p\n\nfunc Rare() { /* changed */ }\n")
	writeRepoFile(t, dir, "many.go", "package p\n\nfunc Many() { /* changed */ }\n")
	got := discover(t, dir)
	if len(got) > 8*1024 || !utf8.ValidString(got) || !strings.Contains(got, "RARE_CALL") || !testBeforeCall(got, "TEST_KEPT", "MANY_00") {
		t.Fatalf("context len=%d valid=%v\n%s", len(got), utf8.ValidString(got), got)
	}
}

func TestDiscoveryContextTruncatesOnUTF8Boundaries(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "a.go", "package p\n\nfunc Keep() {}\n")
	writeRepoFile(t, dir, "b.go", "package p\n\nfunc UseKeep() { Keep() // €kept }\n")
	writeRepoFile(t, dir, "t.go", "package p\n\nfunc Target() {}\n")
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc Use() { Target() // "+strings.Repeat("a", 133)+"€TAIL }\n")
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "a.go", "package p\n\nfunc Keep() { /* changed */ }\n")
	writeRepoFile(t, dir, "t.go", "package p\n\nfunc Target() { /* changed */ }\n")
	got := discover(t, dir)
	if !utf8.ValidString(got) || !strings.Contains(got, "€kept") || !strings.Contains(got, strings.Repeat("a", 40)) || strings.Contains(got, "TAIL") {
		t.Fatalf("utf8 clip:\n%s", got)
	}
}

func TestDiscoveryContextSurvivesGatheringFailure(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "live.go", "package p\n\nfunc Live() {}\n")
	writeRepoFile(t, dir, "caller.go", "package p\n\nfunc Use() { Live() // LIVE_CALL }\n")
	commitAll(t, dir, "base")
	diff := "diff --git a/live.go b/live.go\n--- a/live.go\n+++ b/live.go\n@@ -1,3 +1,3 @@\n package p\n\n-func Live() {}\n+func Live() { /* touch */ }\ndiff --git a/missing.go b/missing.go\n--- a/missing.go\n+++ b/missing.go\n@@ -1,3 +1,3 @@\n package p\n-func Absent() {}\n+func Absent() { /* GONE */ }\n"
	mode := "diff --git a/x.go b/x.go\nold mode 100644\nnew mode 100755\n" + diff
	got, err := renderContext(t.Context(), dir, resolved{}, mode, []ChangedFile{{Path: "x.go"}, {Path: "live.go"}})
	if err != nil || !strings.Contains(got, "LIVE_CALL") || strings.Contains(got, "GONE") {
		t.Fatalf("mode-only section dropped excerpts: err=%v\n%s", err, got)
	}
	plain := t.TempDir()
	if missed, err := renderContext(t.Context(), plain, resolved{}, diff, nil); err != nil || missed != "" {
		t.Fatalf("grep failure err=%v\n%s", err, missed)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := renderContext(ctx, dir, resolved{}, diff, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context returned %v", err)
	}
	writeRepoFile(t, dir, "live.go", "package p\n\nfunc Live() { /* changed */ }\n")
	sel := planTestSelection(t, dir, Spec{})
	plan, err := buildReviewPlan(t.Context(), dir, sel)
	missed, missErr := buildReviewPlan(t.Context(), plain, sel)
	if err != nil || missErr != nil || !strings.Contains(planPromptText(plan), "LIVE_CALL") || strings.Contains(planPromptText(missed), "LIVE_CALL") {
		t.Fatalf("planned callers err=%v miss=%v", err, missErr)
	}
}

func TestRunPromptCarriesCallerContext(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() {}\n")
	writeRepoFile(t, dir, "pkg/caller.go", "package pkg\n\nfunc Use() { Target() // PROMPT_CALL }\n")
	writeRepoFile(t, dir, "pkg/target_test.go", "package pkg\n\nfunc TestTarget(t *testing.T) { Target() // PROMPT_TEST }\n")
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() { /* changed */ }\n")

	var prompt string
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		prompt = req.Prompt
		if err := findings.AppendSummary(req.FindingsPath, findings.CleanVerdict+": checked the changed function."); err != nil {
			return AgentResult{}, err
		}
		return AgentResult{Cost: findings.Cost{AmountUSD: 0.01, Currency: "USD", Requests: 1}}, nil
	})
	if _, err := Run(t.Context(), Options{Workspace: dir, Out: t.TempDir() + "/findings.jsonl", Model: "test", Agent: backend}); err != nil {
		t.Fatal(err)
	}
	if fence := strings.Index(prompt, "```diff"); fence < 0 || !testBeforeCall(prompt[fence:], "PROMPT_TEST", "PROMPT_CALL") {
		t.Fatalf("test excerpt is not before the caller after the diff fence:\n%s", prompt)
	}

	var root string
	backend = accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		root = req.Prompt
		return AgentResult{}, context.Canceled
	})
	if _, err := Run(t.Context(), Options{Workspace: dir, Out: t.TempDir() + "/planned.jsonl", Fresh: true, Decompose: true, Model: "test", Agent: backend}); err == nil || strings.Contains(root, "PROMPT_CALL") {
		t.Fatalf("planned root leaked callers: %v", err)
	}
}

func TestResumeReusesSavedCallerExcerpt(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "t.go", "package p\n\nfunc Target() {}\n")
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc Use() { Target() // SAVED_CALL }\n")
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "t.go", "package p\n\nfunc Target() { /* changed */ }\n")
	out := t.TempDir() + "/findings.jsonl"
	var id, again string
	n := 0
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		n++
		if n == 1 {
			id = req.ReviewID
		} else {
			again = req.Prompt
		}
		if n == 2 && (!req.Resuming || req.ReviewID != id) {
			t.Fatalf("resume=%v id=%s", req.Resuming, req.ReviewID)
		}
		return AgentResult{Cost: findings.Cost{AmountUSD: 0.01, Currency: "USD", Requests: 1}}, context.Canceled
	})
	opts := Options{Workspace: dir, Out: out, Paths: []string{"t.go"}, Model: "test", Agent: backend}
	if _, err := Run(t.Context(), opts); err == nil {
		t.Fatal("first run completed")
	}
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc Use() { Target() // EDITED_CALL }\n")
	if _, err := Run(t.Context(), opts); err == nil || !strings.Contains(again, "SAVED_CALL") || strings.Contains(again, "EDITED_CALL") {
		t.Fatalf("resume excerpt:\n%s", again)
	}
}

func discover(t *testing.T, dir string) string {
	t.Helper()
	sel := planTestSelection(t, dir, Spec{})
	got, err := renderContext(t.Context(), dir, sel.rangeSpec, sel.diff, sel.files)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func renderContext(ctx context.Context, workspace string, r resolved, diff string, files []ChangedFile) (string, error) {
	idx, err := newCallIndex(ctx, workspace, r, diff, files)
	if err != nil {
		return "", err
	}
	return idx.render(nil), nil
}

func testBeforeCall(s, testText, callText string) bool {
	ti, ci := strings.Index(s, testText), strings.Index(s, callText)
	return ti >= 0 && ci > ti
}

func planPromptText(plan *ReviewPlan) string {
	var b strings.Builder
	for _, task := range plan.Tasks {
		b.WriteString(task.Prompt)
	}
	return b.String()
}

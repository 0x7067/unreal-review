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

func TestDiscoveryContextStaysWithinCaps(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() {}\n")
	for i := 0; i < 30; i++ {
		writeRepoFile(t, dir, fmt.Sprintf("pkg/c%02d.go", i), fmt.Sprintf("package pkg\n\nfunc Use%02d() { Target() // MARKER_%02d }\n", i, i))
	}
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() { /* changed */ }\n")
	got := discover(t, dir)
	start := strings.Index(got, "pkg/target.go Target\n")
	if start < 0 || len(got) > 8*1024 || len(got[start:]) > 1024 || !strings.Contains(got, "MARKER_11") || strings.Contains(got, "MARKER_12") {
		t.Fatalf("per-name cap len=%d section=%d\n%s", len(got), len(got[start:]), got)
	}

	held := gitRepo(t)
	writeRepoFile(t, held, "t.go", "package p\n\nfunc Target() {}\n")
	for i := 0; i < 20; i++ {
		writeRepoFile(t, held, fmt.Sprintf("c%02d.go", i), fmt.Sprintf("package p\n\nfunc Use%02d() { Target() }\n", i))
	}
	writeRepoFile(t, held, "z_test.go", "package p\n\nfunc TestTarget(t *testing.T) { Target() // TEST_KEPT }\n")
	commitAll(t, held, "base")
	writeRepoFile(t, held, "t.go", "package p\n\nfunc Target() { /* changed */ }\n")
	if got = discover(t, held); !strings.Contains(got, "TEST_KEPT") {
		t.Fatalf("callers crowded out the test:\n%s", got)
	}

	wide := gitRepo(t)
	for i := 0; i < 8; i++ {
		name := fmt.Sprintf("Fn%02d", i)
		writeRepoFile(t, wide, fmt.Sprintf("a%02d.go", i), "package p\n\nfunc "+name+"() {}\n")
		for j := 0; j < 6; j++ {
			writeRepoFile(t, wide, fmt.Sprintf("c%02d_%d.go", i, j), fmt.Sprintf("package p\n\nfunc Use%02d_%d() { %s() // A%02d %s }\n", i, j, name, i, strings.Repeat("x", 180)))
		}
	}
	// Equal hit counts render by path, so this short section comes after the large ones.
	writeRepoFile(t, wide, "z.go", "package p\n\nfunc Tiny() {}\n")
	for j := 0; j < 6; j++ {
		writeRepoFile(t, wide, fmt.Sprintf("t%d.go", j), fmt.Sprintf("package p\n\nfunc T%d() { Tiny() // SMALL }\n", j))
	}
	commitAll(t, wide, "base")
	for i := 0; i < 8; i++ {
		writeRepoFile(t, wide, fmt.Sprintf("a%02d.go", i), fmt.Sprintf("package p\n\nfunc Fn%02d() { /* changed */ }\n", i))
	}
	writeRepoFile(t, wide, "z.go", "package p\n\nfunc Tiny() { /* changed */ }\n")
	got = discover(t, wide)
	if len(got) > 8*1024 || !strings.Contains(got, "A00") || strings.Contains(got, "A07") || !strings.Contains(got, "SMALL") {
		t.Fatalf("skipped section len=%d\n%s", len(got), got)
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

func TestGrepCallsStopsAtHitCap(t *testing.T) {
	dir := gitRepo(t)
	names := make([]string, 201)
	for i := range names {
		names[i] = fmt.Sprintf("Fn%03d", i)
		writeRepoFile(t, dir, fmt.Sprintf("c%03d.go", i), fmt.Sprintf("package p\n\nfunc Use%03d() { Fn%03d() }\n", i, i))
	}
	commitAll(t, dir, "base")
	hits, err := grepCalls(t.Context(), dir, "", names)
	if err != nil {
		t.Fatal(err)
	}
	sawFirst, sawLast := false, false
	for _, hit := range hits {
		sawFirst = sawFirst || hit.name == "Fn000"
		sawLast = sawLast || hit.name == "Fn200"
	}
	if len(hits) != 200 || !sawFirst || sawLast {
		t.Fatalf("hits=%d first=%v last=%v", len(hits), sawFirst, sawLast)
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
	writeRepoFile(t, plain, "live.go", "package p\n\nfunc Live() {}\n")
	writeRepoFile(t, plain, "caller.go", "package p\n\nfunc Use() { Live() // LIVE_CALL }\n")
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
	plan, err := buildReviewPlan(t.Context(), plain, sel)
	if err != nil {
		t.Fatal(err)
	}
	text := ""
	for _, task := range plan.Tasks {
		text += task.Prompt
	}
	if strings.Contains(text, "LIVE_CALL") {
		t.Fatal("grep failure still attached callers")
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
	fence := strings.Index(prompt, "```diff")
	call := strings.Index(prompt, "PROMPT_CALL")
	testIdx := strings.Index(prompt, "PROMPT_TEST")
	if fence < 0 || call < fence || testIdx < 0 || testIdx > call {
		t.Fatalf("test excerpt is not before the caller after the diff fence:\n%s", prompt)
	}

	var root, planned string
	backend = accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		root = req.Prompt
		for _, task := range req.Plan.Tasks {
			call := strings.Index(task.Prompt, "PROMPT_CALL")
			testIdx := strings.Index(task.Prompt, "PROMPT_TEST")
			if call >= 0 && testIdx >= 0 && testIdx < call {
				planned = task.Prompt
			}
		}
		return AgentResult{}, context.Canceled
	})
	if _, err := Run(t.Context(), Options{Workspace: dir, Out: t.TempDir() + "/planned.jsonl", Fresh: true, Decompose: true, Model: "test", Agent: backend}); err == nil || planned == "" || strings.Contains(root, "PROMPT_CALL") {
		t.Fatalf("planned discovery=%t root leaked=%t err nil", planned != "", strings.Contains(root, "PROMPT_CALL"))
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

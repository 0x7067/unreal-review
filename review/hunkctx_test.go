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
	if start < 0 || len(got) > 8*1024 || len(got[start:]) > 1024 || !strings.Contains(got, "MARKER_00") || strings.Contains(got, "MARKER_29") {
		t.Fatalf("per-function cap len=%d section=%d\n%s", len(got), len(got[start:]), got)
	}

	wide := gitRepo(t)
	for i := 0; i < 12; i++ {
		name := fmt.Sprintf("Fn%02d", i)
		writeRepoFile(t, wide, fmt.Sprintf("f%02d.go", i), "package p\n\nfunc "+name+"() {}\n")
		writeRepoFile(t, wide, fmt.Sprintf("c%02d.go", i), fmt.Sprintf("package p\n\nfunc Use%02d() { %s() // %sM%02d }\n", i, name, strings.Repeat("a", 700), i))
	}
	commitAll(t, wide, "base")
	for i := 0; i < 12; i++ {
		writeRepoFile(t, wide, fmt.Sprintf("f%02d.go", i), fmt.Sprintf("package p\n\nfunc Fn%02d() { /* changed */ }\n", i))
	}
	got = discover(t, wide)
	if len(got) > 8*1024 || !strings.Contains(got, "M00") || strings.Contains(got, "M11") {
		t.Fatalf("prompt cap len=%d\n%s", len(got), got)
	}
}

func TestDiscoveryContextTruncatesOnUTF8Boundaries(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "a.go", "package p\n\nfunc Keep() {}\n")
	writeRepoFile(t, dir, "b.go", "package p\n\nfunc UseKeep() { Keep() // €kept }\n")
	writeRepoFile(t, dir, "t.go", "package p\n\nfunc Target() {}\n")
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc Use() { Target() // "+strings.Repeat("a", 970)+"€TAIL }\n")
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
	got, err := discoveryContext(t.Context(), dir, resolved{}, diff, nil)
	if err != nil || !strings.Contains(got, "LIVE_CALL") || strings.Contains(got, "GONE") {
		t.Fatalf("err=%v\n%s", err, got)
	}
	plain := t.TempDir()
	writeRepoFile(t, plain, "live.go", "package p\n\nfunc Live() {}\n")
	writeRepoFile(t, plain, "caller.go", "package p\n\nfunc Use() { Live() // LIVE_CALL }\n")
	if missed, err := discoveryContext(t.Context(), plain, resolved{}, diff, nil); err != nil || missed != "" {
		t.Fatalf("grep failure err=%v\n%s", err, missed)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := discoveryContext(ctx, dir, resolved{}, diff, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled context returned %v", err)
	}
	writeRepoFile(t, dir, "live.go", "package p\n\nfunc Live() { /* changed */ }\n")
	sel := planTestSelection(t, dir, Spec{})
	plan, err := buildReviewPlan(t.Context(), plain, sel)
	if err != nil || strings.Contains(planPromptText(plan), "LIVE_CALL") {
		t.Fatalf("plan err=%v still has callers", err)
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
	if fence, call := strings.Index(prompt, "```diff"), strings.Index(prompt, "PROMPT_CALL"); fence < 0 || call < fence || !strings.Contains(prompt, "PROMPT_TEST") {
		t.Fatalf("caller context is not after the diff fence:\n%s", prompt)
	}

	var root, planned string
	backend = accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		root = req.Prompt
		for _, task := range req.Plan.Tasks {
			if strings.Contains(task.Prompt, "PROMPT_CALL") && strings.Contains(task.Prompt, "PROMPT_TEST") {
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
	got, err := discoveryContext(t.Context(), dir, sel.rangeSpec, sel.diff, nil)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func planPromptText(plan *ReviewPlan) string {
	var b strings.Builder
	for _, task := range plan.Tasks {
		b.WriteString(task.Prompt)
	}
	return b.String()
}

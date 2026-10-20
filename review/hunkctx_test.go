package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/0x7067/unreal-review/findings"
)

func TestDiscoveryContextLeavesRoomAndOrdersTests(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "rare.go", "package p\n\nfunc Rare() {}\n")
	writeRepoFile(t, dir, "a:b.go", "package p\n\nfunc UseRare() { Rare[int]() // RARE_CALL "+strings.Repeat("a", 133)+"€TAIL }\n")
	for i := 0; i < 9; i++ {
		name := fmt.Sprintf("F%02d", i)
		writeRepoFile(t, dir, fmt.Sprintf("f%02d.go", i), "package p\n\nfunc "+name+"() {}\n")
		for j := 0; j < 6; j++ {
			writeRepoFile(t, dir, fmt.Sprintf("u%02d%d.go", i, j), fmt.Sprintf("package p\n\nfunc U%02d%d() { %s() // %s }\n", i, j, name, strings.Repeat("x", 180)))
		}
	}
	writeRepoFile(t, dir, "late.go", "package p\n\nfunc Late() {}\n")
	writeRepoFile(t, dir, "late_test.go", "package p\n\nfunc TestLate(t *testing.T) { Late() // LATE_TEST }\n")
	for j := 0; j < 6; j++ {
		writeRepoFile(t, dir, fmt.Sprintf("l%d.go", j), fmt.Sprintf("package p\n\nfunc L%d() { Late() // LATE_CALL }\n", j))
	}
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "rare.go", "package p\n\nfunc Rare() { /* changed */ }\n")
	writeRepoFile(t, dir, "late.go", "package p\n\nfunc Late() { /* changed */ }\n")
	for i := 0; i < 9; i++ {
		writeRepoFile(t, dir, fmt.Sprintf("f%02d.go", i), fmt.Sprintf("package p\n\nfunc F%02d() { /* changed */ }\n", i))
	}
	sel := planTestSelection(t, dir, Spec{})
	idx, err := newCallIndex(t.Context(), dir, sel.rangeSpec, sel.diff, sel.files)
	if err != nil {
		t.Fatal(err)
	}
	parts := idx.parts()
	got := renderParts(parts, nil)
	dropped, over := false, false
	for _, part := range parts {
		if len(part.Text) > 1024 {
			over = true
		} else if !strings.Contains(got, part.Text) {
			dropped = true
		}
	}
	if !dropped || over || !utf8.ValidString(got) || strings.Contains(got, "TAIL") || !strings.Contains(got, strings.Repeat("a", 40)) || !strings.Contains(got, "RARE_CALL") || !strings.Contains(got, "LATE_TEST") || strings.Index(got, "LATE_TEST") >= strings.Index(got, "LATE_CALL") {
		t.Fatalf("dropped=%v over=%v\n%s", dropped, over, got)
	}
}
func TestDiscoveryContextSurvivesGatheringFailure(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "live.go", "package p\n\nfunc Live() {}\n")
	writeRepoFile(t, dir, "caller.go", "package p\n\nfunc Use() { Live() // LIVE_CALL }\n")
	writeRepoFile(t, dir, "a.go", "package p\n\nfunc Around() {\n}\n\nfunc Other() {\n}\n")
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc Use() {\n\tAround() // IN_AROUND\n\tOther() // IN_OTHER\n}\n")
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
	del := "diff --git a/a.go b/a.go\n--- a/a.go\n+++ b/a.go\n@@ -3,1 +3,0 @@\n"
	if got, err = renderContext(t.Context(), dir, resolved{}, del, []ChangedFile{{Path: "a.go"}}); err != nil || !strings.Contains(got, "IN_AROUND") || strings.Contains(got, "IN_OTHER") {
		t.Fatalf("deletion hunk: %v\n%s", err, got)
	}
}
func TestRunPromptCarriesCallerContext(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() {}\n")
	writeRepoFile(t, dir, "pkg/caller.go", "package pkg\n\nfunc Use() { Target() // PROMPT_CALL }\n")
	writeRepoFile(t, dir, "pkg/target_test.go", "package pkg\n\nfunc TestTarget(t *testing.T) { Target() // PROMPT_TEST }\n")
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() { /* changed */ }\n")
	var prompt, root, planned string
	cost := findings.Cost{AmountUSD: 0.01, Currency: "USD", Requests: 1}
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		prompt = req.Prompt
		return AgentResult{Cost: cost}, findings.AppendSummary(req.FindingsPath, findings.CleanVerdict+": checked the changed function.")
	})
	if _, err := Run(t.Context(), Options{Workspace: dir, Out: t.TempDir() + "/findings.jsonl", Model: "test", Agent: backend}); err != nil {
		t.Fatal(err)
	}
	if fence := strings.Index(prompt, "```diff"); fence < 0 || !strings.Contains(prompt[fence:], "PROMPT_TEST") || strings.Index(prompt[fence:], "PROMPT_TEST") >= strings.Index(prompt[fence:], "PROMPT_CALL") {
		t.Fatalf("test excerpt is not before the caller after the diff fence:\n%s", prompt)
	}
	backend = accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		root = req.Prompt
		for _, task := range req.Plan.Tasks {
			planned += task.Prompt
		}
		return AgentResult{}, context.Canceled
	})
	if _, err := Run(t.Context(), Options{Workspace: dir, Out: t.TempDir() + "/planned.jsonl", Fresh: true, Decompose: true, Model: "test", Agent: backend}); err == nil || strings.Contains(root, "PROMPT_CALL") || !strings.Contains(planned, "PROMPT_TEST") || strings.Index(planned, "PROMPT_TEST") >= strings.Index(planned, "PROMPT_CALL") {
		t.Fatalf("planned task excerpt missing: %v\n%s", err, planned)
	}
}
func TestStaleCallerExcerptIsIgnored(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "t.go", "package p\n\nfunc Target() {}\n")
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc Use() { Target() // LIVE_NOW }\n")
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "t.go", "package p\n\nfunc Target() { /* changed */ }\n")
	out := t.TempDir() + "/findings.jsonl"
	var n int
	var got, first, id string
	cost := findings.Cost{AmountUSD: 0.01, Currency: "USD", Requests: 1}
	opts := Options{Workspace: dir, Out: out, Paths: []string{"t.go"}, Model: "test", Agent: accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		n++
		if n == 1 {
			first, id = req.Prompt, req.ReviewID
		}
		got = req.Prompt
		if n == 2 && (!req.Resuming || req.ReviewID != id) {
			t.Fatalf("resume=%v id=%s", req.Resuming, req.ReviewID)
		}
		if n < 4 {
			return AgentResult{Cost: cost}, context.Canceled
		}
		return AgentResult{Cost: cost}, findings.AppendSummary(req.FindingsPath, findings.CleanVerdict+": checked.")
	})}
	run := func() error { _, err := Run(t.Context(), opts); return err }
	if run() == nil {
		t.Fatal("first run completed")
	}
	if _, err := os.Stat(out + ".context"); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc Use() { Target() // NEWER_CALL }\n")
	if err := run(); err == nil || got != first || strings.Contains(got, "NEWER_CALL") {
		t.Fatalf("resume: %v\n%s", err, got)
	}
	rep, err := findings.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	plant := func(diff, text string) {
		t.Helper()
		raw, err := json.Marshal(savedExcerpt{rep.Run.Source.BaseSHA, rep.Run.Source.HeadSHA, diff, []savedPart{{"t.go", "t.go T\n" + text + "\n"}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(out+".context", raw, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	plant("not-the-diff", "STALE_KEY")
	if run() == nil || strings.Contains(got, "STALE_KEY") || !strings.Contains(got, "NEWER_CALL") {
		t.Fatalf("wrong key:\n%s", got)
	}
	if run() != nil {
		t.Fatal("completion")
	}
	if _, err := os.Stat(out + ".context"); !os.IsNotExist(err) {
		t.Fatal("completed review left a sidecar")
	}
	plant(rep.Run.Source.DiffSHA, "STALE_DONE")
	if err := run(); err == nil || !strings.Contains(err.Error(), "complete review") {
		t.Fatalf("complete checkpoint: %v", err)
	}
	if body, err := os.ReadFile(out + ".context"); err != nil || !strings.Contains(string(body), "STALE_DONE") {
		t.Fatal("refused run rewrote the sidecar")
	}
	opts.Fresh = true
	if err := run(); err != nil || strings.Contains(got, "STALE_DONE") || !strings.Contains(got, "NEWER_CALL") {
		t.Fatalf("fresh: %v\n%s", err, got)
	}
	if _, err := os.Stat(out + ".context"); !os.IsNotExist(err) {
		t.Fatal("fresh left a sidecar")
	}
}
func renderContext(ctx context.Context, workspace string, r resolved, diff string, files []ChangedFile) (string, error) {
	idx, err := newCallIndex(ctx, workspace, r, diff, files)
	if err != nil {
		return "", err
	}
	return renderParts(idx.parts(), nil), nil
}

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
	writeRepoFile(t, dir, "a:b.go", "package p\n\nfunc UseRare() { Rare[int]() // RARE_CALL }\n")
	writeRepoFile(t, dir, "edge.go", "package p\n\nfunc Edge() {}\n")
	writeRepoFile(t, dir, "edge_c.go", "package p\n\nfunc UseEdge() { Edge() // "+strings.Repeat("a", 133)+"€TAIL }\n")
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
	writeRepoFile(t, dir, "edge.go", "package p\n\nfunc Edge() { /* changed */ }\n")
	writeRepoFile(t, dir, "late.go", "package p\n\nfunc Late() { /* changed */ }\n")
	for i := 0; i < 9; i++ {
		writeRepoFile(t, dir, fmt.Sprintf("f%02d.go", i), fmt.Sprintf("package p\n\nfunc F%02d() { /* changed */ }\n", i))
	}
	sel := planTestSelection(t, dir, Spec{})
	got, err := renderContext(t.Context(), dir, sel.rangeSpec, sel.diff, sel.files)
	if err != nil {
		t.Fatal(err)
	}
	i, j := strings.Index(got, "f00.go F00\n"), strings.Index(got, "\nf01.go")
	if len(got) > 8*1024 || i < 0 || j <= i || j-i > 1024 || !utf8.ValidString(got) || strings.Contains(got, "f08.go") || strings.Contains(got, "TAIL") || !strings.Contains(got, strings.Repeat("a", 40)) || !strings.Contains(got, "RARE_CALL") || !testBeforeCall(got, "LATE_TEST", "LATE_CALL") {
		t.Fatalf("len=%d section=%d valid=%v\n%s", len(got), j-i, utf8.ValidString(got), got)
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
	if fence := strings.Index(prompt, "```diff"); fence < 0 || !testBeforeCall(prompt[fence:], "PROMPT_TEST", "PROMPT_CALL") {
		t.Fatalf("test excerpt is not before the caller after the diff fence:\n%s", prompt)
	}
	backend = accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		root = req.Prompt
		if req.Plan != nil {
			for _, task := range req.Plan.Tasks {
				planned += task.Prompt
			}
		}
		return AgentResult{}, context.Canceled
	})
	if _, err := Run(t.Context(), Options{Workspace: dir, Out: t.TempDir() + "/planned.jsonl", Fresh: true, Decompose: true, Model: "test", Agent: backend}); err == nil || strings.Contains(root, "PROMPT_CALL") || !testBeforeCall(planned, "PROMPT_TEST", "PROMPT_CALL") {
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
	var got string
	cost := findings.Cost{AmountUSD: 0.01, Currency: "USD", Requests: 1}
	opts := Options{Workspace: dir, Out: out, Paths: []string{"t.go"}, Model: "test", Agent: accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		n++
		got = req.Prompt
		if n < 3 {
			return AgentResult{Cost: cost}, context.Canceled
		}
		return AgentResult{Cost: cost}, findings.AppendSummary(req.FindingsPath, findings.CleanVerdict+": checked.")
	})}
	run := func() error { _, err := Run(t.Context(), opts); return err }
	if run() == nil {
		t.Fatal("first run completed")
	}
	if _, err := os.Stat(out + ".context"); !os.IsNotExist(err) {
		t.Fatal("first run wrote a sidecar")
	}
	rep, err := findings.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	plant := func(diff, text string) {
		t.Helper()
		raw, merr := json.Marshal(savedExcerpt{rep.Run.Source.BaseSHA, rep.Run.Source.HeadSHA, diff, text})
		if merr != nil || os.WriteFile(out+".context", raw, 0o644) != nil {
			t.Fatal(merr)
		}
	}
	plant("not-the-diff", "STALE_KEY")
	if run() == nil || strings.Contains(got, "STALE_KEY") || !strings.Contains(got, "LIVE_NOW") {
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
	writeRepoFile(t, dir, "c.go", "package p\n\nfunc Use() { Target() // NEWER_CALL }\n")
	opts.Fresh = true
	if err := run(); err != nil || strings.Contains(got, "STALE_DONE") || !strings.Contains(got, "NEWER_CALL") {
		t.Fatalf("fresh: %v\n%s", err, got)
	}
	if _, err := os.Stat(out + ".context"); !os.IsNotExist(err) {
		t.Fatal("fresh left a sidecar")
	}
	plant(rep.Run.Source.DiffSHA, "STALE_CANCEL")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := callsFor(ctx, dir, resolved{}, "", nil, out, true, rep.Run.Source.BaseSHA, rep.Run.Source.HeadSHA, rep.Run.Source.DiffSHA); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled cache read: %v", err)
	}
}
func renderContext(ctx context.Context, workspace string, r resolved, diff string, files []ChangedFile) (string, error) {
	idx, _, err := newCallIndex(ctx, workspace, r, diff, files)
	if err != nil {
		return "", err
	}
	return idx.render(), nil
}
func testBeforeCall(s, testText, callText string) bool {
	ti, ci := strings.Index(s, testText), strings.Index(s, callText)
	return ti >= 0 && ci > ti
}

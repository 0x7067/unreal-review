package review

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/findings"
)

func TestDiscoveryContextDropsCallersPastTheCap(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() {\n\treturn\n}\n")
	writeRepoFile(t, dir, "pkg/target_test.go", "package pkg\n\nfunc TestTarget(t *testing.T) {\n\tTarget() // TESTMARKER\n}\n")
	for i := 0; i < 30; i++ {
		body := fmt.Sprintf("package pkg\n\nfunc Use%02d() {\n\tTarget() // MARKER_%02d\n}\n", i, i)
		writeRepoFile(t, dir, fmt.Sprintf("pkg/c%02d.go", i), body)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() {\n\treturn // changed\n}\n")

	sel := planTestSelection(t, dir, Spec{})
	got, err := discoveryContext(t.Context(), dir, sel.rangeSpec, sel.diff, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "TESTMARKER") || !strings.Contains(got, "MARKER_00") {
		t.Fatalf("kept context missing early caller or test:\n%s", got)
	}
	if strings.Contains(got, "MARKER_29") {
		t.Fatalf("caller past the cap was included:\n%s", got)
	}
}

func TestDiscoveryContextIsDeterministic(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() {}\n")
	writeRepoFile(t, dir, "z_use.go", "package z\n\nfunc UseZ() { Target() // MARKER_Z }\n")
	writeRepoFile(t, dir, "a_use.go", "package a\n\nfunc UseA() { Target() // MARKER_A }\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() { /* changed */ }\n")
	sel := planTestSelection(t, dir, Spec{})

	first, err := discoveryContext(t.Context(), dir, sel.rangeSpec, sel.diff, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, err := discoveryContext(t.Context(), dir, sel.rangeSpec, sel.diff, nil)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("context changed between reads:\n%s\n---\n%s", first, second)
	}
	a := strings.Index(first, "MARKER_A")
	z := strings.Index(first, "MARKER_Z")
	if a < 0 || z < 0 || a > z {
		t.Fatalf("callers not in path order:\n%s", first)
	}
}

func TestDiscoveryContextSkipsMissingFile(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "live.go", "package p\n\nfunc Live() {}\n")
	writeRepoFile(t, dir, "caller.go", "package p\n\nfunc Use() {\n\tLive() // LIVE_CALL\n\tAbsent() // CALLS_ABSENT\n}\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	diff := `diff --git a/live.go b/live.go
--- a/live.go
+++ b/live.go
@@ -1,3 +1,3 @@
 package p

-func Live() {}
+func Live() { /* touch */ }
diff --git a/missing.go b/missing.go
--- a/missing.go
+++ b/missing.go
@@ -1,3 +1,3 @@
 package p
-func Absent() {}
+func Absent() { /* GONE */ }
`
	got, err := discoveryContext(t.Context(), dir, resolved{}, diff, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "LIVE_CALL") || !strings.Contains(got, "CALLS_ABSENT") {
		t.Fatalf("context dropped a file that exists:\n%s", got)
	}
	if strings.Contains(got, "GONE") {
		t.Fatalf("missing file body was invented:\n%s", got)
	}
}

func TestDiscoveryContextOmitsUncalledFunction(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "pkg/alone.go", "package pkg\n\nfunc Alone() { return }\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	writeRepoFile(t, dir, "pkg/alone.go", "package pkg\n\nfunc Alone() { return /* changed */ }\n")
	sel := planTestSelection(t, dir, Spec{})
	got, err := discoveryContext(t.Context(), dir, sel.rangeSpec, sel.diff, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("uncalled function produced context:\n%s", got)
	}
}

func TestRunPromptCarriesCallerContext(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() {}\n")
	writeRepoFile(t, dir, "pkg/caller.go", "package pkg\n\nfunc Use() { Target() // PROMPT_CALL }\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	writeRepoFile(t, dir, "pkg/target.go", "package pkg\n\nfunc Target() { /* changed */ }\n")

	var prompt string
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		prompt = req.Prompt
		if err := findings.AppendSummary(req.FindingsPath, findings.CleanVerdict+": checked the changed function."); err != nil {
			return AgentResult{}, err
		}
		return AgentResult{Cost: findings.Cost{AmountUSD: 0.01, Currency: "USD", Requests: 1}}, nil
	})
	_, err := Run(t.Context(), Options{Workspace: dir, Out: t.TempDir() + "/findings.jsonl", Model: "test", Agent: backend})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "PROMPT_CALL") {
		t.Fatalf("review prompt omitted the caller:\n%s", prompt)
	}

	var planned string
	backend = accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		if req.Plan == nil {
			t.Fatal("decomposed review had no plan")
		}
		for _, task := range req.Plan.Tasks {
			if strings.Contains(task.Prompt, "PROMPT_CALL") {
				planned = task.Prompt
			}
		}
		return AgentResult{}, context.Canceled
	})
	_, err = Run(t.Context(), Options{
		Workspace: dir, Out: t.TempDir() + "/planned.jsonl", Fresh: true, Decompose: true, Model: "test", Agent: backend,
	})
	if err == nil {
		t.Fatal("expected the canceled planned review to surface")
	}
	if planned == "" {
		t.Fatal("planned discovery prompt omitted the caller")
	}
}

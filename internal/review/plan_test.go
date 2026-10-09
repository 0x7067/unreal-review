package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/internal/findings"
)

func planTestSelection(t *testing.T, dir string, spec Spec) selection {
	t.Helper()
	sel, err := loadGitDiff(context.Background(), dir, spec, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	return sel
}
func planTestBuild(t *testing.T, dir string, sel selection) *ReviewPlan {
	t.Helper()
	plan, err := buildReviewPlan(context.Background(), dir, sel)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidatePlan(plan); err != nil {
		t.Fatal(err)
	}
	return plan
}
func planTestClone(t *testing.T, plan *ReviewPlan) *ReviewPlan {
	t.Helper()
	data, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var copy ReviewPlan
	if err := json.Unmarshal(data, &copy); err != nil {
		t.Fatal(err)
	}
	return &copy
}
func planTestOwned(t *testing.T, plan *ReviewPlan, diff string) {
	t.Helper()
	var spans []DiffSpan
	for _, task := range plan.Tasks {
		if task.Kind == "local" {
			spans = append(spans, task.Spans...)
		} else if len(task.Spans) != 0 {
			t.Fatal("boundary owns bytes")
		}
	}
	slices.SortFunc(spans, func(a, b DiffSpan) int { return a.Start - b.Start })
	var rebuilt strings.Builder
	for _, span := range spans {
		rebuilt.WriteString(diff[span.Start:span.End])
	}
	if rebuilt.String() != diff {
		t.Fatal("ownership did not reconstruct exact original diff")
	}
}

func TestReviewPlanPacksSmallUnrelatedGroupsNearBudget(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	for i := 0; i < 40; i++ {
		var body strings.Builder
		for body.Len() < 8000 {
			fmt.Fprintf(&body, "GROUP_%02d_%04d_%s\n", i, body.Len(), strings.Repeat("x", 80))
		}
		writeRepoFile(t, dir, fmt.Sprintf("groups/%02d.txt", i), body.String())
	}
	sel := planTestSelection(t, dir, Spec{})
	plan := planTestBuild(t, dir, sel)
	planTestOwned(t, plan, sel.diff)
	locals := []PlanTask{}
	for _, task := range plan.Tasks {
		if task.Kind == "local" {
			locals = append(locals, task)
		}
	}
	if len(locals) < 2 || len(locals) > 5 {
		t.Fatalf("expected a few packed locals, got %d", len(locals))
	}
}

func TestReviewPlanKeepsLinkedGroupIntactAcrossPackingBoundary(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "go.mod", "module example.com/packing\n\ngo 1.22\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	lines := func(prefix string, size int) string {
		var body strings.Builder
		for body.Len() < size {
			fmt.Fprintf(&body, "// %s_%06d_%s\n", prefix, body.Len(), strings.Repeat("x", 80))
		}
		return body.String()
	}
	writeRepoFile(t, dir, "a-prefix.txt", lines("PREFIX", planPromptBudget*2/3))
	writeRepoFile(t, dir, "zlink/a.go", "package zlink\n\nimport _ \"example.com/packing/zlink/b\"\n\n"+lines("LINK_A", planPromptBudget/4))
	writeRepoFile(t, dir, "zlink/b/b.go", "package b\n\n"+lines("LINK_B", planPromptBudget/4))
	sel := planTestSelection(t, dir, Spec{})
	plan := planTestBuild(t, dir, sel)
	planTestOwned(t, plan, sel.diff)
	linkedLocal := -1
	for i, task := range plan.Tasks {
		if task.Kind != "local" {
			continue
		}
		hasA := slices.Contains(task.Paths, "zlink/a.go")
		hasB := slices.Contains(task.Paths, "zlink/b/b.go")
		if hasA != hasB {
			t.Fatalf("linked group split in local %d: %q", i, task.Paths)
		}
		if hasA {
			linkedLocal = i
			if slices.Contains(task.Paths, "a-prefix.txt") {
				t.Fatalf("linked group crossed an over-budget packing boundary: %q", task.Paths)
			}
		}
	}
	if linkedLocal < 0 {
		t.Fatal("linked group was not assigned to a local")
	}
}

func TestReviewPlanLargeMultiFileAndDeterminism(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	for file := 0; file < 8; file++ {
		var body strings.Builder
		for line := 0; line < 300; line++ {
			fmt.Fprintf(&body, "MARK_%d_%04d_%s\n", file, line, strings.Repeat("x", 110))
		}
		writeRepoFile(t, dir, fmt.Sprintf("src/file%d.txt", file), body.String())
	}
	sel := planTestSelection(t, dir, Spec{})
	if len(sel.diff) <= 252000 {
		t.Fatalf("fixture is only %d bytes", len(sel.diff))
	}
	plan := planTestBuild(t, dir, sel)
	planTestOwned(t, plan, sel.diff)
	if plan.DiffSHA != sel.source.DiffSHA || plan.DiffBytes != len(sel.diff) {
		t.Fatal("plan changed source binding")
	}
	localCount, boundaryCount := 0, 0
	for _, task := range plan.Tasks {
		if task.Kind == "local" {
			localCount++
		} else {
			boundaryCount++
		}
	}
	if localCount < 3 || boundaryCount < localCount {
		t.Fatalf("local %d boundary %d", localCount, boundaryCount)
	}
	second := planTestBuild(t, dir, sel)
	if plan.Digest != second.Digest {
		t.Fatal("same selected source produced nondeterministic plan")
	}
	slices.Reverse(sel.files)
	third := planTestBuild(t, dir, sel)
	if plan.Digest != third.Digest {
		t.Fatal("numstat order changed plan")
	}
}

func TestReviewPlanManySeparatedHunks(t *testing.T) {
	dir := gitRepo(t)
	var old, new strings.Builder
	for i := 0; i < 3000; i++ {
		line := fmt.Sprintf("line%04d_%s\n", i, strings.Repeat("x", 100))
		old.WriteString(line)
		if i%10 == 5 {
			new.WriteString(strings.Replace(line, "line", "MARK", 1))
		} else {
			new.WriteString(line)
		}
	}
	writeRepoFile(t, dir, "single.txt", old.String())
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	writeRepoFile(t, dir, "single.txt", new.String())
	sel := planTestSelection(t, dir, Spec{})
	if len(sel.diff) <= 200000 || strings.Count(sel.diff, "@@ -") < 200 {
		t.Fatalf("fixture bytes=%d hunks=%d", len(sel.diff), strings.Count(sel.diff, "@@ -"))
	}
	plan := planTestBuild(t, dir, sel)
	planTestOwned(t, plan, sel.diff)
}

func TestReviewPlanRenamesBinaryDeletesModesAndQuotedPaths(t *testing.T) {
	dir := gitRepo(t)
	for path, body := range map[string]string{"old name.txt": "rename\n", "deleted.txt": "gone\n", "mode.txt": "#!/bin/sh\n", "bytes.bin": "old\x00binary", "tab\tname.txt": "before\n"} {
		writeRepoFile(t, dir, path, body)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	if err := os.Rename(filepath.Join(dir, "old name.txt"), filepath.Join(dir, "new name.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "deleted.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(dir, "mode.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRepoFile(t, dir, "bytes.bin", "new\x00binary")
	writeRepoFile(t, dir, "tab\tname.txt", "after\n")
	gitRun(t, dir, "add", ".")
	sel := planTestSelection(t, dir, Spec{})
	plan := planTestBuild(t, dir, sel)
	planTestOwned(t, plan, sel.diff)
	all := []string{}
	for _, task := range plan.Tasks {
		if task.Kind == "local" {
			all = append(all, task.Paths...)
		}
	}
	for _, path := range []string{"new name.txt", "deleted.txt", "mode.txt", "bytes.bin", "tab\tname.txt"} {
		if !slices.Contains(all, path) {
			t.Fatalf("omitted %q, got %q", path, all)
		}
	}
	if !strings.Contains(sel.diff, "rename from old name.txt") || !strings.Contains(sel.diff, "Binary files") || !strings.Contains(sel.diff, "new mode") {
		t.Fatalf("fixture missing metadata: %s", sel.diff)
	}
}

func TestReviewPlanRejectsUnsupportedAndCancellation(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "giant.txt", strings.Repeat("x", MaxPlanPromptBytes+1)+"\n")
	sel := planTestSelection(t, dir, Spec{})
	if _, err := buildReviewPlan(context.Background(), dir, sel); err == nil {
		t.Fatalf("giant line: %v", err)
	}
	small := "diff --git a/x.txt b/x.txt\n--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-a\n+b\n"
	sel = selection{diff: small, source: findings.Source{DiffSHA: diffFingerprint(small)}, files: []ChangedFile{{Path: "x.txt"}}}
	sel.pull.Description = strings.Repeat("d", MaxPlanPromptBytes)
	if _, err := buildReviewPlan(context.Background(), dir, sel); err == nil {
		t.Fatalf("PR context: %v", err)
	}
	sel.pull.Description = ""
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := buildReviewPlan(ctx, dir, sel); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	sel.source.DiffSHA = diffFingerprint("different")
	if _, err := buildReviewPlan(context.Background(), dir, sel); err == nil {
		t.Fatal("accepted different raw source hash")
	}
	metadata := "diff --git a/x.txt b/x.txt\nindex " + strings.Repeat("x", MaxPlanPromptBytes) + "\n"
	sel = selection{diff: metadata, source: findings.Source{DiffSHA: diffFingerprint(metadata)}, files: []ChangedFile{{Path: "x.txt"}}}
	if _, err := buildReviewPlan(context.Background(), dir, sel); err == nil {
		t.Fatalf("metadata: %v", err)
	}
}

func TestValidatePlanRejectsStructuralAndDigestTampering(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "x.txt", "x\n")
	valid := planTestBuild(t, dir, planTestSelection(t, dir, Spec{}))
	cases := []struct {
		name   string
		mutate func(*ReviewPlan)
	}{
		{"gap", func(p *ReviewPlan) { p.Tasks[0].Spans[0].Start++ }},
		{"overlap", func(p *ReviewPlan) { p.Tasks[0].Spans = append(p.Tasks[0].Spans, p.Tasks[0].Spans[0]) }},
		{"end gap", func(p *ReviewPlan) { p.Tasks[0].Spans[0].End-- }},
		{"duplicate id", func(p *ReviewPlan) { p.Tasks = append(p.Tasks, p.Tasks[0]) }},
		{"boundary ownership", func(p *ReviewPlan) { p.Tasks[1].Spans = []DiffSpan{{0, 1}} }},
		{"no boundary", func(p *ReviewPlan) { p.Tasks = p.Tasks[:1] }},
		{"plan digest", func(p *ReviewPlan) { p.DiffSHA = diffFingerprint("different") }},
		{"invalid path", func(p *ReviewPlan) { p.Tasks[0].Paths = []string{"../escape"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := planTestClone(t, valid)
			tc.mutate(p)
			if err := ValidatePlan(p); err == nil {
				t.Fatal("accepted malformed plan")
			}
		})
	}
	if err := ValidatePlan(nil); err == nil {
		t.Fatal("accepted nil")
	}
}

func TestValidateCoverageRequiresEveryCompletedAndVerifiedID(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "x.txt", "x\n")
	plan := planTestBuild(t, dir, planTestSelection(t, dir, Spec{}))
	ids := []string{}
	for _, task := range plan.Tasks {
		ids = append(ids, task.ID)
	}
	coverage := &PlanCoverage{Digest: plan.Digest, Completed: slices.Clone(ids), Verified: slices.Clone(ids)}
	if err := ValidateCoverage(plan, coverage); err != nil {
		t.Fatal(err)
	}
	slices.Reverse(coverage.Verified)
	if err := ValidateCoverage(plan, coverage); err != nil {
		t.Fatal(err)
	}
	for _, verified := range []bool{false, true} {
		for _, kind := range []string{"missing", "duplicate", "unknown"} {
			t.Run(fmt.Sprintf("verified=%t/%s", verified, kind), func(t *testing.T) {
				c := &PlanCoverage{Digest: plan.Digest, Completed: slices.Clone(ids), Verified: slices.Clone(ids)}
				set := &c.Completed
				if verified {
					set = &c.Verified
				}
				switch kind {
				case "missing":
					*set = (*set)[:len(*set)-1]
				case "duplicate":
					*set = append(*set, (*set)[0])
				case "unknown":
					(*set)[0] = "unknown"
				}
				if err := ValidateCoverage(plan, c); err == nil {
					t.Fatal("accepted incomplete/foreign/duplicate coverage")
				}
			})
		}
	}
	coverage.Digest = diffFingerprint("other")
	if err := ValidateCoverage(plan, coverage); err == nil {
		t.Fatal("accepted wrong digest")
	}
	if err := ValidateCoverage(plan, nil); err == nil {
		t.Fatal("accepted nil coverage")
	}
}

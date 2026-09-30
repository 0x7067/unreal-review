package review

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"unreal-review/internal/findings"
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
func planTestRebind(t *testing.T, plan *ReviewPlan) {
	t.Helper()
	for i := range plan.Tasks {
		plan.Tasks[i].ID = planTaskID(plan.Tasks[i])
	}
	var err error
	plan.Digest, err = planDigest(plan)
	if err != nil {
		t.Fatal(err)
	}
}
func planTestOwned(t *testing.T, plan *ReviewPlan, diff string) {
	t.Helper()
	var spans []DiffSpan
	for _, task := range plan.Tasks {
		if len(task.Prompt) > MaxPlanPromptBytes {
			t.Fatalf("%s prompt is %d bytes", task.ID, len(task.Prompt))
		}
		if !strings.Contains(task.Prompt, plan.DiffSHA) {
			t.Fatalf("task omitted whole source hash: %s", task.ID)
		}
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
	for i, task := range locals[:len(locals)-1] {
		if len(task.Prompt) < planPromptBudget/2 {
			t.Fatalf("local %d used only %d of %d bytes", i, len(task.Prompt), planPromptBudget)
		}
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
	for file := 0; file < 8; file++ {
		for line := 0; line < 300; line++ {
			marker := fmt.Sprintf("+MARK_%d_%04d_", file, line)
			found := 0
			for _, task := range plan.Tasks {
				if task.Kind == "local" {
					found += strings.Count(task.Prompt, marker)
				}
			}
			if found != 1 {
				t.Fatalf("marker %s occurs %d times in locals", marker, found)
			}
		}
	}
	for _, task := range plan.Tasks {
		if task.Kind == "boundary" {
			if !strings.Contains(task.Prompt, "whole_scope_manifest:") || !strings.Contains(task.Prompt, "MARK_") || !strings.Contains(task.Prompt, "Other-language and ambiguous dependencies") {
				t.Fatal("boundary lacks manifest, actual context or conservative fallback")
			}
			for _, local := range plan.Tasks {
				if local.Kind == "local" && !strings.Contains(task.Prompt, local.ID) {
					t.Fatal("boundary omitted local from global manifest")
				}
			}
		}
	}
	second := planTestBuild(t, dir, sel)
	if planJSON(plan) != planJSON(second) {
		t.Fatal("same selected source produced nondeterministic plan")
	}
	slices.Reverse(sel.files)
	third := planTestBuild(t, dir, sel)
	if planJSON(plan) != planJSON(third) {
		t.Fatal("numstat order changed plan")
	}
}

// Parse rendered fragments exactly as a reviewer does and check every original
// old/new marker's line coordinate, including zero-count chunks after deletions.
func TestReviewPlanSingletonHunkPreservesCoordinates(t *testing.T) {
	dir := gitRepo(t)
	const count = 1400
	var old, new strings.Builder
	for i := 1; i <= count; i++ {
		fmt.Fprintf(&old, "OLD_%04d_%s\n", i, strings.Repeat("a", 100))
		fmt.Fprintf(&new, "NEW_%04d_%s\n", i, strings.Repeat("b", 100))
	}
	// No-newline marker must stay with its preceding physical diff line.
	writeRepoFile(t, dir, "huge.txt", strings.TrimSuffix(old.String(), "\n"))
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	writeRepoFile(t, dir, "huge.txt", strings.TrimSuffix(new.String(), "\n"))
	sel := planTestSelection(t, dir, Spec{})
	if len(sel.diff) <= 200000 {
		t.Fatal("fixture does not exceed single review limit")
	}
	plan := planTestBuild(t, dir, sel)
	planTestOwned(t, plan, sel.diff)
	seenOld, seenNew := make(map[int]bool), make(map[int]bool)
	localCount := 0
	for _, task := range plan.Tasks {
		if task.Kind != "local" {
			continue
		}
		localCount++
		_, patch, ok := strings.Cut(task.Prompt, "```diff\n")
		if !ok {
			t.Fatal("no diff fragment")
		}
		patch = strings.TrimSuffix(patch, "```\n")
		oldCursor, newCursor := 0, 0
		oldExpected, newExpected, oldSeen, newSeen := 0, 0, 0, 0
		finish := func() {
			if oldSeen != oldExpected || newSeen != newExpected {
				t.Fatalf("bad rendered hunk counts %d/%d vs %d/%d", oldSeen, newSeen, oldExpected, newExpected)
			}
		}
		hasHunk := false
		for _, line := range planLines(patch, 0) {
			if strings.HasPrefix(line.text, "@@ ") {
				if hasHunk {
					finish()
				}
				hasHunk = true
				h, err := planParseHunk(line.text)
				if err != nil {
					t.Fatal(err)
				}
				oldCursor, newCursor = h.oldStart, h.newStart
				oldExpected, newExpected = h.oldCount, h.newCount
				oldSeen, newSeen = 0, 0
				continue
			}
			if strings.HasPrefix(line.text, "diff --git ") {
				if hasHunk {
					finish()
					hasHunk = false
				}
				continue
			}
			if !hasHunk || strings.HasPrefix(line.text, "\\ ") {
				continue
			}
			switch line.text[0] {
			case '-':
				i, _ := strconv.Atoi(line.text[len("-OLD_") : len("-OLD_")+4])
				if i != oldCursor || seenOld[i] {
					t.Fatalf("old marker %d anchored at %d", i, oldCursor)
				}
				seenOld[i] = true
				oldCursor++
				oldSeen++
			case '+':
				i, _ := strconv.Atoi(line.text[len("+NEW_") : len("+NEW_")+4])
				if i != newCursor || seenNew[i] {
					t.Fatalf("new marker %d anchored at %d", i, newCursor)
				}
				seenNew[i] = true
				newCursor++
				newSeen++
			case ' ':
				oldCursor++
				newCursor++
				oldSeen++
				newSeen++
			}
		}
		if hasHunk {
			finish()
		}
	}
	if localCount < 3 || len(seenOld) != count || len(seenNew) != count {
		t.Fatalf("local=%d old=%d new=%d", localCount, len(seenOld), len(seenNew))
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
	for i := 5; i < 3000; i += 10 {
		marker := fmt.Sprintf("+MARK%04d_", i)
		n := 0
		for _, task := range plan.Tasks {
			if task.Kind == "local" {
				n += strings.Count(task.Prompt, marker)
			}
		}
		if n != 1 {
			t.Fatalf("%s occurs %d times", marker, n)
		}
	}
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

func TestReviewPlanUsesCapturedMergeBaseAndDestination(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "x.txt", "base\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	ancestor, err := gitRev(context.Background(), dir, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "checkout", "-q", "-b", "left")
	writeRepoFile(t, dir, "left.txt", "left\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "left")
	gitRun(t, dir, "checkout", "-q", "-b", "right", ancestor)
	writeRepoFile(t, dir, "x.txt", "right\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "right")
	sel := planTestSelection(t, dir, Spec{From: "left", To: "right"})
	gitRun(t, dir, "checkout", "-q", "left")
	writeRepoFile(t, dir, "left.txt", "later\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "later")
	plan := planTestBuild(t, dir, sel)
	for _, task := range plan.Tasks {
		if !strings.Contains(task.Prompt, "diff_old_revision: "+ancestor) || !strings.Contains(task.Prompt, "source_head_sha: "+sel.source.HeadSHA) {
			t.Fatal("plan reread mutable refs or used non-merge-base old source")
		}
		if strings.Contains(task.Prompt, "Destination is the selected working tree") {
			t.Fatal("committed destination mislabeled")
		}
	}
}

func TestReviewPlanRejectsUnsupportedAndCancellation(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "giant.txt", strings.Repeat("x", MaxPlanPromptBytes+1)+"\n")
	sel := planTestSelection(t, dir, Spec{})
	if _, err := buildReviewPlan(context.Background(), dir, sel); err == nil || !strings.Contains(err.Error(), "indivisible diff line") {
		t.Fatalf("giant line: %v", err)
	}
	small := "diff --git a/x.txt b/x.txt\n--- a/x.txt\n+++ b/x.txt\n@@ -1 +1 @@\n-a\n+b\n"
	sel = selection{diff: small, source: findings.Source{DiffSHA: diffFingerprint(small)}, files: []ChangedFile{{Path: "x.txt"}}}
	sel.pull.Description = strings.Repeat("d", MaxPlanPromptBytes)
	if _, err := buildReviewPlan(context.Background(), dir, sel); err == nil || !strings.Contains(err.Error(), "full PR/source context") {
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
	if _, err := buildReviewPlan(context.Background(), dir, sel); err == nil || !strings.Contains(err.Error(), "indivisible metadata") {
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
		rebind bool
	}{
		{"gap", func(p *ReviewPlan) { p.Tasks[0].Spans[0].Start++ }, true},
		{"overlap", func(p *ReviewPlan) { p.Tasks[0].Spans = append(p.Tasks[0].Spans, p.Tasks[0].Spans[0]) }, true},
		{"end gap", func(p *ReviewPlan) { p.Tasks[0].Spans[0].End-- }, true},
		{"budget", func(p *ReviewPlan) { p.Tasks[0].Prompt = strings.Repeat("x", MaxPlanPromptBytes+1) }, true},
		{"empty prompt", func(p *ReviewPlan) { p.Tasks[0].Prompt = "" }, true},
		{"duplicate id", func(p *ReviewPlan) { p.Tasks = append(p.Tasks, p.Tasks[0]) }, false},
		{"boundary ownership", func(p *ReviewPlan) { p.Tasks[1].Spans = []DiffSpan{{0, 1}} }, true},
		{"no boundary", func(p *ReviewPlan) { p.Tasks = p.Tasks[:1] }, true},
		{"task content", func(p *ReviewPlan) { p.Tasks[0].Prompt += "tampered" }, false},
		{"plan digest", func(p *ReviewPlan) { p.DiffSHA = diffFingerprint("different") }, false},
		{"invalid path", func(p *ReviewPlan) { p.Tasks[0].Paths = []string{"../escape"} }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := planTestClone(t, valid)
			tc.mutate(p)
			if tc.rebind {
				planTestRebind(t, p)
			}
			if err := ValidatePlan(p); err == nil {
				t.Fatal("accepted malformed plan")
			}
		})
	}
	p := planTestClone(t, valid)
	p.Tasks[0].Prompt += "x"
	p.Tasks[0].ID = planTaskID(p.Tasks[0])
	if err := ValidatePlan(p); err == nil || !strings.Contains(err.Error(), "digest/content") {
		t.Fatalf("digest tamper: %v", err)
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

func TestPlanSectionPathDecodesGitQuotedBinaryAndModeNames(t *testing.T) {
	for _, metadata := range []string{"old mode 100644\nnew mode 100755\n", "Binary files differ\n"} {
		patch := "diff --git \"a/caf\\303\\251.bin\" \"b/caf\\303\\251.bin\"\n" + metadata
		if path := planSectionPath(patch, []ChangedFile{{Path: "café.bin"}}); path != "café.bin" {
			t.Fatalf("Git octal-quoted path decoded as %q", path)
		}
	}
}

func TestPlanBoundariesRetainKnownCrossScopeEdgesAndRejectHugeManifest(t *testing.T) {
	locals := []planLocal{}
	for i := 0; i < 5; i++ {
		path := fmt.Sprintf("file%d.go", i)
		task := PlanTask{Kind: "local", Paths: []string{path}, Spans: []DiffSpan{{i, i + 1}}, Prompt: "local source"}
		task.ID = planTaskID(task)
		locals = append(locals, planLocal{task: task, patch: fmt.Sprintf("+actual_context_%d\n", i)})
	}
	plan := &ReviewPlan{}
	if err := planAddBoundaries(context.Background(), plan, locals, []importEdge{{from: "file0.go", to: "file3.go"}}, "source framing\n"); err != nil {
		t.Fatal(err)
	}
	if len(plan.Tasks) != 6 {
		t.Fatalf("expected five per-local passes plus non-neighbor edge, got %d", len(plan.Tasks))
	}
	last := plan.Tasks[len(plan.Tasks)-1]
	if !strings.Contains(last.Prompt, "known Go import edge") || !strings.Contains(last.Prompt, "actual_context_0") || !strings.Contains(last.Prompt, "actual_context_3") {
		t.Fatal("known edge boundary omitted actual cross-scope context")
	}
	for _, task := range plan.Tasks {
		if len(task.Prompt) > planPromptBudget {
			t.Fatal("builder failed to reserve executor framing")
		}
	}
	large := make([]planLocal, 1000)
	for i := range large {
		path := fmt.Sprintf("scope/%04d/%s.go", i, strings.Repeat("p", 50))
		large[i] = planLocal{task: PlanTask{ID: "local-" + diffFingerprint(path), Paths: []string{path}, Spans: []DiffSpan{{i, i + 1}}}, patch: "+context\n"}
	}
	if err := planAddBoundaries(context.Background(), &ReviewPlan{}, large, nil, "source\n"); err == nil || !strings.Contains(err.Error(), "whole-PR scope manifest") {
		t.Fatalf("oversized full scope manifest: %v", err)
	}
}

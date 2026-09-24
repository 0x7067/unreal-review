package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadGitDiffReviewsRangeInFull(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")

	large := strings.Repeat("x\n", 4001)
	if err := os.WriteFile(filepath.Join(dir, "large.txt"), []byte(large), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "large.txt", "keep.txt")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	ctx := context.Background()
	diff, source, err := loadGitDiff(ctx, dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "large.txt") {
		t.Fatalf("large file omitted from diff:\n%s", diff)
	}
	if strings.Count(diff, "+x") < 4001 {
		t.Fatalf("large file truncated: %d added lines", strings.Count(diff, "+x"))
	}
	if !strings.Contains(diff, "keep.txt") {
		t.Fatalf("keep.txt omitted from diff:\n%s", diff)
	}
	sum := sha256.Sum256([]byte(diff))
	if source.DiffSHA != hex.EncodeToString(sum[:]) {
		t.Fatalf("diff_sha %s want %s", source.DiffSHA, hex.EncodeToString(sum[:]))
	}

	excluded, excludedSource, err := loadGitDiff(ctx, dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, []string{"large.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(excluded, "large.txt") {
		t.Fatalf("--exclude left large.txt in the diff:\n%s", excluded)
	}
	if !strings.Contains(excluded, "keep.txt") {
		t.Fatalf("--exclude dropped keep.txt:\n%s", excluded)
	}
	if excludedSource.DiffSHA == source.DiffSHA {
		t.Fatal("excluded diff hashed the same as the full range")
	}
}

func TestClusterFilesPairsJSTests(t *testing.T) {
	files, err := parseNumstat("1\t0\tlib/bar.ts\n1\t0\tlib/__tests__/bar.test.ts\n1\t0\tsrc/foo.ts\n1\t0\tsrc/foo.test.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	want := [][]string{
		{"lib/__tests__/bar.test.ts", "lib/bar.ts"},
		{"src/foo.test.ts", "src/foo.ts"},
	}
	if len(got) != len(want) {
		t.Fatalf("groups: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("group %d: %v want %v", i, got[i], want[i])
		}
	}
}

func TestClusterFilesLocaleFamilies(t *testing.T) {
	files, err := parseNumstat("1\t0\tmessages_en.properties\n1\t0\tmessages_zh.properties\n1\t0\tREADME.md\n")
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	if len(got) != 2 {
		t.Fatalf("groups: %v", got)
	}
	if strings.Join(got[0], ",") != "README.md" {
		t.Fatalf("readme: %v", got[0])
	}
	if strings.Join(got[1], ",") != "messages_en.properties,messages_zh.properties" {
		t.Fatalf("locale suffix: %v", got[1])
	}

	files, err = parseNumstat("1\t0\tlocales/en/auth.json\n1\t0\tlocales/zh/auth.json\n1\t0\tlocales/en/common.json\n")
	if err != nil {
		t.Fatal(err)
	}
	got = groupPaths(clusterFiles(files, nil))
	if len(got) != 2 {
		t.Fatalf("locale dirs: %v", got)
	}
	if strings.Join(got[0], ",") != "locales/en/auth.json,locales/zh/auth.json" {
		t.Fatalf("auth locales: %v", got[0])
	}
	if strings.Join(got[1], ",") != "locales/en/common.json" {
		t.Fatalf("common: %v", got[1])
	}

	files, err = parseNumstat("1\t0\tfoo_en.go\n1\t0\tfoo.go\n")
	if err != nil {
		t.Fatal(err)
	}
	got = groupPaths(clusterFiles(files, nil))
	if len(got) != 2 {
		t.Fatalf("go files should not strip locale: %v", got)
	}

	files, err = parseNumstat("1\t0\tpages/src/content/docs/en/review-rules.md\n1\t0\tpages/src/content/docs/zh/review-rules.md\n1\t0\tpages/src/content/docs/en/other.md\n")
	if err != nil {
		t.Fatal(err)
	}
	got = groupPaths(clusterFiles(files, nil))
	if len(got) != 2 {
		t.Fatalf("docs lang dirs: %v", got)
	}
	if strings.Join(got[0], ",") != "pages/src/content/docs/en/other.md" {
		t.Fatalf("other: %v", got[0])
	}
	if strings.Join(got[1], ",") != "pages/src/content/docs/en/review-rules.md,pages/src/content/docs/zh/review-rules.md" {
		t.Fatalf("review-rules locales: %v", got[1])
	}
}

func TestClusterFilesPackageLocalTests(t *testing.T) {
	files, err := parseNumstat("1\t0\tinternal/session/compare.go\n1\t0\tinternal/session/compare_test.go\n1\t0\tinternal/viewer/compare_test.go\n1\t0\tinternal/viewer/handler.go\n")
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	want := [][]string{
		{"internal/session/compare.go", "internal/session/compare_test.go"},
		{"internal/viewer/compare_test.go", "internal/viewer/handler.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("groups: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("group %d: %v want %v", i, got[i], want[i])
		}
	}
}

func TestClusterFilesHeaderPair(t *testing.T) {
	files, err := parseNumstat("1\t0\tinclude/foo.h\n1\t0\tsrc/foo.c\n1\t0\tsrc/bar.c\n")
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	if len(got) != 2 {
		t.Fatalf("groups: %v", got)
	}
	if strings.Join(got[0], ",") != "include/foo.h,src/foo.c" {
		t.Fatalf("header pair: %v", got[0])
	}
	if strings.Join(got[1], ",") != "src/bar.c" {
		t.Fatalf("unpaired: %v", got[1])
	}
}

func TestClusterFilesStemCompanions(t *testing.T) {
	files, err := parseNumstat("1\t0\tButton.tsx\n1\t0\tButton.module.css\n1\t0\tButton.stories.tsx\n1\t0\tREADME.md\n")
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	want := [][]string{
		{"Button.module.css", "Button.stories.tsx", "Button.tsx"},
		{"README.md"},
	}
	if len(got) != len(want) {
		t.Fatalf("groups: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("group %d: %v want %v", i, got[i], want[i])
		}
	}

	files, err = parseNumstat("1\t0\tpkg/foo.go\n1\t0\tpkg/foo_linux.go\n1\t0\tpkg/bar.go\n")
	if err != nil {
		t.Fatal(err)
	}
	got = groupPaths(clusterFiles(files, nil))
	want = [][]string{
		{"pkg/bar.go"},
		{"pkg/foo.go", "pkg/foo_linux.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("go platform: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("go platform group %d: %v want %v", i, got[i], want[i])
		}
	}
}

func TestClusterFilesJavaTest(t *testing.T) {
	files, err := parseNumstat("1\t0\tsrc/Foo.java\n1\t0\ttest/FooTest.java\n1\t0\ttest/OrphanTest.java\n")
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	want := [][]string{
		{"src/Foo.java", "test/FooTest.java"},
		{"test/OrphanTest.java"},
	}
	if len(got) != len(want) {
		t.Fatalf("groups: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("group %d: %v want %v", i, got[i], want[i])
		}
	}
}

func TestClusterFilesLockfiles(t *testing.T) {
	files, err := parseNumstat("1\t0\tgo.mod\n1\t0\tgo.sum\n1\t0\tmain.go\n")
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	want := [][]string{
		{"go.mod", "go.sum"},
		{"main.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("groups: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("group %d: %v want %v", i, got[i], want[i])
		}
	}
}

func TestClusterFilesPacksFatDirectory(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&b, "10\t0\tpkg/f%02d.go\n", i)
	}
	files, err := parseNumstat(b.String())
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	if len(got) != 2 {
		t.Fatalf("groups: %d %v", len(got), got)
	}
	if len(got[0]) != 25 || got[0][0] != "pkg/f00.go" || got[0][24] != "pkg/f24.go" {
		t.Fatalf("first pack: %v", got[0])
	}
	if len(got[1]) != 5 || got[1][0] != "pkg/f25.go" || got[1][4] != "pkg/f29.go" {
		t.Fatalf("second pack: %v", got[1])
	}
}

func TestClusterFilesImportEdges(t *testing.T) {
	files, err := parseNumstat("1\t0\tinternal/api/billing.go\n1\t0\tinternal/billing/charge.go\n1\t0\tinternal/other/x.go\n")
	if err != nil {
		t.Fatal(err)
	}
	src := map[string][]byte{
		"internal/api/billing.go":    []byte("package api\n\nimport \"example/internal/billing\"\n"),
		"internal/billing/charge.go": []byte("package billing\n"),
		"internal/other/x.go":        []byte("package other\n"),
	}
	got := groupPaths(clusterFiles(files, src))
	want := [][]string{
		{"internal/api/billing.go", "internal/billing/charge.go"},
		{"internal/other/x.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("go import: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("go import group %d: %v want %v", i, got[i], want[i])
		}
	}

	files, err = parseNumstat("1\t0\tpkg/a.ts\n1\t0\tlib/b.ts\n")
	if err != nil {
		t.Fatal(err)
	}
	src = map[string][]byte{
		"pkg/a.ts": []byte("import { b } from '../lib/b'\n"),
		"lib/b.ts": []byte("export const b = 1\n"),
	}
	got = groupPaths(clusterFiles(files, src))
	want = [][]string{{"lib/b.ts", "pkg/a.ts"}}
	if len(got) != 1 || strings.Join(got[0], ",") != strings.Join(want[0], ",") {
		t.Fatalf("ts relative: %v", got)
	}

	files, err = parseNumstat("1\t0\tapp/Main.kt\n1\t0\tapp/src/com/foo/Bar.kt\n")
	if err != nil {
		t.Fatal(err)
	}
	src = map[string][]byte{
		"app/Main.kt":            []byte("import com.foo.Bar\n"),
		"app/src/com/foo/Bar.kt": []byte("package com.foo\nclass Bar\n"),
	}
	got = groupPaths(clusterFiles(files, src))
	if len(got) != 1 {
		t.Fatalf("kotlin import: %v", got)
	}
}

func TestClusterFilesImportAmbiguous(t *testing.T) {
	files, err := parseNumstat("1\t0\tinternal/api/billing.go\n1\t0\tinternal/billing/charge.go\n1\t0\tinternal/billing/other.go\n")
	if err != nil {
		t.Fatal(err)
	}
	src := map[string][]byte{
		"internal/api/billing.go":    []byte("package api\n\nimport \"example/internal/billing\"\n"),
		"internal/billing/charge.go": []byte("package billing\n"),
		"internal/billing/other.go":  []byte("package billing\n"),
	}
	got := groupPaths(clusterFiles(files, src))
	want := [][]string{
		{"internal/api/billing.go"},
		{"internal/billing/charge.go", "internal/billing/other.go"},
	}
	if len(got) != len(want) {
		t.Fatalf("ambiguous: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("ambiguous group %d: %v want %v", i, got[i], want[i])
		}
	}
}

func TestClusterFilesSplitsOversizedDirectory(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 13; i++ {
		fmt.Fprintf(&b, "10\t0\tsrc/auth/f%d.go\n", i)
		fmt.Fprintf(&b, "10\t0\tsrc/bill/f%d.go\n", i)
	}
	files, err := parseNumstat(b.String())
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(clusterFiles(files, nil))
	if len(got) != 2 {
		t.Fatalf("groups: %d %v", len(got), got)
	}
	if len(got[0]) != 13 || !strings.HasPrefix(got[0][0], "src/auth/") {
		t.Fatalf("auth: %v", got[0])
	}
	if len(got[1]) != 13 || !strings.HasPrefix(got[1][0], "src/bill/") {
		t.Fatalf("bill: %v", got[1])
	}
}

func TestGroupsSplitsDirectories(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "internal", "review"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "internal", "review", "git.go"), []byte("package review\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 2 {
		t.Fatalf("groups: %+v", groupPaths(result.Groups))
	}
	if result.Groups[0].Title != "cmd" || strings.Join(result.Groups[0].Pathspecs, " ") != "cmd" {
		t.Fatalf("first group: title=%q pathspecs=%v files=%v", result.Groups[0].Title, result.Groups[0].Pathspecs, filePaths(result.Groups[0]))
	}
	if result.Groups[1].Title != "internal/review" || strings.Join(result.Groups[1].Pathspecs, " ") != "internal/review" {
		t.Fatalf("second group: title=%q pathspecs=%v files=%v", result.Groups[1].Title, result.Groups[1].Pathspecs, filePaths(result.Groups[1]))
	}
}

func TestGroupsAttachesCrossDirectoryTests(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	for _, path := range []string{
		filepath.Join(dir, "pkg", "a"),
		filepath.Join(dir, "pkg", "b"),
		filepath.Join(dir, "tests"),
	} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[string]string{
		"pkg/a/a.py":      "a = 1\n",
		"pkg/b/b.py":      "b = 1\n",
		"tests/test_a.py": "def test_a():\n    pass\n",
		"tests/test_b.py": "def test_b():\n    pass\n",
		"tests/test_c.py": "def test_c():\n    pass\n",
	}
	for path, body := range files {
		if err := os.WriteFile(filepath.Join(dir, path), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"pkg/a/a.py", "tests/test_a.py"},
		{"pkg/b/b.py", "tests/test_b.py"},
		{"tests/test_c.py"},
	}
	if len(got) != len(want) {
		t.Fatalf("groups: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("group %d: %v want %v", i, got[i], want[i])
		}
	}
	if strings.Join(result.Groups[0].Pathspecs, " ") != "pkg/a/a.py tests/test_a.py" {
		t.Fatalf("pathspecs: %v", result.Groups[0].Pathspecs)
	}
	if strings.Join(result.Groups[2].Pathspecs, " ") != "tests/test_c.py" {
		t.Fatalf("unmatched test pathspec %v would include paired tests", result.Groups[2].Pathspecs)
	}
}

func TestClusterFilesNestedDirectoryPathspecs(t *testing.T) {
	files, err := parseNumstat("1\t0\tcmd/main.go\n1\t0\tcmd/sub/x.go\n")
	if err != nil {
		t.Fatal(err)
	}
	groups := clusterFiles(files, nil)
	got := groupPaths(groups)
	want := [][]string{{"cmd/main.go"}, {"cmd/sub/x.go"}}
	if len(got) != len(want) {
		t.Fatalf("groups: %v", got)
	}
	for i := range want {
		if strings.Join(got[i], ",") != strings.Join(want[i], ",") {
			t.Fatalf("group %d: %v want %v", i, got[i], want[i])
		}
	}
	if strings.Join(groups[0].Pathspecs, " ") != "cmd/main.go" {
		t.Fatalf("parent pathspec %v includes nested files", groups[0].Pathspecs)
	}
	if strings.Join(groups[1].Pathspecs, " ") != "cmd/sub" {
		t.Fatalf("nested pathspec: %v", groups[1].Pathspecs)
	}
}

func TestGroupsExcludeAndEmpty(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	if err := os.MkdirAll(filepath.Join(dir, "cmd"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "cmd", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.lock"), []byte("lock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	ctx := context.Background()
	result, err := Groups(ctx, dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, []string{"*.lock"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Groups) != 1 || result.Groups[0].Title != "cmd" {
		t.Fatalf("exclude: %+v", groupPaths(result.Groups))
	}
	for _, group := range result.Groups {
		for _, file := range group.Files {
			if strings.Contains(file.Path, "skip.lock") {
				t.Fatalf("excluded lock still present: %v", groupPaths(result.Groups))
			}
		}
	}

	empty, err := Groups(ctx, dir, Spec{From: "HEAD", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty.Groups) != 0 {
		t.Fatalf("empty range: %v", groupPaths(empty.Groups))
	}

	all, err := Groups(ctx, dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(all.Groups)
	if len(got) != 2 || strings.Join(got[0], ",") != "cmd/main.go" || strings.Join(got[1], ",") != "skip.lock" {
		t.Fatalf("root files should not share a group: %v", got)
	}
}

func TestSpecRejectsMixedFlags(t *testing.T) {
	_, err := Spec{From: "main", Commit: "abc"}.mode()
	if err == nil || !strings.Contains(err.Error(), "use only one of --from/--to, --commit, or --branch") {
		t.Fatalf("from+commit: %v", err)
	}
	_, err = Spec{Branch: "feature", To: "HEAD"}.mode()
	if err == nil || !strings.Contains(err.Error(), "use only one of --from/--to, --commit, or --branch") {
		t.Fatalf("branch+to: %v", err)
	}
	_, err = Spec{To: "HEAD"}.mode()
	if err == nil || !strings.Contains(err.Error(), "set --from or use --branch") {
		t.Fatalf("to only: %v", err)
	}
	mode, err := Spec{}.mode()
	if err != nil || mode != specWorkspace {
		t.Fatalf("empty spec: mode=%v err=%v", mode, err)
	}
}

func TestWorkspaceDiffIncludesDirtyAndUntracked(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "base.txt")
	gitRun(t, dir, "commit", "-q", "-m", "main")
	gitRun(t, dir, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "on-branch.txt"), []byte("branch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "on-branch.txt")
	gitRun(t, dir, "commit", "-q", "-m", "feature")
	if err := os.WriteFile(filepath.Join(dir, "base.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "staged.txt")
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("untracked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	diff, source, err := loadGitDiff(ctx, dir, Spec{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if source.Base != "HEAD" || source.Head != "" {
		t.Fatalf("source: %+v", source)
	}
	if !strings.Contains(diff, "base.txt") || !strings.Contains(diff, "staged.txt") || !strings.Contains(diff, "extra.txt") {
		t.Fatalf("workspace omitted a dirty path:\n%s", diff)
	}
	if strings.Contains(diff, "on-branch.txt") {
		t.Fatalf("workspace included committed branch file:\n%s", diff)
	}

	result, err := Groups(ctx, dir, Spec{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, group := range result.Groups {
		paths = append(paths, filePaths(group)...)
	}
	got := strings.Join(paths, ",")
	if !strings.Contains(got, "base.txt") || !strings.Contains(got, "staged.txt") || !strings.Contains(got, "extra.txt") {
		t.Fatalf("workspace groups: %v", got)
	}
	if strings.Contains(got, "on-branch.txt") {
		t.Fatalf("workspace groups included branch file: %v", got)
	}
}

func TestCommitDiffIsParentOnly(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "a.txt")
	gitRun(t, dir, "commit", "-q", "-m", "a")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "b.txt")
	gitRun(t, dir, "commit", "-q", "-m", "b")
	secondCmd := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "HEAD")
	secondOut, err := secondCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %v\n%s", err, secondOut)
	}
	second := strings.TrimSpace(string(secondOut))

	diff, source, err := loadGitDiff(context.Background(), dir, Spec{Commit: second}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if source.Head != second || source.Base != second+"^" {
		t.Fatalf("source: %+v", source)
	}
	if !strings.Contains(diff, "b.txt") {
		t.Fatalf("commit omitted b.txt:\n%s", diff)
	}
	if strings.Contains(diff, "a.txt") {
		t.Fatalf("commit included parent file:\n%s", diff)
	}
}

func TestBranchDiffUsesMergeBase(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "shared.txt")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	gitRun(t, dir, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "feature.txt")
	gitRun(t, dir, "commit", "-q", "-m", "feature")
	gitRun(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "main-only.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "main-only.txt")
	gitRun(t, dir, "commit", "-q", "-m", "main later")
	gitRun(t, dir, "checkout", "-q", "feature")

	ctx := context.Background()
	diff, source, err := loadGitDiff(ctx, dir, Spec{Branch: "feature"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if source.Base != "main" || source.Head != "feature" {
		t.Fatalf("source: %+v", source)
	}
	if !strings.Contains(diff, "feature.txt") {
		t.Fatalf("branch omitted feature.txt:\n%s", diff)
	}
	if strings.Contains(diff, "main-only.txt") {
		t.Fatalf("branch included later main file:\n%s", diff)
	}

	rangeDiff, _, err := loadGitDiff(ctx, dir, Spec{From: "main", To: "feature"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if rangeDiff != diff {
		t.Fatalf("branch and --from/--to diffs differ")
	}
}

func TestFromWithoutToDiffsWorkingTree(t *testing.T) {
	dir := gitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "keep.txt")
	gitRun(t, dir, "commit", "-q", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	diff, source, err := loadGitDiff(context.Background(), dir, Spec{From: "main"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if source.Base != "main" || source.Head != "" {
		t.Fatalf("source: %+v", source)
	}
	if !strings.Contains(diff, "keep.txt") {
		t.Fatalf("working tree omitted keep.txt:\n%s", diff)
	}
}

func groupPaths(groups []FileGroup) [][]string {
	out := make([][]string, len(groups))
	for i, group := range groups {
		out[i] = filePaths(group)
	}
	return out
}

func filePaths(group FileGroup) []string {
	paths := make([]string, len(group.Files))
	for i, file := range group.Files {
		paths[i] = file.Path
	}
	return paths
}

func gitRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_OPTIONAL_LOCKS", "0")
	t.Setenv("GIT_TERMINAL_PROMPT", "0")
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", "-b", "main")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "t")
	return dir
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGroupsPairsUniqueGoImport(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "internal/api/billing.go", "package api\n\nimport \"example/internal/billing\"\n\nfunc Bill() { billing.Charge() }\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n\nfunc Charge() {}\n")
	writeRepoFile(t, dir, "internal/other/x.go", "package other\n\nfunc X() {}\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"go.mod"},
		{"internal/api/billing.go", "internal/billing/charge.go"},
		{"internal/other/x.go"},
	}
	if strings.Join(flatten(got), " | ") != strings.Join(flatten(want), " | ") {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
}

func TestGroupsLeavesAmbiguousGoImportsSeparate(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "internal/api/billing.go", "package api\n\nimport (\n\t\"example/internal/billing\"\n\t\"example/internal/other\"\n)\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n")
	writeRepoFile(t, dir, "internal/other/x.go", "package other\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"go.mod"},
		{"internal/api/billing.go"},
		{"internal/billing/charge.go"},
		{"internal/other/x.go"},
	}
	if strings.Join(flatten(got), " | ") != strings.Join(flatten(want), " | ") {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
}

func TestGroupsReadsImportsFromDestinationCommit(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeImportFixture(t, dir)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "paired")

	writeRepoFile(t, dir, "internal/api/billing.go", "package api\n\nfunc Bill() {}\n")

	committed, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(flatten(groupPaths(committed.Groups)), " | ") != "go.mod | internal/api/billing.go,internal/billing/charge.go | internal/other/x.go" {
		t.Fatalf("commit range used the dirty tree: %v", groupPaths(committed.Groups))
	}

	dirty, err := Groups(context.Background(), dir, Spec{From: "HEAD~1"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := flatten(groupPaths(dirty.Groups))
	if strings.Contains(strings.Join(got, " | "), "billing.go,internal/billing/charge.go") {
		t.Fatalf("--from without --to ignored the working tree: %v", got)
	}
}

func TestGroupsWorkspaceReadsDiskImports(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeImportFixture(t, dir)

	result, err := Groups(context.Background(), dir, Spec{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(flatten(groupPaths(result.Groups)), " | ") != "go.mod | internal/api/billing.go,internal/billing/charge.go | internal/other/x.go" {
		t.Fatalf("workspace groups: %v", groupPaths(result.Groups))
	}
}

func TestGroupsCrossDirTestFollowsImport(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "internal/api/a.go", "package api\n\nimport \"example/internal/billing\"\n")
	writeRepoFile(t, dir, "internal/api/a_test.go", "package api\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n\nfunc Charge() {}\n")
	writeRepoFile(t, dir, "tests/test_a.go", "package tests\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"go.mod"},
		{"internal/api/a.go", "internal/billing/charge.go", "tests/test_a.go"},
		{"internal/api/a_test.go"},
	}
	if strings.Join(flatten(got), " | ") != strings.Join(flatten(want), " | ") {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
}

func TestGroupsImportLeavesDirectorySibling(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "internal/api/a.go", "package api\n\nimport \"example/internal/billing\"\n")
	writeRepoFile(t, dir, "internal/api/helper.go", "package api\n\nfunc Help() {}\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n\nfunc Charge() {}\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"go.mod"},
		{"internal/api/a.go", "internal/billing/charge.go"},
		{"internal/api/helper.go"},
	}
	if strings.Join(flatten(got), " | ") != strings.Join(flatten(want), " | ") {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
}

func TestGroupsImportFollowsChainedRepresentative(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "internal/api/a.go", "package api\n\nimport \"example/internal/billing\"\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n\nimport \"example/internal/other\"\n")
	writeRepoFile(t, dir, "internal/billing/extra.go", "package billing\n\nfunc Extra() {}\n")
	writeRepoFile(t, dir, "internal/other/x.go", "package other\n\nfunc X() {}\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"go.mod"},
		{"internal/api/a.go", "internal/billing/charge.go", "internal/other/x.go"},
		{"internal/billing/extra.go"},
	}
	if strings.Join(flatten(got), " | ") != strings.Join(flatten(want), " | ") {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
}

func TestGroupsImportMutualUsesSiblingRepresentative(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "internal/api/a.go", "package api\n\nfunc A() {}\n")
	writeRepoFile(t, dir, "internal/api/b.go", "package api\n\nimport \"example/internal/billing\"\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n\nimport \"example/internal/api\"\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"go.mod"},
		{"internal/api/a.go", "internal/api/b.go", "internal/billing/charge.go"},
	}
	if strings.Join(flatten(got), " | ") != strings.Join(flatten(want), " | ") {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
}

func TestGoImportEdgesUsePackageRepresentative(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	for _, rel := range []string{"internal/api/a.go", "internal/pay/b.go"} {
		writeRepoFile(t, dir, rel, "package p\n\nimport \"example/internal/billing\"\n")
	}
	for _, rel := range []string{"internal/billing/c1.go", "internal/billing/c2.go", "internal/billing/c3.go"} {
		writeRepoFile(t, dir, rel, "package billing\n")
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	var billing []string
	for _, group := range groupPaths(result.Groups) {
		if strings.Contains(strings.Join(group, ","), "internal/billing/c1.go") {
			billing = group
		}
	}
	want := "internal/api/a.go,internal/billing/c1.go,internal/billing/c2.go,internal/billing/c3.go,internal/pay/b.go"
	if strings.Join(billing, ",") != want {
		t.Fatalf("package group: %v", billing)
	}
}

func TestGroupsWorkspaceRepIsPathMin(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "internal/api/a.go", "package api\n")
	writeRepoFile(t, dir, "internal/billing/extra.go", "package billing\n")
	writeRepoFile(t, dir, "internal/other/x.go", "package other\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	// Tracked edits sort before the untracked charge.go that workspace mode appends.
	// charge.go is still the representative because it is the lesser path.
	writeRepoFile(t, dir, "internal/api/a.go", "package api\n\nimport \"example/internal/billing\"\n")
	writeRepoFile(t, dir, "internal/billing/extra.go", "package billing\n\nfunc Extra() {}\n")
	writeRepoFile(t, dir, "internal/other/x.go", "package other\n\nfunc X() {}\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n\nimport \"example/internal/other\"\n")

	result, err := Groups(context.Background(), dir, Spec{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"internal/api/a.go", "internal/billing/charge.go", "internal/other/x.go"},
		{"internal/billing/extra.go"},
	}
	if strings.Join(flatten(got), " | ") != strings.Join(flatten(want), " | ") {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
}

func TestGoImportEdgesCanceled(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeImportFixture(t, dir)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	ctx := context.Background()
	r, err := resolveSpec(ctx, dir, Spec{From: "HEAD~1", To: "HEAD"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := collectNumstat(ctx, dir, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	edges, err := goImportEdges(canceled, dir, r, files)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("edges=%v err=%v", edges, err)
	}
	_, err = Groups(canceled, dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Groups err=%v", err)
	}
}

func TestGroupsNestedModuleImport(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "sub/go.mod", "module other.example/sub\n")
	writeRepoFile(t, dir, "sub/api/a.go", "package api\n\nimport \"other.example/sub/billing\"\n")
	writeRepoFile(t, dir, "sub/billing/charge.go", "package billing\n\nfunc Charge() {}\n")
	writeRepoFile(t, dir, "cmd/main.go", "package main\n\nimport \"example/sub/billing\"\n")
	writeRepoFile(t, dir, "internal/api/b.go", "package api\n\nimport \"example/internal/pay\"\n")
	writeRepoFile(t, dir, "internal/pay/p.go", "package pay\n\nfunc P() {}\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	want := [][]string{
		{"cmd/main.go"},
		{"go.mod"},
		{"internal/api/b.go", "internal/pay/p.go"},
		{"sub/api/a.go", "sub/billing/charge.go"},
		{"sub/go.mod"},
	}
	if strings.Join(flatten(got), " | ") != strings.Join(flatten(want), " | ") {
		t.Fatalf("groups:\n got %v\nwant %v", got, want)
	}
}

func TestGroupsModuleDirectiveTab(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "go.mod", "module\texample\n")
	writeRepoFile(t, dir, "internal/api/billing.go", "package api\n\nimport \"example/internal/billing\"\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n\nfunc Charge() {}\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	if path, ok := parseModulePath("module\texample\n"); !ok || path != "example" {
		t.Fatalf("parseModulePath = %q %v", path, ok)
	}
	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(flatten(groupPaths(result.Groups)), " | ") != "go.mod | internal/api/billing.go,internal/billing/charge.go" {
		t.Fatalf("groups: %v", groupPaths(result.Groups))
	}
}

func TestGroupsIgnoresGoImportsWithoutModule(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "-m", "base", "--allow-empty")
	writeRepoFile(t, dir, "internal/api/billing.go", "package api\n\nimport \"example/internal/billing\"\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n")
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-q", "-m", "head")

	result, err := Groups(context.Background(), dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := groupPaths(result.Groups)
	if len(got) != 2 {
		t.Fatalf("groups: %v", got)
	}
	if strings.Join(got[0], ",") != "internal/api/billing.go" || strings.Join(got[1], ",") != "internal/billing/charge.go" {
		t.Fatalf("groups: %v", got)
	}
}

func writeImportFixture(t *testing.T, dir string) {
	t.Helper()
	writeRepoFile(t, dir, "go.mod", "module example\n")
	writeRepoFile(t, dir, "internal/api/billing.go", "package api\n\nimport \"example/internal/billing\"\n\nfunc Bill() { billing.Charge() }\n")
	writeRepoFile(t, dir, "internal/billing/charge.go", "package billing\n\nfunc Charge() {}\n")
	writeRepoFile(t, dir, "internal/other/x.go", "package other\n\nfunc X() {}\n")
}

func writeRepoFile(t *testing.T, dir, rel, body string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func flatten(groups [][]string) []string {
	var out []string
	for _, group := range groups {
		out = append(out, strings.Join(group, ","))
	}
	return out
}

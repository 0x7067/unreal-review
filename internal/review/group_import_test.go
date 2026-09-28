package review

import (
	"context"
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

	ctx := context.Background()
	r, err := resolveSpec(ctx, dir, Spec{From: "HEAD~1", To: "HEAD"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	files, err := collectNumstat(ctx, dir, r, nil)
	if err != nil {
		t.Fatal(err)
	}
	edges := goImportEdges(ctx, dir, r, files)
	if len(edges) != 2 {
		t.Fatalf("edges = %d (%+v), want one per importer", len(edges), edges)
	}
	result, err := Groups(ctx, dir, Spec{From: "HEAD~1", To: "HEAD"}, nil, nil)
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

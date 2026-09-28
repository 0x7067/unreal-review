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

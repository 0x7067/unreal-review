package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	diff, source, err := loadGitDiff(ctx, dir, "HEAD~1", "HEAD", nil, nil)
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

	excluded, excludedSource, err := loadGitDiff(ctx, dir, "HEAD~1", "HEAD", nil, []string{"large.txt"})
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

func gitRepo(t *testing.T) string {
	t.Helper()
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

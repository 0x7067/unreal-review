package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"strings"

	"unreal-review/internal/findings"
)

func loadGitDiff(ctx context.Context, workspace, from, to string, paths, exclude []string) (string, findings.Source, error) {
	if from == "" {
		detected, err := detectFrom(ctx, workspace)
		if err != nil {
			return "", findings.Source{}, err
		}
		from = detected
	}
	if _, err := gitRev(ctx, workspace, from); err != nil {
		return "", findings.Source{}, err
	}
	if to != "" {
		if _, err := gitRev(ctx, workspace, to); err != nil {
			return "", findings.Source{}, err
		}
	}

	scope := make([]string, 0, len(paths)+len(exclude)+1)
	scope = append(scope, paths...)
	if len(scope) == 0 && len(exclude) > 0 {
		scope = append(scope, ".")
	}
	for _, pattern := range exclude {
		scope = append(scope, excludeSpec(pattern))
	}

	diff, err := git(ctx, workspace, gitDiffArgs(from, to, scope)...)
	if err != nil {
		return "", findings.Source{}, fmt.Errorf("git diff: %w", err)
	}

	fromSHA, _ := gitRev(ctx, workspace, from)
	toSHA, _ := gitRev(ctx, workspace, headRev(to))
	source := findings.Source{
		Kind:    "git",
		Base:    from,
		Head:    to,
		BaseSHA: fromSHA,
		HeadSHA: toSHA,
		DiffSHA: diffFingerprint(diff),
	}
	return diff, source, nil
}

func excludeSpec(pattern string) string {
	if strings.HasPrefix(pattern, ":(") {
		return pattern
	}
	if strings.ContainsAny(pattern, "*?[]") {
		return ":(exclude,glob)" + pattern
	}
	return ":(exclude)" + pattern
}

func gitDiffArgs(from, to string, pathspecs []string) []string {
	args := []string{"diff", "--no-color", "--no-ext-diff", "--merge-base", from}
	if to != "" {
		args = append(args, to)
	}
	args = append(args, "--")
	return append(args, pathspecs...)
}

func detectFrom(ctx context.Context, workspace string) (string, error) {
	for _, name := range []string{"main", "master", "origin/main", "origin/master"} {
		if _, err := gitRev(ctx, workspace, name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("set --from; could not find main or master")
}

func gitRev(ctx context.Context, workspace, rev string) (string, error) {
	out, err := git(ctx, workspace, "rev-parse", rev)
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %w", rev, err)
	}
	return strings.TrimSpace(out), nil
}

func git(ctx context.Context, workspace string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return "", err
		}
		return "", fmt.Errorf("%s: %w", msg, err)
	}
	return string(out), nil
}

func headRev(to string) string {
	if to == "" {
		return "HEAD"
	}
	return to
}

func diffFingerprint(diff string) string {
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:])
}

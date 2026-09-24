package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"unreal-review/internal/findings"
)

type Spec struct {
	From   string
	To     string
	Commit string
	Branch string
}

type specMode int

const (
	specWorkspace specMode = iota
	specRange
	specCommit
	specBranch
)

func (s Spec) mode() (specMode, error) {
	rangeSet := s.From != "" || s.To != ""
	n := 0
	if rangeSet {
		n++
	}
	if s.Commit != "" {
		n++
	}
	if s.Branch != "" {
		n++
	}
	if n > 1 {
		return 0, fmt.Errorf("use only one of --from/--to, --commit, or --branch")
	}
	if s.Commit != "" {
		return specCommit, nil
	}
	if s.Branch != "" {
		return specBranch, nil
	}
	if s.To != "" && s.From == "" {
		return 0, fmt.Errorf("set --from or use --branch")
	}
	if s.From != "" {
		return specRange, nil
	}
	return specWorkspace, nil
}

func (s Spec) FlagArgs() string {
	mode, err := s.mode()
	if err != nil {
		return ""
	}
	switch mode {
	case specCommit:
		return " --commit " + s.Commit
	case specBranch:
		return " --branch " + s.Branch
	case specRange:
		var b strings.Builder
		if s.From != "" {
			fmt.Fprintf(&b, " --from %s", s.From)
		}
		if s.To != "" {
			fmt.Fprintf(&b, " --to %s", s.To)
		}
		return b.String()
	default:
		return ""
	}
}

type resolved struct {
	base, head       string
	baseSHA, headSHA string
	mergeBase        bool
	untracked        bool
}

func loadGitDiff(ctx context.Context, workspace string, spec Spec, paths, exclude []string) (string, findings.Source, error) {
	r, err := resolveSpec(ctx, workspace, spec)
	if err != nil {
		return "", findings.Source{}, err
	}
	scope := pathspecScope(paths, exclude)
	diff, err := collectDiff(ctx, workspace, r, scope)
	if err != nil {
		return "", findings.Source{}, err
	}
	source := findings.Source{
		Kind:    "git",
		Base:    r.base,
		Head:    r.head,
		BaseSHA: r.baseSHA,
		HeadSHA: r.headSHA,
		DiffSHA: diffFingerprint(diff),
	}
	return diff, source, nil
}

func resolveSpec(ctx context.Context, workspace string, spec Spec) (resolved, error) {
	mode, err := spec.mode()
	if err != nil {
		return resolved{}, err
	}
	switch mode {
	case specWorkspace:
		sha, err := gitRev(ctx, workspace, "HEAD")
		if err != nil {
			return resolved{}, err
		}
		return resolved{base: "HEAD", head: "", baseSHA: sha, headSHA: sha, untracked: true}, nil
	case specRange:
		return resolveRange(ctx, workspace, spec.From, spec.To)
	case specBranch:
		from, err := detectFrom(ctx, workspace)
		if err != nil {
			return resolved{}, err
		}
		return resolveRange(ctx, workspace, from, spec.Branch)
	case specCommit:
		return resolveCommit(ctx, workspace, spec.Commit)
	default:
		return resolved{}, fmt.Errorf("unknown git range")
	}
}

func resolveRange(ctx context.Context, workspace, from, to string) (resolved, error) {
	fromSHA, err := gitRev(ctx, workspace, from)
	if err != nil {
		return resolved{}, err
	}
	toSHA, err := gitRev(ctx, workspace, headRev(to))
	if err != nil {
		return resolved{}, err
	}
	return resolved{base: from, head: to, baseSHA: fromSHA, headSHA: toSHA, mergeBase: true}, nil
}

func resolveCommit(ctx context.Context, workspace, commit string) (resolved, error) {
	headSHA, err := gitRev(ctx, workspace, commit)
	if err != nil {
		return resolved{}, err
	}
	line, err := git(ctx, workspace, "rev-list", "--parents", "-n", "1", commit)
	if err != nil {
		return resolved{}, err
	}
	fields := strings.Fields(strings.TrimSpace(line))
	if len(fields) < 2 {
		return resolved{base: "", head: commit, baseSHA: "", headSHA: headSHA}, nil
	}
	return resolved{base: commit + "^", head: commit, baseSHA: fields[1], headSHA: headSHA}, nil
}

func collectDiff(ctx context.Context, workspace string, r resolved, pathspecs []string) (string, error) {
	diff, err := git(ctx, workspace, gitDiffArgs(r, pathspecs)...)
	if err != nil {
		return "", fmt.Errorf("git diff: %w", err)
	}
	if !r.untracked {
		return diff, nil
	}
	extra, err := untrackedDiff(ctx, workspace, pathspecs)
	if err != nil {
		return "", err
	}
	return diff + extra, nil
}

func collectNumstat(ctx context.Context, workspace string, r resolved, pathspecs []string) ([]ChangedFile, error) {
	raw, err := git(ctx, workspace, gitNumstatArgs(r, pathspecs)...)
	if err != nil {
		return nil, fmt.Errorf("git diff: %w", err)
	}
	files, err := parseNumstat(raw)
	if err != nil {
		return nil, err
	}
	if !r.untracked {
		return files, nil
	}
	extra, err := untrackedNumstat(ctx, workspace, pathspecs)
	if err != nil {
		return nil, err
	}
	return append(files, extra...), nil
}

func untrackedDiff(ctx context.Context, workspace string, pathspecs []string) (string, error) {
	paths, err := untrackedPaths(ctx, workspace, pathspecs)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, path := range paths {
		patch, err := gitDiffNoIndex(ctx, workspace, path, "--no-color", "--no-ext-diff")
		if err != nil {
			return "", err
		}
		b.WriteString(patch)
		if patch != "" && !strings.HasSuffix(patch, "\n") {
			b.WriteByte('\n')
		}
	}
	return b.String(), nil
}

func untrackedNumstat(ctx context.Context, workspace string, pathspecs []string) ([]ChangedFile, error) {
	paths, err := untrackedPaths(ctx, workspace, pathspecs)
	if err != nil {
		return nil, err
	}
	var files []ChangedFile
	for _, path := range paths {
		raw, err := gitDiffNoIndex(ctx, workspace, path, "--numstat", "--no-color", "--no-ext-diff")
		if err != nil {
			return nil, err
		}
		parsed, err := parseNumstat(raw)
		if err != nil {
			return nil, err
		}
		files = append(files, parsed...)
	}
	return files, nil
}

func untrackedPaths(ctx context.Context, workspace string, pathspecs []string) ([]string, error) {
	args := append([]string{"ls-files", "-z", "--others", "--exclude-standard", "--"}, pathspecs...)
	raw, err := git(ctx, workspace, args...)
	if err != nil {
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	var paths []string
	for _, path := range strings.Split(raw, "\x00") {
		if path == "" {
			continue
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func gitDiffNoIndex(ctx context.Context, workspace, path string, flags ...string) (string, error) {
	args := append([]string{"diff"}, flags...)
	args = append(args, "--no-index", "--", os.DevNull, path)
	return gitAllowExit1(ctx, workspace, args...)
}

func pathspecScope(paths, exclude []string) []string {
	scope := make([]string, 0, len(paths)+len(exclude)+1)
	scope = append(scope, paths...)
	if len(scope) == 0 && len(exclude) > 0 {
		scope = append(scope, ".")
	}
	for _, pattern := range exclude {
		scope = append(scope, excludeSpec(pattern))
	}
	return scope
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

func gitDiffArgs(r resolved, pathspecs []string) []string {
	return gitDiffFlags(r, pathspecs, "--no-color", "--no-ext-diff")
}

func gitNumstatArgs(r resolved, pathspecs []string) []string {
	return gitDiffFlags(r, pathspecs, "--numstat", "--no-color", "--no-ext-diff")
}

func gitDiffFlags(r resolved, pathspecs []string, flags ...string) []string {
	args := append([]string{"diff"}, flags...)
	switch {
	case r.mergeBase:
		args = append(args, "--merge-base", r.base)
		if r.head != "" {
			args = append(args, r.head)
		}
	case r.base == "":
		args = append(args, "--root", r.head)
	default:
		args = append(args, r.base)
		if r.head != "" {
			args = append(args, r.head)
		}
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
	return "", fmt.Errorf("could not find main or master; set --from")
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

func gitAllowExit1(ctx context.Context, workspace string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return string(out), nil
		}
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

package review

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"unreal-review/internal/findings"
)

func newIdentifier(ctx context.Context, workspace string, r resolved, prior []findings.Finding) func(findings.Finding) (findings.Finding, error) {
	var (
		mu         sync.Mutex
		seen       = make(map[string]bool, len(prior))
		oldRevOnce sync.Once
		oldRev     string
		oldRevErr  error
	)
	for _, f := range prior {
		seen[f.ID] = true
	}

	resolveOldRev := func() (string, error) {
		oldRevOnce.Do(func() {
			switch {
			case r.mergeBase:
				out, err := git(ctx, workspace, "merge-base", r.base, headRev(r.head))
				if err != nil {
					oldRevErr = fmt.Errorf("resolve old side: %w", err)
					return
				}
				oldRev = strings.TrimSpace(out)
			case r.base == "":
				oldRevErr = fmt.Errorf("this range has no old side (it reviews a root commit)")
			default:
				oldRev = r.base
			}
		})
		return oldRev, oldRevErr
	}

	return func(finding findings.Finding) (findings.Finding, error) {
		var (
			located flaggedCode
			err     error
		)
		if finding.Anchor == findings.AnchorOld {
			rev, revErr := resolveOldRev()
			if revErr != nil {
				return findings.Finding{}, revErr
			}
			located, err = fileLines(ctx, workspace, rev, finding.Path, finding.StartLine, finding.EndLine)
		} else if r.head == "" {
			located, err = fileLines(ctx, workspace, "", finding.Path, finding.StartLine, finding.EndLine)
		} else {
			located, err = fileLines(ctx, workspace, r.headSHA, finding.Path, finding.StartLine, finding.EndLine)
		}
		if err != nil {
			return findings.Finding{}, err
		}

		id := findings.Fingerprint(finding, located.lines, located.occurrence)
		mu.Lock()
		defer mu.Unlock()
		if seen[id] {
			return findings.Finding{}, fmt.Errorf("these lines already have a finding (%s); extend that one instead of recording another", id)
		}
		seen[id] = true
		finding.ID = id
		return finding, nil
	}
}

func newResolver(open []findings.Finding, prior []findings.Resolution) func(findings.Resolution) (findings.Resolution, error) {
	var mu sync.Mutex
	openIDs := make(map[string]bool, len(open))
	for _, f := range open {
		openIDs[f.ID] = true
	}
	resolved := make(map[string]bool, len(prior))
	for _, r := range prior {
		resolved[r.ID] = true
	}
	return func(resolution findings.Resolution) (findings.Resolution, error) {
		mu.Lock()
		defer mu.Unlock()
		if !openIDs[resolution.ID] {
			return findings.Resolution{}, fmt.Errorf("%s is not an open finding on this pull request", resolution.ID)
		}
		if resolved[resolution.ID] {
			return findings.Resolution{}, fmt.Errorf("%s was already resolved", resolution.ID)
		}
		resolved[resolution.ID] = true
		return resolution, nil
	}
}

type flaggedCode struct {
	lines      []string
	occurrence int
}

func fileLines(ctx context.Context, workspace, rev, path string, start, end int) (flaggedCode, error) {
	var (
		body  []byte
		label string
	)
	if rev == "" {
		label = "the working tree"
		content, err := os.ReadFile(filepath.Join(workspace, path))
		if err != nil {
			return flaggedCode{}, fmt.Errorf("%s is missing at %s", path, label)
		}
		body = content
	} else {
		label = rev
		out, err := git(ctx, workspace, "show", rev+":"+path)
		if err != nil {
			return flaggedCode{}, fmt.Errorf("%s is missing at %s", path, label)
		}
		body = []byte(out)
	}
	lines := splitLines(string(body))
	if start < 1 || end > len(lines) {
		return flaggedCode{}, fmt.Errorf("%s lines %d-%d are past the end of the file (%d lines) at %s", path, start, end, len(lines), label)
	}
	flagged := lines[start-1 : end]
	return flaggedCode{lines: flagged, occurrence: occurrencesBefore(lines, flagged, start-1)}, nil
}

func occurrencesBefore(lines, block []string, at int) int {
	want := strings.Join(findings.TrimLines(block), "\n")
	count := 0
	for i := 0; i < at; i++ {
		if strings.Join(findings.TrimLines(lines[i:i+len(block)]), "\n") == want {
			count++
		}
	}
	return count
}

func splitLines(body string) []string {
	if body == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

type Gold struct {
	Path      string
	StartLine int
	EndLine   int
	Severity  findings.Severity
}

type Case struct {
	Name   string
	Class  string
	Base   map[string]string
	Change map[string]string
	Gold   []Gold
}

type Score struct {
	Name         string  `json:"name"`
	Class        string  `json:"class"`
	Status       string  `json:"status"`
	Gold         int     `json:"gold"`
	Matched      int     `json:"matched"`
	SeverityHits int     `json:"severity_hits"`
	Produced     int     `json:"produced"`
	Extra        int     `json:"extra"`
	CostUSD      float64 `json:"cost_usd"`
	Requests     int     `json:"requests"`
	DurationMS   int64   `json:"duration_ms"`
	Err          string  `json:"err,omitempty"`
	FindingsPath string  `json:"findings_path"`
}

func (s Score) Recall() float64 {
	return Agreement(s.Matched, s.Gold)
}

func (s Score) Precision() float64 {
	if s.Produced == 0 {
		return 1
	}
	return float64(s.Produced-s.Extra) / float64(s.Produced)
}

func (s Score) SeverityAgreement() float64 {
	return Agreement(s.SeverityHits, s.Gold)
}

func (s Score) Completed() bool {
	return s.Status == string(findings.StatusComplete)
}

func Agreement(hits, total int) float64 {
	if total == 0 {
		return 1
	}
	return float64(hits) / float64(total)
}

func scoreReport(c Case, report findings.Report) Score {
	score := Score{Name: c.Name, Class: c.Class, Gold: len(c.Gold), Produced: len(report.Findings)}
	if report.Run != nil {
		score.Status = string(report.Run.Status)
		score.CostUSD = report.Run.Cost.AmountUSD
		score.Requests = report.Run.Cost.Requests
	}
	score.Matched, score.SeverityHits, score.Extra = match(c.Gold, report.Findings)
	return score
}

func match(gold []Gold, produced []findings.Finding) (matched, severityHits, extra int) {
	consumed := make([]bool, len(gold))
	for _, f := range produced {
		hit := -1
		for i, g := range gold {
			if !consumed[i] && sameRegion(f, g) {
				hit = i
				break
			}
		}
		if hit < 0 {
			extra++
			continue
		}
		consumed[hit] = true
		matched++
		if f.Severity == gold[hit].Severity {
			severityHits++
		}
	}
	return matched, severityHits, extra
}

type Options struct {
	Model     string
	Agent     review.Agent
	Decompose bool
}

func Run(ctx context.Context, c Case, root string, opts Options) (Score, error) {
	dir := filepath.Join(root, c.Name)
	if err := setup(ctx, c, dir); err != nil {
		return Score{Name: c.Name}, fmt.Errorf("set up %s: %w", c.Name, err)
	}
	findingsPath := filepath.Join(dir, "findings.jsonl")
	start := time.Now()
	result, runErr := review.Run(ctx, review.Options{
		Workspace: dir,
		Out:       findingsPath,
		Decompose: opts.Decompose,
		Model:     opts.Model,
		Agent:     opts.Agent,
	})
	score := scoreReport(c, result.Report)
	score.DurationMS = time.Since(start).Milliseconds()
	score.FindingsPath = findingsPath
	if runErr != nil {
		score.Err = runErr.Error()
	}
	return score, nil
}

func setup(ctx context.Context, c Case, dir string) error {
	git := func(args ...string) error {
		// Planted repositories are synthetic fixtures, not user workspaces. Do
		// not invoke global hooks, signing, maintenance, or filesystem monitors.
		// An inherited GIT_DIR, GIT_WORK_TREE, or GIT_INDEX_FILE would stage
		// and commit this fixture in another repository.
		args = append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgSign=false", "-c", "maintenance.auto=false", "-c", "gc.auto=0", "-c", "core.fsmonitor=false"}, args...)
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		env := os.Environ()
		filtered := make([]string, 0, len(env))
		for _, entry := range env {
			if strings.HasPrefix(entry, "GIT_DIR=") || strings.HasPrefix(entry, "GIT_WORK_TREE=") || strings.HasPrefix(entry, "GIT_INDEX_FILE=") {
				continue
			}
			filtered = append(filtered, entry)
		}
		cmd.Env = filtered
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
		}
		return nil
	}
	exclude := filepath.Join(dir, ".git", "info", "exclude")
	initialized := func() bool {
		_, err := os.Stat(filepath.Join(dir, ".git", "HEAD"))
		return err == nil
	}
	if err := writeFiles(dir, c.Base); err != nil {
		return err
	}
	if initialized() {
		if err := appendFindingsExclude(exclude); err != nil {
			return err
		}
		if err := git("reset", "-q", "--hard", "HEAD"); err != nil {
			return err
		}
		return writeFiles(dir, c.Change)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := git("init", "--template=", "-q"); err != nil {
		return err
	}
	if err := appendFindingsExclude(exclude); err != nil {
		return err
	}
	commit := func() error {
		return git("-c", "user.name=eval", "-c", "user.email=eval@invalid", "commit", "-q", "-m", "base")
	}
	if err := git("add", "-A"); err != nil {
		return err
	}
	if err := commit(); err != nil {
		return err
	}
	return writeFiles(dir, c.Change)
}

func appendFindingsExclude(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	present := map[string]bool{}
	for _, line := range strings.Split(string(body), "\n") {
		present[line] = true
	}
	var missing []string
	for _, pattern := range []string{"/findings.jsonl", "/findings.jsonl.work"} {
		if !present[pattern] {
			missing = append(missing, pattern)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := file.WriteString("\n" + strings.Join(missing, "\n") + "\n")
	return errors.Join(writeErr, file.Close())
}

func writeFiles(dir string, files map[string]string) error {
	for path, body := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func sameRegion(f findings.Finding, g Gold) bool {
	if f.Path != g.Path {
		return false
	}
	return f.StartLine <= g.EndLine && g.StartLine <= f.EndLine
}

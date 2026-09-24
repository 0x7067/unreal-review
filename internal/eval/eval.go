package eval

import (
	"context"
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
	Base   map[string]string
	Change map[string]string
	Gold   []Gold
}

type Score struct {
	Name         string  `json:"name"`
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

func ScoreReport(c Case, report findings.Report) Score {
	score := Score{Name: c.Name, Gold: len(c.Gold), Produced: len(report.Findings)}
	if report.Run != nil {
		score.Status = string(report.Run.Status)
		score.CostUSD = report.Run.Cost.AmountUSD
		score.Requests = report.Run.Cost.Requests
	}
	score.Matched, score.SeverityHits, score.Extra = Match(c.Gold, report.Findings)
	return score
}

func Match(gold []Gold, produced []findings.Finding) (matched, severityHits, extra int) {
	for _, g := range gold {
		if !anyFinding(produced, func(f findings.Finding) bool { return sameRegion(f, g) }) {
			continue
		}
		matched++
		if anyFinding(produced, func(f findings.Finding) bool {
			return sameRegion(f, g) && f.Severity == g.Severity
		}) {
			severityHits++
		}
	}
	for _, f := range produced {
		if !anyGold(gold, func(g Gold) bool { return sameRegion(f, g) }) {
			extra++
		}
	}
	return matched, severityHits, extra
}

type Options struct {
	Model string
	Agent review.Agent
}

func Run(ctx context.Context, c Case, root string, opts Options) (Score, error) {
	dir := filepath.Join(root, c.Name)
	if err := Setup(ctx, c, dir); err != nil {
		return Score{Name: c.Name}, fmt.Errorf("set up %s: %w", c.Name, err)
	}
	findingsPath := filepath.Join(dir, "findings.jsonl")
	start := time.Now()
	result, runErr := review.Run(ctx, review.Options{
		Workspace: dir,
		Out:       findingsPath,
		Fresh:     true,
		Model:     opts.Model,
		Agent:     opts.Agent,
	})
	score := ScoreReport(c, result.Report)
	score.DurationMS = time.Since(start).Milliseconds()
	score.FindingsPath = findingsPath
	if runErr != nil {
		score.Err = runErr.Error()
	}
	return score, nil
}

func Setup(ctx context.Context, c Case, dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	git := func(args ...string) error {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(out)))
		}
		return nil
	}
	if err := writeFiles(dir, c.Base); err != nil {
		return err
	}
	if err := git("init", "-q"); err != nil {
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

func anyFinding(fs []findings.Finding, pred func(findings.Finding) bool) bool {
	for _, f := range fs {
		if pred(f) {
			return true
		}
	}
	return false
}

func anyGold(gs []Gold, pred func(Gold) bool) bool {
	for _, g := range gs {
		if pred(g) {
			return true
		}
	}
	return false
}

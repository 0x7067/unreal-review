package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"unreal-review/internal/findings"
)

const (
	maxBriefDiff     = 200_000
	systemPromptBase = `You review a git unified diff. The process working directory is the repository root. Open files when you need surrounding context. Do not edit files. Do not call git hosting APIs. Do not post comments.

Prefer lines that appear in the diff. One finding per issue. Record every finding with the record tool below, then end with exactly one summary record; if nothing material, record only the summary.

Severity: use "error" when the code does the wrong thing - a crash, hang, race, or corruption, a security compromise, a reported failure the caller can no longer classify so their error handling takes the wrong branch, or a transient fault made permanent with no recovery path. Use "warning" when the code works but weakly - diagnostics silently dropped while behavior stays correct, resources that leak toward exhaustion under sustained load, or capability lost for some inputs while the rest keeps working. Use "note" for anything smaller.`
)

func agentSystemPrompt() string {
	entry, err := os.Executable()
	if err != nil {
		entry = os.Args[0]
	}
	return systemPromptBase + "\n\n" + promptSection(shellQuote(entry), DefaultTools())
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

type Options struct {
	Workspace string
	Spec      Spec
	Paths     []string
	Exclude   []string
	Out       string
	Fresh     bool
	Model     string
	Agent     Agent
	Pull      PullResolver
}

type Result struct {
	Report findings.Report
	Diff   string
}

func Run(ctx context.Context, opts Options) (Result, error) {
	workspace, err := filepath.Abs(opts.Workspace)
	if err != nil {
		return Result{}, fmt.Errorf("workspace: %w", err)
	}
	selected, err := loadGitDiff(ctx, workspace, opts.Spec, opts.Paths, opts.Exclude, opts.Pull)
	if err != nil {
		return Result{}, err
	}
	diff, source := selected.diff, selected.source
	runMeta := newRun(opts.Model, source)
	if strings.TrimSpace(diff) == "" {
		runMeta.Status = findings.StatusComplete
		result := Result{Report: findings.Report{
			Run:     runMeta,
			Summary: "No changes to review.",
		}, Diff: diff}
		if err := persist(opts.Out, result.Report); err != nil {
			return result, err
		}
		return result, nil
	}

	checkpoint, err := loadCheckpoint(opts.Out)
	if err != nil {
		return Result{Diff: diff}, err
	}
	resuming := false
	if !opts.Fresh && checkpoint.Run != nil {
		if checkpoint.Complete() {
			return Result{Report: checkpoint, Diff: diff}, fmt.Errorf(
				"%s is a complete review of %s; pass --fresh to start over",
				opts.Out, formatSource(checkpoint.Run.Source),
			)
		}
		if !checkpoint.Run.Source.SameDiff(source) {
			return Result{Report: checkpoint, Diff: diff}, fmt.Errorf(
				"%s is a review of %s; workspace is %s",
				opts.Out, formatSource(checkpoint.Run.Source), formatSource(source),
			)
		}
		resuming = true
		runMeta.ID = checkpoint.Run.ID
		runMeta.CreatedAt = checkpoint.Run.CreatedAt
		runMeta.Cost = checkpoint.Run.Cost
		if runMeta.Cost.Currency == "" {
			runMeta.Cost.Currency = "USD"
		}
	}

	if opts.Agent == nil {
		return Result{Diff: diff}, fmt.Errorf("agent is required")
	}

	findingsPath, cleanup, err := prepareWork(opts.Out, !resuming)
	if err != nil {
		return Result{Diff: diff}, err
	}
	defer cleanup()

	prior, err := readWork(findingsPath)
	if err != nil {
		return Result{Diff: diff}, err
	}
	if resuming && len(prior.Findings) == 0 && strings.TrimSpace(prior.Summary) == "" {
		prior.Findings = checkpoint.Findings
		prior.Summary = checkpoint.Summary
		if err := writeWork(findingsPath, prior); err != nil {
			return Result{Diff: diff}, err
		}
	}
	runMeta.Status = findings.StatusRunning
	report := findings.Report{Run: runMeta, Findings: prior.Findings, Summary: prior.Summary}
	result := Result{Report: report, Diff: diff}
	if err := persist(opts.Out, report); err != nil {
		return result, err
	}

	agentResult, agentErr := opts.Agent.Run(ctx, AgentRequest{
		Workspace:    workspace,
		ReviewID:     runMeta.ID,
		FindingsPath: findingsPath,
		Prompt:       reviewPrompt(source.Base, source.Head, findingsPath, diff, reportedIn(selected.reported, selected.files)),
		SystemPrompt: agentSystemPrompt(),
		Model:        opts.Model,
	})
	interrupted := agentErr != nil && (errors.Is(agentErr, context.Canceled) || errors.Is(agentErr, context.DeadlineExceeded) || ctx.Err() != nil)
	runMeta.Cost = runMeta.Cost.Add(agentResult.Cost)

	work, err := readWork(findingsPath)
	if err != nil {
		return result, err
	}
	report.Findings = mergeFindings(work.Findings)
	report.Summary = work.Summary
	result.Report = report

	switch {
	case agentErr == nil && strings.TrimSpace(report.Summary) != "" && runMeta.Cost.Recorded():
		runMeta.Status = findings.StatusComplete
		report.Run = runMeta
		result.Report = report
		if err := persist(opts.Out, report); err != nil {
			return result, err
		}
		if fileOut(opts.Out) {
			_ = os.Remove(findingsPath)
		}
		return result, nil
	case interrupted:
		runMeta.Status = findings.StatusRunning
		report.Run = runMeta
		result.Report = report
		if err := persist(opts.Out, report); err != nil {
			return result, err
		}
		if fileOut(opts.Out) {
			return result, fmt.Errorf("review paused; resume with the same --out %s", opts.Out)
		}
		if agentErr != nil {
			return result, agentErr
		}
		return result, fmt.Errorf("review paused")
	default:
		runMeta.Status = findings.StatusFailed
		report.Run = runMeta
		result.Report = report
		if err := persist(opts.Out, report); err != nil {
			return result, err
		}
		if agentErr != nil {
			return result, agentErr
		}
		if strings.TrimSpace(report.Summary) == "" {
			return result, fmt.Errorf("review did not write a summary")
		}
		return result, fmt.Errorf("review did not record cost")
	}
}

func reviewPrompt(from, to, findingsPath, diff string, reported []findings.Finding) string {
	target := to
	if target == "" {
		target = "working tree"
	}
	body := diff
	if len(body) > maxBriefDiff {
		body = body[:maxBriefDiff] + "\n\n[diff truncated]\n"
	}
	return fmt.Sprintf(
		"Write findings JSONL to %s\n\nFrom: %s\nTo: %s\n\n%s```diff\n%s\n```\n",
		findingsPath,
		from,
		target,
		reportedSection(reported),
		body,
	)
}

func reportedIn(reported []findings.Finding, files []ChangedFile) []findings.Finding {
	if len(reported) == 0 {
		return nil
	}
	touched := make(map[string]struct{}, len(files))
	for _, file := range files {
		touched[file.Path] = struct{}{}
	}
	out := make([]findings.Finding, 0, len(reported))
	for _, item := range reported {
		if _, ok := touched[item.Path]; ok {
			out = append(out, item)
		}
	}
	return out
}

func reportedSection(reported []findings.Finding) string {
	if len(reported) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Already reported on this pull request:\n")
	for _, item := range reported {
		parts := []string{"`" + item.Path + "`"}
		if item.StartLine > 0 {
			parts = append(parts, fmt.Sprintf("%d-%d", item.StartLine, item.EndLine))
		}
		if item.Severity != "" {
			parts = append(parts, string(item.Severity))
		}
		fmt.Fprintf(&b, "- %s: %s\n", strings.Join(parts, " "), strings.Join(strings.Fields(item.Body), " "))
	}
	b.WriteString("\nReport a problem this list does not cover, or a material change in one it does. Do not restate it.\n\n")
	return b.String()
}

func newRun(model string, source findings.Source) *findings.Run {
	return &findings.Run{
		ID:        uuid.New().String(),
		CreatedAt: time.Now().UTC(),
		Model:     model,
		Status:    findings.StatusRunning,
		Source:    source,
		Cost:      findings.Cost{Currency: "USD"},
	}
}

func formatSource(source findings.Source) string {
	return fmt.Sprintf("from %s to %s diff %s", shortSHA(source.BaseSHA), shortSHA(source.HeadSHA), shortSHA(source.DiffSHA))
}

func shortSHA(sha string) string {
	if sha == "" {
		return "-"
	}
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func fileOut(path string) bool {
	return path != "" && path != "-"
}

func loadCheckpoint(path string) (findings.Report, error) {
	if !fileOut(path) {
		return findings.Report{}, nil
	}
	report, err := findings.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return findings.Report{}, nil
	}
	return report, err
}

func prepareWork(out string, truncate bool) (string, func(), error) {
	if fileOut(out) {
		path := out + ".work"
		if err := openWork(path, truncate); err != nil {
			return "", func() {}, err
		}
		return path, func() {}, nil
	}
	file, err := os.CreateTemp("", "unreal-review-findings-*.jsonl")
	if err != nil {
		return "", func() {}, fmt.Errorf("create findings file: %w", err)
	}
	path := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", func() {}, fmt.Errorf("close findings file: %w", err)
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func openWork(path string, truncate bool) error {
	flags := os.O_RDWR | os.O_CREATE
	if truncate {
		flags |= os.O_TRUNC
	}
	file, err := os.OpenFile(path, flags, 0o644)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	return nil
}

func writeWork(path string, report findings.Report) error {
	return findings.WriteFile(path, findings.Report{Findings: report.Findings, Summary: report.Summary})
}

func readWork(path string) (findings.Report, error) {
	report, err := findings.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return findings.Report{}, nil
	}
	return report, err
}

func persist(path string, report findings.Report) error {
	if !fileOut(path) {
		return nil
	}
	return findings.WriteFile(path, report)
}

func mergeFindings(items []findings.Finding) []findings.Finding {
	seen := make(map[string]int, len(items))
	out := make([]findings.Finding, 0, len(items))
	for _, item := range items {
		if item.ID == "" {
			out = append(out, item)
			continue
		}
		if i, ok := seen[item.ID]; ok {
			out[i] = item
			continue
		}
		seen[item.ID] = len(out)
		out = append(out, item)
	}
	return out
}

package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"unreal-review/internal/findings"
)

const (
	maxBriefDiff = 200_000
	systemPrompt = `You review a git unified diff. The process working directory is the repository root. Open files when you need surrounding context. Do not edit files. Do not call git hosting APIs. Do not post comments.

Write findings as JSONL to the path given in the user message. Each line is one JSON object. Schema version v is 1. Every finding must include severity.

A finding:
{"v":1,"type":"finding","path":"src/foo.go","start_line":12,"end_line":14,"anchor":"new","severity":"warning","body":"This map write races with the reader on line 40."}

path is repository-relative. start_line and end_line are inclusive 1-based. anchor is new (post-change file) or old (deleted lines). severity is error, warning, or note. body is markdown.

A summary:
{"v":1,"type":"summary","body":"Two races in the cache; the rest looks sound."}

Prefer lines that appear in the diff. One finding per issue. If nothing material, write only a summary.`
)

type Options struct {
	Workspace string
	Base      string
	Head      string
	Out       string
	Fresh     bool
	Model     string
	Agent     Agent
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
	base := opts.Base
	if base == "" {
		detected, err := detectBase(ctx, workspace)
		if err != nil {
			return Result{}, err
		}
		base = detected
	}
	diff, err := gitDiff(ctx, workspace, base, opts.Head)
	if err != nil {
		return Result{}, err
	}
	baseSHA, _ := gitRev(ctx, workspace, base)
	headSHA, _ := gitRev(ctx, workspace, headRev(opts.Head))
	source := findings.Source{
		Kind:    "git",
		Base:    base,
		Head:    opts.Head,
		BaseSHA: baseSHA,
		HeadSHA: headSHA,
		DiffSHA: diffFingerprint(diff),
	}
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
		return Result{}, err
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
		return Result{}, fmt.Errorf("agent is required")
	}

	findingsPath, cleanup, err := prepareWork(opts.Out, !resuming)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	prior, err := readWork(findingsPath)
	if err != nil {
		return Result{}, err
	}
	if resuming && len(prior.Findings) == 0 && strings.TrimSpace(prior.Summary) == "" {
		prior.Findings = checkpoint.Findings
		prior.Summary = checkpoint.Summary
		if err := writeWork(findingsPath, prior); err != nil {
			return Result{}, err
		}
	}
	runMeta.Status = findings.StatusRunning
	report := findings.Report{Run: runMeta, Findings: prior.Findings, Summary: prior.Summary}
	if err := persist(opts.Out, report); err != nil {
		return Result{Report: report, Diff: diff}, err
	}

	agentResult, agentErr := opts.Agent.Run(ctx, AgentRequest{
		Workspace:    workspace,
		ReviewID:     runMeta.ID,
		FindingsPath: findingsPath,
		Prompt:       reviewPrompt(base, opts.Head, findingsPath, diff),
		SystemPrompt: systemPrompt,
		Model:        opts.Model,
	})
	interrupted := agentErr != nil && (errors.Is(agentErr, context.Canceled) || errors.Is(agentErr, context.DeadlineExceeded) || ctx.Err() != nil)
	runMeta.Cost = runMeta.Cost.Add(agentResult.Cost)

	work, err := readWork(findingsPath)
	if err != nil {
		return Result{Report: report, Diff: diff}, err
	}
	report.Findings = mergeFindings(work.Findings)
	report.Summary = work.Summary

	switch {
	case agentErr == nil && strings.TrimSpace(report.Summary) != "" && runMeta.Cost.Recorded():
		runMeta.Status = findings.StatusComplete
		report.Run = runMeta
		if err := persist(opts.Out, report); err != nil {
			return Result{Report: report, Diff: diff}, err
		}
		if fileOut(opts.Out) {
			_ = os.Remove(findingsPath)
		}
		return Result{Report: report, Diff: diff}, nil
	case interrupted:
		runMeta.Status = findings.StatusRunning
		report.Run = runMeta
		if err := persist(opts.Out, report); err != nil {
			return Result{Report: report, Diff: diff}, err
		}
		if fileOut(opts.Out) {
			return Result{Report: report, Diff: diff}, fmt.Errorf("review paused; resume with the same --out %s", opts.Out)
		}
		if agentErr != nil {
			return Result{Report: report, Diff: diff}, agentErr
		}
		return Result{Report: report, Diff: diff}, fmt.Errorf("review paused")
	default:
		runMeta.Status = findings.StatusFailed
		report.Run = runMeta
		if err := persist(opts.Out, report); err != nil {
			return Result{Report: report, Diff: diff}, err
		}
		if agentErr != nil {
			return Result{Report: report, Diff: diff}, agentErr
		}
		if strings.TrimSpace(report.Summary) == "" {
			return Result{Report: report, Diff: diff}, fmt.Errorf("review did not write a summary")
		}
		return Result{Report: report, Diff: diff}, fmt.Errorf("review did not record cost")
	}
}

func reviewPrompt(base, head, findingsPath, diff string) string {
	target := head
	if target == "" {
		target = "working tree"
	}
	body := diff
	if len(body) > maxBriefDiff {
		body = body[:maxBriefDiff] + "\n\n[diff truncated]\n"
	}
	return fmt.Sprintf(
		"Write findings JSONL to %s\n\nBase: %s\nHead: %s\n\n```diff\n%s\n```\n",
		findingsPath,
		base,
		target,
		body,
	)
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

func detectBase(ctx context.Context, workspace string) (string, error) {
	for _, name := range []string{"main", "master", "origin/main", "origin/master"} {
		if _, err := gitRev(ctx, workspace, name); err == nil {
			return name, nil
		}
	}
	return "", fmt.Errorf("set --base; could not find main or master")
}

func gitDiff(ctx context.Context, workspace, base, head string) (string, error) {
	args := []string{"-C", workspace, "diff", "--no-color", "--no-ext-diff", "--merge-base", base}
	if head != "" {
		args = append(args, head)
	}
	out, err := exec.CommandContext(ctx, "git", args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git diff: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return string(out), nil
}

func gitRev(ctx context.Context, workspace, rev string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "-C", workspace, "rev-parse", rev).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse %s: %s: %w", rev, strings.TrimSpace(string(out)), err)
	}
	return strings.TrimSpace(string(out)), nil
}

func headRev(head string) string {
	if head == "" {
		return "HEAD"
	}
	return head
}

func diffFingerprint(diff string) string {
	sum := sha256.Sum256([]byte(diff))
	return hex.EncodeToString(sum[:])
}

func formatSource(source findings.Source) string {
	return fmt.Sprintf("base %s head %s diff %s", shortSHA(source.BaseSHA), shortSHA(source.HeadSHA), shortSHA(source.DiffSHA))
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

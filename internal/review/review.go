package review

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"unreal-review/internal/agent"
	"unreal-review/internal/findings"
)

//go:embed skill.md
var skillMarkdown string

const (
	systemPrompt = `You are a code review agent. Follow the pr-review skill. Write findings.jsonl and stop.`
	userPrompt   = `Use the pr-review skill. The review brief is in brief.md. The repository is in repo/. Write findings.jsonl at the workspace root. Every finding must include severity.`
	maxBriefDiff = 200_000
)

type Options struct {
	Workspace     string
	Base          string
	Head          string
	Runner        string
	Model         string
	ThinkingLevel string
	Provider      string
	OpenRouterKey string
	Timeout       time.Duration
	AgentLog      io.Writer
	Stderr        io.Writer
	KeepWork      bool
}

type Result struct {
	Report findings.Report
	Diff   string
	Work   string
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
	runMeta := newRun(opts.Model, base, opts.Head, baseSHA, headSHA)
	if strings.TrimSpace(diff) == "" {
		return Result{Report: findings.Report{
			Run:     runMeta,
			Summary: "No changes to review.",
		}, Diff: diff}, nil
	}
	work, err := os.MkdirTemp("", "unreal-review-")
	if err != nil {
		return Result{}, fmt.Errorf("create work directory: %w", err)
	}
	if !opts.KeepWork {
		defer func() { _ = os.RemoveAll(work) }()
	}
	if err := prepareWork(work, workspace, brief(base, opts.Head, diff)); err != nil {
		return Result{}, err
	}
	level, err := agent.SanitizeLevel(opts.ThinkingLevel)
	if err != nil {
		return Result{}, err
	}
	if _, err := agent.LookPath(opts.Runner); err != nil {
		return Result{}, err
	}
	var logBuf bytes.Buffer
	logWriter := io.Writer(&logBuf)
	if opts.AgentLog != nil {
		logWriter = io.MultiWriter(&logBuf, opts.AgentLog)
	}
	if err := agent.Run(ctx, agent.Request{
		Workspace:     work,
		Runner:        opts.Runner,
		Model:         opts.Model,
		ThinkingLevel: level,
		SystemPrompt:  systemPrompt,
		Prompt:        userPrompt,
		Provider:      opts.Provider,
		OpenRouterKey: opts.OpenRouterKey,
		Log:           logWriter,
		Stderr:        opts.Stderr,
		Timeout:       opts.Timeout,
	}); err != nil {
		return Result{}, err
	}
	cost, generationIDs, err := agent.ParseLogCost(bytes.NewReader(logBuf.Bytes()))
	if err != nil {
		return Result{}, err
	}
	if !agent.CostRecorded(cost) && opts.OpenRouterKey != "" && len(generationIDs) > 0 {
		fetched, fetchErr := agent.FetchOpenRouterCost(ctx, opts.OpenRouterKey, generationIDs)
		if fetchErr != nil {
			return Result{}, fmt.Errorf("track review cost: %w", fetchErr)
		}
		cost = fetched
	}
	if !agent.CostRecorded(cost) {
		return Result{}, fmt.Errorf("track review cost: agent log had no usage; OpenRouter should return usage.cost on every response")
	}
	runMeta.Cost = cost
	report, err := agent.ReadFindings(filepath.Join(work, "findings.jsonl"))
	if err != nil {
		return Result{}, err
	}
	if report.Run == nil {
		report.Run = runMeta
	} else {
		report.Run.Cost = cost
		if report.Run.Model == "" {
			report.Run.Model = opts.Model
		}
		if report.Run.Source.Kind == "" {
			report.Run.Source = runMeta.Source
		}
	}
	return Result{Report: report, Diff: diff, Work: work}, nil
}

func newRun(model, base, head, baseSHA, headSHA string) *findings.Run {
	return &findings.Run{
		ID:        uuid.New().String(),
		CreatedAt: time.Now().UTC(),
		Model:     model,
		Source: findings.Source{
			Kind:    "git",
			Base:    base,
			Head:    head,
			BaseSHA: baseSHA,
			HeadSHA: headSHA,
		},
		Cost: findings.Cost{Currency: "USD"},
	}
}

func prepareWork(work, repo, briefText string) error {
	skillDir := filepath.Join(work, ".harness", "skills", "pr-review")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		return fmt.Errorf("create skill directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMarkdown), 0o644); err != nil {
		return fmt.Errorf("write skill: %w", err)
	}
	if err := os.WriteFile(filepath.Join(work, "brief.md"), []byte(briefText), 0o644); err != nil {
		return fmt.Errorf("write brief: %w", err)
	}
	if err := os.Symlink(repo, filepath.Join(work, "repo")); err != nil {
		return fmt.Errorf("link repository: %w", err)
	}
	return nil
}

func brief(base, head, diff string) string {
	target := head
	if target == "" {
		target = "working tree"
	}
	body := diff
	if len(body) > maxBriefDiff {
		body = body[:maxBriefDiff] + "\n\n[diff truncated]\n"
	}
	return fmt.Sprintf("# Review brief\n\nBase: `%s`\nHead: `%s`\n\n## Diff\n\n```diff\n%s\n```\n", base, target, body)
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

package review

import (
	"bytes"
	"context"
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
	runMeta := newRun(opts.Model, base, opts.Head, baseSHA, headSHA)
	if strings.TrimSpace(diff) == "" {
		return Result{Report: findings.Report{
			Run:     runMeta,
			Summary: "No changes to review.",
		}, Diff: diff}, nil
	}
	findingsFile, err := os.CreateTemp("", "unreal-review-findings-*.jsonl")
	if err != nil {
		return Result{}, fmt.Errorf("create findings file: %w", err)
	}
	findingsPath := findingsFile.Name()
	if err := findingsFile.Close(); err != nil {
		return Result{}, fmt.Errorf("close findings file: %w", err)
	}
	defer func() { _ = os.Remove(findingsPath) }()

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
		Workspace:     workspace,
		Runner:        opts.Runner,
		Model:         opts.Model,
		ThinkingLevel: level,
		SystemPrompt:  systemPrompt,
		Prompt:        reviewPrompt(base, opts.Head, findingsPath, diff),
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
	report, err := agent.ReadFindings(findingsPath)
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
	return Result{Report: report, Diff: diff}, nil
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

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workspace := fs.String("workspace", ".", "git repository to review")
	base := fs.String("base", "", "git ref to diff against (default: main or master)")
	head := fs.String("head", "", "git ref to diff; empty uses the working tree")
	outPath := fs.String("out", "findings.jsonl", "findings JSONL path, or - for stdout")
	runner := fs.String("runner", "unreal-agent-runner", "unreal-agent-runner binary")
	model := fs.String("model", os.Getenv("UNREAL_HARNESS_LLM_MODEL"), "OpenRouter model id")
	thinking := fs.String("thinking-level", "high", "low, medium, high, xhigh, or max")
	provider := fs.String("provider", "", "LLM provider (default openrouter)")
	timeout := fs.Duration("timeout", 20*time.Minute, "agent timeout")
	agentLog := fs.String("agent-log", "", "optional path for unreal-agent-runner JSONL")
	keepWork := fs.Bool("keep-work", false, "keep the temporary agent workspace")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("run takes no positional arguments")
	}
	if *model == "" {
		return fmt.Errorf("set --model or UNREAL_HARNESS_LLM_MODEL")
	}
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		key = os.Getenv("UNREAL_HARNESS_LLM_API_KEY")
	}
	if key == "" {
		return fmt.Errorf("set OPENROUTER_API_KEY")
	}
	var logWriter io.Writer
	if *agentLog != "" {
		file, err := os.Create(*agentLog)
		if err != nil {
			return fmt.Errorf("agent log: %w", err)
		}
		defer func() { _ = file.Close() }()
		logWriter = file
	}
	result, err := review.Run(context.Background(), review.Options{
		Workspace:     *workspace,
		Base:          *base,
		Head:          *head,
		Runner:        *runner,
		Model:         *model,
		ThinkingLevel: *thinking,
		Provider:      *provider,
		OpenRouterKey: key,
		Timeout:       *timeout,
		AgentLog:      logWriter,
		Stderr:        os.Stderr,
		KeepWork:      *keepWork,
	})
	if err != nil {
		return err
	}
	out, err := openOut(*outPath)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if err := findings.Write(out, result.Report); err != nil {
		return err
	}
	if result.Report.Run != nil {
		fmt.Fprintf(os.Stderr, "cost: %s\n", result.Report.Run.Cost.Format())
	}
	if *keepWork && result.Work != "" {
		fmt.Fprintf(os.Stderr, "work: %s\n", result.Work)
	}
	return nil
}

func openOut(path string) (io.WriteCloser, error) {
	if path == "" || path == "-" {
		return nopWriteCloser{os.Stdout}, nil
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	return file, nil
}

type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

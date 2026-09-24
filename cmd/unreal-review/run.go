package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"unreal-review/internal/agent"
	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workspace := fs.String("workspace", ".", "git repository to review")
	spec := addSpecFlags(fs)
	var exclude stringList
	fs.Var(&exclude, "exclude", "git glob to omit from the diff; repeatable")
	outPath := fs.String("out", "findings.jsonl", "findings JSONL path, or - for stdout")
	fresh := fs.Bool("fresh", false, "start a new review even if --out already exists")
	runner := fs.String("runner", "unreal-agent-runner", "unreal-agent-runner binary")
	model := fs.String("model", os.Getenv("UNREAL_HARNESS_LLM_MODEL"), "OpenRouter model id")
	thinking := fs.String("thinking-level", "high", "low, medium, high, xhigh, or max")
	provider := fs.String("provider", "", "LLM provider (default openrouter)")
	timeout := fs.Duration("timeout", 20*time.Minute, "agent timeout")
	agentLog := fs.String("agent-log", "", "optional path for unreal-agent-runner JSONL")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
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
	level, err := agent.SanitizeLevel(*thinking)
	if err != nil {
		return err
	}
	if _, err := agent.LookPath(*runner); err != nil {
		return err
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result, err := review.Run(ctx, review.Options{
		Workspace: *workspace,
		Spec:      spec.spec(),
		Paths:     fs.Args(),
		Exclude:   exclude,
		Out:       *outPath,
		Fresh:     *fresh,
		Model:     *model,
		Agent: agent.Runner{
			Bin:           *runner,
			ThinkingLevel: level,
			Provider:      *provider,
			APIKey:        key,
			Log:           logWriter,
			Stderr:        os.Stderr,
			Timeout:       *timeout,
		},
	})
	if (*outPath == "" || *outPath == "-") && result.Report.Run != nil {
		out, writeErr := openOut(*outPath)
		if writeErr != nil {
			return writeErr
		}
		defer func() { _ = out.Close() }()
		if writeErr := findings.Write(out, result.Report); writeErr != nil {
			return writeErr
		}
	}
	if result.Report.Run != nil {
		fmt.Fprintf(os.Stderr, "cost: %s\n", result.Report.Run.Cost.Format())
		if result.Report.Run.Status != "" && result.Report.Run.Status != findings.StatusComplete {
			fmt.Fprintf(os.Stderr, "status: %s\n", result.Report.Run.Status)
		}
	}
	return err
}

type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ", ")
}

func (s *stringList) Set(value string) error {
	if value == "" {
		return fmt.Errorf("empty --exclude")
	}
	*s = append(*s, value)
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

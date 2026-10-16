package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/0x7067/unreal-review/agent"
	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/review"
)

func cmdRun(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	workspace := fs.String("workspace", ".", "git repository to review")
	spec := addSpecFlags(fs)
	pr := fs.String("pr", "", "pull request to review: owner/repo#n, a URL, or a number")
	repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "owner/repo when --pr is a number")
	var exclude stringList
	fs.Var(&exclude, "exclude", "git glob to omit from the diff; repeatable")
	outPath := fs.String("out", "findings.jsonl", "findings JSONL path, or - for stdout")
	fresh := fs.Bool("fresh", false, "start a new review even if --out already exists")
	decompose := fs.Bool("decompose", false, "use bounded scopes and aggregate coverage even below the automatic large-diff threshold")
	model := fs.String("model", os.Getenv("UNREAL_HARNESS_LLM_MODEL"), "OpenRouter model id")
	thinking := fs.String("thinking-level", "high", "low, medium, high, xhigh, or max")
	strategy := fs.String("strategy", "single", "single or focused (four discovery passes plus verification)")
	timeout := fs.Duration("timeout", 20*time.Minute, "timeout for the whole review")
	agentLog := fs.String("agent-log", "", "optional path for the harness session JSONL log")
	if err := fs.Parse(args); err != nil {
		return successOnHelp(err)
	}
	if *model == "" {
		return fmt.Errorf("set --model or UNREAL_HARNESS_LLM_MODEL")
	}
	key := secret("OPENROUTER_API_KEY")
	if strings.TrimSpace(key) == "" {
		return fmt.Errorf("set OPENROUTER_API_KEY")
	}
	level, err := agent.SanitizeLevel(*thinking)
	if err != nil {
		return err
	}
	selected := spec.spec()
	selected.Pull = *pr
	var resolver review.PullResolver
	if *pr != "" {
		resolver, err = newPullResolver(secret("GH_TOKEN"), *repo)
		if err != nil {
			return err
		}
	}
	// The harness writes session JSONL only after it starts. Opening the file
	// here would truncate an existing log when --strategy, the git range, or
	// an empty diff fails first.
	var logWriter io.Writer
	if *agentLog != "" {
		logFile := &agentLogFile{path: *agentLog}
		defer func() { _ = logFile.Close() }()
		logWriter = logFile
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	reviewer, err := agent.ReviewPipeline(*strategy, agent.Harness{
		APIKey: key, ThinkingLevel: level, Log: logWriter, Timeout: *timeout,
	})
	if err != nil {
		return err
	}
	result, err := review.Run(ctx, review.Options{
		Workspace: *workspace,
		Spec:      selected,
		Paths:     fs.Args(),
		Exclude:   exclude,
		Out:       *outPath,
		Fresh:     *fresh,
		Decompose: *decompose,
		Model:     *model,
		Pull:      resolver,
		Agent:     reviewer,
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

// agentLogFile creates path on the first Write so a run that returns
// before the harness leaves an existing log untouched.
type agentLogFile struct {
	path string
	file *os.File
}

func (f *agentLogFile) Write(p []byte) (int, error) {
	if f.file == nil {
		file, err := os.Create(f.path)
		if err != nil {
			return 0, fmt.Errorf("agent log: %w", err)
		}
		f.file = file
	}
	return f.file.Write(p)
}

func (f *agentLogFile) Close() error {
	if f.file == nil {
		return nil
	}
	return f.file.Close()
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

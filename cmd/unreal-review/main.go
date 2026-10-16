package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

var (
	ErrAPIKey        = errors.New("set OPENROUTER_API_KEY")
	ErrUnknownCorpus = errors.New("unknown corpus")
)

func main() {
	if err := isolateSecrets(); err != nil {
		fmt.Fprintf(os.Stderr, "unreal-review: %v\n", err)
		os.Exit(1)
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "unreal-review: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		if err := printUsage(os.Stderr); err != nil {
			return err
		}
		return fmt.Errorf("command required")
	}
	switch args[0] {
	case "run":
		return cmdRun(args[1:])
	case "group":
		return cmdGroup(args[1:])
	case "eval":
		return cmdEval(args[1:])
	case "render":
		return cmdRender(args[1:])
	case "help", "-h", "--help":
		return printUsage(os.Stdout)
	default:
		if err := printUsage(os.Stderr); err != nil {
			return err
		}
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w *os.File) error {
	_, err := fmt.Fprint(w, `Review a git diff with the unreal-agent harness and write findings.jsonl.
Render that file for GitHub inline comments, markdown, or another host.

Usage:
  unreal-review run [--workspace <dir>] [--from <rev> [--to <rev>] | --commit <rev> | --branch <name> | --pr owner/repo#n]
                    [--repo owner/repo] [--exclude <glob>] [--out <path>] [--fresh] [--model <id>] [--strategy single|focused]
                    [--decompose] [--timeout <duration>] [--thinking-level low|medium|high|xhigh|max]
                    [--agent-log <path>] [paths...]
  unreal-review group [--workspace <dir>] [--from <rev> [--to <rev>] | --commit <rev> | --branch <name>]
                      [--exclude <glob>] [paths...]
  unreal-review eval [--model <id>] [--out <dir>] [--json] [--corpus planted|martian|pr38|labeled]
                     [--runs N] [--profile core|strict|all] [--parallel N] [--cases a,b] [--judge-model <id>]
                     [--thinking-level low|medium|high|xhigh|max] [--strategy single|focused]
                     [--decompose] [--timeout <duration>]
  unreal-review render github [--pr owner/repo#n] [--repo owner/repo] [--commit <sha>] [--dry-run] [findings.jsonl]
  unreal-review render markdown [--out <path>] [findings.jsonl]

Run writes a findings JSONL document. That file is the review and the
checkpoint: it records status and the reviewed commit SHAs. With no
range flags, run reviews staged, unstaged, and untracked changes
against HEAD. --from/--to is merge-base of those refs; omit --to to
include the working tree. --commit reviews one commit against its
parent. --branch reviews a branch since it diverged from main or
master. --workspace selects the checkout. --model names the OpenRouter
model. --fresh starts over an existing --out. --strategy is single or
focused. --decompose requests bounded tasks below the automatic size
threshold. --timeout limits the review. --thinking-level defaults to
high (low|medium|high|xhigh|max). --agent-log writes the harness
session JSONL. Interrupt to pause; run again with the same --out to continue.
Group prints related files; it does not start the agent. --pr narrows the
range to the commits pushed since the newest commit carrying an
unreal-review check run and names findings already posted so the agent
does not restate them; render github then suppresses duplicates, tags comments with
markers, and keeps a status comment with the cumulative cost. Eval
runs a planted-issue corpus through the same pipeline as run and
scores the findings file against the planted issues; --corpus martian
reviews the Martian Code Review Bench pull requests and matches findings
to golden comments with an LLM judge; --corpus pr38 reviews this
repository's pull request #38 and scores real and weak labels from
earlier runs of that range. --corpus labeled reviews that range plus
other labeled pull requests of this repository and prints each case
with a pooled score. --runs repeats the pr38 and labeled corpora. Eval calls the
model and costs money. Render reads that document and produces a
display-specific output. GitHub inline comments are a renderer, not
the review itself. GitHub posting requires a complete review.

Environment:
  OPENROUTER_API_KEY             required for run
  UNREAL_HARNESS_LLM_MODEL       default --model
  GH_TOKEN                       required for --pr and to post a GitHub review
`)
	return err
}

// successOnHelp turns -h/--help into success. The flag set already printed usage.
func successOnHelp(err error) error {
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	return err
}

package main

import (
	"fmt"
	"os"
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
		printUsage(os.Stderr)
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
		printUsage(os.Stdout)
		return nil
	default:
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w *os.File) {
	_, _ = fmt.Fprint(w, `Review a git diff with the unreal-agent harness and write findings.jsonl.
Render that file for GitHub inline comments, markdown, or another host.

Usage:
  unreal-review run [--workspace <dir>] [--from <rev> [--to <rev>] | --commit <rev> | --branch <name> | --pr owner/repo#n]
                    [--repo owner/repo] [--exclude <glob>] [--out <path>] [--fresh] [--model <id>] [--strategy single|focused]
                    [--decompose] [--timeout <duration>] [--thinking-level low|medium|high|xhigh|max]
                    [--compaction off|<tokens>|<percent>%] [--context-window <tokens>]
                    [--agent-log <path>] [paths...]
  unreal-review group [--workspace <dir>] [--from <rev> [--to <rev>] | --commit <rev> | --branch <name>]
                      [--exclude <glob>] [paths...]
  unreal-review eval [--model <id>] [--out <dir>] [--json] [--corpus planted|martian]
                     [--profile core|strict|all] [--parallel N] [--cases a,b] [--judge-model <id>]
                     [--thinking-level low|medium|high|xhigh|max] [--strategy single|focused]
                     [--compaction off|<tokens>|<percent>%] [--context-window <tokens>]
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
high (low|medium|high|xhigh|max). --compaction is off, a token count of
40000 or more, or a percent of the context window such as 75%. The default is 75%
when the window is known from --context-window or the harness model table,
and off when it is not or when that default cutoff is below 40000. An explicit percent with an unknown window is an
error. Stderr states the cutoff in tokens, or that compaction is off. A zero
threshold is rejected. --agent-log writes the harness
session JSONL. Interrupt to pause; run again with the same --out to continue.
Group prints related files; it does not start the agent. --pr narrows the
range to the commits pushed since the newest commit carrying an
unreal-review check run and names findings already posted so the agent
does not restate them; render github then suppresses duplicates, tags comments with
markers, and keeps a status comment with the cumulative cost. Eval
runs a planted-issue corpus through the same pipeline as run and
scores the findings file against the planted issues; --corpus martian
reviews the Martian Code Review Bench pull requests and matches findings
to golden comments with an LLM judge. Eval calls the
model and costs money. Render reads that document and produces a
display-specific output. GitHub inline comments are a renderer, not
the review itself. GitHub posting requires a complete review.

Environment:
  OPENROUTER_API_KEY             required for run
  UNREAL_HARNESS_LLM_MODEL       default --model
  UNREAL_REVIEW_COMPACTION       default --compaction
  UNREAL_REVIEW_CONTEXT_WINDOW   default --context-window
  GH_TOKEN                       required for --pr and to post a GitHub review
`)
}

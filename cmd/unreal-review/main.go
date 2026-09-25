package main

import (
	"fmt"
	"os"

	"unreal-review/internal/review"
)

func main() {
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
		if tool, ok := review.FindTool(review.DefaultTools(), args[0]); ok {
			return runAgentTool(tool, args[1:])
		}
		printUsage(os.Stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func printUsage(w *os.File) {
	_, _ = fmt.Fprint(w, `Review a git diff with unreal-agent-runner and write findings.jsonl.
Render that file for GitHub inline comments, markdown, or another host.

Usage:
  unreal-review run [--from <rev> [--to <rev>] | --commit <rev> | --branch <name> | --pr owner/repo#n] [--exclude <glob>] [paths...]
  unreal-review group [--from <rev> [--to <rev>] | --commit <rev> | --branch <name>] [--exclude <glob>] [paths...]
  unreal-review eval [--model <id>] [--out <dir>] [--json]
  unreal-review render github [--pr owner/repo#n] [--dry-run] [findings.jsonl]
  unreal-review render markdown [flags] [findings.jsonl]

Run writes a findings JSONL document. That file is the review and the
checkpoint: it records status and the reviewed commit SHAs. With no
range flags, run reviews staged, unstaged, and untracked changes
against HEAD. --from/--to is merge-base of those refs; omit --to to
include the working tree. --commit reviews one commit against its
parent. --branch reviews a branch since it diverged from main or
master. Interrupt to pause; run again with the same --out to continue.
Group prints related files from the same git range so a large change
can be reviewed in pieces; it does not start the agent. --pr narrows the
range to the commits pushed since the last posted review of that pull
request and names findings already posted so the agent does not restate
them; render github then suppresses duplicates, tags comments with
markers, and keeps a status comment with the cumulative cost. Eval
runs a planted-issue corpus through the same pipeline as run and
scores the findings file against the planted issues; it calls the
model and costs money. Render reads that document and produces a
display-specific output. GitHub inline comments are a renderer, not
the review itself. GitHub posting requires a complete review.

Environment:
  OPENROUTER_API_KEY             required for run
  UNREAL_HARNESS_LLM_MODEL       default --model
  UNREAL_HARNESS_LLM_PROVIDER    default openrouter
  GH_TOKEN or GITHUB_TOKEN       required to post a GitHub review
`)
}

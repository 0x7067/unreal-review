package main

import (
	"fmt"
	"os"
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
	_, _ = fmt.Fprint(w, `Review a git diff with unreal-agent-runner and write findings.jsonl.
Render that file for GitHub inline comments, markdown, or another host.

Usage:
  unreal-review run [flags]
  unreal-review render github [flags] [findings.jsonl]
  unreal-review render markdown [flags] [findings.jsonl]

Run writes a findings JSONL document. That file is the review and the
checkpoint: it records status and the reviewed commit SHAs. Interrupt to
pause; run again with the same --out to continue. Render reads that
document and produces a display-specific output. GitHub inline comments
are a renderer, not the review itself. GitHub posting requires a
complete review.

Environment:
  OPENROUTER_API_KEY             required for run
  UNREAL_HARNESS_LLM_MODEL       default --model
  UNREAL_HARNESS_LLM_PROVIDER    default openrouter
  GH_TOKEN or GITHUB_TOKEN       required to post a GitHub review
`)
}

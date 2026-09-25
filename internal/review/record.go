package review

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"unreal-review/internal/findings"
)

func recordTool() Tool {
	return Tool{
		Name:    "record",
		Purpose: "Append one finding or the summary to the findings file.",
		Usage: `record --out <findings-path> --path <repo-relative file> --start <line> [--end <line>] [--anchor new|old] --severity error|warning|note --body-file - <<'EOF'
<finding body, markdown>
EOF

record --out <findings-path> --kind summary --body-file - <<'EOF'
<summary body>
EOF`,
		Run: runRecord,
	}
}

func runRecord(ctx context.Context, args []string, stdin string) (string, error) {
	fs := flag.NewFlagSet("record", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	out := fs.String("out", "", "findings path from the user message")
	kind := fs.String("kind", "finding", "finding or summary")
	path := fs.String("path", "", "repository-relative file the finding is about")
	start := fs.Int("start", 0, "first inclusive 1-based line")
	end := fs.Int("end", 0, "last inclusive 1-based line")
	anchor := fs.String("anchor", "new", "new or old")
	severity := fs.String("severity", "", "error, warning, or note")
	bodyFile := fs.String("body-file", "-", "body file, - for standard input")
	if err := fs.Parse(args); err != nil {
		return "", err
	}
	if *out == "" {
		return "", fmt.Errorf("record: --out is required")
	}
	body, err := readBody(*bodyFile, stdin)
	if err != nil {
		return "", err
	}
	switch *kind {
	case "finding":
		recorded, err := findings.AppendFinding(*out, findings.Finding{
			Path:      *path,
			StartLine: *start,
			EndLine:   *end,
			Anchor:    findings.Anchor(*anchor),
			Severity:  findings.Severity(*severity),
			Body:      body,
		})
		if err != nil {
			return "", fmt.Errorf("record: %w", err)
		}
		return fmt.Sprintf("recorded finding %s for %s:%d-%d", recorded.ID, recorded.Path, recorded.StartLine, recorded.EndLine), nil
	case "summary":
		if err := findings.AppendSummary(*out, body); err != nil {
			return "", fmt.Errorf("record: %w", err)
		}
		return "recorded summary", nil
	default:
		return "", fmt.Errorf("record: --kind must be finding or summary")
	}
}

func readBody(bodyFile, stdin string) (string, error) {
	if bodyFile == "-" {
		return stdin, nil
	}
	raw, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", fmt.Errorf("record: read body: %w", err)
	}
	return string(raw), nil
}

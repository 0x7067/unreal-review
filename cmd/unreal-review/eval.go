package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"unreal-review/internal/agent"
	"unreal-review/internal/eval"
)

type evalSummary struct {
	Cases []eval.Score `json:"cases"`
	Total evalTotals   `json:"total"`
}

type evalTotals struct {
	Cases        int     `json:"cases"`
	Completed    int     `json:"completed"`
	Matched      int     `json:"matched"`
	SeverityHits int     `json:"severity_hits"`
	Gold         int     `json:"gold"`
	Produced     int     `json:"produced"`
	Extra        int     `json:"extra"`
	CostUSD      float64 `json:"cost_usd"`
	Requests     int     `json:"requests"`
}

func cmdEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outDir := fs.String("out", "", "directory for eval artifacts (default: a fresh temp dir)")
	model := fs.String("model", os.Getenv("UNREAL_HARNESS_LLM_MODEL"), "OpenRouter model id")
	runner := fs.String("runner", "unreal-agent-runner", "unreal-agent-runner binary")
	thinking := fs.String("thinking-level", "high", "low, medium, high, xhigh, or max")
	provider := fs.String("provider", "", "LLM provider (default openrouter)")
	timeout := fs.Duration("timeout", 20*time.Minute, "agent timeout per case")
	asJSON := fs.Bool("json", false, "print machine-readable JSON instead of a table")
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
	root := *outDir
	if root == "" {
		temp, err := os.MkdirTemp("", "unreal-review-eval-")
		if err != nil {
			return fmt.Errorf("create eval dir: %w", err)
		}
		root = temp
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("create eval dir: %w", err)
	}
	scores := make([]eval.Score, 0, len(eval.Corpus))
	for _, c := range eval.Corpus {
		score, err := eval.Run(context.Background(), c, root, eval.Options{
			Model: *model,
			Agent: agent.Runner{
				Bin:           *runner,
				ThinkingLevel: level,
				Provider:      *provider,
				APIKey:        key,
				Stderr:        os.Stderr,
				Timeout:       *timeout,
			},
		})
		if err != nil {
			return err
		}
		scores = append(scores, score)
	}
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(evalSummary{Cases: scores, Total: totals(scores)})
	}
	printScores(scores)
	fmt.Fprintf(os.Stderr, "artifacts: %s\n", root)
	return nil
}

func totals(scores []eval.Score) evalTotals {
	total := evalTotals{Cases: len(scores)}
	for _, score := range scores {
		if score.Completed() {
			total.Completed++
		}
		total.Gold += score.Gold
		total.Matched += score.Matched
		total.SeverityHits += score.SeverityHits
		total.Produced += score.Produced
		total.Extra += score.Extra
		total.CostUSD += score.CostUSD
		total.Requests += score.Requests
	}
	return total
}

func printScores(scores []eval.Score) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	defer func() { _ = writer.Flush() }()
	_, _ = fmt.Fprintln(writer, "case\tstatus\trecall\tprecision\tseverity\tfound\tgold\textra\tcost\ttime")
	for _, score := range scores {
		note := ""
		if score.Err != "" {
			note = " " + shortErr(score.Err)
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s%s\t%.2f\t%.2f\t%.2f\t%d\t%d\t%d\t$%.4f\t%ds\n",
			score.Name, score.Status, note, score.Recall(), score.Precision(), score.SeverityAgreement(),
			score.Matched, score.Gold, score.Extra, score.CostUSD, score.DurationMS/1000)
	}
	total := totals(scores)
	_, _ = fmt.Fprintf(writer, "total\t%d/%d complete\t\t\t%.2f\t%d\t%d\t%d\t$%.4f\t\n",
		total.Completed, total.Cases, eval.Agreement(total.SeverityHits, total.Gold),
		total.Matched, total.Gold, total.Extra, total.CostUSD)
}

func shortErr(msg string) string {
	if len(msg) > 60 {
		return msg[:60]
	}
	return msg
}

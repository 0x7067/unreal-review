package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/0x7067/unreal-review/internal/eval"
	"github.com/0x7067/unreal-review/review"
)

func evalLabeled(ctx context.Context, root, model string, reviewer review.Agent, runs int, decompose, asJSON bool, names []string) error {
	return runLabeledCases(ctx, os.Stdout, os.Stderr, root, runs, asJSON, names, func(n int, name string) (eval.PR38Score, error) {
		runRoot := filepath.Join(root, fmt.Sprintf("run-%d", n))
		if err := os.MkdirAll(runRoot, 0o755); err != nil {
			return eval.PR38Score{}, fmt.Errorf("create eval dir: %w", err)
		}
		return eval.RunLabeled(ctx, runRoot, name, eval.Options{Model: model, Agent: reviewer, Decompose: decompose})
	})
}

func runLabeledCases(ctx context.Context, stdout, stderr io.Writer, root string, runs int, asJSON bool, names []string, run func(n int, name string) (eval.PR38Score, error)) error {
	byCase := make([][]eval.PR38Score, len(names))
	var stop error
	for n := 1; n <= runs; n++ {
		if err := ctx.Err(); err != nil {
			stop = err
			break
		}
		for i, name := range names {
			score, err := run(n, name)
			if err != nil {
				stop = err
				break
			}
			score.Run = n
			byCase[i] = append(byCase[i], score)
			if score.Completed() {
				_, _ = fmt.Fprintf(stderr, "%s run %d done\n", name, n)
			}
		}
		if stop != nil {
			break
		}
	}
	report, err := eval.ReportLabeled(names, byCase)
	if err != nil {
		return err
	}
	finishErr := finishLabeled(stdout, stderr, report, asJSON, root)
	if stop != nil {
		return stop
	}
	return finishErr
}

func finishLabeled(stdout, stderr io.Writer, report eval.LabeledReport, asJSON bool, root string) error {
	for _, c := range report.Cases {
		for _, score := range c.Runs {
			if score.Completed() {
				continue
			}
			text := score.Err
			if text == "" {
				text = "status " + score.Status
				if score.Status == "" {
					text = "no status"
				}
			}
			_, _ = fmt.Fprintf(stderr, "%s run %d failed: %s\n", c.Corpus, score.Run, text)
		}
	}
	var err error
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		err = enc.Encode(report)
	} else {
		err = printLabeled(stdout, report)
		if err == nil {
			_, err = fmt.Fprintf(stderr, "artifacts: %s\n", root)
		}
	}
	if err != nil {
		return err
	}
	if report.Pooled.Failed > 0 {
		return fmt.Errorf("%d of %d labeled runs failed", report.Pooled.Failed, len(report.Pooled.Runs))
	}
	return nil
}

func printLabeled(w io.Writer, report eval.LabeledReport) error {
	for i, c := range report.Cases {
		if i > 0 {
			if _, err := fmt.Fprintln(w); err != nil {
				return err
			}
		}
		if _, err := fmt.Fprintln(w, c.Corpus); err != nil {
			return err
		}
		if err := printScoreTable(w, c); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "pooled"); err != nil {
		return err
	}
	if err := printScoreTable(w, report.Pooled); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "spent $%.4f %ds\n", report.SpentCostUSD, report.SpentDurationMS/1000); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, eval.LabeledRule())
	return err
}

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/0x7067/unreal-review/internal/eval"
	"github.com/0x7067/unreal-review/review"
)

func evalPR38(ctx context.Context, root, model string, reviewer review.Agent, runs int, decompose, asJSON bool) error {
	return runPR38s(ctx, os.Stdout, os.Stderr, root, runs, asJSON, func(n int) (eval.PR38Score, error) {
		runRoot := filepath.Join(root, fmt.Sprintf("run-%d", n))
		if err := os.MkdirAll(runRoot, 0o755); err != nil {
			return eval.PR38Score{}, fmt.Errorf("create eval dir: %w", err)
		}
		return eval.RunPR38(ctx, runRoot, eval.Options{Model: model, Agent: reviewer, Decompose: decompose})
	})
}

func runPR38s(ctx context.Context, stdout, stderr io.Writer, root string, runs int, asJSON bool, run func(n int) (eval.PR38Score, error)) error {
	scores := make([]eval.PR38Score, 0, runs)
	var stop error
	for n := 1; n <= runs; n++ {
		if err := ctx.Err(); err != nil {
			stop = err
			break
		}
		score, err := run(n)
		if err != nil {
			stop = err
			break
		}
		score.Run = n
		scores = append(scores, score)
		if score.Completed() {
			if _, err := fmt.Fprintf(stderr, "pr38 run %d done\n", n); err != nil {
				return err
			}
		}
	}
	err := finishPR38(stdout, stderr, eval.AggregatePR38(scores), asJSON, root)
	if stop != nil {
		return stop
	}
	return err
}

func finishPR38(stdout, stderr io.Writer, report eval.PR38Report, asJSON bool, root string) error {
	for _, score := range report.Runs {
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
		if _, err := fmt.Fprintf(stderr, "pr38 run %d failed: %s\n", score.Run, text); err != nil {
			return err
		}
	}
	var err error
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		err = enc.Encode(report)
	} else {
		err = printPR38(stdout, report)
		if err == nil {
			_, err = fmt.Fprintf(stderr, "artifacts: %s\n", root)
		}
	}
	if err != nil {
		return err
	}
	if report.Failed > 0 {
		return fmt.Errorf("%d of %d pr38 runs failed", report.Failed, len(report.Runs))
	}
	return nil
}

func printPR38(w io.Writer, report eval.PR38Report) error {
	if err := printScoreTable(w, report); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, eval.PR38Rule())
	return err
}

func printScoreTable(w io.Writer, report eval.PR38Report) error {
	writer := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "run\tstatus\trecall\tprecision\thits\tweak\tunlabeled\terror\tfindings\tcost\trequests\ttime"); err != nil {
		return err
	}
	for _, score := range report.Runs {
		note := ""
		if score.Err != "" {
			note = " " + shortErr(score.Err)
		}
		if _, err := fmt.Fprintf(writer, "%d\t%s%s\t%.2f\t%s\t%d/%d\t%d\t%d\t%.2f\t%d\t$%.4f\t%d\t%ds\n",
			score.Run, score.Status, note, score.Recall, formatPrecision(score.Precision), score.Hits, report.Real,
			score.Weak, score.Unlabeled, score.ErrorShare, score.Produced, score.CostUSD, score.Requests, score.DurationMS/1000); err != nil {
			return err
		}
	}
	mean := report.Mean
	if _, err := fmt.Fprintf(writer, "mean\t%d/%d complete\t%.2f\t%s\t\t\t\t%.2f\t%.1f\t$%.4f\t%.1f\t%ds\n",
		report.Completed, len(report.Runs), mean.Recall, formatPrecision(mean.Precision), mean.ErrorShare,
		mean.Produced, mean.CostUSD, mean.Requests, int64(mean.DurationMS)/1000); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "union\t%s\t\t\t%d/%d\t\t\t\t\t$%.4f total\n", strings.Join(report.UnionHits, ","), report.Union, report.Real, report.CostUSD); err != nil {
		return err
	}
	return writer.Flush()
}

func formatPrecision(p *float64) string {
	if p == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.2f", *p)
}

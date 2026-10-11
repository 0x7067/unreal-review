package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/0x7067/unreal-review/internal/eval"
	"github.com/0x7067/unreal-review/review"
)

func evalPR38(ctx context.Context, root, model string, reviewer review.Agent, runs int, decompose, asJSON bool) error {
	scores := make([]eval.PR38Score, 0, runs)
	for n := 1; n <= runs; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		runRoot := filepath.Join(root, fmt.Sprintf("run-%d", n))
		if err := os.MkdirAll(runRoot, 0o755); err != nil {
			return fmt.Errorf("create eval dir: %w", err)
		}
		score, err := eval.RunPR38(ctx, runRoot, eval.Options{Model: model, Agent: reviewer, Decompose: decompose})
		if err != nil {
			return err
		}
		score.Run = n
		scores = append(scores, score)
		fmt.Fprintf(os.Stderr, "pr38 run %d done\n", n)
	}
	report := eval.AggregatePR38(scores)
	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(report)
	}
	printPR38(report)
	fmt.Fprintf(os.Stderr, "artifacts: %s\n", root)
	return nil
}

func printPR38(report eval.PR38Report) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "run\tstatus\trecall\tprecision\thits\tweak\tunlabeled\terror\tfindings\tcost\trequests\ttime")
	for _, score := range report.Runs {
		note := ""
		if score.Err != "" {
			note = " " + shortErr(score.Err)
		}
		_, _ = fmt.Fprintf(writer, "%d\t%s%s\t%.2f\t%.2f\t%d/%d\t%d (%.2f)\t%d (%.2f)\t%.2f\t%d\t$%.4f\t%d\t%ds\n",
			score.Run, score.Status, note, score.Recall, score.Precision, score.Hits, score.Real,
			score.Weak, score.WeakRate, score.Unlabeled, score.UnlabeledRate, score.ErrorShare,
			score.Produced, score.CostUSD, score.Requests, score.DurationMS/1000)
	}
	mean := report.Mean
	_, _ = fmt.Fprintf(writer, "mean\t\t%.2f\t%.2f\t%.1f/%d\t%.1f\t%.1f\t%.2f\t%.1f\t$%.4f\t%.1f\t%ds\n",
		mean.Recall, mean.Precision, mean.Hits, report.Real, mean.Weak, mean.Unlabeled, mean.ErrorShare,
		mean.Produced, mean.CostUSD, mean.Requests, int64(mean.DurationMS)/1000)
	_, _ = fmt.Fprintf(writer, "union\t%s\t\t\t%d/%d\n", strings.Join(report.UnionHits, ","), report.Union, report.Real)
	_ = writer.Flush()
	fmt.Println(report.Note)
}

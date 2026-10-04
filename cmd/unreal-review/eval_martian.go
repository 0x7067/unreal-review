package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"text/tabwriter"

	"unreal-review/internal/agent"
	"unreal-review/internal/eval"
	"unreal-review/internal/review"
)

type martianFlags struct {
	profile    string
	parallel   int
	cases      string
	judgeModel string
	asJSON     bool
	decompose  bool
}

type martianSummary struct {
	Corpus     string                 `json:"corpus"`
	Profile    string                 `json:"profile"`
	JudgeModel string                 `json:"judge_model"`
	Cases      []eval.MartianScore    `json:"cases"`
	Total      evalTotals             `json:"total"`
	BySeverity map[string]eval.Tally  `json:"by_severity"`
	ByProfile  map[string]eval.Counts `json:"by_profile"`
	JudgeCost  float64                `json:"judge_cost_usd"`
}

type martianReady struct {
	flags martianFlags
	cases []eval.MartianCase
	base  string
}

func prepareMartian(flags martianFlags) (martianReady, error) {
	if flags.parallel < 1 {
		return martianReady{}, fmt.Errorf("--parallel must be at least 1")
	}
	all, err := eval.MartianCorpus()
	if err != nil {
		return martianReady{}, err
	}
	if _, err := eval.MartianProfile(flags.profile); err != nil {
		return martianReady{}, err
	}
	cases, err := selectNamed(all, flags.cases, "martian", func(c eval.MartianCase) string { return c.Name })
	if err != nil {
		return martianReady{}, err
	}
	base, err := agent.OpenRouterBase()
	if err != nil {
		return martianReady{}, err
	}
	return martianReady{flags: flags, cases: cases, base: base}, nil
}

func evalMartian(ctx context.Context, root, model string, reviewer review.Agent, apiKey string, ready martianReady) error {
	flags := ready.flags
	judge := eval.Judge{APIKey: apiKey, Model: flags.judgeModel, Base: ready.base}
	scores := make([]eval.MartianScore, len(ready.cases))
	errs := make([]error, len(ready.cases))
	slots := make(chan struct{}, flags.parallel)
	var wg sync.WaitGroup
	for i, c := range ready.cases {
		wg.Add(1)
		go func() {
			defer wg.Done()
			slots <- struct{}{}
			defer func() { <-slots }()
			scores[i], errs[i] = eval.RunMartian(ctx, c, root, eval.Options{Model: model, Agent: reviewer, Decompose: flags.decompose}, judge, flags.profile)
			fmt.Fprintf(os.Stderr, "%s done\n", c.Name)
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			scores[i].Err = err.Error()
		}
	}
	summary := martianSummary{Corpus: "martian", Profile: flags.profile, JudgeModel: flags.judgeModel, Cases: scores, BySeverity: eval.SumBySeverity(scores), ByProfile: eval.SumProfiles(scores)}
	plain := make([]eval.Score, len(scores))
	for i, score := range scores {
		plain[i] = score.Score
		summary.JudgeCost += score.JudgeCostUSD
	}
	summary.Total = totals(plain)
	if flags.asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(summary)
	}
	printMartian(summary)
	fmt.Fprintf(os.Stderr, "artifacts: %s\n", root)
	return nil
}
func printMartian(summary martianSummary) {
	writer := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "case\tstatus\trecall\tseverity\tfound\tgold\textra\tcost\ttime")
	for _, score := range summary.Cases {
		note := ""
		if score.Err != "" {
			note = " " + shortErr(score.Err)
		} else if score.JudgeErr != "" {
			note = " judge: " + shortErr(score.JudgeErr)
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s%s\t%.2f\t%.2f\t%d\t%d\t%d\t$%.4f\t%ds\n",
			score.Name, score.Status, note, score.Recall(), score.SeverityAgreement(),
			score.Matched, score.Gold, score.Extra, score.CostUSD, score.DurationMS/1000)
	}
	total := summary.Total
	_, _ = fmt.Fprintf(writer, "total\t%d/%d complete\t%.2f\t%.2f\t%d\t%d\t%d\t$%.4f\t\n",
		total.Completed, total.Cases, eval.Agreement(total.Matched, total.Gold), eval.Agreement(total.SeverityHits, total.Gold),
		total.Matched, total.Gold, total.Extra, total.CostUSD)
	_ = writer.Flush()
	writer = tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "\nmartian severity\tmaps to\trecall\tseverity\tfound\tgold")
	for _, severity := range eval.MartianSeverities {
		tally := summary.BySeverity[severity]
		if tally.Gold == 0 {
			_, _ = fmt.Fprintf(writer, "%s\t%s\t-\t-\t0\t0\n", severity, eval.MartianSeverity(severity))
			continue
		}
		_, _ = fmt.Fprintf(writer, "%s\t%s\t%.2f\t%.2f\t%d\t%d\n", severity, eval.MartianSeverity(severity),
			eval.Agreement(tally.Matched, tally.Gold), eval.Agreement(tally.SeverityHits, tally.Gold), tally.Matched, tally.Gold)
	}
	_ = writer.Flush()
	writer = tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	_, _ = fmt.Fprintln(writer, "\nmartian profile\tprecision\trecall\tf1\ttp\tfp\tfn")
	for _, profile := range eval.MartianProfiles {
		counts := summary.ByProfile[profile]
		_, _ = fmt.Fprintf(writer, "%s\t%.1f\t%.1f\t%.1f\t%d\t%d\t%d\n", profile,
			100*counts.Precision(), 100*counts.Recall(), 100*counts.F1(), counts.TP, counts.FP, counts.FN)
	}
	_ = writer.Flush()
	fmt.Printf("profile %s, judge %s ($%.4f). Precision, recall, and F1 use the built-in direct matcher with Martian's profile accounting: fp counts every extra, and a match on a golden comment outside a profile counts in neither tp nor fp. For comparable benchmark scores, export findings through Martian's pinned step 2/2.5/3 runner. Extras match no golden comment; the golden set is sparse on minor issues, so extras are not all false positives.\n",
		summary.Profile, summary.JudgeModel, summary.JudgeCost)
}

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/0x7067/unreal-review/agent"
	"github.com/0x7067/unreal-review/internal/eval"
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

type evalFlags struct {
	out, model, thinking, strategy, corpus, profile, only, judgeModel *string
	decompose, asJSON                                                 *bool
	timeout                                                           *time.Duration
	parallel, runs                                                    *int
}

func registerEvalFlags(fs *flag.FlagSet) evalFlags {
	var f evalFlags
	f.out = fs.String("out", "", "directory for eval artifacts (default: a fresh temp dir)")
	f.model = fs.String("model", os.Getenv("UNREAL_HARNESS_LLM_MODEL"), "OpenRouter model id")
	f.thinking = fs.String("thinking-level", "high", "low, medium, high, xhigh, or max")
	f.strategy = fs.String("strategy", "single", "single or focused (four discovery passes plus verification)")
	f.decompose = fs.Bool("decompose", false, "use bounded aggregate scopes for every review case")
	f.timeout = fs.Duration("timeout", 20*time.Minute, "agent timeout per case")
	f.asJSON = fs.Bool("json", false, "print machine-readable JSON instead of a table")
	f.corpus = fs.String("corpus", "planted", "planted, martian, pr38, or labeled")
	f.profile = fs.String("profile", "core", "martian golden categories: core, strict, or all")
	f.parallel = fs.Int("parallel", 8, "martian cases reviewed at once")
	f.only = fs.String("cases", "", "comma-separated case names for the selected corpus (default: all)")
	f.judgeModel = fs.String("judge-model", "anthropic/claude-sonnet-5.5", "OpenRouter model that matches martian findings to golden comments")
	f.runs = fs.Int("runs", 1, "repeat the pr38 or labeled corpus and print each run, the mean, and the union of hits")
	return f
}

// progressf writes a status line. A failed write is ignored so stderr progress cannot fail a run that otherwise succeeded.
func progressf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...) //nolint:errcheck // stderr progress is best-effort
}

func cmdEval(args []string) error {
	fs := flag.NewFlagSet("eval", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	f := registerEvalFlags(fs)
	if err := fs.Parse(args); err != nil {
		return successOnHelp(err)
	}
	switch *f.corpus {
	case "planted", "martian", "pr38", "labeled":
	default:
		return fmt.Errorf("%w %q: use planted, martian, pr38, or labeled", ErrUnknownCorpus, *f.corpus)
	}
	if *f.runs < 1 {
		return fmt.Errorf("--runs must be at least 1")
	}
	if *f.runs != 1 && *f.corpus != "pr38" && *f.corpus != "labeled" {
		return fmt.Errorf("--runs applies to the pr38 and labeled corpora")
	}
	if *f.model == "" {
		return fmt.Errorf("set --model or UNREAL_HARNESS_LLM_MODEL")
	}
	key := secret("OPENROUTER_API_KEY")
	if strings.TrimSpace(key) == "" {
		return ErrAPIKey
	}
	level, err := agent.SanitizeLevel(*f.thinking)
	if err != nil {
		return err
	}
	sessionDir, err := defaultSessionDir()
	if err != nil {
		return err
	}
	openRouterAPI := os.Getenv("UNREAL_REVIEW_OPENROUTER_API")
	reviewer, err := agent.ReviewPipeline(*f.strategy, agent.Harness{APIKey: key, ThinkingLevel: level, Timeout: *f.timeout})
	if err != nil {
		return err
	}
	planted := eval.Corpus
	if *f.corpus == "planted" {
		planted, err = selectNamed(planted, *f.only, "planted", func(c eval.Case) string { return c.Name })
		if err != nil {
			return err
		}
	}
	var ready martianReady
	if *f.corpus == "martian" {
		ready, err = prepareMartian(martianFlags{
			profile: *f.profile, parallel: *f.parallel, cases: *f.only, judgeModel: *f.judgeModel, asJSON: *f.asJSON, decompose: *f.decompose,
		}, openRouterAPI)
		if err != nil {
			return err
		}
	}
	if *f.corpus == "pr38" {
		if _, err = selectNamed(eval.PR38CaseNames(), *f.only, "pr38", func(name string) string { return name }); err != nil {
			return err
		}
	}
	var labeledNames []string
	if *f.corpus == "labeled" {
		labeledNames, err = selectNamed(eval.LabeledCaseNames(), *f.only, "labeled", func(name string) string { return name })
		if err != nil {
			return err
		}
	}
	root := *f.out
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch *f.corpus {
	case "planted":
	case "martian":
		return evalMartian(ctx, root, *f.model, reviewer, key, ready, sessionDir, openRouterAPI)
	case "pr38":
		return evalPR38(ctx, root, *f.model, reviewer, *f.runs, *f.decompose, *f.asJSON, sessionDir, openRouterAPI)
	case "labeled":
		return evalLabeled(ctx, root, *f.model, reviewer, *f.runs, *f.decompose, *f.asJSON, labeledNames, sessionDir, openRouterAPI)
	}
	scores := make([]eval.Score, 0, len(planted))
	for _, c := range planted {
		score, err := eval.Run(ctx, c, root, eval.Options{
			Model:         *f.model,
			Agent:         reviewer,
			Decompose:     *f.decompose,
			SessionDir:    sessionDir,
			OpenRouterAPI: openRouterAPI,
		})
		if err != nil {
			return err
		}
		scores = append(scores, score)
		fmt.Fprintf(os.Stderr, "%s done\n", c.Name)
	}
	if err := writePlanted(os.Stdout, scores, *f.asJSON); err != nil {
		return err
	}
	if !*f.asJSON {
		fmt.Fprintf(os.Stderr, "artifacts: %s\n", root)
	}
	return nil
}

func writePlanted(w io.Writer, scores []eval.Score, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(evalSummary{Cases: scores, Total: totals(scores)})
	}
	return printScores(w, scores)
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

func printScores(w io.Writer, scores []eval.Score) error {
	writer := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(writer, "case\tstatus\trecall\tprecision\tseverity\tfound\tgold\textra\tcost\ttime"); err != nil {
		return err
	}
	for _, score := range scores {
		note := ""
		if score.Err != "" {
			note = " " + shortErr(score.Err)
		}
		if _, err := fmt.Fprintf(writer, "%s\t%s%s\t%.2f\t%.2f\t%.2f\t%d\t%d\t%d\t$%.4f\t%ds\n",
			score.Name, score.Status, note, score.Recall(), score.Precision(), score.SeverityAgreement(),
			score.Matched, score.Gold, score.Extra, score.CostUSD, score.DurationMS/1000); err != nil {
			return err
		}
	}
	total := totals(scores)
	if _, err := fmt.Fprintf(writer, "total\t%d/%d complete\t\t\t%.2f\t%d\t%d\t%d\t$%.4f\t\n",
		total.Completed, total.Cases, eval.Agreement(total.SeverityHits, total.Gold),
		total.Matched, total.Gold, total.Extra, total.CostUSD); err != nil {
		return err
	}
	return writer.Flush()
}

func shortErr(msg string) string {
	if len(msg) > 60 {
		return msg[:60]
	}
	return msg
}

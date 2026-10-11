package eval

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/review"
)

const (
	pr38Name = "pr38"
	pr38Repo = "0x7067/unreal-review"
	pr38Base = "942c78c32f5d45ef05a4db9c284f4e396d31bd95"
	pr38Head = "107a3230e4cc9cfb21df2c8997298aa64cafd78f"
	pr38Note = "Precision is real-label hits / (real-label hits + findings that overlap a weak label). Another finding on a real label already hit does not change that numerator. Findings that match no label are unlabeled: they are not errors and they are not in that denominator. Recall is distinct real labels hit out of the real labels. F1 counts once if either call site overlaps; F9 counts once as a weak label if either file overlaps."
)

type pr38Site struct {
	Path      string
	StartLine int
	EndLine   int
}

type pr38Label struct {
	ID          string
	Verdict     string
	Description string
	Reason      string
	Sites       []pr38Site
}

// pr38Labels are the PR #38 regression labels at head 107a3230.
// Sites of one label are alternatives: overlapping any of them hits that label.
// Real sites do not overlap each other, so a tight finding is one hit.
var pr38Labels = []pr38Label{
	{
		ID:          "F1",
		Verdict:     "real",
		Description: "eval.Run and martianReviewOptions omit Fresh: true, so a second run in the same output directory scores the completed checkpoint instead of reviewing the reset fixture.",
		Reason:      "PR #38 removed Fresh: true from both call sites so an interrupted case can resume; a finished checkpoint is refused and the old report is what gets scored.",
		Sites: []pr38Site{
			{Path: "internal/eval/eval.go", StartLine: 121, EndLine: 127},
			{Path: "internal/eval/martian.go", StartLine: 331, EndLine: 337},
		},
	},
	{
		ID:          "F2",
		Verdict:     "real",
		Description: "initialized treats an existing .git/HEAD as a committed repository, so an unborn repository takes the retry path and git reset --hard HEAD fails.",
		Reason:      "A setup that stops after git init leaves .git/HEAD with no commit; the next attempt stats that file, enters the retry branch, and reset exits 128. The label is the predicate (150-153) and the if initialized branch (157), not the reset line shared with the stale-base label.",
		Sites: []pr38Site{
			{Path: "internal/eval/eval.go", StartLine: 150, EndLine: 153},
			{Path: "internal/eval/eval.go", StartLine: 157, EndLine: 157},
		},
	},
	{
		ID:          "F3",
		Verdict:     "real",
		Description: "The planned root prompt is only a coverage header, so verification never sees the pull-request text or findings already reported on the PR.",
		Reason:      "Task prompts append pullSection and reportedSection, but this root prompt does not, and the verifier copies req.Prompt as its source context.",
		Sites: []pr38Site{
			{Path: "internal/review/review.go", StartLine: 146, EndLine: 146},
		},
	},
	{
		ID:          "F5",
		Verdict:     "real",
		Description: "Every resumed setup appends the findings exclude rules again, so .git/info/exclude grows on each retry.",
		Reason:      "The retry path opens the exclude file with O_APPEND and writes the same two patterns with no check for lines already present.",
		Sites: []pr38Site{
			{Path: "internal/eval/eval.go", StartLine: 161, EndLine: 165},
		},
	},
	{
		ID:          "F6",
		Verdict:     "real",
		Description: "On a reused directory, setup writes the new fixture base and then git reset --hard HEAD restores the previously committed tree.",
		Reason:      "The base is written at lines 154-156 before the reset at line 169, so a changed fixture is discarded and the review sees the stale committed files.",
		Sites: []pr38Site{
			{Path: "internal/eval/eval.go", StartLine: 154, EndLine: 156},
			{Path: "internal/eval/eval.go", StartLine: 169, EndLine: 169},
		},
	},
	{
		ID:          "F8",
		Verdict:     "weak",
		Description: "Planned revalidation runs git diff on the stored range, which in workspace mode is the name HEAD rather than the pinned SHA.",
		Reason:      "Harm needs HEAD to move during the run while the diff hash stays the same; the check only compares that hash.",
		Sites: []pr38Site{
			{Path: "internal/review/review.go", StartLine: 162, EndLine: 165},
		},
	},
	{
		ID:          "F9",
		Verdict:     "weak",
		Description: "A failed receipt or manifest write still leaves the in-memory cost ahead of the durable ledger.",
		Reason:      "planned.go comments that returned spend is included when a filesystem failure prevents durability, and both sites need a disk write error to matter.",
		Sites: []pr38Site{
			{Path: "internal/agent/planned.go", StartLine: 358, EndLine: 364},
			{Path: "internal/agent/focused.go", StartLine: 259, EndLine: 265},
		},
	},
	{
		ID:          "F11",
		Verdict:     "weak",
		Description: "ReviewPipeline zeroes Harness.Timeout before building the direct reviewer, which reads as if the direct path drops --timeout.",
		Reason:      "That assignment is dead. Planned.Run applies p.Timeout to the context before the direct path and the planned path, so the deadline is not dropped.",
		Sites: []pr38Site{
			{Path: "internal/agent/strategy.go", StartLine: 41, EndLine: 42},
		},
	},
}

func pr38RealCount() int {
	n := 0
	for _, label := range pr38Labels {
		if label.Verdict == "real" {
			n++
		}
	}
	return n
}

// PR38CaseNames is the single pr38 case, for --cases selection.
func PR38CaseNames() []string {
	return []string{pr38Name}
}

// PR38Score is one review of the PR #38 range.
type PR38Score struct {
	Name          string   `json:"name"`
	Run           int      `json:"run,omitempty"`
	Status        string   `json:"status"`
	Hits          int      `json:"hits"`
	Real          int      `json:"real"`
	Recall        float64  `json:"recall"`
	Weak          int      `json:"weak"`
	WeakRate      float64  `json:"weak_rate"`
	Unlabeled     int      `json:"unlabeled"`
	UnlabeledRate float64  `json:"unlabeled_rate"`
	Precision     float64  `json:"precision"`
	ErrorShare    float64  `json:"error_share"`
	Produced      int      `json:"produced"`
	CostUSD       float64  `json:"cost_usd"`
	Requests      int      `json:"requests"`
	DurationMS    int64    `json:"duration_ms"`
	HitIDs        []string `json:"hit_ids"`
	Err           string   `json:"err,omitempty"`
	FindingsPath  string   `json:"findings_path,omitempty"`
}

// PR38Mean averages per-run rates. Counts are means, not sums.
type PR38Mean struct {
	Recall        float64 `json:"recall"`
	Precision     float64 `json:"precision"`
	ErrorShare    float64 `json:"error_share"`
	Hits          float64 `json:"hits"`
	Weak          float64 `json:"weak"`
	WeakRate      float64 `json:"weak_rate"`
	Unlabeled     float64 `json:"unlabeled"`
	UnlabeledRate float64 `json:"unlabeled_rate"`
	Produced      float64 `json:"produced"`
	CostUSD       float64 `json:"cost_usd"`
	Requests      float64 `json:"requests"`
	DurationMS    float64 `json:"duration_ms"`
}

// PR38Report is the machine-readable pr38 result: each run, the mean, and the union of real hits.
type PR38Report struct {
	Corpus    string      `json:"corpus"`
	Note      string      `json:"note"`
	Runs      []PR38Score `json:"runs"`
	Mean      PR38Mean    `json:"mean"`
	UnionHits []string    `json:"union_hits"`
	Union     int         `json:"union"`
	Real      int         `json:"real"`
}

// ScorePR38 classifies findings against the PR #38 labels.
// A finding that overlaps a real label is a hit on every such label and is not weak or unlabeled.
// A finding that overlaps only a weak label counts as one weak finding.
// Anything else is unlabeled.
// Precision uses distinct real labels hit, not the number of findings on those labels.
func ScorePR38(report findings.Report) PR38Score {
	score := PR38Score{Name: pr38Name, Real: pr38RealCount(), HitIDs: []string{}}
	if report.Run != nil {
		score.Status = string(report.Run.Status)
		score.CostUSD = report.Run.Cost.AmountUSD
		score.Requests = report.Run.Cost.Requests
	}
	score.Produced = len(report.Findings)
	hit := map[string]bool{}
	errors := 0
	for _, finding := range report.Findings {
		if finding.Severity == findings.SeverityError {
			errors++
		}
		matchedReal := false
		for _, label := range pr38Labels {
			if label.Verdict != "real" || !overlapsLabel(finding, label) {
				continue
			}
			hit[label.ID] = true
			matchedReal = true
		}
		if matchedReal {
			continue
		}
		if overlapsWeak(finding) {
			score.Weak++
			continue
		}
		score.Unlabeled++
	}
	for _, label := range pr38Labels {
		if label.Verdict == "real" && hit[label.ID] {
			score.Hits++
			score.HitIDs = append(score.HitIDs, label.ID)
		}
	}
	score.Recall = Agreement(score.Hits, score.Real)
	score.Precision = pr38Precision(score.Hits, score.Weak)
	score.WeakRate = pr38Rate(score.Weak, score.Produced)
	score.UnlabeledRate = pr38Rate(score.Unlabeled, score.Produced)
	score.ErrorShare = pr38Rate(errors, score.Produced)
	return score
}

func pr38Precision(hits, weak int) float64 {
	if hits+weak == 0 {
		return 1
	}
	return float64(hits) / float64(hits+weak)
}

func pr38Rate(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func overlapsWeak(finding findings.Finding) bool {
	for _, label := range pr38Labels {
		if label.Verdict == "weak" && overlapsLabel(finding, label) {
			return true
		}
	}
	return false
}

func overlapsLabel(finding findings.Finding, label pr38Label) bool {
	for _, site := range label.Sites {
		gold := Gold{Path: site.Path, StartLine: site.StartLine, EndLine: site.EndLine}
		if sameRegion(finding, gold) {
			return true
		}
	}
	return false
}

// AggregatePR38 averages the runs and unions the real labels hit by any run.
func AggregatePR38(scores []PR38Score) PR38Report {
	report := PR38Report{
		Corpus:    pr38Name,
		Note:      pr38Note,
		Runs:      scores,
		Real:      pr38RealCount(),
		UnionHits: []string{},
	}
	if len(scores) == 0 {
		return report
	}
	seen := map[string]bool{}
	for _, score := range scores {
		report.Mean.Recall += score.Recall
		report.Mean.Precision += score.Precision
		report.Mean.ErrorShare += score.ErrorShare
		report.Mean.Hits += float64(score.Hits)
		report.Mean.Weak += float64(score.Weak)
		report.Mean.WeakRate += score.WeakRate
		report.Mean.Unlabeled += float64(score.Unlabeled)
		report.Mean.UnlabeledRate += score.UnlabeledRate
		report.Mean.Produced += float64(score.Produced)
		report.Mean.CostUSD += score.CostUSD
		report.Mean.Requests += float64(score.Requests)
		report.Mean.DurationMS += float64(score.DurationMS)
		for _, id := range score.HitIDs {
			seen[id] = true
		}
	}
	n := float64(len(scores))
	report.Mean.Recall /= n
	report.Mean.Precision /= n
	report.Mean.ErrorShare /= n
	report.Mean.Hits /= n
	report.Mean.Weak /= n
	report.Mean.WeakRate /= n
	report.Mean.Unlabeled /= n
	report.Mean.UnlabeledRate /= n
	report.Mean.Produced /= n
	report.Mean.CostUSD /= n
	report.Mean.Requests /= n
	report.Mean.DurationMS /= n
	for _, label := range pr38Labels {
		if label.Verdict == "real" && seen[label.ID] {
			report.UnionHits = append(report.UnionHits, label.ID)
		}
	}
	report.Union = len(report.UnionHits)
	return report
}

// RunPR38 checks out the PR #38 range the way a Martian case is checked out
// and reviews that commit. Scoring is separate so tests can classify canned findings.
func RunPR38(ctx context.Context, root string, opts Options) (PR38Score, error) {
	dir := filepath.Join(root, pr38Name)
	head, err := checkoutMartian(ctx, MartianCase{
		Name:  pr38Name,
		Repo:  pr38Repo,
		Base:  pr38Base,
		Head:  pr38Head,
		Title: "PR #38",
	}, dir)
	if err != nil {
		return PR38Score{Name: pr38Name, HitIDs: []string{}}, fmt.Errorf("set up %s: %w", pr38Name, err)
	}
	findingsPath := filepath.Join(root, pr38Name+".findings.jsonl")
	start := time.Now()
	result, runErr := review.Run(ctx, martianReviewOptions(dir, head, findingsPath, opts))
	score := ScorePR38(result.Report)
	score.DurationMS = time.Since(start).Milliseconds()
	score.FindingsPath = findingsPath
	if runErr != nil {
		score.Err = runErr.Error()
	}
	return score, nil
}

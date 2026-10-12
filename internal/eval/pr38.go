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
	pr38Note = "Each finding is assigned to at most one label, the one with the largest line overlap; ties go to a weak label, then to the earlier label. Recall is distinct real labels hit over the five real labels. Precision is those hits over hits plus findings assigned to a weak label, and is n/a when a run has no real hit and no weak finding. A second finding on a real label already hit does not change the numerator. Unlabeled findings match no label, are not errors, and are outside precision; known-weak F4 and other unlabeled clusters sit there, so unlabeled is not a clean precision signal. Means use completed runs only. F1 is one hit if either Fresh site is chosen. A finding only at eval.go:169 is F6."
)

type pr38Site struct {
	Path      string
	StartLine int
	EndLine   int
}

type pr38Label struct {
	ID      string
	Verdict string
	Sites   []pr38Site
}

var pr38Labels = []pr38Label{
	// F1: Fresh: true dropped from eval.Run and martianReviewOptions.
	{ID: "F1", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/eval.go", StartLine: 121, EndLine: 127}, {Path: "internal/eval/martian.go", StartLine: 331, EndLine: 337}}},
	// F2: unborn .git/HEAD is treated as initialized. The reset line belongs to F6.
	{ID: "F2", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/eval.go", StartLine: 150, EndLine: 153}, {Path: "internal/eval/eval.go", StartLine: 157, EndLine: 157}}},
	// F3: the verifier copies the root prompt, not PR text or already-reported findings.
	{ID: "F3", Verdict: "real", Sites: []pr38Site{{Path: "internal/review/review.go", StartLine: 141, EndLine: 152}, {Path: "internal/agent/planned.go", StartLine: 490, EndLine: 492}, {Path: "internal/agent/focused.go", StartLine: 373, EndLine: 375}}},
	// F5: findings exclude rules are appended again on every retry.
	{ID: "F5", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/eval.go", StartLine: 161, EndLine: 165}}},
	// F6: a reused fixture writes a new base, then reset restores the old tree.
	{ID: "F6", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/eval.go", StartLine: 154, EndLine: 156}, {Path: "internal/eval/eval.go", StartLine: 169, EndLine: 169}}},
	// F8: workspace revalidation resolves HEAD again.
	{ID: "F8", Verdict: "weak", Sites: []pr38Site{{Path: "internal/review/review.go", StartLine: 162, EndLine: 165}}},
	// F9: a failed receipt or manifest write leaves the in-memory cost ahead.
	{ID: "F9", Verdict: "weak", Sites: []pr38Site{{Path: "internal/agent/planned.go", StartLine: 358, EndLine: 364}, {Path: "internal/agent/focused.go", StartLine: 259, EndLine: 265}}},
	// F11: ReviewPipeline zeroes Harness.Timeout; Planned.Run still applies it.
	{ID: "F11", Verdict: "weak", Sites: []pr38Site{{Path: "internal/agent/strategy.go", StartLine: 41, EndLine: 42}}},
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

func PR38CaseNames() []string { return []string{pr38Name} }

func PR38Rule() string { return pr38Note }

// Precision is nil when the run has no real hit and no weak finding.
type PR38Score struct {
	Run        int      `json:"run,omitempty"`
	Status     string   `json:"status"`
	Hits       int      `json:"hits"`
	Recall     float64  `json:"recall"`
	Weak       int      `json:"weak"`
	Unlabeled  int      `json:"unlabeled"`
	Precision  *float64 `json:"precision"`
	ErrorShare float64  `json:"error_share"`
	Produced   int      `json:"produced"`
	CostUSD    float64  `json:"cost_usd"`
	Requests   int      `json:"requests"`
	DurationMS int64    `json:"duration_ms"`
	HitIDs     []string `json:"hit_ids"`
	Err        string   `json:"err,omitempty"`
}

// Completed is a finished review whose findings were saved. review.Run can
// return a complete report together with a persistence error.
func (s PR38Score) Completed() bool {
	return s.Status == string(findings.StatusComplete) && s.Err == ""
}

type PR38Mean struct {
	Recall     float64  `json:"recall"`
	Precision  *float64 `json:"precision"`
	ErrorShare float64  `json:"error_share"`
	Produced   float64  `json:"produced"`
	CostUSD    float64  `json:"cost_usd"`
	Requests   float64  `json:"requests"`
	DurationMS float64  `json:"duration_ms"`
}

// PR38Report is the pr38 result. CostUSD is the total over completed runs.
type PR38Report struct {
	Corpus    string      `json:"corpus"`
	Completed int         `json:"completed"`
	Failed    int         `json:"failed"`
	Runs      []PR38Score `json:"runs"`
	Mean      PR38Mean    `json:"mean"`
	CostUSD   float64     `json:"cost_usd"`
	UnionHits []string    `json:"union_hits"`
	Union     int         `json:"union"`
	Real      int         `json:"real"`
}

func ScorePR38(report findings.Report) PR38Score {
	score := PR38Score{HitIDs: []string{}}
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
		idx, ok := assignedLabel(finding)
		if !ok {
			score.Unlabeled++
			continue
		}
		label := pr38Labels[idx]
		if label.Verdict == "real" {
			hit[label.ID] = true
			continue
		}
		score.Weak++
	}
	for _, label := range pr38Labels {
		if label.Verdict == "real" && hit[label.ID] {
			score.Hits++
			score.HitIDs = append(score.HitIDs, label.ID)
		}
	}
	score.Recall = Agreement(score.Hits, pr38RealCount())
	score.Precision = pr38Precision(score.Hits, score.Weak)
	score.ErrorShare = pr38Rate(errors, score.Produced)
	return score
}

func pr38Precision(hits, weak int) *float64 {
	if hits+weak == 0 {
		return nil
	}
	v := float64(hits) / float64(hits+weak)
	return &v
}

func pr38Rate(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func assignedLabel(finding findings.Finding) (int, bool) {
	best, bestN := -1, 0
	for i, label := range pr38Labels {
		n := labelOverlap(finding, label)
		weakWins := n == bestN && best >= 0 && pr38Labels[best].Verdict != "weak" && label.Verdict == "weak"
		if n > 0 && (best < 0 || n > bestN || weakWins) {
			best, bestN = i, n
		}
	}
	return best, best >= 0
}

func labelOverlap(finding findings.Finding, label pr38Label) int {
	best := 0
	for _, site := range label.Sites {
		if finding.Path != site.Path {
			continue
		}
		if n := overlapLen(finding.StartLine, finding.EndLine, site.StartLine, site.EndLine); n > best {
			best = n
		}
	}
	return best
}

func overlapLen(a0, a1, b0, b1 int) int {
	lo, hi := max(a0, b0), min(a1, b1)
	if hi < lo {
		return 0
	}
	return hi - lo + 1
}

func AggregatePR38(scores []PR38Score) PR38Report {
	report := PR38Report{Corpus: pr38Name, Runs: scores, Real: pr38RealCount(), UnionHits: []string{}}
	seen := map[string]bool{}
	var precSum float64
	var precN int
	for _, score := range scores {
		if !score.Completed() {
			report.Failed++
			continue
		}
		report.Completed++
		report.Mean.Recall += score.Recall
		if score.Precision != nil {
			precSum += *score.Precision
			precN++
		}
		report.Mean.ErrorShare += score.ErrorShare
		report.Mean.Produced += float64(score.Produced)
		report.Mean.CostUSD += score.CostUSD
		report.Mean.Requests += float64(score.Requests)
		report.Mean.DurationMS += float64(score.DurationMS)
		report.CostUSD += score.CostUSD
		for _, id := range score.HitIDs {
			seen[id] = true
		}
	}
	if report.Completed > 0 {
		n := float64(report.Completed)
		report.Mean.Recall /= n
		report.Mean.ErrorShare /= n
		report.Mean.Produced /= n
		report.Mean.CostUSD /= n
		report.Mean.Requests /= n
		report.Mean.DurationMS /= n
	}
	if precN > 0 {
		v := precSum / float64(precN)
		report.Mean.Precision = &v
	}
	for _, label := range pr38Labels {
		if label.Verdict == "real" && seen[label.ID] {
			report.UnionHits = append(report.UnionHits, label.ID)
		}
	}
	report.Union = len(report.UnionHits)
	return report
}

func RunPR38(ctx context.Context, root string, opts Options) (PR38Score, error) {
	dir := filepath.Join(root, pr38Name)
	head, err := checkoutMartian(ctx, MartianCase{
		Name: pr38Name, Repo: pr38Repo, Base: pr38Base, Head: pr38Head, Title: "PR #38",
	}, dir)
	if err != nil {
		return PR38Score{}, fmt.Errorf("set up %s: %w", pr38Name, err)
	}
	findingsPath := filepath.Join(root, pr38Name+".findings.jsonl")
	start := time.Now()
	result, runErr := review.Run(ctx, martianReviewOptions(dir, head, findingsPath, opts))
	score := ScorePR38(result.Report)
	score.DurationMS = time.Since(start).Milliseconds()
	if runErr != nil {
		score.Err = runErr.Error()
	}
	return score, nil
}

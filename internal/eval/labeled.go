package eval

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/review"
)

const labeledNote = "Each finding is assigned to at most one label, the one with the largest line overlap; ties go to a weak label, then to the earlier label. Recall is distinct real labels hit over that case's real labels. Precision is those hits over hits plus findings assigned to a weak label, and is n/a when a run has no real hit and no weak finding. A second finding on a real label already hit does not change the numerator. Unlabeled findings match no label and are outside precision, but they still count in the finding total and in error share. Findings anchored to removed lines (anchor=old) use base coordinates and are scored as unlabeled. Means use completed runs only. A completed run with null precision is excluded from the precision mean. The pooled row sums hits, weak findings, cost, and real labels across the selected cases; it counts a run only when every case completed."

// LabeledReport is one labeled-corpus invocation: a report per case and a pooled report.
// Pooled recall is the sum of distinct real hits over the sum of real labels.
// A pooled run is complete only when every selected case completed.
type LabeledReport struct {
	Corpus string       `json:"corpus"`
	Cases  []PR38Report `json:"cases"`
	Pooled PR38Report   `json:"pooled"`
}

func LabeledRule() string { return labeledNote }

func LabeledCaseNames() []string {
	cases := labeledCorpus()
	names := make([]string, len(cases))
	for i, c := range cases {
		names[i] = c.Name
	}
	return names
}

func ScoreLabeled(name string, report findings.Report) (PR38Score, error) {
	c, ok := labeledCaseByName(name)
	if !ok {
		return PR38Score{}, fmt.Errorf("unknown labeled case %q", name)
	}
	return scoreLabels(c.Labels, report), nil
}

// ReportLabeled aggregates each case's runs and pools a run only when that
// repetition has a score for every selected case.
func ReportLabeled(names []string, byCase [][]PR38Score) (LabeledReport, error) {
	if len(names) != len(byCase) {
		return LabeledReport{}, fmt.Errorf("labeled cases: %d names and %d score columns", len(names), len(byCase))
	}
	report := LabeledReport{Corpus: "labeled", Cases: make([]PR38Report, len(names))}
	var ids []string
	n := 0
	if len(byCase) > 0 {
		n = len(byCase[0])
	}
	for i, name := range names {
		c, ok := labeledCaseByName(name)
		if !ok {
			return LabeledReport{}, fmt.Errorf("unknown labeled case %q", name)
		}
		report.Cases[i] = aggregateLabels(c.Name, realIDs(c.Labels), byCase[i])
		ids = append(ids, realIDs(c.Labels)...)
		if len(byCase[i]) < n {
			n = len(byCase[i])
		}
	}
	pooled := make([]PR38Score, 0, n)
	for run := 0; run < n; run++ {
		col := make([]PR38Score, len(names))
		for i := range names {
			col[i] = byCase[i][run]
		}
		pooled = append(pooled, poolLabeled(names, col))
	}
	report.Pooled = aggregateLabels("labeled", ids, pooled)
	return report, nil
}

func poolLabeled(names []string, scores []PR38Score) PR38Score {
	out := PR38Score{HitIDs: []string{}, Status: string(findings.StatusComplete)}
	if len(scores) > 0 {
		out.Run = scores[0].Run
	}
	real := 0
	errors := 0
	var errs []string
	for i, score := range scores {
		c, _ := labeledCaseByName(names[i])
		real += realCount(c.Labels)
		if !score.Completed() {
			out.Status = string(findings.StatusFailed)
			text := score.Err
			if text == "" {
				text = "status " + score.Status
				if score.Status == "" {
					text = "no status"
				}
			}
			errs = append(errs, names[i]+": "+text)
		}
		out.Hits += score.Hits
		out.Weak += score.Weak
		out.Unlabeled += score.Unlabeled
		out.Produced += score.Produced
		out.CostUSD += score.CostUSD
		out.Requests += score.Requests
		out.DurationMS += score.DurationMS
		errors += labeledErrors(score)
		out.HitIDs = append(out.HitIDs, score.HitIDs...)
	}
	if len(errs) > 0 {
		out.Err = strings.Join(errs, "; ")
	}
	out.Recall = Agreement(out.Hits, real)
	out.Precision = pr38Precision(out.Hits, out.Weak)
	out.ErrorShare = pr38Rate(errors, out.Produced)
	return out
}

func labeledErrors(score PR38Score) int {
	if score.Produced == 0 {
		return 0
	}
	return int(math.Round(score.ErrorShare * float64(score.Produced)))
}

func RunLabeled(ctx context.Context, root, name string, opts Options) (PR38Score, error) {
	c, ok := labeledCaseByName(name)
	if !ok {
		return PR38Score{}, fmt.Errorf("unknown labeled case %q", name)
	}
	return runLabeled(ctx, root, c, opts)
}

func runLabeled(ctx context.Context, root string, c labeledCase, opts Options) (PR38Score, error) {
	dir := filepath.Join(root, c.Name)
	head, err := checkoutMartian(ctx, MartianCase{
		Name: c.Name, Repo: c.Repo, Base: c.Base, Head: c.Head, Title: c.Title,
	}, dir)
	if err != nil {
		return PR38Score{}, fmt.Errorf("set up %s: %w", c.Name, err)
	}
	findingsPath := filepath.Join(root, c.Name+".findings.jsonl")
	start := time.Now()
	result, runErr := review.Run(ctx, pr38ReviewOptions(dir, head, findingsPath, opts))
	score := scoreLabels(c.Labels, result.Report)
	score.DurationMS = time.Since(start).Milliseconds()
	if runErr != nil {
		score.Err = runErr.Error()
	}
	return score, nil
}

type labeledCase struct {
	Name   string
	Repo   string
	Base   string
	Head   string
	Title  string
	Labels []pr38Label
}

func labeledCaseByName(name string) (labeledCase, bool) {
	for _, c := range labeledCorpus() {
		if c.Name == name {
			return c, true
		}
	}
	return labeledCase{}, false
}

func labeledCorpus() []labeledCase {
	return []labeledCase{
		{Name: pr38Name, Repo: pr38Repo, Base: pr38Base, Head: pr38Head, Title: "PR #38", Labels: pr38Labels},
		{
			Name: "pr31", Repo: pr38Repo, Title: "PR #31",
			Base:   "89539063f7a3f4489897466aa96a622a9fb8d961",
			Head:   "991513341f4d01c59330c16e90a00871ccba633e",
			Labels: pr31Labels,
		},
		{
			Name: "pr25", Repo: pr38Repo, Title: "PR #25",
			Base:   "123592e8386fdc395809384bed5434bd7159fe59",
			Head:   "dd9e2114d692e362246915f30ed93dc266c9ae4a",
			Labels: pr25Labels,
		},
		{
			Name: "pr3", Repo: pr38Repo, Title: "PR #3",
			Base:   "8ad9a39da6d3e5fa92a8864be4ccdae5254d10fe",
			Head:   "6addc09ff75f1fffce3262cc7eb7b636800e29e6",
			Labels: pr3Labels,
		},
	}
}

var pr31Labels = []pr38Label{
	// MartianProfile's error is discarded, so an unknown profile counts every golden comment as out of profile. Fixed by aef1f4e (#80).
	{ID: "pr31-profile", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/martian.go", StartLine: 197, EndLine: 197}}},
	// filepath.Clean collapses link/.. before EvalSymlinks, so find can list outside the workspace. https://github.com/0x7067/unreal-review/pull/31#discussion_r4135920205
	{ID: "pr31-symlink", Verdict: "real", Sites: []pr38Site{{Path: "internal/agent/bash_boundary.go", StartLine: 108, EndLine: 111}}},
	// An escaped newline is written into the token, so fi\\\nnd reaches the shell as find. https://github.com/0x7067/unreal-review/pull/31#discussion_r4135920218
	{ID: "pr31-newline", Verdict: "real", Sites: []pr38Site{{Path: "internal/agent/bash_boundary.go", StartLine: 153, EndLine: 157}, {Path: "internal/agent/bash_boundary.go", StartLine: 179, EndLine: 180}}},
	// The root check rejects $ and ` but not an unquoted ~, which the shell expands to home. https://github.com/0x7067/unreal-review/pull/31#discussion_r4136392239
	{ID: "pr31-tilde", Verdict: "real", Sites: []pr38Site{{Path: "internal/agent/bash_boundary.go", StartLine: 96, EndLine: 98}}},
	// A non-simple command with no literal find token is handed to the shell. https://github.com/0x7067/unreal-review/pull/31#discussion_r4136392220
	{ID: "pr31-subst", Verdict: "real", Sites: []pr38Site{{Path: "internal/agent/bash_boundary.go", StartLine: 67, EndLine: 71}}},
	// mentionsFind rejects a filename argument that is itself named find. https://github.com/0x7067/unreal-review/pull/31#discussion_r4136345675
	{ID: "pr31-name", Verdict: "real", Sites: []pr38Site{{Path: "internal/agent/bash_boundary.go", StartLine: 73, EndLine: 77}}},
}

var pr25Labels = []pr38Label{
	// The judge uses http.DefaultClient, so a stalled endpoint hangs the eval. Fixed by f727a82 (#62).
	{ID: "pr25-timeout", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/martian.go", StartLine: 266, EndLine: 266}}},
	// Workers call RunMartian with context.Background, so Ctrl+C does not cancel them. Fixed by d8c1aa1 (#35).
	{ID: "pr25-cancel", Verdict: "real", Sites: []pr38Site{{Path: "cmd/unreal-review/eval_martian.go", StartLine: 65, EndLine: 65}, {Path: "cmd/unreal-review/eval.go", StartLine: 79, EndLine: 81}}},
	// Repeated --cases names are appended, so a,a reviews one checkout twice. Fixed by 66884bd (#42).
	{ID: "pr25-cases", Verdict: "real", Sites: []pr38Site{{Path: "cmd/unreal-review/eval_martian.go", StartLine: 101, EndLine: 108}}},
	// The checkout git helper inherits GIT_DIR. Fixed by 7077e36 (#53).
	{ID: "pr25-git-env", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/martian.go", StartLine: 332, EndLine: 337}}},
	// ScoreMartian still runs when the judge returns an error, and the run returns nil. https://github.com/0x7067/unreal-review/pull/25#discussion_r4136583090
	{ID: "pr25-judge", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/martian.go", StartLine: 314, EndLine: 325}}},
}

var pr3Labels = []pr38Label{
	// A successful check older than 50 commits is treated as never reviewed. Fixed by 226faf7 (#58).
	{ID: "pr3-receipt", Verdict: "real", Sites: []pr38Site{{Path: "cmd/unreal-review/pull.go", StartLine: 45, EndLine: 59}}},
	// The planted fixture git helper inherits GIT_DIR. Fixed by 7077e36 (#53).
	{ID: "pr3-git-env", Verdict: "real", Sites: []pr38Site{{Path: "internal/eval/eval.go", StartLine: 139, EndLine: 144}}},
	// Status complete with a persist error still counts as a finished review. Fixed by 9770a25 (#101).
	{ID: "pr3-unsaved", Verdict: "real", Sites: []pr38Site{
		{Path: "internal/eval/eval.go", StartLine: 62, EndLine: 63},
		{Path: "internal/eval/eval.go", StartLine: 129, EndLine: 131},
		{Path: "internal/review/review.go", StartLine: 65, EndLine: 72},
		{Path: "internal/review/review.go", StartLine: 152, EndLine: 157},
	}},
}

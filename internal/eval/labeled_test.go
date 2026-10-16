package eval

import (
	"math"
	"testing"

	"github.com/0x7067/unreal-review/findings"
)

func TestScoreLabeledPR38MatchesThePR38Scorer(t *testing.T) {
	report := pr38Run(findings.StatusComplete, 0.48, 83,
		pr38Finding("internal/eval/eval.go", 121, 127, findings.SeverityError),
		pr38Finding("internal/review/review.go", 162, 165, findings.SeverityError),
		pr38Finding("internal/eval/eval.go", 169, 169, findings.SeverityNote),
	)
	direct := ScorePR38(report)
	via, err := ScoreLabeled("pr38", report)
	if err != nil {
		t.Fatal(err)
	}
	if via.Hits != 2 || direct.Hits != 2 || len(via.HitIDs) != 2 || via.HitIDs[0] != "F1" || via.HitIDs[1] != "F6" ||
		via.Weak != 1 || direct.Weak != 1 || via.Unlabeled != 0 || via.Recall != direct.Recall ||
		via.Precision == nil || direct.Precision == nil || *via.Precision != *direct.Precision ||
		via.ErrorShare != direct.ErrorShare || via.Produced != 3 || via.CostUSD != 0.48 {
		t.Fatalf("labeled pr38=%+v direct=%+v", via, direct)
	}
}

func TestScoreLabeledPR31(t *testing.T) {
	score, err := ScoreLabeled("pr31", pr38Run(findings.StatusComplete, 0.12, 4,
		pr38Finding("internal/eval/martian.go", 197, 197, findings.SeverityError),
		pr38Finding("internal/agent/bash_boundary.go", 108, 111, findings.SeverityError),
		pr38Finding("internal/agent/bash_boundary.go", 179, 180, findings.SeverityError),
		pr38Finding("internal/agent/bash_boundary.go", 153, 157, findings.SeverityNote),
		pr38Finding("internal/agent/bash_boundary.go", 96, 98, findings.SeverityWarning),
		pr38Finding("internal/agent/bash_boundary.go", 67, 71, findings.SeverityError),
		pr38Finding("internal/agent/bash_boundary.go", 73, 77, findings.SeverityNote),
		pr38Finding("internal/eval/martian.go", 183, 183, findings.SeverityError),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pr31-profile", "pr31-symlink", "pr31-newline", "pr31-tilde", "pr31-subst"}
	if score.Hits != 5 || score.Weak != 1 || score.Unlabeled != 1 || score.Produced != 8 || score.Recall != 1 {
		t.Fatalf("hits=%d weak=%d unlabeled=%d produced=%d recall=%v ids=%v", score.Hits, score.Weak, score.Unlabeled, score.Produced, score.Recall, score.HitIDs)
	}
	if len(score.HitIDs) != len(want) {
		t.Fatalf("ids %v", score.HitIDs)
	}
	for i, id := range want {
		if score.HitIDs[i] != id {
			t.Fatalf("ids %v", score.HitIDs)
		}
	}
	if score.Precision == nil || math.Abs(*score.Precision-5.0/6.0) > 1e-12 || math.Abs(score.ErrorShare-5.0/8.0) > 1e-12 {
		t.Fatalf("precision=%v error=%v", score.Precision, score.ErrorShare)
	}
	span, err := ScoreLabeled("pr31", findings.Report{Findings: []findings.Finding{
		pr38Finding("internal/agent/bash_boundary.go", 67, 77, findings.SeverityError),
	}})
	if err != nil || span.Hits != 0 || len(span.HitIDs) != 0 || span.Weak != 1 || span.Unlabeled != 0 {
		t.Fatalf("tie span: err=%v %+v", err, span)
	}
	old := pr38Finding("internal/eval/martian.go", 197, 197, findings.SeverityError)
	old.Anchor = findings.AnchorOld
	anchored, err := ScoreLabeled("pr31", findings.Report{Findings: []findings.Finding{old}})
	if err != nil || anchored.Hits != 0 || anchored.Unlabeled != 1 || anchored.Precision != nil {
		t.Fatalf("anchor old: err=%v %+v", err, anchored)
	}
}

func TestScoreLabeledPR25(t *testing.T) {
	score, err := ScoreLabeled("pr25", pr38Run(findings.StatusComplete, 0.3, 9,
		pr38Finding("internal/eval/martian.go", 266, 266, findings.SeverityError),
		pr38Finding("cmd/unreal-review/eval.go", 79, 81, findings.SeverityWarning),
		pr38Finding("cmd/unreal-review/eval_martian.go", 65, 65, findings.SeverityError),
		pr38Finding("cmd/unreal-review/eval_martian.go", 102, 108, findings.SeverityError),
		pr38Finding("internal/eval/martian.go", 333, 336, findings.SeverityNote),
		pr38Finding("internal/eval/martian.go", 320, 325, findings.SeverityError),
		pr38Finding("internal/eval/martian.go", 197, 197, findings.SeverityError),
	))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pr25-timeout", "pr25-cancel", "pr25-cases", "pr25-git-env"}
	if score.Hits != 4 || score.Recall != 1 || score.Weak != 1 || score.Unlabeled != 1 || score.Produced != 7 {
		t.Fatalf("hits=%d recall=%v weak=%d unlabeled=%d produced=%d ids=%v", score.Hits, score.Recall, score.Weak, score.Unlabeled, score.Produced, score.HitIDs)
	}
	if len(score.HitIDs) != len(want) {
		t.Fatalf("ids %v", score.HitIDs)
	}
	for i, id := range want {
		if score.HitIDs[i] != id {
			t.Fatalf("ids %v", score.HitIDs)
		}
	}
	wide, err := ScoreLabeled("pr25", findings.Report{Findings: []findings.Finding{
		pr38Finding("cmd/unreal-review/eval_martian.go", 65, 108, findings.SeverityError),
	}})
	if err != nil || wide.Hits != 1 || len(wide.HitIDs) != 1 || wide.HitIDs[0] != "pr25-cases" {
		t.Fatalf("wider overlap: err=%v %+v", err, wide)
	}
}

func TestScoreLabeledPR3(t *testing.T) {
	score, err := ScoreLabeled("pr3", pr38Run(findings.StatusComplete, 0.4, 11,
		pr38Finding("cmd/unreal-review/pull.go", 45, 59, findings.SeverityError),
		pr38Finding("internal/eval/eval.go", 140, 144, findings.SeverityError),
		pr38Finding("internal/eval/eval.go", 62, 63, findings.SeverityNote),
		pr38Finding("internal/review/review.go", 152, 157, findings.SeverityWarning),
		pr38Finding("internal/review/review.go", 128, 129, findings.SeverityError),
	))
	if err != nil {
		t.Fatal(err)
	}
	if score.Hits != 3 || score.Recall != 1 || score.Weak != 0 || score.Unlabeled != 2 || score.Produced != 5 ||
		len(score.HitIDs) != 3 || score.HitIDs[0] != "pr3-receipt" || score.HitIDs[1] != "pr3-git-env" || score.HitIDs[2] != "pr3-unsaved" {
		t.Fatalf("score %+v", score)
	}
	if score.Precision == nil || *score.Precision != 1 || math.Abs(score.ErrorShare-3.0/5.0) > 1e-12 {
		t.Fatalf("precision=%v error=%v", score.Precision, score.ErrorShare)
	}
	wide, err := ScoreLabeled("pr3", findings.Report{Findings: []findings.Finding{
		pr38Finding("internal/eval/eval.go", 62, 144, findings.SeverityError),
	}})
	if err != nil || wide.Hits != 1 || len(wide.HitIDs) != 1 || wide.HitIDs[0] != "pr3-git-env" || wide.Unlabeled != 0 {
		t.Fatalf("wider overlap: err=%v %+v", err, wide)
	}
	empty, err := ScoreLabeled("pr3", pr38Run(findings.StatusComplete, 0, 0))
	if err != nil || empty.Hits != 0 || empty.Precision != nil || empty.Recall != 0 {
		t.Fatalf("empty: err=%v %+v", err, empty)
	}
}

func TestReportLabeledPoolsCompleteRunsOnly(t *testing.T) {
	pr31, err := ScoreLabeled("pr31", pr38Run(findings.StatusComplete, 0.10, 2,
		pr38Finding("internal/eval/martian.go", 197, 197, findings.SeverityError),
		pr38Finding("internal/agent/bash_boundary.go", 1, 1, findings.SeverityNote),
	))
	if err != nil {
		t.Fatal(err)
	}
	pr31.Run, pr31.DurationMS = 1, 1000
	pr25, err := ScoreLabeled("pr25", pr38Run(findings.StatusComplete, 0.20, 3,
		pr38Finding("internal/eval/martian.go", 266, 266, findings.SeverityError),
		pr38Finding("internal/eval/martian.go", 267, 267, findings.SeverityError),
		pr38Finding("README.md", 1, 1, findings.SeverityNote),
	))
	if err != nil {
		t.Fatal(err)
	}
	pr25.Run, pr25.DurationMS = 1, 2000
	pr31b := pr31
	pr31b.Run, pr31b.DurationMS, pr31b.CostUSD = 2, 1500, 0.11
	failed, err := ScoreLabeled("pr25", pr38Run(findings.StatusFailed, 9, 40,
		pr38Finding("internal/eval/martian.go", 266, 266, findings.SeverityError),
		pr38Finding("cmd/unreal-review/eval_martian.go", 65, 65, findings.SeverityError),
	))
	if err != nil {
		t.Fatal(err)
	}
	failed.Run, failed.Err = 2, "judge stalled"
	blank, err := ScoreLabeled("pr31", pr38Run(findings.StatusComplete, 0.05, 1,
		pr38Finding("internal/eval/martian.go", 183, 183, findings.SeverityNote),
	))
	if err != nil {
		t.Fatal(err)
	}
	blank.Run = 3
	blank25, err := ScoreLabeled("pr25", pr38Run(findings.StatusComplete, 0.05, 1))
	if err != nil {
		t.Fatal(err)
	}
	blank25.Run = 3

	report, err := ReportLabeled([]string{"pr31", "pr25"}, [][]PR38Score{
		{pr31, pr31b, blank},
		{pr25, failed, blank25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Corpus != "labeled" || len(report.Cases) != 2 || report.Cases[0].Corpus != "pr31" || report.Cases[1].Corpus != "pr25" {
		t.Fatalf("cases %+v", report.Cases)
	}
	if report.Cases[0].Completed != 3 || report.Cases[0].Failed != 0 || report.Cases[0].Real != 5 || report.Cases[0].Union != 1 || report.Cases[0].UnionHits[0] != "pr31-profile" {
		t.Fatalf("pr31 %+v", report.Cases[0])
	}
	if report.Cases[1].Completed != 2 || report.Cases[1].Failed != 1 || math.Abs(report.Cases[1].CostUSD-0.25) > 1e-12 || report.Cases[1].Union != 1 {
		t.Fatalf("pr25 %+v", report.Cases[1])
	}
	if report.Pooled.Completed != 2 || report.Pooled.Failed != 1 || report.Pooled.Real != 9 || len(report.Pooled.Runs) != 3 {
		t.Fatalf("pooled completed=%d failed=%d real=%d runs=%d", report.Pooled.Completed, report.Pooled.Failed, report.Pooled.Real, len(report.Pooled.Runs))
	}
	if report.Pooled.Union != 2 || report.Pooled.UnionHits[0] != "pr31-profile" || report.Pooled.UnionHits[1] != "pr25-timeout" {
		t.Fatalf("union %v", report.Pooled.UnionHits)
	}
	if math.Abs(report.Pooled.CostUSD-0.40) > 1e-12 || math.Abs(report.SpentCostUSD-9.51) > 1e-12 || report.SpentDurationMS != 4500 || math.Abs(report.Pooled.Mean.Recall-(2.0/9)/2) > 1e-12 {
		t.Fatalf("cost=%v spent=%v time=%d recall=%v", report.Pooled.CostUSD, report.SpentCostUSD, report.SpentDurationMS, report.Pooled.Mean.Recall)
	}
	if report.Pooled.Mean.Precision == nil || math.Abs(*report.Pooled.Mean.Precision-1) > 1e-12 {
		t.Fatalf("precision mean %v", report.Pooled.Mean.Precision)
	}
	first := report.Pooled.Runs[0]
	if first.Hits != 2 || first.Unlabeled != 3 || first.Produced != 5 || first.Requests != 5 || first.DurationMS != 3000 ||
		math.Abs(first.Recall-2.0/9) > 1e-12 || math.Abs(first.ErrorShare-3.0/5.0) > 1e-12 || first.Precision == nil || math.Abs(*first.Precision-1) > 1e-12 {
		t.Fatalf("pooled run %+v", first)
	}
	if report.Pooled.Runs[1].Completed() || report.Pooled.Runs[2].Precision != nil {
		t.Fatalf("failed=%v blank precision=%v", report.Pooled.Runs[1].Completed(), report.Pooled.Runs[2].Precision)
	}
	failedRow := report.Pooled.Runs[1]
	if failedRow.Hits != 3 || math.Abs(failedRow.Recall-float64(failedRow.Hits)/float64(report.Pooled.Real)) > 1e-12 {
		t.Fatalf("failed row hits=%d recall=%v real=%d", failedRow.Hits, failedRow.Recall, report.Pooled.Real)
	}
}

func TestReportLabeledOmitsIncompleteRepetitions(t *testing.T) {
	pr31, err := ScoreLabeled("pr31", pr38Run(findings.StatusComplete, 0.10, 1,
		pr38Finding("internal/eval/martian.go", 197, 197, findings.SeverityError),
	))
	if err != nil {
		t.Fatal(err)
	}
	pr31.Run, pr31.DurationMS = 1, 1000
	pr25, err := ScoreLabeled("pr25", pr38Run(findings.StatusComplete, 0.20, 1,
		pr38Finding("internal/eval/martian.go", 266, 266, findings.SeverityError),
	))
	if err != nil {
		t.Fatal(err)
	}
	pr25.Run, pr25.DurationMS = 1, 2000
	extra := pr31
	extra.Run, extra.DurationMS, extra.CostUSD = 2, 3000, 0.30

	report, err := ReportLabeled([]string{"pr31", "pr25"}, [][]PR38Score{
		{pr31, extra},
		{pr25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Pooled.Runs) != 1 || report.Pooled.Completed != 1 || report.Pooled.Failed != 0 || report.Pooled.Real != 9 {
		t.Fatalf("pooled completed=%d failed=%d real=%d runs=%d", report.Pooled.Completed, report.Pooled.Failed, report.Pooled.Real, len(report.Pooled.Runs))
	}
	row := report.Pooled.Runs[0]
	if row.Hits != 2 || math.Abs(row.CostUSD-0.30) > 1e-12 || row.DurationMS != 3000 || math.Abs(row.Recall-float64(row.Hits)/float64(report.Pooled.Real)) > 1e-12 {
		t.Fatalf("pooled run hits=%d cost=%v time=%d recall=%v real=%d", row.Hits, row.CostUSD, row.DurationMS, row.Recall, report.Pooled.Real)
	}
	if math.Abs(report.Pooled.CostUSD-0.30) > 1e-12 || math.Abs(report.SpentCostUSD-0.60) > 1e-12 || report.SpentDurationMS != 6000 {
		t.Fatalf("cost=%v spent=%v time=%d", report.Pooled.CostUSD, report.SpentCostUSD, report.SpentDurationMS)
	}
	if report.Cases[0].Completed != 2 || math.Abs(report.Cases[0].CostUSD-0.40) > 1e-12 || report.Cases[1].Completed != 1 {
		t.Fatalf("cases pr31=%+v pr25=%+v", report.Cases[0], report.Cases[1])
	}
}

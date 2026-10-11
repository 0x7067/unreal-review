package eval

import (
	"math"
	"testing"

	"github.com/0x7067/unreal-review/findings"
)

func TestScorePR38CountsHitWeakUnlabeledAndErrorShare(t *testing.T) {
	report := findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.48, Requests: 83}},
		Findings: []findings.Finding{
			{Path: "internal/eval/martian.go", StartLine: 335, EndLine: 335, Severity: findings.SeverityNote},
			{Path: "internal/eval/eval.go", StartLine: 150, EndLine: 152, Severity: findings.SeverityError},
			{Path: "internal/review/review.go", StartLine: 146, EndLine: 146, Severity: findings.SeverityWarning},
			{Path: "internal/eval/eval.go", StartLine: 161, EndLine: 165, Severity: findings.SeverityNote},
			{Path: "internal/review/review.go", StartLine: 162, EndLine: 165, Severity: findings.SeverityError},
			{Path: "internal/agent/focused.go", StartLine: 260, EndLine: 264, Severity: findings.SeverityError},
			{Path: "internal/agent/strategy.go", StartLine: 41, EndLine: 42, Severity: findings.SeverityError},
			{Path: "internal/agent/planned.go", StartLine: 96, EndLine: 100, Severity: findings.SeverityError},
		},
	}
	score := ScorePR38(report)
	if score.Hits != 4 || score.Real != 5 || score.Recall != 0.8 {
		t.Fatalf("hits=%d real=%d recall=%v, want 4/5 = 0.8 (F6 not cited)", score.Hits, score.Real, score.Recall)
	}
	wantHits := []string{"F1", "F2", "F3", "F5"}
	if len(score.HitIDs) != len(wantHits) {
		t.Fatalf("hit ids %v, want %v", score.HitIDs, wantHits)
	}
	for i, id := range wantHits {
		if score.HitIDs[i] != id {
			t.Fatalf("hit ids %v, want %v", score.HitIDs, wantHits)
		}
	}
	if score.Weak != 3 || score.Unlabeled != 1 || score.Produced != 8 {
		t.Fatalf("weak=%d unlabeled=%d produced=%d, want 3 1 8", score.Weak, score.Unlabeled, score.Produced)
	}
	if math.Abs(score.Precision-4.0/7.0) > 1e-12 {
		t.Fatalf("precision=%v, want 4/7", score.Precision)
	}
	if math.Abs(score.WeakRate-3.0/8.0) > 1e-12 || math.Abs(score.UnlabeledRate-1.0/8.0) > 1e-12 {
		t.Fatalf("weak rate=%v unlabeled rate=%v, want 3/8 and 1/8", score.WeakRate, score.UnlabeledRate)
	}
	if math.Abs(score.ErrorShare-5.0/8.0) > 1e-12 {
		t.Fatalf("error share=%v, want 5/8", score.ErrorShare)
	}
	if score.CostUSD != 0.48 || score.Requests != 83 || score.Status != "complete" {
		t.Fatalf("cost=%v requests=%d status=%s", score.CostUSD, score.Requests, score.Status)
	}

	bothSites := ScorePR38(findings.Report{Findings: []findings.Finding{
		{Path: "internal/eval/eval.go", StartLine: 121, EndLine: 127, Severity: findings.SeverityError},
		{Path: "internal/eval/martian.go", StartLine: 331, EndLine: 337, Severity: findings.SeverityError},
	}})
	if bothSites.Hits != 1 || len(bothSites.HitIDs) != 1 || bothSites.HitIDs[0] != "F1" || bothSites.Weak != 0 || bothSites.Unlabeled != 0 {
		t.Fatalf("both Fresh sites: %+v, want one F1 hit and no weak or unlabeled", bothSites)
	}

	resetOnly := ScorePR38(findings.Report{Findings: []findings.Finding{
		{Path: "internal/eval/eval.go", StartLine: 169, EndLine: 169, Severity: findings.SeverityError},
	}})
	if resetOnly.Hits != 1 || len(resetOnly.HitIDs) != 1 || resetOnly.HitIDs[0] != "F6" {
		t.Fatalf("reset line: hit ids %v, want only F6", resetOnly.HitIDs)
	}
	staleWrite := ScorePR38(findings.Report{Findings: []findings.Finding{
		{Path: "internal/eval/eval.go", StartLine: 154, EndLine: 156, Severity: findings.SeverityError},
	}})
	if staleWrite.Hits != 1 || len(staleWrite.HitIDs) != 1 || staleWrite.HitIDs[0] != "F6" {
		t.Fatalf("base write: hit ids %v, want only F6", staleWrite.HitIDs)
	}
}

func TestScorePR38UnlabeledStaysOutOfPrecision(t *testing.T) {
	onlyUnlabeled := ScorePR38(findings.Report{Findings: []findings.Finding{
		{Path: "internal/agent/planned.go", StartLine: 632, EndLine: 637, Severity: findings.SeverityError},
	}})
	if onlyUnlabeled.Hits != 0 || onlyUnlabeled.Weak != 0 || onlyUnlabeled.Unlabeled != 1 || onlyUnlabeled.Precision != 1 || onlyUnlabeled.Recall != 0 {
		t.Fatalf("unlabeled only: %+v, want precision 1, weak 0, unlabeled 1, recall 0", onlyUnlabeled)
	}
	if onlyUnlabeled.ErrorShare != 1 {
		t.Fatalf("error share=%v, want 1", onlyUnlabeled.ErrorShare)
	}

	onlyWeak := ScorePR38(findings.Report{Findings: []findings.Finding{
		{Path: "internal/agent/planned.go", StartLine: 358, EndLine: 364, Severity: findings.SeverityWarning},
	}})
	if onlyWeak.Hits != 0 || onlyWeak.Weak != 1 || onlyWeak.Unlabeled != 0 || onlyWeak.Precision != 0 || onlyWeak.ErrorShare != 0 {
		t.Fatalf("weak only: %+v, want precision 0, weak 1, error share 0", onlyWeak)
	}
}

func TestAggregatePR38MeansAndUnionOfHits(t *testing.T) {
	first := ScorePR38(findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.50, Requests: 80}},
		Findings: []findings.Finding{
			{Path: "internal/eval/eval.go", StartLine: 124, EndLine: 124, Severity: findings.SeverityError},
		},
	})
	first.Run = 1
	first.DurationMS = 1000
	second := ScorePR38(findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.40, Requests: 60}},
		Findings: []findings.Finding{
			{Path: "internal/eval/eval.go", StartLine: 157, EndLine: 160, Severity: findings.SeverityError},
			{Path: "internal/review/review.go", StartLine: 163, EndLine: 164, Severity: findings.SeverityError},
		},
	})
	second.Run = 2
	second.DurationMS = 3000

	report := AggregatePR38([]PR38Score{first, second})
	if report.Union != 2 || len(report.UnionHits) != 2 || report.UnionHits[0] != "F1" || report.UnionHits[1] != "F2" {
		t.Fatalf("union %v (%d), want F1,F2", report.UnionHits, report.Union)
	}
	if report.Real != 5 || report.Corpus != "pr38" || report.Note == "" {
		t.Fatalf("corpus=%s real=%d note=%q", report.Corpus, report.Real, report.Note)
	}
	if math.Abs(report.Mean.Recall-0.2) > 1e-12 || math.Abs(report.Mean.Precision-0.75) > 1e-12 {
		t.Fatalf("mean recall=%v precision=%v, want 0.2 and 0.75", report.Mean.Recall, report.Mean.Precision)
	}
	if math.Abs(report.Mean.ErrorShare-1) > 1e-12 || math.Abs(report.Mean.Weak-0.5) > 1e-12 {
		t.Fatalf("mean error share=%v weak=%v, want 1 and 0.5", report.Mean.ErrorShare, report.Mean.Weak)
	}
	if math.Abs(report.Mean.CostUSD-0.45) > 1e-12 || math.Abs(report.Mean.Requests-70) > 1e-12 || math.Abs(report.Mean.DurationMS-2000) > 1e-12 {
		t.Fatalf("mean cost=%v requests=%v time=%v, want 0.45, 70, 2000", report.Mean.CostUSD, report.Mean.Requests, report.Mean.DurationMS)
	}
	if math.Abs(report.Mean.Hits-1) > 1e-12 || math.Abs(report.Mean.Produced-1.5) > 1e-12 || math.Abs(report.Mean.Unlabeled) > 1e-12 {
		t.Fatalf("mean hits=%v produced=%v unlabeled=%v", report.Mean.Hits, report.Mean.Produced, report.Mean.Unlabeled)
	}
}

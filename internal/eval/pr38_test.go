package eval

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"

	"github.com/0x7067/unreal-review/findings"
)

func pr38Finding(path string, start, end int, sev findings.Severity) findings.Finding {
	return findings.Finding{Path: path, StartLine: start, EndLine: end, Severity: sev}
}

func pr38Run(status findings.Status, cost float64, requests int, items ...findings.Finding) findings.Report {
	return findings.Report{Run: &findings.Run{Status: status, Cost: findings.Cost{AmountUSD: cost, Requests: requests}}, Findings: items}
}

func TestScorePR38CountsHitWeakUnlabeledAndErrorShare(t *testing.T) {
	score := ScorePR38(pr38Run(findings.StatusComplete, 0.48, 83,
		pr38Finding("internal/eval/martian.go", 335, 335, findings.SeverityNote),
		pr38Finding("internal/eval/eval.go", 150, 152, findings.SeverityError),
		pr38Finding("internal/review/review.go", 146, 146, findings.SeverityWarning),
		pr38Finding("internal/eval/eval.go", 161, 165, findings.SeverityNote),
		pr38Finding("internal/review/review.go", 162, 165, findings.SeverityError),
		pr38Finding("internal/agent/focused.go", 260, 264, findings.SeverityError),
		pr38Finding("internal/agent/strategy.go", 41, 42, findings.SeverityError),
		pr38Finding("internal/agent/planned.go", 96, 100, findings.SeverityError),
	))
	if score.Hits != 4 || score.Recall != 0.8 || score.Weak != 3 || score.Unlabeled != 1 || score.Produced != 8 {
		t.Fatalf("hits=%d recall=%v weak=%d unlabeled=%d produced=%d", score.Hits, score.Recall, score.Weak, score.Unlabeled, score.Produced)
	}
	if len(score.HitIDs) != 4 || score.HitIDs[0] != "F1" || score.HitIDs[1] != "F2" || score.HitIDs[2] != "F3" || score.HitIDs[3] != "F5" {
		t.Fatalf("hit ids %v", score.HitIDs)
	}
	if score.Precision == nil || math.Abs(*score.Precision-4.0/7.0) > 1e-12 || math.Abs(score.ErrorShare-5.0/8.0) > 1e-12 {
		t.Fatalf("precision=%v error=%v", score.Precision, score.ErrorShare)
	}
	if score.CostUSD != 0.48 || score.Requests != 83 || !score.Completed() {
		t.Fatalf("cost=%v requests=%d status=%s", score.CostUSD, score.Requests, score.Status)
	}
	both := ScorePR38(findings.Report{Findings: []findings.Finding{
		pr38Finding("internal/eval/eval.go", 121, 127, findings.SeverityError),
		pr38Finding("internal/eval/martian.go", 331, 337, findings.SeverityError),
	}})
	if both.Hits != 1 || len(both.HitIDs) != 1 || both.HitIDs[0] != "F1" || both.Weak != 0 {
		t.Fatalf("both Fresh sites: %v weak=%d", both.HitIDs, both.Weak)
	}
	copies := ScorePR38(findings.Report{Findings: []findings.Finding{
		pr38Finding("internal/agent/planned.go", 490, 492, findings.SeverityError),
		pr38Finding("internal/agent/focused.go", 373, 375, findings.SeverityError),
	}})
	if copies.Hits != 1 || len(copies.HitIDs) != 1 || copies.HitIDs[0] != "F3" {
		t.Fatalf("verifier copies: %v", copies.HitIDs)
	}
}

func TestScorePR38AssignsOneLabel(t *testing.T) {
	cases := []struct {
		path             string
		start, end       int
		hits, weak, bare int
		id               string
	}{
		{"internal/eval/eval.go", 157, 170, 1, 0, 0, "F5"},
		{"internal/eval/eval.go", 153, 154, 1, 0, 0, "F2"},
		{"internal/review/review.go", 149, 165, 0, 1, 0, ""},
		{"internal/review/review.go", 146, 165, 1, 0, 0, "F3"},
		{"internal/eval/eval.go", 169, 169, 1, 0, 0, "F6"},
		{"internal/eval/eval.go", 154, 156, 1, 0, 0, "F6"},
		{"internal/agent/planned.go", 488, 489, 0, 0, 1, ""},
		{"internal/agent/focused.go", 371, 372, 0, 0, 1, ""},
	}
	for _, tc := range cases {
		score := ScorePR38(findings.Report{Findings: []findings.Finding{pr38Finding(tc.path, tc.start, tc.end, findings.SeverityError)}})
		got := ""
		if len(score.HitIDs) == 1 {
			got = score.HitIDs[0]
		}
		if score.Hits != tc.hits || got != tc.id || score.Weak != tc.weak || score.Unlabeled != tc.bare || len(score.HitIDs) != tc.hits {
			t.Fatalf("%s:%d-%d hits=%v weak=%d unlabeled=%d, want id=%s hits=%d weak=%d unlabeled=%d", tc.path, tc.start, tc.end, score.HitIDs, score.Weak, score.Unlabeled, tc.id, tc.hits, tc.weak, tc.bare)
		}
	}
}

func TestScorePR38UnlabeledAndZeroFindings(t *testing.T) {
	unlabeled := ScorePR38(findings.Report{Findings: []findings.Finding{
		pr38Finding("internal/agent/planned.go", 632, 637, findings.SeverityError),
		pr38Finding("internal/review/review.go", 75, 76, findings.SeverityWarning),
	}})
	if unlabeled.Hits != 0 || unlabeled.Weak != 0 || unlabeled.Unlabeled != 2 || unlabeled.Precision != nil || unlabeled.ErrorShare != 0.5 {
		t.Fatalf("unlabeled: hits=%d weak=%d unlabeled=%d precision=%v error=%v", unlabeled.Hits, unlabeled.Weak, unlabeled.Unlabeled, unlabeled.Precision, unlabeled.ErrorShare)
	}
	weak := ScorePR38(findings.Report{Findings: []findings.Finding{pr38Finding("internal/agent/planned.go", 358, 364, findings.SeverityWarning)}})
	if weak.Weak != 1 || weak.Precision == nil || *weak.Precision != 0 || weak.ErrorShare != 0 {
		t.Fatalf("weak only: weak=%d precision=%v error=%v", weak.Weak, weak.Precision, weak.ErrorShare)
	}
	empty := ScorePR38(pr38Run(findings.StatusComplete, 0, 0))
	raw, err := json.Marshal(empty)
	if err != nil || empty.Produced != 0 || empty.ErrorShare != 0 || math.IsNaN(empty.ErrorShare) || empty.Precision != nil || !bytes.Contains(raw, []byte(`"precision":null`)) {
		t.Fatalf("zero findings: produced=%d error=%v precision=%v err=%v json=%s", empty.Produced, empty.ErrorShare, empty.Precision, err, raw)
	}
}

func TestAggregatePR38MeansUnionAndFailedRuns(t *testing.T) {
	first := ScorePR38(pr38Run(findings.StatusComplete, 0.50, 80, pr38Finding("internal/eval/eval.go", 124, 124, findings.SeverityError)))
	first.Run, first.DurationMS = 1, 1000
	second := ScorePR38(pr38Run(findings.StatusComplete, 0.40, 60,
		pr38Finding("internal/eval/eval.go", 157, 160, findings.SeverityError),
		pr38Finding("internal/review/review.go", 163, 164, findings.SeverityError),
	))
	second.Run, second.DurationMS = 2, 3000
	report := AggregatePR38([]PR38Score{first, second})
	if report.Completed != 2 || report.Failed != 0 || report.Union != 2 || len(report.UnionHits) != 2 || report.UnionHits[0] != "F1" || report.UnionHits[1] != "F2" {
		t.Fatalf("union %v completed=%d", report.UnionHits, report.Completed)
	}
	if report.Mean.Precision == nil || math.Abs(report.Mean.Recall-0.2) > 1e-12 || math.Abs(*report.Mean.Precision-0.75) > 1e-12 || math.Abs(report.Mean.ErrorShare-1) > 1e-12 || math.Abs(report.Mean.Produced-1.5) > 1e-12 {
		t.Fatalf("mean recall=%v precision=%v error=%v findings=%v", report.Mean.Recall, report.Mean.Precision, report.Mean.ErrorShare, report.Mean.Produced)
	}
	if math.Abs(report.Mean.CostUSD-0.45) > 1e-12 || math.Abs(report.CostUSD-0.90) > 1e-12 || math.Abs(report.Mean.Requests-70) > 1e-12 || math.Abs(report.Mean.DurationMS-2000) > 1e-12 {
		t.Fatalf("cost mean=%v total=%v requests=%v time=%v", report.Mean.CostUSD, report.CostUSD, report.Mean.Requests, report.Mean.DurationMS)
	}
	blank := ScorePR38(pr38Run(findings.StatusComplete, 0.10, 4, pr38Finding("internal/review/review.go", 75, 76, findings.SeverityNote)))
	withBlank := AggregatePR38([]PR38Score{first, blank})
	if blank.Precision != nil || withBlank.Completed != 2 || withBlank.Mean.Precision == nil || math.Abs(*withBlank.Mean.Precision-1) > 1e-12 || math.Abs(withBlank.Mean.Recall-0.1) > 1e-12 {
		t.Fatalf("nil precision changed the mean: precision=%v mean=%v recall=%v completed=%d", blank.Precision, withBlank.Mean.Precision, withBlank.Mean.Recall, withBlank.Completed)
	}
	failed := ScorePR38(pr38Run(findings.StatusFailed, 9, 400,
		pr38Finding("internal/eval/eval.go", 121, 127, findings.SeverityError),
		pr38Finding("internal/eval/eval.go", 150, 153, findings.SeverityError),
		pr38Finding("internal/review/review.go", 146, 146, findings.SeverityError),
		pr38Finding("internal/eval/eval.go", 161, 165, findings.SeverityError),
		pr38Finding("internal/eval/eval.go", 169, 169, findings.SeverityError),
	))
	skipped := AggregatePR38([]PR38Score{first, failed})
	if skipped.Completed != 1 || skipped.Failed != 1 || math.Abs(skipped.Mean.Recall-0.2) > 1e-12 || math.Abs(skipped.CostUSD-0.50) > 1e-12 || skipped.Union != 1 || skipped.UnionHits[0] != "F1" {
		t.Fatalf("failed run leaked: completed=%d failed=%d recall=%v cost=%v union=%v", skipped.Completed, skipped.Failed, skipped.Mean.Recall, skipped.CostUSD, skipped.UnionHits)
	}
	onlyFailed := AggregatePR38([]PR38Score{failed})
	raw, err := json.Marshal(onlyFailed)
	if err != nil || onlyFailed.Completed != 0 || onlyFailed.Failed != 1 || onlyFailed.Mean.Precision != nil || onlyFailed.CostUSD != 0 || math.IsNaN(onlyFailed.Mean.Recall) || math.IsNaN(onlyFailed.Mean.ErrorShare) || bytes.Contains(raw, []byte("NaN")) || !bytes.Contains(raw, []byte(`"precision":null`)) {
		t.Fatalf("no completed: err=%v mean=%+v json=%s", err, onlyFailed.Mean, raw)
	}
	none := AggregatePR38(nil)
	if _, err := json.Marshal(none); err != nil || none.Completed != 0 || none.Failed != 0 || math.IsNaN(none.Mean.Recall) || none.Mean.Precision != nil {
		t.Fatalf("nil aggregate: err=%v mean=%+v", err, none.Mean)
	}
}

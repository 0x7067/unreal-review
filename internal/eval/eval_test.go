package eval

import (
	"testing"

	"unreal-review/internal/findings"
)

func TestMatchOverlappingLines(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 21, EndLine: 21}}
	matched, severityHits, extra := Match(gold, produced)
	if matched != 1 || severityHits != 0 || extra != 0 {
		t.Fatalf("matched=%d severityHits=%d extra=%d, want 1 0 0", matched, severityHits, extra)
	}
}

func TestMatchIgnoresDisjointLines(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 5, EndLine: 8}}
	matched, _, extra := Match(gold, produced)
	if matched != 0 {
		t.Fatalf("matched=%d, want 0", matched)
	}
	if extra != 1 {
		t.Fatalf("extra=%d, want 1: an unrelated finding in the same file is not a hit", extra)
	}
}

func TestMatchRequiresSamePath(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{{Path: "other.go", StartLine: 20, EndLine: 23}}
	matched, _, extra := Match(gold, produced)
	if matched != 0 || extra != 1 {
		t.Fatalf("matched=%d extra=%d, want 0 1", matched, extra)
	}
}

func TestMatchSpanningFindingCoversBothGolds(t *testing.T) {
	gold := []Gold{
		{Path: "cache.go", StartLine: 10, EndLine: 12, Severity: findings.SeverityError},
		{Path: "cache.go", StartLine: 20, EndLine: 22, Severity: findings.SeverityError},
	}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 10, EndLine: 22}}
	matched, severityHits, extra := Match(gold, produced)
	if matched != 2 || severityHits != 0 || extra != 0 {
		t.Fatalf("matched=%d severityHits=%d extra=%d, want 2 0 0", matched, severityHits, extra)
	}
}

func TestMatchCountsUnmatchedProducedAsExtra(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{
		{Path: "cache.go", StartLine: 20, EndLine: 23},
		{Path: "cache.go", StartLine: 27, EndLine: 31},
	}
	matched, _, extra := Match(gold, produced)
	if matched != 1 || extra != 1 {
		t.Fatalf("matched=%d extra=%d, want 1 1", matched, extra)
	}
}

func TestMatchSeverityAgreesWhenEqual(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 21, EndLine: 21, Severity: findings.SeverityError}}
	matched, severityHits, extra := Match(gold, produced)
	if matched != 1 || severityHits != 1 || extra != 0 {
		t.Fatalf("matched=%d severityHits=%d extra=%d, want 1 1 0", matched, severityHits, extra)
	}
}

func TestMatchSeverityMissesWhenWeaker(t *testing.T) {
	gold := []Gold{{Path: "server.go", StartLine: 15, EndLine: 19, Severity: findings.SeverityWarning}}
	produced := []findings.Finding{{Path: "server.go", StartLine: 16, EndLine: 18, Severity: findings.SeverityError}}
	matched, severityHits, _ := Match(gold, produced)
	if matched != 1 || severityHits != 0 {
		t.Fatalf("matched=%d severityHits=%d, want 1 0: finding the leak but grading it error is not agreement", matched, severityHits)
	}
}

func TestMatchSeverityHitsAnyOverlappingFinding(t *testing.T) {
	gold := []Gold{{Path: "server.go", StartLine: 15, EndLine: 19, Severity: findings.SeverityWarning}}
	produced := []findings.Finding{
		{Path: "server.go", StartLine: 15, EndLine: 19, Severity: findings.SeverityError},
		{Path: "server.go", StartLine: 16, EndLine: 16, Severity: findings.SeverityWarning},
	}
	_, severityHits, _ := Match(gold, produced)
	if severityHits != 1 {
		t.Fatalf("severityHits=%d, want 1", severityHits)
	}
}

func TestAgreement(t *testing.T) {
	cases := []struct {
		hits, total int
		want        float64
	}{
		{0, 0, 1},
		{3, 3, 1},
		{1, 4, 0.25},
		{0, 2, 0},
	}
	for _, tc := range cases {
		if got := Agreement(tc.hits, tc.total); got != tc.want {
			t.Fatalf("Agreement(%d, %d)=%f, want %f", tc.hits, tc.total, got, tc.want)
		}
	}
}

func TestRecallIsOneWithoutGold(t *testing.T) {
	score := Score{Name: "clean"}
	if score.Recall() != 1 || score.SeverityAgreement() != 1 {
		t.Fatalf("recall=%f severity=%f, want 1 1 when nothing was planted", score.Recall(), score.SeverityAgreement())
	}
}

func TestPrecisionIsOneWithoutFindings(t *testing.T) {
	score := Score{Name: "clean", Gold: 0}
	if score.Precision() != 1 {
		t.Fatalf("precision=%f, want 1 when nothing was produced", score.Precision())
	}
}

func TestScoreReportReadsRunStatus(t *testing.T) {
	c := Case{Name: "race", Gold: []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}}
	report := findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.5, Requests: 2}},
		Findings: []findings.Finding{
			{Path: "cache.go", StartLine: 21, EndLine: 21, Severity: findings.SeverityError},
		},
	}
	score := ScoreReport(c, report)
	if !score.Completed() {
		t.Fatal("want completed")
	}
	if score.Matched != 1 || score.SeverityHits != 1 || score.Produced != 1 || score.Requests != 2 || score.CostUSD != 0.5 {
		t.Fatalf("score=%+v", score)
	}
	if score.Status != "complete" {
		t.Fatalf("status=%q", score.Status)
	}
}

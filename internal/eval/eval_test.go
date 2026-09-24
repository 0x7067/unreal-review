package eval

import (
	"testing"

	"unreal-review/internal/findings"
)

func TestMatchOverlappingLines(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23}}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 21, EndLine: 21}}
	matched, extra := Match(gold, produced)
	if matched != 1 || extra != 0 {
		t.Fatalf("matched=%d extra=%d, want 1 0", matched, extra)
	}
}

func TestMatchIgnoresDisjointLines(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23}}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 5, EndLine: 8}}
	matched, extra := Match(gold, produced)
	if matched != 0 {
		t.Fatalf("matched=%d, want 0", matched)
	}
	if extra != 1 {
		t.Fatalf("extra=%d, want 1: an unrelated finding in the same file is not a hit", extra)
	}
}

func TestMatchRequiresSamePath(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23}}
	produced := []findings.Finding{{Path: "other.go", StartLine: 20, EndLine: 23}}
	matched, extra := Match(gold, produced)
	if matched != 0 || extra != 1 {
		t.Fatalf("matched=%d extra=%d, want 0 1", matched, extra)
	}
}

func TestMatchSpanningFindingCoversBothGolds(t *testing.T) {
	gold := []Gold{
		{Path: "cache.go", StartLine: 10, EndLine: 12},
		{Path: "cache.go", StartLine: 20, EndLine: 22},
	}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 10, EndLine: 22}}
	matched, extra := Match(gold, produced)
	if matched != 2 || extra != 0 {
		t.Fatalf("matched=%d extra=%d, want 2 0", matched, extra)
	}
}

func TestMatchCountsUnmatchedProducedAsExtra(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23}}
	produced := []findings.Finding{
		{Path: "cache.go", StartLine: 20, EndLine: 23},
		{Path: "cache.go", StartLine: 27, EndLine: 31},
	}
	matched, extra := Match(gold, produced)
	if matched != 1 || extra != 1 {
		t.Fatalf("matched=%d extra=%d, want 1 1", matched, extra)
	}
}

func TestRecallIsOneWithoutGold(t *testing.T) {
	score := Score{Name: "clean"}
	if score.Recall() != 1 {
		t.Fatalf("recall=%f, want 1 when nothing was planted", score.Recall())
	}
}

func TestPrecisionIsOneWithoutFindings(t *testing.T) {
	score := Score{Name: "clean", Gold: 0}
	if score.Precision() != 1 {
		t.Fatalf("precision=%f, want 1 when nothing was produced", score.Precision())
	}
}

func TestScoreReportReadsRunStatus(t *testing.T) {
	c := Case{Name: "race", Gold: []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23}}}
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
	if score.Matched != 1 || score.Produced != 1 || score.Requests != 2 || score.CostUSD != 0.5 {
		t.Fatalf("score=%+v", score)
	}
	if score.Status != "complete" {
		t.Fatalf("status=%q", score.Status)
	}
}

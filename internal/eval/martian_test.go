package eval

import (
	"math"
	"testing"

	"unreal-review/internal/findings"
)

func TestMartianSeverityMapsToReviewSeverity(t *testing.T) {
	cases := map[string]findings.Severity{
		"Critical": findings.SeverityError,
		"High":     findings.SeverityError,
		"Medium":   findings.SeverityWarning,
		"Low":      findings.SeverityNote,
	}
	for martian, want := range cases {
		if got := MartianSeverity(martian); got != want {
			t.Fatalf("MartianSeverity(%q)=%q, want %q", martian, got, want)
		}
	}
}

func TestMartianCorpusMatchesPublishedCounts(t *testing.T) {
	all, err := MartianCorpus()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 50 {
		t.Fatalf("cases=%d, want 50", len(all))
	}
	want := map[string]int{"strict": 139, "core": 158, "all": 173}
	for profile, count := range want {
		keep, err := MartianProfile(profile)
		if err != nil {
			t.Fatal(err)
		}
		got := 0
		for _, c := range all {
			for _, comment := range c.Comments {
				if keep[comment.Category] {
					got++
				}
			}
		}
		if got != count {
			t.Fatalf("profile %s: comments=%d, want %d", profile, got, count)
		}
	}
	if _, err := MartianProfile("loose"); err == nil {
		t.Fatal("want error for unknown profile")
	}
}

func TestScoreMartianLeaderboardMetricsPerProfile(t *testing.T) {
	c := MartianCase{Name: "pr", Comments: []MartianComment{
		{Comment: "nil deref", Severity: "High", Category: "bug"},
		{Comment: "rename", Severity: "Low", Category: "style"},
		{Comment: "slow loop", Severity: "Medium", Category: "perf"},
		{Comment: "sql injection", Severity: "Critical", Category: "security"},
	}}
	report := findings.Report{Findings: make([]findings.Finding, 5)}
	score := ScoreMartian(c, report, []Pair{{Golden: 0, Finding: 4}, {Golden: 1, Finding: 0}}, "strict")
	want := map[string]Counts{
		"strict": {TP: 1, FP: 3, FN: 1},
		"core":   {TP: 1, FP: 3, FN: 2},
		"all":    {TP: 2, FP: 3, FN: 2},
	}
	for profile, counts := range want {
		if score.ByProfile[profile] != counts {
			t.Fatalf("%s=%+v, want %+v", profile, score.ByProfile[profile], counts)
		}
	}
	if score.Gold != 2 || score.Matched != 1 || score.Extra != 3 || score.MatchedExcluded != 1 {
		t.Fatalf("score=%+v excluded=%d: a style match is excluded from strict, not an extra", score.Score, score.MatchedExcluded)
	}
	all := SumProfiles([]MartianScore{score, score})["all"]
	if all != (Counts{TP: 4, FP: 6, FN: 4}) {
		t.Fatalf("sum=%+v", all)
	}
	if all.Precision() != 0.4 || all.Recall() != 0.5 || math.Abs(all.F1()-4.0/9) > 1e-12 {
		t.Fatalf("p=%v r=%v f1=%v", all.Precision(), all.Recall(), all.F1())
	}
	if empty := (Counts{FN: 3}); empty.Precision() != 0 || empty.F1() != 0 {
		t.Fatalf("no findings: p=%v f1=%v, want 0 as Martian scores it", empty.Precision(), empty.F1())
	}
}

func TestParsePairsDropsOutOfBoundsAndDuplicates(t *testing.T) {
	reply := "Here you go:\n```json\n" + `{"matches":[{"golden":0,"finding":1},{"golden":0,"finding":2},{"golden":1,"finding":1},{"golden":2,"finding":0},{"golden":-1,"finding":0},{"golden":1,"finding":3},{"golden":1,"finding":0}]}` + "\n```"
	pairs, err := parsePairs(reply, 2, 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []Pair{{Golden: 0, Finding: 1}, {Golden: 1, Finding: 0}}
	if len(pairs) != len(want) {
		t.Fatalf("pairs=%+v, want %+v", pairs, want)
	}
	for i := range want {
		if pairs[i] != want[i] {
			t.Fatalf("pairs=%+v, want %+v", pairs, want)
		}
	}
}

func TestParsePairsRejectsProse(t *testing.T) {
	if _, err := parsePairs("no matches found", 1, 1); err == nil {
		t.Fatal("want error for a reply without JSON")
	}
}

func TestScoreMartianTalliesPerSeverity(t *testing.T) {
	c := MartianCase{Name: "pr", Repo: "o/r", Comments: []MartianComment{
		{Comment: "race", Severity: "Critical", Category: "bug"},
		{Comment: "leak", Severity: "High", Category: "bug"},
		{Comment: "off by one", Severity: "Medium", Category: "bug"},
		{Comment: "typo", Severity: "Low", Category: "bug"},
	}}
	report := findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.25, Requests: 3}},
		Findings: []findings.Finding{
			{Severity: findings.SeverityError},
			{Severity: findings.SeverityError},
			{Severity: findings.SeverityNote},
		},
	}
	score := ScoreMartian(c, report, []Pair{{Golden: 0, Finding: 0}, {Golden: 2, Finding: 1}}, "all")
	if score.Gold != 4 || score.Matched != 2 || score.SeverityHits != 1 || score.Extra != 1 || !score.Completed() {
		t.Fatalf("score=%+v", score.Score)
	}
	critical, medium, low := score.BySeverity["Critical"], score.BySeverity["Medium"], score.BySeverity["Low"]
	if critical != (Tally{Gold: 1, Matched: 1, SeverityHits: 1}) {
		t.Fatalf("critical=%+v", critical)
	}
	if medium != (Tally{Gold: 1, Matched: 1, SeverityHits: 0}) {
		t.Fatalf("medium=%+v: an error finding for a Medium comment is not agreement", medium)
	}
	if low != (Tally{Gold: 1}) {
		t.Fatalf("low=%+v", low)
	}
	sum := SumBySeverity([]MartianScore{score, score})
	if sum["Critical"] != (Tally{Gold: 2, Matched: 2, SeverityHits: 2}) || sum["High"] != (Tally{Gold: 2}) {
		t.Fatalf("sum=%+v", sum)
	}
}

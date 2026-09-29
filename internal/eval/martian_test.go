package eval

import (
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
		cases, err := MartianProfile(all, profile)
		if err != nil {
			t.Fatal(err)
		}
		got := 0
		for _, c := range cases {
			got += len(c.Comments)
		}
		if got != count || len(cases) != 50 {
			t.Fatalf("profile %s: comments=%d cases=%d, want %d 50", profile, got, len(cases), count)
		}
	}
}

func TestMartianProfileDropsOutOfProfileCategories(t *testing.T) {
	cases := []MartianCase{{Name: "pr", Comments: []MartianComment{
		{Comment: "nil deref", Category: "bug"},
		{Comment: "rename", Category: "style"},
		{Comment: "slow loop", Category: "perf"},
	}}}
	strict, err := MartianProfile(cases, "strict")
	if err != nil {
		t.Fatal(err)
	}
	if len(strict[0].Comments) != 1 || strict[0].Comments[0].Comment != "nil deref" {
		t.Fatalf("strict=%+v", strict[0].Comments)
	}
	if len(cases[0].Comments) != 3 {
		t.Fatal("profile filtering mutated the input")
	}
	if _, err := MartianProfile(cases, "loose"); err == nil {
		t.Fatal("want error for unknown profile")
	}
}

func TestParsePairsDropsOutOfBoundsAndDuplicates(t *testing.T) {
	reply := "Here you go:\n```json\n" + `{"matches":[{"golden":0,"finding":1},{"golden":0,"finding":2},{"golden":1,"finding":1},{"golden":2,"finding":0},{"golden":-1,"finding":0},{"golden":1,"finding":3},{"golden":1,"finding":0}]}` + "\n```"
	pairs, err := ParsePairs(reply, 2, 3)
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
	if _, err := ParsePairs("no matches found", 1, 1); err == nil {
		t.Fatal("want error for a reply without JSON")
	}
}

func TestScoreMartianTalliesPerSeverity(t *testing.T) {
	c := MartianCase{Name: "pr", Repo: "o/r", Comments: []MartianComment{
		{Comment: "race", Severity: "Critical"},
		{Comment: "leak", Severity: "High"},
		{Comment: "off by one", Severity: "Medium"},
		{Comment: "typo", Severity: "Low"},
	}}
	report := findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.25, Requests: 3}},
		Findings: []findings.Finding{
			{Severity: findings.SeverityError},
			{Severity: findings.SeverityError},
			{Severity: findings.SeverityNote},
		},
	}
	score := ScoreMartian(c, report, []Pair{{Golden: 0, Finding: 0}, {Golden: 2, Finding: 1}})
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

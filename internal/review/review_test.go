package review

import (
	"strings"
	"testing"

	"unreal-review/internal/findings"
)

func TestReportedInKeepsOnlyTouchedFiles(t *testing.T) {
	reported := []findings.Finding{
		{ID: "a", Path: "src/touched.go", StartLine: 1, EndLine: 2, Severity: findings.SeverityWarning, Body: "First."},
		{ID: "b", Path: "src/untouched.go", StartLine: 5, EndLine: 5, Severity: findings.SeverityError, Body: "Second."},
	}
	got := reportedIn(reported, []ChangedFile{{Path: "src/touched.go", Added: 2}})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("reported: %+v", got)
	}
	if reportedIn(nil, []ChangedFile{{Path: "src/touched.go"}}) != nil {
		t.Fatal("nothing reported should stay empty")
	}
}

func TestReportedSectionListsEachFindingOnce(t *testing.T) {
	if got := reportedSection(nil); got != "" {
		t.Fatalf("an empty list should add nothing to the prompt: %q", got)
	}
	got := reportedSection([]findings.Finding{
		{Path: "src/foo.go", StartLine: 12, EndLine: 14, Severity: findings.SeverityWarning, Body: "This map write\nraces with the reader."},
		{Path: "src/gone.go", Severity: findings.SeverityNote, Body: "Line unknown."},
	})
	want := "Already reported on this pull request:\n" +
		"- `src/foo.go` 12-14 warning: This map write races with the reader.\n" +
		"- `src/gone.go` note: Line unknown.\n" +
		"\nReport a problem this list does not cover, or a material change in one it does. Do not restate it.\n\n"
	if got != want {
		t.Fatalf("section:\n%q\nwant:\n%q", got, want)
	}
}

func TestReviewPromptCarriesTheReportedList(t *testing.T) {
	prompt := reviewPrompt("abc123", "def456", "/tmp/f.jsonl", "diff --git a/x b/x\n", []findings.Finding{
		{Path: "x", StartLine: 1, EndLine: 1, Severity: findings.SeverityError, Body: "Boom."},
	})
	if !strings.Contains(prompt, "From: abc123\nTo: def456\n") {
		t.Fatalf("prompt:\n%s", prompt)
	}
	reported := strings.Index(prompt, "Already reported on this pull request:")
	diff := strings.Index(prompt, "```diff")
	if reported < 0 || diff < 0 || reported > diff {
		t.Fatalf("the reported list should come before the diff:\n%s", prompt)
	}
}

func TestReviewPromptWithoutReportedFindings(t *testing.T) {
	prompt := reviewPrompt("main", "", "/tmp/f.jsonl", "diff --git a/x b/x\n", nil)
	if strings.Contains(prompt, "Already reported") {
		t.Fatalf("prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "To: working tree\n") {
		t.Fatalf("prompt:\n%s", prompt)
	}
}

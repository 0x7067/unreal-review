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
		{ID: "abcd1234abcd1234", Path: "src/foo.go", StartLine: 12, EndLine: 14, Severity: findings.SeverityWarning, Body: "This map write\nraces with the reader."},
		{ID: "deadbeefdeadbeef", Path: "src/gone.go", Severity: findings.SeverityNote, Body: "Line unknown."},
	})
	want := "Already reported on this pull request:\n" +
		"- id `abcd1234abcd1234` `src/foo.go` 12-14 warning: This map write races with the reader.\n" +
		"- id `deadbeefdeadbeef` `src/gone.go` note: Line unknown.\n" +
		"\nReport a problem this list does not cover, or a material change in one it does. Do not restate it. If this diff fixes one of these, call resolve_finding with its id and one sentence on how; do not re-record it.\n\n"
	if got != want {
		t.Fatalf("section:\n%q\nwant:\n%q", got, want)
	}
	if !strings.Contains(got, "id `abcd1234abcd1234`") {
		t.Fatalf("section should show the finding id:\n%s", got)
	}
}

func TestReviewPromptCarriesTheReportedList(t *testing.T) {
	prompt := reviewPrompt(selection{
		diff:   "diff --git a/x b/x\n",
		source: findings.Source{Base: "abc123", Head: "def456"},
		files:  []ChangedFile{{Path: "x"}},
		resolved: resolved{pull: Pull{Reported: []findings.Finding{
			{Path: "x", StartLine: 1, EndLine: 1, Severity: findings.SeverityError, Body: "Boom."},
		}}},
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
	prompt := reviewPrompt(selection{diff: "diff --git a/x b/x\n", source: findings.Source{Base: "main"}})
	if strings.Contains(prompt, "Already reported") || strings.Contains(prompt, "pull request author") {
		t.Fatalf("prompt:\n%s", prompt)
	}
	if !strings.Contains(prompt, "To: working tree\n") {
		t.Fatalf("prompt:\n%s", prompt)
	}
}

func TestNewResolverValidatesAgainstOpenFindings(t *testing.T) {
	open := []findings.Finding{{ID: "abcd1234abcd1234", Path: "src/foo.go"}}
	resolve := newResolver(open, nil)

	if _, err := resolve(findings.Resolution{ID: "abcd1234abcd1234", Body: "Added the nil check back."}); err != nil {
		t.Fatalf("open id should be accepted: %v", err)
	}
	if _, err := resolve(findings.Resolution{ID: "deadbeefdeadbeef", Body: "Not tracked."}); err == nil {
		t.Fatal("an id not on the open list should be rejected")
	}
	if _, err := resolve(findings.Resolution{ID: "abcd1234abcd1234", Body: "Again."}); err == nil {
		t.Fatal("a second resolution of the same id in one run should be rejected")
	}
}

func TestNewResolverSeedsAlreadyResolvedFromPriorWork(t *testing.T) {
	open := []findings.Finding{{ID: "abcd1234abcd1234", Path: "src/foo.go"}}
	resolve := newResolver(open, []findings.Resolution{{ID: "abcd1234abcd1234", Body: "Fixed before resume."}})

	if _, err := resolve(findings.Resolution{ID: "abcd1234abcd1234", Body: "Again."}); err == nil {
		t.Fatal("a resolution already recorded before resume should be rejected")
	}
}

func TestReviewPromptCarriesThePullRequestIntentBeforeTheDiff(t *testing.T) {
	prompt := reviewPrompt(selection{
		diff:     "diff --git a/x b/x\n",
		source:   findings.Source{Base: "abc123", Head: "def456"},
		resolved: resolved{pull: Pull{Title: "Cache user lookups", Description: "Adds an LRU in front of the user store."}},
	})
	title := strings.Index(prompt, "Title: Cache user lookups\n")
	description := strings.Index(prompt, "Description:\nAdds an LRU in front of the user store.\n")
	diff := strings.Index(prompt, "```diff")
	if title < 0 || description < title || diff < description {
		t.Fatalf("the title and description should come before the diff:\n%s", prompt)
	}
}

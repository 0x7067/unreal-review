package render

import (
	"fmt"
	"strings"
	"testing"

	"unreal-review/internal/diffmap"
	"unreal-review/internal/findings"
	"unreal-review/internal/github"
)

func TestGitHubSuppressesFindingsItAlreadyPosted(t *testing.T) {
	already := newFinding("src/foo.go", 12, 14, "This map write races with the reader.")
	fresh := newFinding("src/bar.go", 3, 3, "A new problem.")

	result := GitHub(reportOf(fresh, already), GitHubOptions{Posted: []github.PostedComment{postedComment(already)}})

	if len(result.Payload.Review.Comments) != 1 {
		t.Fatalf("comments: %+v", result.Payload.Review.Comments)
	}
	if result.Payload.Review.Comments[0].Path != "src/bar.go" {
		t.Fatalf("posted the wrong finding: %+v", result.Payload.Review.Comments[0])
	}
	if len(result.Duplicates) != 1 || result.Duplicates[0].ID != already.ID {
		t.Fatalf("duplicates: %+v", result.Duplicates)
	}
	if strings.Contains(result.Payload.Review.Body, "src/foo.go") {
		t.Fatalf("the review body repeats a finding already on the pull request:\n%s", result.Payload.Review.Body)
	}
}

func TestGitHubSuppressionDoesNotConsumeTheCommentCap(t *testing.T) {
	var (
		already  []findings.Finding
		comments []github.PostedComment
	)
	for i := range maxInlineComments {
		item := newFinding("src/foo.go", i+1, i+1, fmt.Sprintf("An old problem %d.", i))
		already = append(already, item)
		comments = append(comments, postedComment(item))
	}
	fresh := newFinding("src/bar.go", 200, 200, "A new problem.")

	result := GitHub(reportOf(append(already, fresh)...), GitHubOptions{Posted: comments})

	if len(result.Payload.Review.Comments) != 1 {
		t.Fatalf("the one new finding should still fit under the cap: %d comments, %d dropped",
			len(result.Payload.Review.Comments), len(result.Dropped))
	}
	if len(result.Duplicates) != maxInlineComments {
		t.Fatalf("duplicates: %d, want %d", len(result.Duplicates), maxInlineComments)
	}
}

func TestGitHubPostReview(t *testing.T) {
	already := newFinding("src/foo.go", 12, 14, "This map write races with the reader.")
	fresh := newFinding("src/bar.go", 3, 3, "A new problem.")
	lines := diffLines(t, "src/foo.go", "@@ -1,2 +1,3 @@\n keep\n+added\n keep\n")
	offDiff := newFinding("src/foo.go", 90, 90, "A finding on a line the pull request does not touch.")

	for _, tc := range []struct {
		name   string
		report findings.Report
		opts   GitHubOptions
		want   bool
	}{
		{"a new finding", reportOf(fresh), GitHubOptions{}, true},
		{"only findings already posted", reportOf(already), GitHubOptions{Posted: []github.PostedComment{postedComment(already)}}, false},
		{"no findings at all", reportOf(), GitHubOptions{}, false},
		{"a finding outside the pull request diff", reportOf(offDiff), GitHubOptions{Lines: lines, HasLines: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := GitHub(tc.report, tc.opts).PostReview(); got != tc.want {
				t.Fatalf("PostReview() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGitHubCommentCarriesItsFingerprint(t *testing.T) {
	item := newFinding("src/foo.go", 12, 14, "This map write races with the reader.")
	comment := githubComment(item)

	id, ok := github.ParseFinding(comment.Body)
	if !ok || id != item.ID {
		t.Fatalf("marker = %q, %v; want %q", id, ok, item.ID)
	}
	if !strings.HasPrefix(comment.Body, "**warning**\n\nThis map write races with the reader.\n\n") {
		t.Fatalf("body: %q", comment.Body)
	}
}

func TestReportedFindingsReadsBackWhatWasPosted(t *testing.T) {
	right := newFinding("src/foo.go", 12, 14, "This map write races with the reader.")
	left := newFinding("src/gone.go", 7, 7, "This guard was removed.")
	left.Anchor = findings.AnchorOld
	left.ID = findings.Fingerprint(left)
	leftComment := githubComment(left)

	got := ReportedFindings([]github.PostedComment{
		postedComment(right),
		{Path: left.Path, StartLine: 7, EndLine: 7, Side: leftComment.Side, Body: leftComment.Body},
		{Path: "x.go", StartLine: 1, EndLine: 1, Side: "RIGHT", Body: `<!-- devin-review-comment {"id": "BUG_x_0001"} -->`},
		{Path: "y.go", StartLine: 2, EndLine: 2, Side: "RIGHT", Body: "a human comment"},
	})

	if len(got) != 2 {
		t.Fatalf("reported: %+v", got)
	}
	if got[0] != right {
		t.Fatalf("right side: %+v want %+v", got[0], right)
	}
	if got[1] != left {
		t.Fatalf("left side: %+v want %+v", got[1], left)
	}
}

func TestStatusBodyCarriesTheProgressMarker(t *testing.T) {
	result := GitHub(reportOf(newFinding("src/bar.go", 3, 3, "A new problem.")), GitHubOptions{})
	body := StatusBody(RunSummary{
		HeadSHA: "42227a3148c46baf3636706415dd01840062297b",
		Runs:    3,
		Cost:    findings.Cost{Currency: "USD", AmountUSD: 0.007011, InputTokens: 41586, OutputTokens: 9299, Requests: 3},
		Total:   0.021134,
		Result:  result,
		Summary: "One race in the cache.",
	})

	status, ok := github.ParseStatus(body)
	if !ok {
		t.Fatalf("no status marker in:\n%s", body)
	}
	if status.Head != "42227a3148c46baf3636706415dd01840062297b" || status.Runs != 3 || status.CostUSD != 0.021134 {
		t.Fatalf("status: %+v", status)
	}
	if !strings.Contains(body, "reviewed through `42227a3`") {
		t.Fatalf("body does not name the reviewed head:\n%s", body)
	}
	if !strings.Contains(body, "1 new, 0 already reported, 0 not postable") {
		t.Fatalf("body does not report the counts:\n%s", body)
	}
	if !strings.Contains(body, "One race in the cache.") {
		t.Fatalf("body drops the summary:\n%s", body)
	}
	if _, ok := github.ParseFinding(body); ok {
		t.Fatal("a status body must not read as a finding marker")
	}
}

func newFinding(path string, start, end int, body string) findings.Finding {
	item := findings.Finding{
		Path:      path,
		StartLine: start,
		EndLine:   end,
		Anchor:    findings.AnchorNew,
		Severity:  findings.SeverityWarning,
		Body:      body,
	}
	item.ID = findings.Fingerprint(item)
	return item
}

func postedComment(item findings.Finding) github.PostedComment {
	return github.PostedComment{
		Path:      item.Path,
		StartLine: item.StartLine,
		EndLine:   item.EndLine,
		Side:      "RIGHT",
		Body:      githubComment(item).Body,
	}
}

func reportOf(items ...findings.Finding) findings.Report {
	return findings.Report{
		Run: &findings.Run{
			Status: findings.StatusComplete,
			Cost:   findings.Cost{Currency: "USD", AmountUSD: 0.007011, InputTokens: 41586, OutputTokens: 9299, Requests: 3},
		},
		Findings: items,
		Summary:  "One race in the cache.",
	}
}

func diffLines(t *testing.T, path, patch string) diffmap.Map {
	t.Helper()
	lines, err := diffmap.ParseGitDiff(strings.NewReader(fmt.Sprintf("diff --git a/%s b/%s\n+++ b/%s\n%s", path, path, path, patch)))
	if err != nil {
		t.Fatal(err)
	}
	return lines
}

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

func TestGitHubEmptyFindingsPostLGTM(t *testing.T) {
	result := GitHub(reportOf(), GitHubOptions{})
	if !result.LGTM || result.Payload.Review.Body != "LGTM" {
		t.Fatalf("empty findings must post LGTM: lgtm=%v body=%q", result.LGTM, result.Payload.Review.Body)
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
		{"no findings at all", reportOf(), GitHubOptions{}, true},
		{"a finding outside the pull request diff", reportOf(offDiff), GitHubOptions{Lines: lines, HasLines: true}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := GitHub(tc.report, tc.opts).PostReview(); got != tc.want {
				t.Fatalf("PostReview() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGitHubOutsideDiffDropIsPostedOnceAndKeepsTheReceipt(t *testing.T) {
	lines := diffLines(t, "src/foo.go", "@@ -1,2 +1,3 @@\n keep\n+added\n keep\n")
	offDiff := newFinding("src/foo.go", 90, 90, "A finding on a line the pull request does not touch.")
	report := reportOf(offDiff)

	first := GitHub(report, GitHubOptions{Lines: lines, HasLines: true})
	if len(first.Dropped) != 1 || !first.PostReview() || !first.Receipt() {
		t.Fatalf("first render: dropped=%d post=%v receipt=%v", len(first.Dropped), first.PostReview(), first.Receipt())
	}

	posted := []github.PostedReview{{CommitID: "head", Body: first.Payload.Review.Body}}
	second := GitHub(report, GitHubOptions{Lines: lines, HasLines: true, Reviews: posted})
	if len(second.Dropped) != 0 || len(second.Duplicates) != 1 {
		t.Fatalf("second render: dropped=%d duplicates=%d", len(second.Dropped), len(second.Duplicates))
	}
	if second.PostReview() || !second.Receipt() {
		t.Fatalf("second render: post=%v receipt=%v", second.PostReview(), second.Receipt())
	}
}

func TestGitHubCapDropBlocksTheReceiptUntilPosted(t *testing.T) {
	var items []findings.Finding
	for i := range maxInlineComments + 1 {
		items = append(items, newFinding("src/foo.go", i+1, i+1, fmt.Sprintf("Problem %d.", i)))
	}
	report := reportOf(items...)

	first := GitHub(report, GitHubOptions{})
	if len(first.Dropped) != 1 || first.Receipt() {
		t.Fatalf("first render: dropped=%d receipt=%v", len(first.Dropped), first.Receipt())
	}

	var comments []github.PostedComment
	for _, item := range items[:maxInlineComments] {
		comments = append(comments, postedComment(item))
	}
	posted := []github.PostedReview{{CommitID: "head", Body: first.Payload.Review.Body}}
	second := GitHub(report, GitHubOptions{Posted: comments, Reviews: posted})
	if len(second.Payload.Review.Comments) != 1 || len(second.Dropped) != 0 || !second.Receipt() {
		t.Fatalf("second render: comments=%d dropped=%d receipt=%v",
			len(second.Payload.Review.Comments), len(second.Dropped), second.Receipt())
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
	left.ID = findings.Fingerprint(left, []string{left.Body}, 0)
	leftComment := githubComment(left)

	got := ReportedFindings([]github.PostedComment{
		postedComment(right),
		{Path: left.Path, StartLine: 7, EndLine: 7, Side: leftComment.Side, Body: leftComment.Body},
		{Path: "x.go", StartLine: 1, EndLine: 1, Side: "RIGHT", Body: `<!-- devin-review-comment {"id": "BUG_x_0001"} -->`},
		{Path: "y.go", StartLine: 2, EndLine: 2, Side: "RIGHT", Body: "a human comment"},
	}, nil)

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

func TestReportedFindingsReadsBackAFindingListedInTheReviewBody(t *testing.T) {
	lines := diffLines(t, "src/foo.go", "@@ -1,2 +1,3 @@\n keep\n+added\n keep\n")
	offDiff := newFinding("src/foo.go", 90, 92, "A finding on a line the pull request does not touch.")
	body := GitHub(reportOf(offDiff), GitHubOptions{Lines: lines, HasLines: true}).Payload.Review.Body

	got := ReportedFindings(nil, []github.PostedReview{
		{CommitID: "head", Body: body},
		{CommitID: "head", Body: "<!-- unreal-review dropped not-base64! -->"},
	})

	if len(got) != 1 || got[0] != offDiff {
		t.Fatalf("reported: %+v want %+v", got, offDiff)
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

func TestGitHubAnswers(t *testing.T) {
	fixed := newFinding("src/foo.go", 12, 14, "This map write races with the reader.")
	stillOpen := newFinding("src/bar.go", 3, 3, "Not fixed.")
	alreadyResolved := newFinding("src/baz.go", 1, 1, "Also fixed but the thread is already resolved.")
	alreadyReplied := newFinding("src/qux.go", 5, 5, "Fixed twice.")

	openPosted := postedComment(fixed)
	openPosted.ThreadID = "thread_open"
	openPosted.CommentID = 501

	resolvedPosted := postedComment(alreadyResolved)
	resolvedPosted.ThreadID = "thread_resolved"
	resolvedPosted.CommentID = 502
	resolvedPosted.ThreadResolved = true

	repliedPosted := postedComment(alreadyReplied)
	repliedPosted.ThreadID = "thread_replied"
	repliedPosted.CommentID = 503
	repliedPosted.Replied = []string{alreadyReplied.ID}

	report := reportOf(stillOpen)
	report.Resolved = []findings.Resolution{
		{ID: fixed.ID, Body: "Added the missing lock."},
		{ID: alreadyResolved.ID, Body: "Fixed regardless."},
		{ID: alreadyReplied.ID, Body: "Fixed again."},
		{ID: "unknown-id-not-posted", Body: "No matching thread."},
	}

	result := GitHub(report, GitHubOptions{
		Posted:   []github.PostedComment{openPosted, resolvedPosted, repliedPosted},
		CommitID: "dab3e1c9d4e5f60708090a0b0c0d0e0f10111213",
	})

	if len(result.Answers) != 2 {
		t.Fatalf("answers: %+v", result.Answers)
	}
	byThread := make(map[string]ThreadAnswer, len(result.Answers))
	for _, a := range result.Answers {
		byThread[a.ThreadID] = a
	}
	open, ok := byThread["thread_open"]
	if !ok || !open.Resolve || open.Reply == "" || !strings.Contains(open.Reply, "Added the missing lock.") {
		t.Fatalf("open thread answer: %+v", open)
	}
	if !strings.Contains(open.Reply, "Fixed in `dab3e1c`:") {
		t.Fatalf("reply should name the short head sha: %q", open.Reply)
	}
	if !strings.Contains(open.Reply, github.ResolvedMarker(fixed.ID)) {
		t.Fatalf("reply should carry the resolved marker: %q", open.Reply)
	}
	replied, ok := byThread["thread_replied"]
	if !ok || !replied.Resolve || replied.Reply != "" {
		t.Fatalf("a thread that already has our reply should resolve without a second reply: %+v", replied)
	}
	if _, ok := byThread["thread_resolved"]; ok {
		t.Fatal("a resolved thread must not be answered again")
	}
}

func TestReportedFindingsDropsResolvedThreadFindingsWhileDedupStillSuppressesReposting(t *testing.T) {
	resolved := newFinding("src/gone.go", 1, 1, "Was fixed already.")
	comment := postedComment(resolved)
	comment.ThreadResolved = true

	reported := ReportedFindings([]github.PostedComment{comment}, nil)
	if len(reported) != 0 {
		t.Fatalf("a resolved thread must not be handed to the model again: %+v", reported)
	}

	dup := GitHub(reportOf(resolved), GitHubOptions{Posted: []github.PostedComment{comment}})
	if len(dup.Payload.Review.Comments) != 0 || len(dup.Duplicates) != 1 {
		t.Fatalf("a resolved thread's finding must still count as posted, not be re-posted: %+v", dup)
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
	item.ID = findings.Fingerprint(item, []string{item.Body}, 0)
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

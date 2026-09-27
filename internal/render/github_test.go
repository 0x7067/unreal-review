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

	result := GitHub(reportOf(fresh, already), GitHubOptions{History: History{Comments: []github.PostedComment{postedComment(already)}}})

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

func TestGitHubEmptyFindingsPostLGTMOncePerHead(t *testing.T) {
	result := GitHub(reportOf(), GitHubOptions{CommitID: "bbb"})
	if !result.PostReview() || result.Payload.Review.Body != "LGTM" {
		t.Fatalf("empty findings must post LGTM: post=%v body=%q", result.PostReview(), result.Payload.Review.Body)
	}
	older := History{Reviews: []github.PostedReview{{CommitID: "aaa", Body: "LGTM"}}}
	if !GitHub(reportOf(), GitHubOptions{CommitID: "bbb", History: older}).PostReview() {
		t.Fatal("an LGTM on an older head must not stop the LGTM for this head")
	}
	same := History{Reviews: []github.PostedReview{{CommitID: "bbb", Body: "LGTM - no findings in aaa..bbb."}}}
	if GitHub(reportOf(), GitHubOptions{CommitID: "bbb", History: same}).PostReview() {
		t.Fatal("a second LGTM for the same head was posted")
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

	result := GitHub(reportOf(append(already, fresh)...), GitHubOptions{History: History{Comments: comments}})

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
		{"only findings already posted", reportOf(already), GitHubOptions{History: History{Comments: []github.PostedComment{postedComment(already)}}}, false},
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

func TestGitHubOutOfPatchFindingIsReportedOnceAndReceipts(t *testing.T) {
	lines := diffLines(t, "src/foo.go", "@@ -1,2 +1,3 @@\n keep\n+added\n keep\n")
	offDiff := newFinding("src/foo.go", 90, 90, "A finding on a line the pull request does not touch.")
	opts := GitHubOptions{Lines: lines, HasLines: true}

	first := GitHub(reportOf(offDiff), opts)
	if !first.PostReview() || !first.Receipt() {
		t.Fatalf("first render: post=%v receipt=%v, want both true", first.PostReview(), first.Receipt())
	}
	want := "Not in the pull request diff, so not posted inline:\n- **warning** `src/foo.go` L90: A finding on a line the pull request does not touch. <!-- unreal-review finding " + offDiff.ID + " -->"
	if !strings.Contains(first.Payload.Review.Body, want) {
		t.Fatalf("review body lacks %q:\n%s", want, first.Payload.Review.Body)
	}

	opts.History.Reviews = []github.PostedReview{{Body: "an unrelated human review"}, {CommitID: "abc", Body: first.Payload.Review.Body}}
	second := GitHub(reportOf(offDiff), opts)
	if second.PostReview() {
		t.Fatalf("second render repeats the drop:\n%s", second.Payload.Review.Body)
	}
	if len(second.Duplicates) != 1 || second.Duplicates[0].ID != offDiff.ID || len(second.Dropped) != 0 {
		t.Fatalf("second render: duplicates=%+v dropped=%+v", second.Duplicates, second.Dropped)
	}
	if !second.Receipt() {
		t.Fatal("second render must still receipt the head")
	}
}

func TestGitHubOverCapFindingBlocksReceiptAndCarriesNoMarker(t *testing.T) {
	var items []findings.Finding
	for i := range maxInlineComments + 1 {
		items = append(items, newFinding("src/foo.go", i+1, i+1, fmt.Sprintf("Problem %d.", i)))
	}
	over := items[maxInlineComments]

	result := GitHub(reportOf(items...), GitHubOptions{})

	if len(result.Payload.Review.Comments) != maxInlineComments || len(result.Dropped) != 1 || result.Dropped[0].Finding.ID != over.ID {
		t.Fatalf("comments=%d dropped=%+v", len(result.Payload.Review.Comments), result.Dropped)
	}
	if result.Receipt() {
		t.Fatal("a finding cut by the cap is still postable; the head must not be receipted")
	}
	if !strings.Contains(result.Payload.Review.Body, "Not posted, the review already has 50 inline comments:\n- `src/foo.go` L51") {
		t.Fatalf("review body:\n%s", result.Payload.Review.Body)
	}
	history := History{Reviews: []github.PostedReview{{Body: result.Payload.Review.Body}}}
	if got := history.Reported(); len(got) != 0 {
		t.Fatalf("an over-cap finding must stay postable, but the body reports %+v", got)
	}
}

func TestGitHubTreatsTheSameLinesAsTheSameIssue(t *testing.T) {
	posted := newFinding("src/foo.go", 10, 12, "This map write races with the reader.")
	history := History{Comments: []github.PostedComment{postedComment(posted)}}

	for _, tc := range []struct {
		name    string
		finding findings.Finding
		dup     bool
	}{
		{"reworded on the same lines", newFinding("src/foo.go", 10, 12, "Concurrent writes to the map race."), true},
		{"shifted but overlapping", newFinding("src/foo.go", 12, 15, "The writer and reader race."), true},
		{"adjacent but disjoint", newFinding("src/foo.go", 13, 15, "Another problem."), false},
		{"same lines in another file", newFinding("src/bar.go", 10, 12, "Another problem."), false},
		{"same lines on the old side", func() findings.Finding {
			f := newFinding("src/foo.go", 10, 12, "A deleted guard.")
			f.Anchor = findings.AnchorOld
			return f
		}(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := GitHub(reportOf(tc.finding), GitHubOptions{CommitID: "head", History: history})
			if got := len(result.Duplicates) == 1; got != tc.dup {
				t.Fatalf("duplicate=%v want %v (comments=%d)", got, tc.dup, len(result.Payload.Review.Comments))
			}
		})
	}
}

func TestGitHubIgnoresLocationOfOutdatedComments(t *testing.T) {
	posted := newFinding("src/foo.go", 10, 12, "This map write races with the reader.")
	stale := postedComment(posted)
	stale.Outdated = true
	moved := newFinding("src/foo.go", 10, 12, "A different issue now on these lines.")

	result := GitHub(reportOf(moved, posted), GitHubOptions{CommitID: "head", History: History{Comments: []github.PostedComment{stale}}})

	if len(result.Payload.Review.Comments) != 1 || len(result.Duplicates) != 1 || result.Duplicates[0].ID != posted.ID {
		t.Fatalf("an outdated comment matches by id only: comments=%d duplicates=%+v", len(result.Payload.Review.Comments), result.Duplicates)
	}
}

func TestGitHubOutOfPatchLocationMatchesOnlyOnTheSameHead(t *testing.T) {
	lines := diffLines(t, "src/foo.go", "@@ -1,2 +1,3 @@\n keep\n+added\n keep\n")
	dropped := newFinding("src/foo.go", 90, 90, "A finding the patch does not show.")
	body := reviewBody("", nil, []DroppedFinding{{Finding: dropped}}, nil)
	reworded := newFinding("src/foo.go", 90, 90, "Same issue, new words.")

	same := GitHub(reportOf(reworded), GitHubOptions{CommitID: "head", Lines: lines, HasLines: true, History: History{Reviews: []github.PostedReview{{CommitID: "head", Body: body}}}})
	if len(same.Duplicates) != 1 {
		t.Fatalf("same head: duplicates=%+v dropped=%+v", same.Duplicates, same.Dropped)
	}
	older := GitHub(reportOf(reworded), GitHubOptions{CommitID: "head", Lines: lines, HasLines: true, History: History{Reviews: []github.PostedReview{{CommitID: "older", Body: body}}}})
	if len(older.Duplicates) != 0 || len(older.Dropped) != 1 {
		t.Fatalf("older head: line numbers may have moved, so only the id matches: duplicates=%+v", older.Duplicates)
	}
}

func TestGitHubHonorsDuplicateOf(t *testing.T) {
	posted := newFinding("src/foo.go", 10, 12, "This map write races with the reader.")
	stale := postedComment(posted)
	stale.Outdated = true
	restated := newFinding("src/foo.go", 40, 41, "The race moved with the refactor.")
	restated.DuplicateOf = posted.ID
	wrong := newFinding("src/foo.go", 60, 60, "Claims a duplicate that was never posted.")
	wrong.DuplicateOf = "ffffffffffffffff"

	result := GitHub(reportOf(restated, wrong), GitHubOptions{CommitID: "head", History: History{Comments: []github.PostedComment{stale}}})

	if len(result.Duplicates) != 1 || result.Duplicates[0].ID != restated.ID {
		t.Fatalf("duplicates=%+v", result.Duplicates)
	}
	if len(result.Payload.Review.Comments) != 1 || result.Payload.Review.Comments[0].Line != 60 {
		t.Fatalf("a duplicate_of naming no posted finding must still post: %+v", result.Payload.Review.Comments)
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

func TestHistoryReportedReadsBackWhatWasPosted(t *testing.T) {
	right := newFinding("src/foo.go", 12, 14, "This map write races with the reader.")
	left := newFinding("src/gone.go", 7, 7, "This guard was removed.")
	left.Anchor = findings.AnchorOld
	left.ID = findings.Fingerprint(left, []string{left.Body}, 0)
	leftComment := githubComment(left)

	outside := newFinding("src/far.go", 40, 42, "A problem the patch does not show,\nacross two lines.")
	outside.Severity = findings.SeverityError
	outsideOld := newFinding("src/far.go", 5, 5, "A deleted guard.")
	outsideOld.Anchor = findings.AnchorOld
	body := reviewBody("Summary.", nil, []DroppedFinding{{Finding: outside}, {Finding: outsideOld}}, nil)

	got := History{
		Comments: []github.PostedComment{
			postedComment(right),
			{Path: left.Path, StartLine: 7, EndLine: 7, Side: leftComment.Side, Body: leftComment.Body},
			{Path: "x.go", StartLine: 1, EndLine: 1, Side: "RIGHT", Body: `<!-- devin-review-comment {"id": "BUG_x_0001"} -->`},
			{Path: "y.go", StartLine: 2, EndLine: 2, Side: "RIGHT", Body: "a human comment"},
		},
		Reviews: []github.PostedReview{{Body: "a human review"}, {Body: body}},
	}.Reported()

	wantOutside := findings.Finding{ID: outside.ID, Path: "src/far.go", StartLine: 40, EndLine: 42, Anchor: findings.AnchorNew, Severity: findings.SeverityError, Body: "A problem the patch does not show, across two lines."}
	wantOld := findings.Finding{ID: outsideOld.ID, Path: "src/far.go", StartLine: 5, EndLine: 5, Anchor: findings.AnchorOld, Severity: findings.SeverityWarning, Body: "A deleted guard."}
	if len(got) != 4 || got[2] != wantOutside || got[3] != wantOld {
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

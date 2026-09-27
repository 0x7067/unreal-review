package render

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"unreal-review/internal/diffmap"
	"unreal-review/internal/findings"
	"unreal-review/internal/github"
)

const maxInlineComments = 50

type GitHubOptions struct {
	Owner      string
	Repo       string
	PullNumber int
	CommitID   string
	Lines      diffmap.Map
	HasLines   bool
	Posted     []github.PostedComment
	Reviews    []github.PostedReview
}

type GitHubResult struct {
	Payload    github.Payload
	Dropped    []DroppedFinding
	Duplicates []findings.Finding
	LGTM       bool
}

func (r GitHubResult) PostReview() bool {
	return len(r.Payload.Review.Comments) > 0 || len(r.Dropped) > 0 || r.LGTM
}

func (r GitHubResult) Receipt() bool {
	for _, item := range r.Dropped {
		if item.Reason == OverCap {
			return false
		}
	}
	return true
}

type DropReason int

const (
	OutsideDiff DropReason = iota
	OverCap
)

func (r DropReason) String() string {
	if r == OverCap {
		return fmt.Sprintf("review already has %d inline comments", maxInlineComments)
	}
	return "line is not in the pull request diff"
}

type DroppedFinding struct {
	Finding findings.Finding
	Reason  DropReason
}

func GitHub(report findings.Report, opts GitHubOptions) GitHubResult {
	result := GitHubResult{
		Payload: github.Payload{
			Owner:      opts.Owner,
			Repo:       opts.Repo,
			PullNumber: opts.PullNumber,
			Review: github.Review{
				CommitID: opts.CommitID,
				Event:    "COMMENT",
			},
		},
	}
	posted := postedFingerprints(opts.Posted, opts.Reviews)
	var placed []findings.Finding
	for _, finding := range report.Findings {
		switch {
		case posted[finding.ID]:
			result.Duplicates = append(result.Duplicates, finding)
		case opts.HasLines && !commentable(opts.Lines, finding):
			result.Dropped = append(result.Dropped, DroppedFinding{Finding: finding, Reason: OutsideDiff})
		case len(result.Payload.Review.Comments) >= maxInlineComments:
			result.Dropped = append(result.Dropped, DroppedFinding{Finding: finding, Reason: OverCap})
		default:
			result.Payload.Review.Comments = append(result.Payload.Review.Comments, githubComment(finding))
			placed = append(placed, finding)
		}
	}
	var cost *findings.Cost
	if report.Run != nil {
		c := report.Run.Cost
		cost = &c
	}
	result.Payload.Review.Body = reviewBody(report.Summary, placed, result.Dropped, cost)
	if len(report.Findings) == 0 {
		result.LGTM = true
		result.Payload.Review.Body = "LGTM"
		if report.Run != nil && report.Run.Source.BaseSHA != "" && report.Run.Source.HeadSHA != "" {
			result.Payload.Review.Body = fmt.Sprintf("LGTM - no findings in %s..%s.", shortSHA(report.Run.Source.BaseSHA), shortSHA(report.Run.Source.HeadSHA))
		}
	}
	return result
}

func postedFingerprints(comments []github.PostedComment, reviews []github.PostedReview) map[string]bool {
	reported := ReportedFindings(comments, reviews)
	out := make(map[string]bool, len(reported))
	for _, finding := range reported {
		out[finding.ID] = true
	}
	return out
}

func ReportedFindings(comments []github.PostedComment, reviews []github.PostedReview) []findings.Finding {
	return append(inlineFindings(comments), droppedFindings(reviews)...)
}

func droppedMarker(finding findings.Finding) string {
	encoded, err := json.Marshal(finding)
	if err != nil {
		panic(fmt.Sprintf("encode dropped finding %s: %v", finding.ID, err))
	}
	return github.DroppedMarker(base64.RawURLEncoding.EncodeToString(encoded))
}

func droppedFindings(reviews []github.PostedReview) []findings.Finding {
	var out []findings.Finding
	for _, review := range reviews {
		for _, payload := range github.ParseDropped(review.Body) {
			raw, err := base64.RawURLEncoding.DecodeString(payload)
			if err != nil {
				continue
			}
			var finding findings.Finding
			if err := json.Unmarshal(raw, &finding); err != nil || finding.ID == "" {
				continue
			}
			out = append(out, finding)
		}
	}
	return out
}

func inlineFindings(comments []github.PostedComment) []findings.Finding {
	var out []findings.Finding
	for _, comment := range comments {
		id, ok := github.ParseFinding(comment.Body)
		if !ok {
			continue
		}
		severity, body := splitPostedBody(comment.Body)
		out = append(out, findings.Finding{
			ID:        id,
			Path:      comment.Path,
			StartLine: comment.StartLine,
			EndLine:   comment.EndLine,
			Anchor:    anchorOf(comment.Side),
			Severity:  severity,
			Body:      body,
		})
	}
	return out
}

func anchorOf(side string) findings.Anchor {
	if side == "LEFT" {
		return findings.AnchorOld
	}
	return findings.AnchorNew
}

func splitPostedBody(body string) (findings.Severity, string) {
	text := strings.TrimSpace(github.WithoutMarker(body))
	rest, ok := strings.CutPrefix(text, "**")
	if !ok {
		return "", text
	}
	severity, tail, ok := strings.Cut(rest, "**")
	if !ok {
		return "", text
	}
	return findings.Severity(severity), strings.TrimSpace(tail)
}

type RunSummary struct {
	HeadSHA string
	Runs    int
	Cost    findings.Cost
	Total   float64
	Result  GitHubResult
	Summary string
}

func StatusBody(r RunSummary) string {
	var b strings.Builder
	b.WriteString(github.StatusMarker(github.Status{Head: r.HeadSHA, Runs: r.Runs, CostUSD: r.Total}))
	fmt.Fprintf(&b, "\n**unreal-review** · reviewed through `%s` · run %d · %d new, %d already reported, %d not postable · cost %s (cumulative USD %.6f)\n",
		shortSHA(r.HeadSHA),
		r.Runs,
		len(r.Result.Payload.Review.Comments),
		len(r.Result.Duplicates),
		len(r.Result.Dropped),
		r.Cost.Format(),
		r.Total,
	)
	if summary := strings.TrimSpace(r.Summary); summary != "" {
		b.WriteString("\n" + summary + "\n")
	}
	return b.String()
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

func commentable(lines diffmap.Map, finding findings.Finding) bool {
	for line := finding.StartLine; line <= finding.EndLine; line++ {
		if !lines.Contains(finding.Path, finding.Anchor, line) {
			return false
		}
	}
	return true
}

func githubComment(finding findings.Finding) github.ReviewComment {
	side := "RIGHT"
	if finding.Anchor == findings.AnchorOld {
		side = "LEFT"
	}
	comment := github.ReviewComment{
		Path: finding.Path,
		Body: fmt.Sprintf("**%s**\n\n%s\n\n%s", finding.Severity, finding.Body, github.FindingMarker(finding.ID)),
		Line: finding.EndLine,
		Side: side,
	}
	if finding.StartLine != finding.EndLine {
		comment.StartLine = finding.StartLine
		comment.StartSide = side
	}
	return comment
}

func reviewBody(summary string, placed []findings.Finding, dropped []DroppedFinding, cost *findings.Cost) string {
	var b strings.Builder
	if strings.TrimSpace(summary) != "" {
		b.WriteString(strings.TrimSpace(summary))
		b.WriteByte('\n')
	} else if len(placed) == 0 && len(dropped) == 0 {
		b.WriteString("No findings.\n")
	}
	if cost != nil {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "Cost: %s\n", cost.Format())
	}
	if len(dropped) == 0 {
		return strings.TrimSpace(b.String())
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "%d finding(s) were not posted as inline comments:\n", len(dropped))
	for _, item := range dropped {
		fmt.Fprintf(&b, "- `%s` %s: %s", item.Finding.Path, formatLines(item.Finding.StartLine, item.Finding.EndLine), item.Reason)
		if item.Reason == OutsideDiff {
			b.WriteString(" " + droppedMarker(item.Finding))
		}
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

package render

import (
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

type DroppedFinding struct {
	Finding findings.Finding
	Reason  string
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
	posted := postedFingerprints(opts.Posted)
	var placed []findings.Finding
	for _, finding := range report.Findings {
		switch {
		case posted[fingerprint(finding)]:
			result.Duplicates = append(result.Duplicates, finding)
		case opts.HasLines && !commentable(opts.Lines, finding):
			result.Dropped = append(result.Dropped, DroppedFinding{
				Finding: finding,
				Reason:  "line is not in the pull request diff",
			})
		case len(result.Payload.Review.Comments) >= maxInlineComments:
			result.Dropped = append(result.Dropped, DroppedFinding{
				Finding: finding,
				Reason:  fmt.Sprintf("review already has %d inline comments", maxInlineComments),
			})
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

func postedFingerprints(comments []github.PostedComment) map[string]bool {
	out := make(map[string]bool, len(comments))
	for _, comment := range comments {
		if id, ok := github.ParseFinding(comment.Body); ok {
			out[id] = true
		}
	}
	return out
}

func fingerprint(finding findings.Finding) string {
	if finding.ID != "" {
		return finding.ID
	}
	return findings.Fingerprint(finding)
}

func ReportedFindings(comments []github.PostedComment) []findings.Finding {
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
		Body: fmt.Sprintf("**%s**\n\n%s\n\n%s", finding.Severity, finding.Body, github.FindingMarker(fingerprint(finding))),
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
		fmt.Fprintf(&b, "- `%s` %s: %s\n", item.Finding.Path, formatLines(item.Finding.StartLine, item.Finding.EndLine), item.Reason)
	}
	return strings.TrimSpace(b.String())
}

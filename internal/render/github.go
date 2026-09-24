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
}

type GitHubResult struct {
	Payload github.Payload
	Dropped []DroppedFinding
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
	var placed []findings.Finding
	for _, finding := range report.Findings {
		if opts.HasLines && !commentable(opts.Lines, finding) {
			result.Dropped = append(result.Dropped, DroppedFinding{
				Finding: finding,
				Reason:  "line is not in the pull request diff",
			})
			continue
		}
		if len(result.Payload.Review.Comments) >= maxInlineComments {
			result.Dropped = append(result.Dropped, DroppedFinding{
				Finding: finding,
				Reason:  fmt.Sprintf("review already has %d inline comments", maxInlineComments),
			})
			continue
		}
		result.Payload.Review.Comments = append(result.Payload.Review.Comments, githubComment(finding))
		placed = append(placed, finding)
	}
	var cost *findings.Cost
	if report.Run != nil {
		c := report.Run.Cost
		cost = &c
	}
	result.Payload.Review.Body = reviewBody(report.Summary, placed, result.Dropped, cost)
	return result
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
		Body: fmt.Sprintf("**%s**\n\n%s", finding.Severity, finding.Body),
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

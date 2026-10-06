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

const (
	maxInlineComments   = 50
	droppedMarkerPrefix = "<!-- unreal-review dropped "
)

type GitHubOptions struct {
	Owner      string
	Repo       string
	PullNumber int
	CommitID   string
	Lines      diffmap.Map
	HasLines   bool
	History    History
}

type GitHubResult struct {
	Payload    github.Payload
	Dropped    []DroppedFinding
	Duplicates []findings.Finding
	LGTM       bool
	LGTMPosted bool
}

func (r GitHubResult) PostReview() bool {
	if r.LGTM {
		return !r.LGTMPosted
	}
	return len(r.Payload.Review.Comments) > 0 || len(r.Dropped) > 0
}

func (r GitHubResult) Receipt() bool {
	for _, item := range r.Dropped {
		if item.Kind == DropOverCap {
			return false
		}
	}
	return true
}

type DropKind int

const (
	DropOutsidePatch DropKind = iota
	DropOverCap
)

type DroppedFinding struct {
	Finding findings.Finding
	Kind    DropKind
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
	if len(report.Findings) == 0 {
		result.LGTM = true
		result.LGTMPosted = opts.History.HasLGTM(opts.CommitID)
		result.Payload.Review.Body = "LGTM"
		if report.Run != nil && report.Run.Source.BaseSHA != "" && report.Run.Source.HeadSHA != "" {
			result.Payload.Review.Body = fmt.Sprintf("LGTM - no findings in %s..%s.", shortSHA(report.Run.Source.BaseSHA), shortSHA(report.Run.Source.HeadSHA))
		}
		return result
	}
	prior := opts.History.Reported()
	for _, finding := range report.Findings {
		switch {
		case alreadyReported(prior, finding):
			result.Duplicates = append(result.Duplicates, finding)
		case opts.HasLines && !commentable(opts.Lines, finding):
			result.Dropped = append(result.Dropped, DroppedFinding{Finding: finding, Kind: DropOutsidePatch})
		case len(result.Payload.Review.Comments) >= maxInlineComments:
			result.Dropped = append(result.Dropped, DroppedFinding{Finding: finding, Kind: DropOverCap})
		default:
			result.Payload.Review.Comments = append(result.Payload.Review.Comments, githubComment(finding))
		}
	}
	var cost *findings.Cost
	if report.Run != nil {
		c := report.Run.Cost
		cost = &c
	}
	result.Payload.Review.Body = reviewBody(report.Summary, len(result.Payload.Review.Comments), result.Dropped, cost)
	return result
}

type History struct {
	Comments []github.PostedComment
	Reviews  []github.PostedReview
}

func HistoryOf(state github.PullState) History {
	return History{Comments: state.Comments, Reviews: state.Reviews}
}

func (h History) HasLGTM(commit string) bool {
	for _, review := range h.Reviews {
		if review.CommitID == commit && strings.HasPrefix(strings.TrimSpace(review.Body), "LGTM") {
			return true
		}
	}
	return false
}

func (h History) Reported() []findings.Finding {
	var out []findings.Finding
	for _, comment := range h.Comments {
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
	for _, review := range h.Reviews {
		for _, line := range strings.Split(review.Body, "\n") {
			if item, ok := parseDroppedMarker(line); ok {
				out = append(out, item)
			}
		}
	}
	return out
}

func alreadyReported(prior []findings.Finding, finding findings.Finding) bool {
	for _, item := range prior {
		if item.ID == finding.ID {
			return true
		}
	}
	return false
}

func droppedLine(finding findings.Finding) string {
	anchor := ""
	if finding.Anchor == findings.AnchorOld {
		anchor = " old"
	}
	return fmt.Sprintf("- **%s** `%s`%s %s: %s %s %s",
		finding.Severity, finding.Path, anchor,
		formatLines(finding.StartLine, finding.EndLine),
		strings.Join(strings.Fields(finding.Body), " "),
		github.FindingMarker(finding.ID), droppedMarker(finding))
}

func droppedMarker(finding findings.Finding) string {
	finding.Body = strings.Join(strings.Fields(finding.Body), " ")
	raw, err := json.Marshal(finding)
	if err != nil {
		return ""
	}
	return droppedMarkerPrefix + base64.RawURLEncoding.EncodeToString(raw) + " -->"
}

func parseDroppedMarker(line string) (findings.Finding, bool) {
	at := strings.Index(line, droppedMarkerPrefix)
	if at < 0 {
		return findings.Finding{}, false
	}
	rest := line[at+len(droppedMarkerPrefix):]
	encoded, _, ok := strings.Cut(rest, " -->")
	if !ok {
		return findings.Finding{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return findings.Finding{}, false
	}
	var finding findings.Finding
	if err := json.Unmarshal(raw, &finding); err != nil || finding.ID == "" {
		return findings.Finding{}, false
	}
	return finding, true
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

func reviewBody(summary string, inline int, dropped []DroppedFinding, cost *findings.Cost) string {
	var b strings.Builder
	if strings.TrimSpace(summary) != "" {
		b.WriteString(strings.TrimSpace(summary))
		b.WriteByte('\n')
	} else if inline == 0 && len(dropped) == 0 {
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
	var outside, overCap []findings.Finding
	for _, item := range dropped {
		if item.Kind == DropOverCap {
			overCap = append(overCap, item.Finding)
		} else {
			outside = append(outside, item.Finding)
		}
	}
	if len(outside) > 0 {
		fmt.Fprintf(&b, "\nNot in the pull request diff, so not posted inline:\n")
		for _, finding := range outside {
			b.WriteString(droppedLine(finding) + "\n")
		}
	}
	if len(overCap) > 0 {
		fmt.Fprintf(&b, "\nNot posted, the review already has %d inline comments:\n", maxInlineComments)
		for _, finding := range overCap {
			fmt.Fprintf(&b, "- `%s` %s\n", finding.Path, formatLines(finding.StartLine, finding.EndLine))
		}
	}
	return strings.TrimSpace(b.String())
}

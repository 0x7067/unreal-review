package render

import (
	"fmt"
	"io"
	"strings"

	"unreal-review/internal/findings"
)

func Markdown(w io.Writer, report findings.Report) error {
	if report.Run != nil && report.Run.Status != "" && report.Run.Status != findings.StatusComplete {
		if _, err := fmt.Fprintf(w, "Status: %s\n\n", report.Run.Status); err != nil {
			return err
		}
	}
	if report.Run != nil {
		if _, err := fmt.Fprintf(w, "Cost: %s\n\n", report.Run.Cost.Format()); err != nil {
			return err
		}
	}
	if report.Summary != "" {
		if _, err := fmt.Fprintln(w, report.Summary); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w); err != nil {
			return err
		}
	}
	if len(report.Findings) == 0 {
		if report.Summary == "" {
			_, err := fmt.Fprintln(w, "No findings.")
			return err
		}
		return nil
	}
	current := ""
	for _, finding := range report.Findings {
		if finding.Path != current {
			if current != "" {
				if _, err := fmt.Fprintln(w); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintf(w, "## `%s`\n\n", finding.Path); err != nil {
				return err
			}
			current = finding.Path
		}
		loc := formatLines(finding.StartLine, finding.EndLine)
		if _, err := fmt.Fprintf(w, "- **%s** %s (%s): %s\n", finding.Severity, loc, finding.Anchor, oneLine(finding.Body)); err != nil {
			return err
		}
		if strings.Contains(finding.Body, "\n") {
			if _, err := fmt.Fprintf(w, "\n%s\n", indent(finding.Body)); err != nil {
				return err
			}
		}
	}
	return nil
}

func formatLines(start, end int) string {
	if start == end {
		return fmt.Sprintf("L%d", start)
	}
	return fmt.Sprintf("L%d–L%d", start, end)
}

func oneLine(body string) string {
	line, _, _ := strings.Cut(body, "\n")
	return strings.TrimSpace(line)
}

func indent(body string) string {
	lines := strings.Split(strings.TrimSuffix(body, "\n"), "\n")
	for i, line := range lines {
		if line == "" {
			lines[i] = ">"
			continue
		}
		lines[i] = "> " + line
	}
	return strings.Join(lines, "\n")
}

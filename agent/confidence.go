package agent

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/review"
)

// Code execution during verification is allowed only for a low-confidence
// candidate. The threshold uses the discovery severity field and the selected
// diff; there is no separate confidence field.
//
// High-confidence: severity warning or note. Those claims are checked by reading.
// Diff-visible: the cited inclusive line range includes a changed line of the
// selected diff. Anchor new matches an added line; anchor old matches a removed
// line. Context lines in a hunk are not changed lines. A rename records each
// changed line under both the old path and the new path.
// Low-confidence: severity error and not diff-visible.
//
// A planned verification batch is entirely one class. Low-confidence batches
// keep the sandbox code budget (sandboxTaskWall). Every other batch has no
// code-execution budget; reads still run. A focused verification pass is one
// batch and allows code execution only when it contains a low-confidence
// candidate.
func findingLowConfidence(f findings.Finding, lines diffLines) bool {
	if f.Severity != findings.SeverityError {
		return false
	}
	return !findingDiffVisible(f, lines)
}

func verifierExecWall(candidates []findings.Finding, lines diffLines) time.Duration {
	for _, candidate := range candidates {
		if findingLowConfidence(candidate, lines) {
			return sandboxTaskWall
		}
	}
	return 0
}

func findingDiffVisible(f findings.Finding, lines diffLines) bool {
	var set map[int]struct{}
	switch f.Anchor {
	case findings.AnchorNew:
		set = lines.added[cleanFindingPath(f.Path)]
	case findings.AnchorOld:
		set = lines.removed[cleanFindingPath(f.Path)]
	default:
		return false
	}
	if len(set) == 0 {
		return false
	}
	start, end := f.StartLine, f.EndLine
	if end < start {
		end = start
	}
	for line := start; line <= end; line++ {
		if _, ok := set[line]; ok {
			return true
		}
	}
	return false
}

func cleanFindingPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

type diffLines struct {
	added   map[string]map[int]struct{}
	removed map[string]map[int]struct{}
}

func reviewDiffText(req review.AgentRequest) string {
	var b strings.Builder
	writeDiffFences(&b, req.Prompt)
	if req.Plan != nil {
		for _, task := range req.Plan.Tasks {
			writeDiffFences(&b, task.Prompt)
		}
	}
	return b.String()
}

func writeDiffFences(b *strings.Builder, text string) {
	rest := text
	for {
		i := strings.Index(rest, "```diff\n")
		if i < 0 {
			return
		}
		rest = rest[i+len("```diff\n"):]
		body, after, ok := strings.Cut(rest, "\n```")
		if !ok {
			b.WriteString(rest)
			b.WriteByte('\n')
			return
		}
		b.WriteString(body)
		b.WriteByte('\n')
		rest = after
	}
}

func changedLines(diff string) diffLines {
	lines := diffLines{
		added:   map[string]map[int]struct{}{},
		removed: map[string]map[int]struct{}{},
	}
	var oldPath, newPath string
	var oldLine, newLine int
	inHunk := false
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
			oldPath, newPath = "", ""
		case strings.HasPrefix(line, "--- "):
			oldPath = diffPath(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			newPath = diffPath(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "@@ "):
			var ok bool
			oldLine, newLine, ok = hunkStarts(line)
			inHunk = ok
		case !inHunk || strings.HasPrefix(line, `\`):
		case strings.HasPrefix(line, "+"):
			addChangedLine(lines.added, newPath, oldPath, newLine)
			newLine++
		case strings.HasPrefix(line, "-"):
			addChangedLine(lines.removed, oldPath, newPath, oldLine)
			oldLine++
		default:
			oldLine++
			newLine++
		}
	}
	return lines
}

func addChangedLine(sets map[string]map[int]struct{}, path, also string, line int) {
	addDiffLine(sets, path, line)
	if also != "" && also != path {
		addDiffLine(sets, also, line)
	}
}

func addDiffLine(sets map[string]map[int]struct{}, path string, line int) {
	if path == "" || line < 1 {
		return
	}
	set := sets[path]
	if set == nil {
		set = map[int]struct{}{}
		sets[path] = set
	}
	set[line] = struct{}{}
}

func diffPath(field string) string {
	field = strings.TrimSpace(field)
	if tab := strings.IndexByte(field, '\t'); tab >= 0 {
		field = field[:tab]
	}
	if len(field) >= 2 && field[0] == '"' && field[len(field)-1] == '"' {
		field = field[1 : len(field)-1]
	}
	if field == "" || field == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(field, "a/") || strings.HasPrefix(field, "b/") {
		field = field[2:]
	}
	return filepath.ToSlash(filepath.Clean(field))
}

func hunkStarts(line string) (int, int, bool) {
	rest := strings.TrimPrefix(line, "@@ ")
	oldPart, rest, ok := strings.Cut(rest, " ")
	if !ok || !strings.HasPrefix(oldPart, "-") {
		return 0, 0, false
	}
	newPart, _, ok := strings.Cut(rest, " ")
	if !ok || !strings.HasPrefix(newPart, "+") {
		return 0, 0, false
	}
	oldStart := leadingInt(oldPart[1:])
	newStart := leadingInt(newPart[1:])
	if oldStart < 0 || newStart < 0 {
		return 0, 0, false
	}
	return oldStart, newStart, true
}

func leadingInt(s string) int {
	if s == "" {
		return -1
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

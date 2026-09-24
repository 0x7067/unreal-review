package diffmap

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"unreal-review/internal/findings"
)

type Map struct {
	files map[string]fileLines
}

type fileLines struct {
	old map[int]struct{}
	new map[int]struct{}
}

func New() Map {
	return Map{files: map[string]fileLines{}}
}

func (m Map) Contains(path string, anchor findings.Anchor, line int) bool {
	current, ok := m.files[path]
	if !ok {
		return false
	}
	var lines map[int]struct{}
	if anchor == findings.AnchorOld {
		lines = current.old
	} else {
		lines = current.new
	}
	_, ok = lines[line]
	return ok
}

func (m Map) add(path string, anchor findings.Anchor, line int) {
	if path == "" || line < 1 {
		return
	}
	current, ok := m.files[path]
	if !ok {
		current = fileLines{old: map[int]struct{}{}, new: map[int]struct{}{}}
		m.files[path] = current
	}
	if anchor == findings.AnchorOld {
		current.old[line] = struct{}{}
		return
	}
	current.new[line] = struct{}{}
}

func ParseGitDiff(r io.Reader) (Map, error) {
	m := New()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(nil, 4<<20)
	path := ""
	var oldLine, newLine int
	inHunk := false
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "diff --git "):
			inHunk = false
			path = gitDiffPath(line)
		case strings.HasPrefix(line, "+++ "):
			if p := plusPlusPath(line); p != "" {
				path = p
			}
		case strings.HasPrefix(line, "@@ "):
			var err error
			oldLine, newLine, err = parseHunkHeader(line)
			if err != nil {
				return Map{}, err
			}
			inHunk = true
		case !inHunk:
			continue
		case strings.HasPrefix(line, "\\"):
			continue
		case strings.HasPrefix(line, "+"):
			m.add(path, findings.AnchorNew, newLine)
			newLine++
		case strings.HasPrefix(line, "-"):
			m.add(path, findings.AnchorOld, oldLine)
			oldLine++
		default:
			m.add(path, findings.AnchorOld, oldLine)
			m.add(path, findings.AnchorNew, newLine)
			oldLine++
			newLine++
		}
	}
	if err := scanner.Err(); err != nil {
		return Map{}, fmt.Errorf("read diff: %w", err)
	}
	return m, nil
}

func MergePatch(m Map, path, patch string) error {
	parsed, err := ParseGitDiff(strings.NewReader(formatPatch(path, patch)))
	if err != nil {
		return err
	}
	for name, lines := range parsed.files {
		for line := range lines.old {
			m.add(name, findings.AnchorOld, line)
		}
		for line := range lines.new {
			m.add(name, findings.AnchorNew, line)
		}
	}
	return nil
}

func formatPatch(path, patch string) string {
	if strings.Contains(patch, "diff --git ") {
		return patch
	}
	return "diff --git a/" + path + " b/" + path + "\n+++ b/" + path + "\n" + patch
}

func gitDiffPath(line string) string {
	rest := strings.TrimPrefix(line, "diff --git ")
	parts := strings.Split(rest, " ")
	if len(parts) < 2 {
		return ""
	}
	return strings.TrimPrefix(parts[len(parts)-1], "b/")
}

func plusPlusPath(line string) string {
	path := strings.TrimPrefix(line, "+++ ")
	if path == "/dev/null" {
		return ""
	}
	return strings.TrimPrefix(path, "b/")
}

func parseHunkHeader(line string) (int, int, error) {
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return 0, 0, fmt.Errorf("invalid hunk header %q", line)
	}
	oldStart, err := hunkStart(fields[1], '-')
	if err != nil {
		return 0, 0, err
	}
	newStart, err := hunkStart(fields[2], '+')
	if err != nil {
		return 0, 0, err
	}
	return oldStart, newStart, nil
}

func hunkStart(field string, prefix byte) (int, error) {
	if field == "" || field[0] != prefix {
		return 0, fmt.Errorf("invalid hunk field %q", field)
	}
	body := field[1:]
	start, _, _ := strings.Cut(body, ",")
	n, err := strconv.Atoi(start)
	if err != nil {
		return 0, fmt.Errorf("invalid hunk line %q: %w", field, err)
	}
	if n < 0 {
		return 0, fmt.Errorf("invalid hunk line %q", field)
	}
	if n == 0 {
		return 1, nil
	}
	return n, nil
}

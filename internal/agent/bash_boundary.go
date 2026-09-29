package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"
)

// boundedBash keeps dependency searches rooted in the review workspace before
// handing commands to the harness shell runner. The runner remains responsible
// for process lifetime and cancellation.
type boundedBash struct {
	workspace string
	inner     tool.Translator
}

func newBoundedBash(workspace string, inner tool.Translator) tool.Translator {
	workspace = filepath.Clean(workspace)
	if resolved, err := filepath.EvalSymlinks(workspace); err == nil {
		workspace = resolved
	}
	return boundedBash{workspace: workspace, inner: inner}
}

func (b boundedBash) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	command, err := bashCommand(call.Arguments)
	if err != nil {
		return tool.ErrorStatus(err.Error(), 0)
	}
	if err := b.validateFind(command); err != nil {
		return tool.ErrorStatus(err.Error(), 0)
	}
	return b.inner.Translate(ctx, call)
}

func (b boundedBash) TranslateResult(callID string, status tool.CallStatus, operations []operation.Operation) (llm.ToolResult, error) {
	return b.inner.TranslateResult(callID, status, operations)
}

func bashCommand(arguments string) (string, error) {
	var args struct {
		Command *string `json:"command"`
	}
	if err := json.Unmarshal([]byte(arguments), &args); err != nil {
		return "", fmt.Errorf("decode Bash arguments: %w", err)
	}
	if args.Command == nil {
		return "", nil
	}
	return *args.Command, nil
}

func (b boundedBash) validateFind(command string) error {
	words, simple, err := simpleShellWords(command)
	if err != nil {
		if mentionsFind(command) {
			return boundedFindError()
		}
		return nil
	}
	if !simple || len(words) == 0 || filepath.Base(words[0]) != "find" {
		if mentionsFind(command) {
			return boundedFindError()
		}
		return nil
	}
	for _, arg := range words[1:] {
		if mentionsFind(arg) {
			return boundedFindError()
		}
	}

	args := words[1:]
	for len(args) > 0 && (args[0] == "-H" || args[0] == "-L" || args[0] == "-P") {
		if args[0] != "-P" {
			return boundedFindError()
		}
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	} else if len(args) > 0 && (args[0] == "-D" || strings.HasPrefix(args[0], "-O")) {
		return boundedFindError()
	}
	roots := findRoots(args)
	if len(roots) == 0 {
		return boundedFindError()
	}
	for _, root := range roots {
		if strings.ContainsAny(root, "$`") || !b.withinWorkspace(root) {
			return boundedFindError()
		}
	}
	return nil
}

func (b boundedBash) withinWorkspace(root string) bool {
	candidate := root
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(b.workspace, candidate)
	}
	candidate = filepath.Clean(candidate)
	if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
		candidate = resolved
	}
	rel, err := filepath.Rel(b.workspace, candidate)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func findRoots(args []string) []string {
	var roots []string
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") || arg == "!" || arg == "(" {
			break
		}
		roots = append(roots, arg)
	}
	return roots
}

func mentionsFind(command string) bool {
	for _, field := range strings.FieldsFunc(command, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-' && r != '/' && r != '.'
	}) {
		if filepath.Base(field) == "find" {
			return true
		}
	}
	return false
}

func boundedFindError() error {
	return fmt.Errorf("find searches must be a direct command rooted inside the review workspace; retry with `find . ...`")
}

func simpleShellWords(command string) ([]string, bool, error) {
	var words []string
	var word strings.Builder
	quote := rune(0)
	escaped := false
	flush := func() {
		if word.Len() > 0 {
			words = append(words, word.String())
			word.Reset()
		}
	}
	for _, r := range command {
		if escaped {
			word.WriteRune(r)
			escaped = false
			continue
		}
		if quote == '\'' {
			if r == '\'' {
				quote = 0
			} else {
				word.WriteRune(r)
			}
			continue
		}
		if quote == '"' {
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				word.WriteRune(r)
			}
			continue
		}
		switch {
		case r == '\\':
			escaped = true
		case r == '\'' || r == '"':
			quote = r
		case unicode.IsSpace(r):
			flush()
		case strings.ContainsRune(";&|<>`(){}", r):
			return nil, false, nil
		default:
			word.WriteRune(r)
		}
	}
	if escaped || quote != 0 {
		return nil, false, fmt.Errorf("unterminated shell quote or escape")
	}
	flush()
	return words, true, nil
}

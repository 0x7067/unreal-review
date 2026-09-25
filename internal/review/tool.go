package review

import (
	"context"
	"fmt"
	"strings"
)

type Tool struct {
	Name    string
	Purpose string
	Usage   string
	Run     func(ctx context.Context, args []string, stdin string) (string, error)
}

func DefaultTools() []Tool {
	return []Tool{recordTool()}
}

func FindTool(tools []Tool, name string) (Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool, true
		}
	}
	return Tool{}, false
}

func promptSection(entry string, tools []Tool) string {
	var b strings.Builder
	b.WriteString("Record findings and the summary with these commands instead of writing the findings file yourself. Run them from the repository root. --out is the findings path given in the user message. Pass the body on standard input exactly as it should appear, with no shell or JSON escaping; end it with a heredoc. If a command reports an error, fix the arguments and run it again.\n\n")
	for _, tool := range tools {
		_, _ = fmt.Fprintf(&b, "%s %s\n\n%s\n\n", entry, tool.Name, tool.Usage)
	}
	return strings.TrimRight(b.String(), "\n")
}

package agent

import (
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

type recordRegistry struct {
	tool.Registry
	finding *recordFindingTranslator
	summary *recordSummaryTranslator
}

func newRecordRegistry(inner tool.Registry, observer *sessionObserver) tool.Registry {
	return &recordRegistry{
		Registry: inner,
		finding:  &recordFindingTranslator{observer: observer},
		summary:  &recordSummaryTranslator{observer: observer},
	}
}

func (r *recordRegistry) StaticDefinitions() []tool.Definition {
	defs := r.Registry.StaticDefinitions()
	return append(defs, recordFindingDefinition(), recordSummaryDefinition())
}

func (r *recordRegistry) Resolve(name string) (tool.Translator, bool) {
	switch name {
	case review.RecordFindingTool:
		return r.finding, true
	case review.RecordSummaryTool:
		return r.summary, true
	default:
		return r.Registry.Resolve(name)
	}
}

func recordFindingDefinition() tool.Definition {
	return tool.Definition{Tool: llm.Tool{
		Type:        llm.ToolFunction,
		Name:        review.RecordFindingTool,
		Description: "Record one finding about the reviewed diff. Call once per issue, then finish with " + review.RecordSummaryTool + ".",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Repository-relative path of the file the finding is about.",
				},
				"start_line": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"description": "First inclusive 1-based line the finding is about.",
				},
				"end_line": map[string]any{
					"type":        "integer",
					"description": "Last inclusive 1-based line; defaults to start_line.",
				},
				"anchor": map[string]any{
					"type":        "string",
					"enum":        []any{"new", "old"},
					"default":     "new",
					"description": "Whether start_line/end_line are on the new or old side of the diff.",
				},
				"severity": map[string]any{
					"type":        "string",
					"enum":        []any{"error", "warning", "note"},
					"description": "error for a wrong-output defect, warning for a working-but-weak issue, note for anything smaller.",
				},
				"body": map[string]any{
					"type":        "string",
					"description": "Markdown finding body.",
				},
			},
			"required": []any{"path", "start_line", "severity", "body"},
		},
	}}
}

func recordSummaryDefinition() tool.Definition {
	return tool.Definition{Tool: llm.Tool{
		Type:        llm.ToolFunction,
		Name:        review.RecordSummaryTool,
		Description: "Record the review summary. Call exactly once, after every " + review.RecordFindingTool + " call.",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"body": map[string]any{
					"type":        "string",
					"description": "Markdown summary body.",
				},
			},
			"required": []any{"body"},
		},
	}}
}

type recordFindingArgs struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Anchor    string `json:"anchor"`
	Severity  string `json:"severity"`
	Body      string `json:"body"`
}

type recordSummaryArgs struct {
	Body string `json:"body"`
}

type recordFindingTranslator struct {
	observer *sessionObserver
}

var _ tool.Translator = (*recordFindingTranslator)(nil)

func (t *recordFindingTranslator) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	var args recordFindingArgs
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return tool.ErrorStatus(fmt.Sprintf("decode arguments: %v", err), 0)
	}
	finding, err := findings.Normalize(findings.Finding{
		Path:      args.Path,
		StartLine: args.StartLine,
		EndLine:   args.EndLine,
		Anchor:    findings.Anchor(args.Anchor),
		Severity:  findings.Severity(args.Severity),
		Body:      args.Body,
	})
	if err != nil {
		return tool.ErrorStatus(err.Error(), 0)
	}
	id, err := submitValue(ctx, finding.ID)
	if err != nil {
		return tool.ErrorStatus(err.Error(), 0)
	}
	t.observer.stashFinding(call.CallID, finding)
	return tool.CallStatus{WaitingFor: []operation.ID{id}}
}

func (t *recordFindingTranslator) TranslateResult(callID string, status tool.CallStatus, _ []operation.Operation) (llm.ToolResult, error) {
	return recordResult(callID, status), nil
}

type recordSummaryTranslator struct {
	observer *sessionObserver
}

var _ tool.Translator = (*recordSummaryTranslator)(nil)

func (t *recordSummaryTranslator) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	var args recordSummaryArgs
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return tool.ErrorStatus(fmt.Sprintf("decode arguments: %v", err), 0)
	}
	body := strings.TrimSpace(args.Body)
	if body == "" {
		return tool.ErrorStatus("body must be set", 0)
	}
	id, err := submitValue(ctx, "summary")
	if err != nil {
		return tool.ErrorStatus(err.Error(), 0)
	}
	t.observer.stashSummary(call.CallID, body)
	return tool.CallStatus{WaitingFor: []operation.ID{id}}
}

func (t *recordSummaryTranslator) TranslateResult(callID string, status tool.CallStatus, _ []operation.Operation) (llm.ToolResult, error) {
	return recordResult(callID, status), nil
}

func recordResult(callID string, status tool.CallStatus) llm.ToolResult {
	text := "recorded"
	if status.Error != "" {
		text = "Error: " + status.Error
	}
	return llm.ToolResult{CallID: callID, Output: []llm.ToolResultOutput{{Kind: llm.ToolResultText, Value: text}}}
}

func submitValue(ctx tool.Context, value string) (operation.ID, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode operation value: %w", err)
	}
	spec, err := operation.NewValueSpec(jsontext.Value(encoded))
	if err != nil {
		return "", fmt.Errorf("build operation: %w", err)
	}
	return ctx.Submit(spec), nil
}

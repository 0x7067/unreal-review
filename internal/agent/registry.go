package agent

import (
	"encoding/json"
	"encoding/json/jsontext"
	"fmt"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

type recordRegistry struct {
	tool.Registry
}

func newRecordRegistry(inner tool.Registry) tool.Registry {
	return recordRegistry{Registry: inner}
}

func (r recordRegistry) StaticDefinitions() []tool.Definition {
	defs := r.Registry.StaticDefinitions()
	return append(defs, recordFindingDefinition())
}

func (r recordRegistry) Resolve(name string) (tool.Translator, bool) {
	switch name {
	case review.RecordFindingTool:
		return recordFindingTranslator{}, true
	default:
		return r.Registry.Resolve(name)
	}
}

func recordFindingDefinition() tool.Definition {
	return tool.Definition{Tool: llm.Tool{
		Type:        llm.ToolFunction,
		Name:        review.RecordFindingTool,
		Description: "Record one finding about the reviewed diff. Call once per issue.",
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
				"duplicate_of": map[string]any{
					"type":        "string",
					"description": "Id of an already-reported finding this one restates, from the list in the prompt. Omit for a new issue.",
				},
			},
			"required": []any{"path", "start_line", "severity", "body"},
		},
	}}
}

type recordFindingArgs struct {
	Path        string `json:"path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	Anchor      string `json:"anchor"`
	Severity    string `json:"severity"`
	Body        string `json:"body"`
	DuplicateOf string `json:"duplicate_of"`
}

type recordFindingTranslator struct{}

func (recordFindingTranslator) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	var args recordFindingArgs
	if err := json.Unmarshal([]byte(call.Arguments), &args); err != nil {
		return tool.ErrorStatus(fmt.Sprintf("decode arguments: %v", err), 0)
	}
	finding, err := findings.Normalize(findings.Finding{
		Path:        args.Path,
		StartLine:   args.StartLine,
		EndLine:     args.EndLine,
		Anchor:      findings.Anchor(args.Anchor),
		Severity:    findings.Severity(args.Severity),
		Body:        args.Body,
		DuplicateOf: args.DuplicateOf,
	})
	if err != nil {
		return tool.ErrorStatus(err.Error(), 0)
	}
	return submitRecord(ctx, finding)
}

func (recordFindingTranslator) TranslateResult(callID string, status tool.CallStatus, _ []operation.Operation) (llm.ToolResult, error) {
	return recordResult(callID, status), nil
}

func recordResult(callID string, status tool.CallStatus) llm.ToolResult {
	text := "recorded"
	if status.Error != "" {
		text = "Error: " + status.Error
	}
	return llm.ToolResult{CallID: callID, Output: []llm.ToolResultOutput{{Kind: llm.ToolResultText, Value: text}}}
}

func submitRecord(ctx tool.Context, finding findings.Finding) tool.CallStatus {
	encoded, err := json.Marshal(finding)
	if err != nil {
		return tool.ErrorStatus(fmt.Sprintf("encode record: %v", err), 0)
	}
	spec, err := operation.NewValueSpec(jsontext.Value(encoded))
	if err != nil {
		return tool.ErrorStatus(fmt.Sprintf("build record operation: %v", err), 0)
	}
	return tool.CallStatus{WaitingFor: []operation.ID{ctx.Submit(spec)}}
}

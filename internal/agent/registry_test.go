package agent

import (
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"testing"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"
	"github.com/unreallabsai/unreal-agent/harness/tool/bash"
	"github.com/unreallabsai/unreal-agent/harness/tool/viewimage"

	"github.com/0x7067/unreal-review/internal/findings"
	"github.com/0x7067/unreal-review/internal/review"
)

type recordingContext struct {
	specs []operation.Spec
}

func (c *recordingContext) Submit(spec operation.Spec) operation.ID {
	c.specs = append(c.specs, spec)
	var zero operation.ID
	return zero
}

func innerRegistry(t *testing.T) tool.Registry {
	t.Helper()
	dir := t.TempDir()
	return tool.NewRegistry(tool.StaticTranslators{
		Bash:      bash.New(bash.Config{Shell: "/bin/sh", Directory: dir, BaseDirectory: dir}),
		ViewImage: viewimage.New(viewimage.Config{Directory: dir}),
	}, tool.BashName, tool.ViewImageName)
}

func TestTranslateRecordsANormalizedFinding(t *testing.T) {
	registry := newRecordRegistry(innerRegistry(t))
	translator, ok := registry.Resolve(review.RecordFindingTool)
	if !ok {
		t.Fatal("Resolve: want the record translator")
	}
	ctx := &recordingContext{}

	status := translator.Translate(ctx, llm.ToolCall{
		CallID:    "call-3",
		Name:      review.RecordFindingTool,
		Arguments: `{"path":" cmd/run.go ","start_line":9,"end_line":12,"anchor":"old","severity":" note ","body":"The flag is parsed twice.","id":" a1b2c3d4e5f60708 "}`,
	})
	if status.Error != "" {
		t.Fatalf("translate: unexpected error %q", status.Error)
	}
	if len(ctx.specs) != 1 {
		t.Fatalf("submitted specs: got %d, want 1", len(ctx.specs))
	}

	var state operation.ValueState
	if err := jsonv2.Unmarshal(ctx.specs[0].State, &state); err != nil {
		t.Fatalf("decode spec state: %v", err)
	}
	var finding findings.Finding
	if err := json.Unmarshal(state.Value, &finding); err != nil {
		t.Fatalf("decode finding: %v", err)
	}
	want := findings.Finding{
		ID:        "a1b2c3d4e5f60708",
		Path:      "cmd/run.go",
		StartLine: 9,
		EndLine:   12,
		Anchor:    findings.AnchorOld,
		Severity:  findings.SeverityNote,
		Body:      "The flag is parsed twice.",
	}
	if finding != want {
		t.Errorf("finding: got %+v, want %+v", finding, want)
	}
}

func TestTranslateDefaultsEndLineAndAnchor(t *testing.T) {
	registry := newRecordRegistry(innerRegistry(t))
	translator, _ := registry.Resolve(review.RecordFindingTool)
	ctx := &recordingContext{}

	status := translator.Translate(ctx, llm.ToolCall{
		CallID:    "call-4",
		Name:      review.RecordFindingTool,
		Arguments: `{"path":"main.go","start_line":3,"severity":"error","body":"Nil map write."}`,
	})
	if status.Error != "" {
		t.Fatalf("translate: unexpected error %q", status.Error)
	}

	var state operation.ValueState
	if err := jsonv2.Unmarshal(ctx.specs[0].State, &state); err != nil {
		t.Fatalf("decode spec state: %v", err)
	}
	var finding findings.Finding
	if err := json.Unmarshal(state.Value, &finding); err != nil {
		t.Fatalf("decode finding: %v", err)
	}
	if finding.EndLine != 3 || finding.Anchor != findings.AnchorNew {
		t.Errorf("defaults: got lines %d-%d anchor %q, want 3-3 anchor %q",
			finding.StartLine, finding.EndLine, finding.Anchor, findings.AnchorNew)
	}
}

func TestTranslateRejectsInvalidArguments(t *testing.T) {
	registry := newRecordRegistry(innerRegistry(t))
	translator, _ := registry.Resolve(review.RecordFindingTool)

	tests := []struct {
		name      string
		arguments string
	}{
		{"malformed json", `{"path":`},
		{"unknown severity", `{"path":"a.go","start_line":1,"severity":"critical","body":"x"}`},
		{"missing severity", `{"path":"a.go","start_line":1,"body":"x"}`},
		{"missing path", `{"start_line":1,"severity":"error","body":"x"}`},
		{"zero line", `{"path":"a.go","start_line":0,"severity":"error","body":"x"}`},
		{"empty body", `{"path":"a.go","start_line":1,"severity":"error","body":"  "}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := &recordingContext{}
			status := translator.Translate(ctx, llm.ToolCall{
				CallID:    "call-5",
				Name:      review.RecordFindingTool,
				Arguments: test.arguments,
			})
			if status.Error == "" {
				t.Error("invalid arguments were accepted")
			}
			if len(ctx.specs) != 0 {
				t.Errorf("submitted specs: got %d, want none for rejected arguments", len(ctx.specs))
			}
		})
	}
}

func TestTranslateResultReportsTheOutcome(t *testing.T) {
	translator := recordFindingTranslator{}

	result, err := translator.TranslateResult("call-7", tool.CallStatus{}, nil)
	if err != nil {
		t.Fatalf("translate result: %v", err)
	}
	if len(result.Output) != 1 || result.Output[0].Kind != llm.ToolResultText || result.Output[0].Value != "recorded" {
		t.Errorf("success result: got %+v, want one text output %q", result.Output, "recorded")
	}
	if result.CallID != "call-7" {
		t.Errorf("call id: got %q, want %q", result.CallID, "call-7")
	}

	failed, err := translator.TranslateResult("call-8", tool.CallStatus{Error: "boom"}, nil)
	if err != nil {
		t.Fatalf("translate result: %v", err)
	}
	if len(failed.Output) != 1 || failed.Output[0].Value != "Error: boom" {
		t.Errorf("failure result: got %+v, want one text output %q", failed.Output, "Error: boom")
	}
}

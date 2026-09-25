package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"unreal-review/internal/findings"
)

func TestRecordAppendsFindingAndSummary(t *testing.T) {
	work := filepath.Join(t.TempDir(), "findings.jsonl.work")
	tool, ok := FindTool(DefaultTools(), "record")
	if !ok {
		t.Fatal("record tool missing")
	}
	out, err := tool.Run(context.Background(), []string{
		"--out", work, "--path", "src/foo.go", "--start", "12", "--severity", "warning", "--body-file", "-",
	}, "This map write races with the reader on line 40.")
	if err != nil {
		t.Fatalf("finding: %v", err)
	}
	if !strings.Contains(out, "src/foo.go:12-12") {
		t.Fatalf("confirmation: %q", out)
	}
	if _, err := tool.Run(context.Background(), []string{"--out", work, "--kind", "summary", "--body-file", "-"}, "Two races in the cache."); err != nil {
		t.Fatalf("summary: %v", err)
	}
	report, err := findings.ReadFile(work)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("findings: %+v", report.Findings)
	}
	finding := report.Findings[0]
	if finding.Path != "src/foo.go" || finding.StartLine != 12 || finding.EndLine != 12 || finding.Severity != findings.SeverityWarning || finding.Body != "This map write races with the reader on line 40." || finding.ID == "" {
		t.Fatalf("finding: %+v", finding)
	}
	if report.Summary != "Two races in the cache." {
		t.Fatalf("summary: %q", report.Summary)
	}
}

func TestRecordContinuesFileWithoutTrailingNewline(t *testing.T) {
	work := filepath.Join(t.TempDir(), "findings.jsonl.work")
	existing := `{"v":1,"type":"summary","body":"earlier"}`
	if err := os.WriteFile(work, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	tool, ok := FindTool(DefaultTools(), "record")
	if !ok {
		t.Fatal("record tool missing")
	}
	if _, err := tool.Run(context.Background(), []string{"--out", work, "--path", "b.go", "--start", "1", "--severity", "error", "--body-file", "-"}, "later"); err != nil {
		t.Fatalf("append: %v", err)
	}
	report, err := findings.ReadFile(work)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if report.Summary != "earlier" || len(report.Findings) != 1 || report.Findings[0].Path != "b.go" {
		t.Fatalf("merged: %+v", report)
	}
}

func TestRecordRejectsInvalidRecords(t *testing.T) {
	work := filepath.Join(t.TempDir(), "findings.jsonl.work")
	tool, ok := FindTool(DefaultTools(), "record")
	if !ok {
		t.Fatal("record tool missing")
	}
	cases := [][]string{
		{"--out", work, "--path", "a.go", "--start", "1", "--severity", "critical", "--body-file", "-"},
		{"--out", work, "--path", " ", "--start", "1", "--severity", "note", "--body-file", "-"},
		{"--out", work, "--path", "a.go", "--start", "3", "--end", "1", "--severity", "note", "--body-file", "-"},
		{"--out", "", "--kind", "summary", "--body-file", "-"},
		{"--out", work, "--kind", "both", "--body-file", "-"},
	}
	for _, args := range cases {
		if _, err := tool.Run(context.Background(), args, "body"); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	valid := []string{"--out", work, "--path", "a.go", "--start", "1", "--severity", "note", "--body-file", "-"}
	if _, err := tool.Run(context.Background(), valid, "  "); err == nil {
		t.Fatal("accepted empty body")
	}
}

func TestPromptSectionListsUsage(t *testing.T) {
	section := promptSection("/usr/local/bin/unreal-review", DefaultTools())
	for _, want := range []string{"/usr/local/bin/unreal-review record", "--kind summary", "<<'EOF'", "--severity error|warning|note"} {
		if !strings.Contains(section, want) {
			t.Fatalf("missing %q in:\n%s", want, section)
		}
	}
}

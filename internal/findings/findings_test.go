package findings

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseRunRejectsBackendFields(t *testing.T) {
	valid := `{"v":1,"type":"run","id":"1","created_at":"2026-09-23T12:00:00Z","status":"complete","source":{"kind":"git","base":"main","head":"HEAD","base_sha":"a","head_sha":"b","diff_sha":"c"},"cost":{"amount_usd":0,"currency":"USD","input_tokens":0,"output_tokens":0,"requests":0}}`
	report, err := Parse(strings.NewReader(valid))
	if err != nil {
		t.Fatalf("valid run: %v", err)
	}
	if report.Run == nil || report.Run.ID != "1" || report.Run.Status != StatusComplete {
		t.Fatalf("valid run: %+v", report.Run)
	}

	for _, field := range []string{"session_id", "backend", "renderer"} {
		leaked := `{"v":1,"type":"run","id":"1","created_at":"2026-09-23T12:00:00Z","status":"complete",` + `"` + field + `":"x","source":{"kind":"git"},"cost":{"amount_usd":0,"currency":"USD"}}`
		_, err := Parse(strings.NewReader(leaked))
		if err == nil {
			t.Fatalf("%s: accepted", field)
		}
		if !strings.Contains(err.Error(), `unknown field "`+field+`"`) {
			t.Fatalf("%s: got %v", field, err)
		}
	}
}

func TestParseFindingAcceptsExtraFields(t *testing.T) {
	raw := `{"v":1,"type":"finding","id":"a1b2c3d4e5f60708","path":"a.go","start_line":1,"end_line":1,"anchor":"new","severity":"note","body":"ok","session_id":"ignored"}`
	report, err := Parse(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("finding with extra field: %v", err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Path != "a.go" {
		t.Fatalf("findings: %+v", report.Findings)
	}
}

func TestReportComplete(t *testing.T) {
	omitted := `{"v":1,"type":"run","id":"1","created_at":"2026-09-23T12:00:00Z","source":{"kind":"git"},"cost":{"amount_usd":0,"currency":"USD"}}`
	report, err := Parse(strings.NewReader(omitted))
	if err != nil {
		t.Fatalf("legacy run: %v", err)
	}
	if !report.Complete() {
		t.Fatal("omitted status should be complete")
	}

	running := `{"v":1,"type":"run","id":"1","created_at":"2026-09-23T12:00:00Z","status":"running","source":{"kind":"git"},"cost":{"amount_usd":0,"currency":"USD"}}`
	report, err = Parse(strings.NewReader(running))
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if report.Complete() {
		t.Fatal("running should not be complete")
	}
}

func TestWriteRunRoundTrip(t *testing.T) {
	in := Report{Run: &Run{
		ID:        "1",
		CreatedAt: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC),
		Model:     "x",
		Status:    StatusComplete,
		Source:    Source{Kind: "git", Base: "main", Head: "HEAD", BaseSHA: "a", HeadSHA: "b", DiffSHA: "c"},
		Cost:      Cost{AmountUSD: 0.5, Currency: "USD", Requests: 1},
	}}
	var buf bytes.Buffer
	if err := Write(&buf, in); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "session_id") || strings.Contains(buf.String(), "backend") || strings.Contains(buf.String(), "renderer") {
		t.Fatalf("run JSON named a backend: %s", buf.String())
	}
	out, err := Parse(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if out.Run == nil || out.Run.ID != "1" || out.Run.Status != StatusComplete || out.Run.Source.DiffSHA != "c" {
		t.Fatalf("round trip: %+v", out.Run)
	}
}

func TestAppendFindingNormalizesLikeParse(t *testing.T) {
	work := filepath.Join(t.TempDir(), "work.jsonl")
	finding, err := AppendFinding(work, Finding{ID: "a1b2c3d4e5f60708", Path: " a.go ", StartLine: 2, Severity: SeverityWarning, Body: "  body  "})
	if err != nil {
		t.Fatalf("append: %v", err)
	}
	if finding.Path != "a.go" || finding.EndLine != 2 || finding.Anchor != AnchorNew || finding.ID == "" {
		t.Fatalf("normalized: %+v", finding)
	}
	parsed, err := ReadFile(work)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(parsed.Findings) != 1 || parsed.Findings[0] != finding {
		t.Fatalf("round trip: %+v vs %+v", parsed.Findings, finding)
	}
}

func TestAppendContinuesFileWithoutTrailingNewline(t *testing.T) {
	work := filepath.Join(t.TempDir(), "work.jsonl")
	existing := `{"v":1,"type":"summary","body":"earlier"}`
	if err := os.WriteFile(work, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AppendFinding(work, Finding{ID: "deadbeefdeadbeef", Path: "b.go", StartLine: 1, Severity: SeverityError, Body: "later"}); err != nil {
		t.Fatalf("append: %v", err)
	}
	parsed, err := ReadFile(work)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if parsed.Summary != "earlier" || len(parsed.Findings) != 1 || parsed.Findings[0].Path != "b.go" {
		t.Fatalf("merged: %+v", parsed)
	}
}

func TestAppendSummaryRequiresBody(t *testing.T) {
	work := filepath.Join(t.TempDir(), "work.jsonl")
	if err := AppendSummary(work, "   "); err == nil {
		t.Fatal("accepted empty summary")
	}
}

func TestNormalizeRejectsInvalidFindings(t *testing.T) {
	cases := []struct {
		name    string
		finding Finding
	}{
		{"bad severity", Finding{Path: "a.go", StartLine: 1, Severity: "critical", Body: "body"}},
		{"blank path", Finding{Path: " ", StartLine: 1, Severity: SeverityNote, Body: "body"}},
		{"end before start", Finding{Path: "a.go", StartLine: 3, EndLine: 1, Severity: SeverityNote, Body: "body"}},
		{"blank body", Finding{Path: "a.go", StartLine: 1, Severity: SeverityNote, Body: "   "}},
	}
	for _, c := range cases {
		if _, err := Normalize(c.finding); err == nil {
			t.Fatalf("%s: accepted %+v", c.name, c.finding)
		}
	}
}

func TestFingerprintDependsOnCodeNotWordingOrLines(t *testing.T) {
	a := Finding{Path: "a.go", StartLine: 10, EndLine: 12, Anchor: AnchorNew, Body: "old wording"}
	b := Finding{Path: "a.go", StartLine: 40, EndLine: 42, Anchor: AnchorNew, Body: "new wording, longer"}
	code := []string{"func f() {", "  return 1", "}"}
	if Fingerprint(a, code, 0) != Fingerprint(b, code, 0) {
		t.Fatal("reworded or line-shifted finding over identical code should keep its ID")
	}

	otherCode := []string{"func f() {", "  return 2", "}"}
	if Fingerprint(a, code, 0) == Fingerprint(a, otherCode, 0) {
		t.Fatal("different flagged code should change the ID")
	}

	padded := []string{"func f() {  ", "\treturn 1", "}"}
	if Fingerprint(a, code, 0) != Fingerprint(a, padded, 0) {
		t.Fatal("leading or trailing whitespace on a line should not change the ID")
	}
}

func TestParseFindingRejectsMissingID(t *testing.T) {
	raw := `{"v":1,"type":"finding","path":"a.go","start_line":1,"end_line":1,"anchor":"new","severity":"note","body":"ok"}`
	_, err := Parse(strings.NewReader(raw))
	if err == nil || !strings.Contains(err.Error(), "id must be set") {
		t.Fatalf("err = %v, want id must be set", err)
	}
}

func TestCheckSummaryMatchesVerdictToFindings(t *testing.T) {
	if _, err := CheckSummary("No material issues; the retry path keeps its backoff.", 0); err != nil {
		t.Fatalf("clean summary without findings: %v", err)
	}
	if _, err := CheckSummary("The cache write races with the reader, so readers can see torn entries.", 2); err != nil {
		t.Fatalf("verdict summary with findings: %v", err)
	}
	if _, err := CheckSummary("No material issues.", 1); err == nil {
		t.Fatal("accepted a clean verdict over a finding")
	}
	if _, err := CheckSummary("Looks fine overall.", 0); err == nil {
		t.Fatal("accepted a summary without the clean verdict when nothing was found")
	}
}

func TestCheckSummaryRejectsStructuredText(t *testing.T) {
	cases := []string{
		"",
		"The map write races.\n\nSummary: one error.",
		"- Error: the map write races.",
		"## Review",
		"1. The map write races.",
		"The map write races. ```go\nx\n```",
		strings.Repeat("a", MaxSummaryLength+1),
	}
	for _, body := range cases {
		if _, err := CheckSummary(body, 1); err == nil {
			t.Fatalf("accepted %q", body)
		}
	}
}

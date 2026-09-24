package findings

import (
	"bytes"
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
	raw := `{"v":1,"type":"finding","path":"a.go","start_line":1,"end_line":1,"anchor":"new","severity":"note","body":"ok","session_id":"ignored"}`
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

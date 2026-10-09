package review

import (
	"testing"

	"github.com/0x7067/unreal-review/internal/findings"
)

func TestReportedInKeepsOnlyTouchedFiles(t *testing.T) {
	reported := []findings.Finding{
		{ID: "a", Path: "src/touched.go", StartLine: 1, EndLine: 2, Severity: findings.SeverityWarning, Body: "First."},
		{ID: "b", Path: "src/untouched.go", StartLine: 5, EndLine: 5, Severity: findings.SeverityError, Body: "Second."},
	}
	got := reportedIn(reported, []ChangedFile{{Path: "src/touched.go", Added: 2}})
	if len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("reported: %+v", got)
	}
	if reportedIn(nil, []ChangedFile{{Path: "src/touched.go"}}) != nil {
		t.Fatal("nothing reported should stay empty")
	}
}

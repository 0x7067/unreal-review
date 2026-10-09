package findings

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// specFile locates a file under the repository spec/ directory by walking up
// from the test working directory to the module root. Stdlib only: this
// package is public and cannot import internal/.
func specFile(t *testing.T, name string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "spec", name)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("go.mod not found above %s", wd)
		}
		dir = parent
	}
}

// specVariants returns the constructor names in a `type X is Data:` block.
func specVariants(t *testing.T, src, typeName string) []string {
	t.Helper()
	pat := regexp.MustCompile(`(?m)^type ` + regexp.QuoteMeta(typeName) + ` is Data:\n((?:  .*\n)+)`)
	m := pat.FindSubmatch([]byte(src))
	if m == nil {
		t.Fatalf("spec/findings.bend: want type %s is Data:, found none", typeName)
	}
	var out []string
	for _, line := range regexp.MustCompile(`(?m)^  ([A-Za-z0-9_]+)\{\}\n?`).FindAllSubmatch(m[1], -1) {
		out = append(out, string(line[1]))
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// spec/findings.bend models the finding vocabularies this package persists.
// The wire values must match in both directions: a renamed variant or a
// changed string breaks the schema contract the model proves properties of.
func TestBendStatusMatchesGo(t *testing.T) {
	src, err := os.ReadFile(specFile(t, "findings.bend"))
	if err != nil {
		t.Fatalf("read spec/findings.bend: %v", err)
	}
	if got, want := specVariants(t, string(src), "Status"),
		[]string{"StatusNone", "StatusRunning", "StatusComplete", "StatusFailed"}; !equalStrings(got, want) {
		t.Errorf("spec Status variants = %v, want %v", got, want)
	}
	// StatusNone has no Go constant: the zero value "" is the none case.
	if string(StatusRunning) != "running" || string(StatusComplete) != "complete" || string(StatusFailed) != "failed" {
		t.Errorf("Go Status values = %q %q %q, want running complete failed",
			string(StatusRunning), string(StatusComplete), string(StatusFailed))
	}
	var zero Status
	if string(zero) != "" {
		t.Errorf("Go Status zero value = %q, want empty (StatusNone)", string(zero))
	}
}

func TestBendAnchorMatchesGo(t *testing.T) {
	src, err := os.ReadFile(specFile(t, "findings.bend"))
	if err != nil {
		t.Fatalf("read spec/findings.bend: %v", err)
	}
	if got, want := specVariants(t, string(src), "Anchor"),
		[]string{"AnchorNew", "AnchorOld"}; !equalStrings(got, want) {
		t.Errorf("spec Anchor variants = %v, want %v", got, want)
	}
	if string(AnchorNew) != "new" || string(AnchorOld) != "old" {
		t.Errorf("Go Anchor values = %q %q, want new old", string(AnchorNew), string(AnchorOld))
	}
}

func TestBendSeverityMatchesGo(t *testing.T) {
	src, err := os.ReadFile(specFile(t, "findings.bend"))
	if err != nil {
		t.Fatalf("read spec/findings.bend: %v", err)
	}
	if got, want := specVariants(t, string(src), "Severity"),
		[]string{"SeverityError", "SeverityWarning", "SeverityNote"}; !equalStrings(got, want) {
		t.Errorf("spec Severity variants = %v, want %v", got, want)
	}
	if string(SeverityError) != "error" || string(SeverityWarning) != "warning" || string(SeverityNote) != "note" {
		t.Errorf("Go Severity values = %q %q %q, want error warning note",
			string(SeverityError), string(SeverityWarning), string(SeverityNote))
	}
}

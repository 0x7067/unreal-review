package review

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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

func TestBendPlanCapsMatchGo(t *testing.T) {
	src, err := os.ReadFile(specFile(t, "plan.bend"))
	if err != nil {
		t.Fatalf("read spec/plan.bend: %v", err)
	}
	for _, tc := range []struct {
		def  string
		want int
	}{{"maxPromptBytes", MaxPlanPromptBytes}, {"directThreshold", maxBriefDiff}} {
		pat := "(?m)^def " + regexp.QuoteMeta(tc.def) + "\\(\\) -> Nat:\\n  (\\d+)n$"
		match := regexp.MustCompile(pat).FindSubmatch(src)
		if match == nil {
			t.Errorf("spec/plan.bend: want literal def %s() -> Nat:, found none", tc.def)
			continue
		}
		got, err := strconv.Atoi(string(match[1]))
		if err != nil || got != tc.want {
			t.Errorf("spec/plan.bend %s()=%d err=%v, Go uses %d", tc.def, got, err, tc.want)
		}
	}
}

func TestBendModelCapsMatchGo(t *testing.T) {
	src, err := os.ReadFile(specFile(t, "group.bend"))
	if err != nil {
		t.Fatalf("read spec/group.bend: %v", err)
	}
	for _, tc := range []struct {
		def  string
		want int
	}{
		{"maxFiles", maxGroupFiles},
		{"maxLines", maxGroupLines},
	} {
		pat := "(?m)^def " + regexp.QuoteMeta(tc.def) + "\\(\\) -> Nat:\n  (\\d+)n$"
		m := regexp.MustCompile(pat).FindSubmatch(src)
		if m == nil {
			t.Errorf("spec/group.bend: want a literal def %s() -> Nat:, found none", tc.def)
			continue
		}
		got, err := strconv.Atoi(string(m[1]))
		if err != nil {
			t.Errorf("spec/group.bend: %s: %v", tc.def, err)
			continue
		}
		if got != tc.want {
			t.Errorf("spec/group.bend %s() = %d, but this package uses %d", tc.def, got, tc.want)
		}
	}
}

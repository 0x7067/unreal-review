package review

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// spec/group.bend is a hand-written Bend model of the grouping logic in this
// package, and it keeps its own copies of the caps. Nothing else ties the two
// together: the laws would stay green while proving things about numbers the
// program no longer uses. Pin each shared constant to the one it models.
func TestBendModelCapsMatchGo(t *testing.T) {
	src, err := os.ReadFile("../../spec/group.bend")
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

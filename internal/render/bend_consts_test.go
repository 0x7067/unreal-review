package render

import (
	"os"
	"regexp"
	"strconv"
	"testing"
)

// spec/github.bend models the inline-comment cap this package enforces, and
// keeps its own copy of the number. Pin them together so a change to one side
// cannot leave the model describing a limit the renderer does not use.
func TestBendModelInlineCapMatchesGo(t *testing.T) {
	src, err := os.ReadFile("../../spec/github.bend")
	if err != nil {
		t.Fatalf("read spec/github.bend: %v", err)
	}
	pat := "(?m)^def cap50\\(\\) -> Nat:\n  (\\d+)n$"
	m := regexp.MustCompile(pat).FindSubmatch(src)
	if m == nil {
		t.Fatal("spec/github.bend: want a literal def cap50() -> Nat:, found none")
	}
	got, err := strconv.Atoi(string(m[1]))
	if err != nil {
		t.Fatal(err)
	}
	if got != maxInlineComments {
		t.Errorf("spec/github.bend cap50() = %d, but this package uses %d", got, maxInlineComments)
	}
}

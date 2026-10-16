package eval

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReadmeQuotesScoringRules(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	readme := strings.Join(strings.Fields(string(raw)), " ")
	for _, rule := range []string{PR38Rule(), LabeledRule()} {
		norm := strings.Join(strings.Fields(rule), " ")
		if !strings.Contains(readme, norm) {
			t.Fatalf("README missing scoring rule:\n%s", norm)
		}
	}
}

package diffmap

import (
	"strings"
	"testing"

	"unreal-review/internal/findings"
)

func TestParseGitDiffKeepsAddedPlusPlusLineOnOriginalPath(t *testing.T) {
	const path = "pkg/widget.go"
	diff := strings.Join([]string{
		"diff --git a/" + path + " b/" + path,
		"--- a/" + path,
		"+++ b/" + path,
		"@@ -1,0 +4,2 @@",
		"+++ keep me",
		"+func next() {}",
		"",
	}, "\n")

	got, err := ParseGitDiff(strings.NewReader(diff))
	if err != nil {
		t.Fatal(err)
	}
	if !got.Contains(path, findings.AnchorNew, 4) {
		t.Fatalf("%s new line 4 missing", path)
	}
	if !got.Contains(path, findings.AnchorNew, 5) {
		t.Fatalf("%s new line 5 missing", path)
	}
}

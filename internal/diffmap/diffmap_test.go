package diffmap

import (
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/internal/findings"
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

func TestMergePatchKeepsAddedDiffGitLineOnFilePath(t *testing.T) {
	const path = "pkg/widget.go"
	patch := strings.Join([]string{
		"@@ -4,2 +4,3 @@",
		" before()",
		"+diff --git a/foo b/foo",
		" after()",
		"",
	}, "\n")

	got := New()
	if err := MergePatch(got, path, patch); err != nil {
		t.Fatal(err)
	}
	if !got.Contains(path, findings.AnchorNew, 4) {
		t.Fatalf("%s new line 4 missing", path)
	}
	if !got.Contains(path, findings.AnchorNew, 5) {
		t.Fatalf("%s new line 5 missing", path)
	}
	if !got.Contains(path, findings.AnchorNew, 6) {
		t.Fatalf("%s new line 6 missing", path)
	}
	if got.Contains("foo", findings.AnchorNew, 5) {
		t.Fatal("added diff header text was recorded on foo")
	}
}

func TestMergePatchParsesFullDiff(t *testing.T) {
	const path = "pkg/widget.go"
	patch := strings.Join([]string{
		"diff --git a/" + path + " b/" + path,
		"--- a/" + path,
		"+++ b/" + path,
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"",
	}, "\n")

	got := New()
	if err := MergePatch(got, "other.go", patch); err != nil {
		t.Fatal(err)
	}
	if !got.Contains(path, findings.AnchorOld, 1) {
		t.Fatalf("%s old line 1 missing", path)
	}
	if !got.Contains(path, findings.AnchorNew, 1) {
		t.Fatalf("%s new line 1 missing", path)
	}
}

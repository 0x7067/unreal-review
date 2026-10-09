package specconf

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/internal/specpath"
)

var lawPattern = regexp.MustCompile(`(?m)^law ([A-Za-z0-9_]+):`)
var funcPattern = regexp.MustCompile(`(?m)^func ([A-Za-z0-9_]+)\(`)

// TestLawsClassified enforces the witness manifest: every law in LAWS.bend is
// either witnessed by a named Go test or explicitly model-only. A new law
// with no classification fails here, as does a witness pointing at a test
// that no longer exists.
func TestLawsClassified(t *testing.T) {
	root, err := moduleRoot()
	if err != nil {
		t.Fatalf("locate module root: %v", err)
	}
	lawsSrc, err := os.ReadFile(filepath.Join(root, "LAWS.bend"))
	if err != nil {
		t.Fatalf("read LAWS.bend: %v", err)
	}
	var laws []string
	for _, m := range lawPattern.FindAllSubmatch(lawsSrc, -1) {
		laws = append(laws, string(m[1]))
	}
	if len(laws) == 0 {
		t.Fatal("LAWS.bend: found no laws")
	}

	witnessed, modelOnly := parseManifest(t, filepath.Join(root, "spec", "witnesses.txt"))

	seen := map[string]string{}
	for law, test := range witnessed {
		if prev, dup := seen[law]; dup {
			t.Errorf("witnesses.txt: law %s listed twice (%s, %s)", law, prev, test)
		}
		seen[law] = "witnessed:" + test
	}
	for _, law := range modelOnly {
		if prev, dup := seen[law]; dup {
			t.Errorf("witnesses.txt: law %s in both sections (%s)", law, prev)
		}
		seen[law] = "model-only"
	}

	for _, law := range laws {
		if _, ok := seen[law]; !ok {
			t.Errorf("LAWS.bend law %s has no entry in spec/witnesses.txt", law)
		}
	}
	for law := range seen {
		found := false
		for _, l := range laws {
			if l == law {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("spec/witnesses.txt names unknown law %s", law)
		}
	}

	testFuncs := collectTestFuncs(t, root)
	for law, test := range witnessed {
		if _, ok := testFuncs[test]; !ok {
			t.Errorf("witnesses.txt: law %s names missing test func %s", law, test)
		}
	}
}

func parseManifest(t *testing.T, path string) (map[string]string, []string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read witnesses.txt: %v", err)
	}
	witnessed := map[string]string{}
	var modelOnly []string
	section := ""
	for i, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = line
			continue
		}
		switch section {
		case "[witnessed]":
			parts := strings.Split(line, "\t")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				t.Fatalf("witnesses.txt line %d: want law<TAB>TestName, got %q", i+1, line)
			}
			witnessed[parts[0]] = parts[1]
		case "[model-only]":
			if strings.Contains(line, "\t") || strings.Contains(line, " ") {
				t.Fatalf("witnesses.txt line %d: want a bare law name, got %q", i+1, line)
			}
			modelOnly = append(modelOnly, line)
		default:
			t.Fatalf("witnesses.txt line %d: outside any section: %q", i+1, line)
		}
	}
	return witnessed, modelOnly
}

func moduleRoot() (string, error) {
	dir, err := specpath.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Dir(dir), nil
}

func collectTestFuncs(t *testing.T, root string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	skip := map[string]bool{".git": true, ".worktrees": true, "bin": true, "vendor": true}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skip[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range funcPattern.FindAllSubmatch(src, -1) {
			out[string(m[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk Go tests: %v", err)
	}
	return out
}

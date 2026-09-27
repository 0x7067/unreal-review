package review

import (
	"fmt"
	"sort"
	"testing"
)

func TestClusterFilesHoldsBendInvariants(t *testing.T) {
	for _, tc := range clusterScenarios() {
		t.Run(tc.name, func(t *testing.T) {
			groups := clusterFiles(tc.files, nil)

			want := pathCounts(tc.files)
			var got []string
			for _, g := range groups {
				got = append(got, pathCounts(g.Files)...)
			}
			sort.Strings(got)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("clusterFiles did not keep every file exactly once:\n want %v\n got  %v", want, got)
			}

			for _, g := range groups {
				if len(g.Files) < 2 {
					continue
				}
				if len(g.Files) > maxGroupFiles {
					t.Errorf("group %q holds %d files, cap is %d", g.Title, len(g.Files), maxGroupFiles)
				}
				if g.Lines() > maxGroupLines {
					t.Errorf("group %q holds %d lines, cap is %d", g.Title, g.Lines(), maxGroupLines)
				}
			}
		})
	}
}

func pathCounts(files []ChangedFile) []string {
	counts := map[string]int{}
	for _, f := range files {
		counts[f.Path]++
	}
	out := make([]string, 0, len(counts))
	for path, n := range counts {
		out = append(out, fmt.Sprintf("%s x%d", path, n))
	}
	sort.Strings(out)
	return out
}

func changedFile(path string, lines int) ChangedFile {
	return ChangedFile{Path: path, Added: lines}
}

func dirFiles(dir string, n, linesEach int) []ChangedFile {
	files := make([]ChangedFile, 0, n)
	for i := range n {
		files = append(files, changedFile(fmt.Sprintf("%s/f%03d.go", dir, i), linesEach))
	}
	return files
}

func clusterScenarios() []struct {
	name  string
	files []ChangedFile
} {
	overFiles := dirFiles("pkg/big", maxGroupFiles+15, 10)
	overLines := dirFiles("pkg/long", maxGroupFiles-1, 200)

	return []struct {
		name  string
		files []ChangedFile
	}{
		{"no files", nil},
		{"one file", []ChangedFile{changedFile("a.go", 3)}},
		{"one file over the line cap", []ChangedFile{changedFile("huge.go", maxGroupLines*3)}},
		{"files at the repository root", []ChangedFile{
			changedFile("main.go", 5), changedFile("README.md", 5), changedFile("go.mod", 2),
		}},
		{"one directory", dirFiles("pkg/small", 4, 10)},
		{"several directories", append(append(dirFiles("pkg/a", 3, 10), dirFiles("pkg/b", 3, 10)...), dirFiles("pkg/c", 3, 10)...)},
		{"over the file cap", overFiles},
		{"over the line cap", overLines},
		{"test beside its implementation", []ChangedFile{
			changedFile("pkg/a/thing.go", 20), changedFile("pkg/a/thing_test.go", 20),
			changedFile("pkg/b/other.go", 20), changedFile("pkg/b/other_test.go", 20),
		}},
		{"module and its lockfiles", []ChangedFile{
			changedFile("go.mod", 2), changedFile("go.sum", 30), changedFile("pkg/a/x.go", 10),
		}},
		{"header beside its source", []ChangedFile{
			changedFile("src/thing.h", 10), changedFile("src/thing.c", 10), changedFile("src/other.c", 10),
		}},
		{"deep tree", []ChangedFile{
			changedFile("a/b/c/d/e/one.go", 4), changedFile("a/b/c/d/e/two.go", 4),
			changedFile("a/b/c/d/three.go", 4), changedFile("a/one.go", 4),
		}},
	}
}

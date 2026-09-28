package review

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// importEdge joins two changed files. goImportEdges emits one when a .go file
// imports exactly one changed package that lives in another directory.
type importEdge struct {
	from string
	to   string
}

func applyImportEdges(files []ChangedFile, groupKey []string, edges []importEdge) {
	index := make(map[string]int, len(files))
	for i, file := range files {
		index[file.Path] = i
	}
	for _, edge := range edges {
		from, okFrom := index[edge.from]
		to, okTo := index[edge.to]
		if !okFrom || !okTo || from == to {
			continue
		}
		unionGroupKey(groupKey, from, to)
	}
}

func unionGroupKey(groupKey []string, a, b int) {
	from, to := groupKey[a], groupKey[b]
	if from == to {
		return
	}
	for i := range groupKey {
		if groupKey[i] == to {
			groupKey[i] = from
		}
	}
}

// goImportEdges reads changed .go files under workspace. The module path comes
// from workspace/go.mod. A file is paired with every changed file of the one
// package it imports in a different directory. Two such packages, a missing
// module, or a file that does not parse contribute no edge.
type importedPkg struct {
	dir   string
	files []string
}

func goImportEdges(workspace string, files []ChangedFile) []importEdge {
	modPath, ok := modulePath(filepath.Join(workspace, "go.mod"))
	if !ok {
		return nil
	}
	byImport := map[string]*importedPkg{}
	var goFiles []string
	for _, file := range files {
		if !strings.HasSuffix(file.Path, ".go") {
			continue
		}
		imp, dir, ok := fileImportPath(modPath, file.Path)
		if !ok {
			continue
		}
		entry := byImport[imp]
		if entry == nil {
			entry = &importedPkg{dir: dir}
			byImport[imp] = entry
		}
		entry.files = append(entry.files, file.Path)
		goFiles = append(goFiles, file.Path)
	}
	var edges []importEdge
	for _, path := range goFiles {
		dir := filepath.ToSlash(filepath.Dir(path))
		hits := uniqueImportedPackage(byImport, dir, parseGoImports(filepath.Join(workspace, filepath.FromSlash(path))))
		if hits == nil {
			continue
		}
		for _, other := range hits.files {
			edges = append(edges, importEdge{from: path, to: other})
		}
	}
	return edges
}

func uniqueImportedPackage(byImport map[string]*importedPkg, importerDir string, imports []string) *importedPkg {
	var hit *importedPkg
	seen := map[string]bool{}
	for _, imp := range imports {
		entry := byImport[imp]
		if entry == nil || entry.dir == importerDir || seen[imp] {
			continue
		}
		seen[imp] = true
		if hit != nil {
			return nil
		}
		hit = entry
	}
	return hit
}

func fileImportPath(modPath, rel string) (imp, dir string, ok bool) {
	dir = filepath.ToSlash(filepath.Dir(rel))
	if dir == "." {
		return modPath, dir, modPath != ""
	}
	if strings.HasPrefix(dir, "../") || dir == ".." {
		return "", "", false
	}
	return modPath + "/" + dir, dir, true
}

func modulePath(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if i := strings.Index(line, "//"); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		rest, ok := strings.CutPrefix(line, "module ")
		if !ok {
			continue
		}
		rest = strings.Trim(strings.TrimSpace(rest), `"`)
		if rest == "" || strings.ContainsAny(rest, " \t") {
			return "", false
		}
		return rest, true
	}
	return "", false
}

func parseGoImports(path string) []string {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), path, src, parser.ImportsOnly)
	if err != nil || file == nil {
		return nil
	}
	var out []string
	for _, spec := range file.Imports {
		imp, err := strconv.Unquote(spec.Path.Value)
		if err != nil || imp == "" || imp == "C" {
			continue
		}
		out = append(out, imp)
	}
	return out
}

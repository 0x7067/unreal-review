package review

import (
	"context"
	"errors"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// importEdge joins the importing file to one representative of the package it
// imports. Only that file's group key changes; directory siblings stay in
// their own group. The representative stands for every changed file that
// still shares its key, so a package of many files does not become one edge each.
type importEdge struct {
	from string
	to   string
}

// applyImportEdges moves each importing file onto the stable root of its
// import parents. The parent map is file indexes, not directory keys, so a
// sibling that did not import is never rewritten. Roots are resolved from
// the whole edge list before any key is copied, so a representative that is
// itself an importer does not leave earlier importers on a stale key.
func applyImportEdges(files []ChangedFile, groupKey []string, edges []importEdge) {
	if len(edges) == 0 {
		return
	}
	index := make(map[string]int, len(files))
	for i, file := range files {
		index[file.Path] = i
	}
	parent := make([]int, len(files))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	source := make([]bool, len(files))
	for _, edge := range edges {
		from, okFrom := index[edge.from]
		to, okTo := index[edge.to]
		if !okFrom || !okTo || from == to {
			continue
		}
		ra, rb := find(from), find(to)
		if ra != rb {
			parent[ra] = rb
		}
		source[from] = true
	}
	orig := append([]string(nil), groupKey...)
	for i, isSource := range source {
		if isSource {
			groupKey[i] = orig[find(i)]
		}
	}
}

// goImportEdges pairs a changed .go file with the single changed package it
// imports in another directory. go.mod and those sources are read from the
// destination of the range: the working tree when head is empty (workspace
// mode, or --from with no --to), otherwise the resolved head commit. A later
// checkout does not supply imports for --to, --commit, or --branch.
func goImportEdges(ctx context.Context, workspace string, r resolved, files []ChangedFile) ([]importEdge, error) {
	modData, err := readRangeFile(ctx, workspace, r, "go.mod")
	if err != nil {
		return nil, err
	}
	if modData == nil {
		return nil, nil
	}
	modPath, ok := parseModulePath(string(modData))
	if !ok {
		return nil, nil
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
		// Path-min, not first-seen. Workspace numstat appends untracked files
		// after tracked ones, so first-seen would pick a different representative
		// than a commit range whose paths sort the other way.
		entry := byImport[imp]
		if entry == nil || strings.Compare(file.Path, entry.rep) < 0 {
			byImport[imp] = &importedPkg{dir: dir, rep: file.Path}
		}
		goFiles = append(goFiles, file.Path)
	}
	var edges []importEdge
	for _, path := range goFiles {
		src, err := readRangeFile(ctx, workspace, r, path)
		if err != nil {
			return nil, err
		}
		dir := filepath.ToSlash(filepath.Dir(path))
		hit := uniqueImportedPackage(byImport, dir, parseGoImports(src))
		if hit == nil || hit.rep == path {
			continue
		}
		edges = append(edges, importEdge{from: path, to: hit.rep})
	}
	return edges, nil
}

type importedPkg struct {
	dir string
	rep string
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

// readRangeFile returns the destination bytes. A missing file is (nil, nil).
// Cancellation and any other read failure are returned so grouping fails
// instead of dropping import edges.
func readRangeFile(ctx context.Context, workspace string, r resolved, rel string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.head == "" {
		data, err := os.ReadFile(filepath.Join(workspace, filepath.FromSlash(rel)))
		if err == nil {
			return data, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	rev := r.headSHA
	if rev == "" {
		rev = r.head
	}
	return gitBlob(ctx, workspace, rev, rel)
}

func parseModulePath(data string) (string, bool) {
	for _, line := range strings.Split(data, "\n") {
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

func parseGoImports(src []byte) []string {
	if len(src) == 0 {
		return nil
	}
	file, err := parser.ParseFile(token.NewFileSet(), "", src, parser.ImportsOnly)
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

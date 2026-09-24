package review

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/odvcencio/gotreesitter"
	csharp "github.com/odvcencio/gotreesitter/grammars/c_sharp"
	golang "github.com/odvcencio/gotreesitter/grammars/go"
	"github.com/odvcencio/gotreesitter/grammars/java"
	"github.com/odvcencio/gotreesitter/grammars/javascript"
	"github.com/odvcencio/gotreesitter/grammars/kotlin"
	"github.com/odvcencio/gotreesitter/grammars/php"
	"github.com/odvcencio/gotreesitter/grammars/python"
	"github.com/odvcencio/gotreesitter/grammars/rust"
	"github.com/odvcencio/gotreesitter/grammars/swift"
	"github.com/odvcencio/gotreesitter/grammars/tsx"
	"github.com/odvcencio/gotreesitter/grammars/typescript"
	"github.com/odvcencio/gotreesitter/grammars/vue"
)

func attachImports(files []ChangedFile, groupKey []string, src map[string][]byte) {
	if len(src) == 0 {
		return
	}
	parent := make(map[string]string, len(files))
	for _, key := range groupKey {
		parent[key] = key
	}
	union := func(a, b string) {
		ra, rb := findGroup(parent, a), findGroup(parent, b)
		if ra != rb {
			parent[rb] = ra
		}
	}
	for i, file := range files {
		body, ok := src[file.Path]
		if !ok {
			continue
		}
		for _, spec := range importSpecifiers(file.Path, body) {
			j, ok := uniqueImportMatch(file.Path, spec, files)
			if !ok || j == i {
				continue
			}
			union(groupKey[i], groupKey[j])
		}
	}
	for i := range files {
		groupKey[i] = findGroup(parent, groupKey[i])
	}
}

func findGroup(parent map[string]string, k string) string {
	for parent[k] != "" && parent[k] != k {
		if p := parent[parent[k]]; p != "" {
			parent[k] = p
		}
		k = parent[k]
	}
	return k
}

func importSpecifiers(path string, src []byte) []string {
	lang := languageFor(path)
	if lang == nil {
		return nil
	}
	tree, err := gotreesitter.NewParser(lang).Parse(src)
	if err != nil || tree == nil {
		return nil
	}
	defer tree.Release()
	switch lang.Name {
	case "go", "java", "python":
		return specsFromRefs(gotreesitter.ExtractImports(tree))
	default:
		return specsFromWalk(tree, lang, src)
	}
}

func specsFromRefs(refs []gotreesitter.ImportRef) []string {
	var out []string
	for _, ref := range refs {
		if ref.Kind == "package" {
			continue
		}
		if ref.Relative > 0 {
			rel := strings.Repeat("../", ref.Relative)
			if ref.Relative == 1 {
				rel = "./"
			}
			base := ref.Path
			if base == "" {
				base = joinDotted(ref.From, ref.Name)
			}
			base = strings.ReplaceAll(base, ".", "/")
			if base != "" {
				out = append(out, rel+base)
			}
			continue
		}
		spec := ref.Path
		if spec == "" {
			spec = ref.From
		}
		if spec != "" {
			out = append(out, spec)
		}
	}
	return out
}

func joinDotted(parts ...string) string {
	var bits []string
	for _, p := range parts {
		if p != "" && p != "*" {
			bits = append(bits, p)
		}
	}
	return strings.Join(bits, ".")
}

func specsFromWalk(tree *gotreesitter.Tree, lang *gotreesitter.Language, src []byte) []string {
	var out []string
	var walk func(*gotreesitter.Node)
	walk = func(n *gotreesitter.Node) {
		if n == nil {
			return
		}
		kind := n.Type(lang)
		if isImportNode(lang.Name, kind) {
			if spec := specifierFromNode(n, lang, src); spec != "" {
				out = append(out, spec)
			}
			if kind != "call_expression" {
				return
			}
		}
		if kind == "call_expression" && isRequireCall(n, lang, src) {
			if spec := firstStringLiteral(n, lang, src); spec != "" {
				out = append(out, spec)
			}
			return
		}
		for i := 0; i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return out
}

func isImportNode(lang, kind string) bool {
	switch lang {
	case "javascript", "typescript", "tsx", "vue":
		return kind == "import_statement"
	case "kotlin":
		return kind == "import_header"
	case "rust":
		return kind == "use_declaration"
	case "php":
		return kind == "namespace_use_clause" || kind == "namespace_use_declaration"
	case "c_sharp":
		return kind == "using_directive" || kind == "global_using_directive"
	case "swift":
		return kind == "import_declaration"
	default:
		return false
	}
}

func isRequireCall(n *gotreesitter.Node, lang *gotreesitter.Language, src []byte) bool {
	fn := n.ChildByFieldName("function", lang)
	if fn == nil {
		if n.NamedChildCount() > 0 {
			fn = n.NamedChild(0)
		}
	}
	if fn == nil {
		return false
	}
	return fn.Type(lang) == "identifier" && fn.Text(src) == "require"
}

func specifierFromNode(n *gotreesitter.Node, lang *gotreesitter.Language, src []byte) string {
	if s := firstStringLiteral(n, lang, src); s != "" {
		return s
	}
	return cleanImportText(n.Text(src), lang.Name)
}

func firstStringLiteral(n *gotreesitter.Node, lang *gotreesitter.Language, src []byte) string {
	var found string
	var walk func(*gotreesitter.Node)
	walk = func(c *gotreesitter.Node) {
		if c == nil || found != "" {
			return
		}
		switch c.Type(lang) {
		case "string", "string_literal", "interpreted_string_literal", "raw_string_literal", "encoded_string_literal":
			found = unquoteImport(c.Text(src))
			return
		}
		for i := 0; i < c.ChildCount(); i++ {
			walk(c.Child(i))
		}
	}
	walk(n)
	return found
}

func cleanImportText(text, lang string) string {
	t := strings.TrimSpace(text)
	t = strings.TrimSuffix(t, ";")
	switch lang {
	case "kotlin", "swift", "java":
		t = strings.TrimSpace(strings.TrimPrefix(t, "import"))
	case "c_sharp":
		t = strings.TrimSpace(strings.TrimPrefix(t, "global using"))
		t = strings.TrimSpace(strings.TrimPrefix(t, "using"))
		t = strings.TrimSpace(strings.TrimPrefix(t, "static"))
	case "rust":
		t = strings.TrimSpace(strings.TrimPrefix(t, "use"))
		if i := strings.IndexAny(t, "{;"); i >= 0 {
			t = strings.TrimSpace(t[:i])
		}
		t = strings.TrimSuffix(t, "::")
	case "php":
		t = strings.TrimSpace(strings.TrimPrefix(t, "use"))
		t = strings.TrimSpace(strings.TrimPrefix(t, "function"))
		t = strings.TrimSpace(strings.TrimPrefix(t, "const"))
	}
	if i := strings.Index(t, " as "); i >= 0 {
		t = strings.TrimSpace(t[:i])
	}
	return strings.TrimSpace(t)
}

func unquoteImport(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, ";")
	if len(s) >= 2 {
		switch s[0] {
		case '"', '\'', '`':
			if s[len(s)-1] == s[0] {
				return s[1 : len(s)-1]
			}
		}
	}
	return s
}

func uniqueImportMatch(fromPath, spec string, files []ChangedFile) (int, bool) {
	spec = unquoteImport(spec)
	if spec == "" || spec == "*" {
		return 0, false
	}
	var hits []int
	if isRelativeSpec(spec) {
		hits = matchRelative(fromPath, spec, files)
	} else {
		hits = matchPackage(fromPath, spec, files)
	}
	if len(hits) != 1 {
		return 0, false
	}
	return hits[0], true
}

func isRelativeSpec(spec string) bool {
	return spec == "." || spec == ".." || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../")
}

func matchRelative(fromPath, spec string, files []ChangedFile) []int {
	dir := filepath.ToSlash(filepath.Dir(fromPath))
	target := path.Clean(dir + "/" + spec)
	ext := filepath.Ext(fromPath)
	cands := []string{
		target,
		target + ext,
		target + "/index" + ext,
		target + "/__init__.py",
		target + "/mod.rs",
		target + ".vue",
	}
	seen := map[string]struct{}{}
	var hits []int
	for i, file := range files {
		p := filepath.ToSlash(file.Path)
		for _, cand := range cands {
			if _, ok := seen[p]; ok {
				break
			}
			if p == cand {
				seen[p] = struct{}{}
				hits = append(hits, i)
			}
		}
	}
	return hits
}

func matchPackage(fromPath, spec string, files []ChangedFile) []int {
	ext := strings.ToLower(filepath.Ext(fromPath))
	switch ext {
	case ".go":
		if !strings.Contains(spec, "/") {
			return nil
		}
		return matchGoImport(spec, files)
	case ".py":
		p := strings.ReplaceAll(spec, ".", "/")
		return matchExact(files, p+".py", p+"/__init__.py")
	case ".java":
		return matchExact(files, strings.ReplaceAll(spec, ".", "/")+".java")
	case ".kt", ".kts":
		base := strings.ReplaceAll(spec, ".", "/")
		return matchExact(files, base+".kt", base+".kts")
	case ".cs":
		return matchExact(files, strings.ReplaceAll(spec, ".", "/")+".cs")
	case ".php":
		return matchExact(files, strings.ReplaceAll(spec, "\\", "/")+".php")
	case ".rs":
		p := strings.TrimPrefix(spec, "crate::")
		p = strings.ReplaceAll(p, "::", "/")
		if p == spec && !strings.Contains(spec, "::") && !strings.Contains(spec, "/") {
			return nil
		}
		return matchExact(files, p+".rs", p+"/mod.rs")
	default:
		return nil
	}
}

func matchGoImport(spec string, files []ChangedFile) []int {
	var hits []int
	for i, file := range files {
		if strings.ToLower(filepath.Ext(file.Path)) != ".go" {
			continue
		}
		dir := filepath.ToSlash(filepath.Dir(file.Path))
		if dir == spec || strings.HasSuffix(spec, "/"+dir) || strings.HasSuffix(dir, "/"+spec) {
			hits = append(hits, i)
		}
	}
	return hits
}

func matchExact(files []ChangedFile, cands ...string) []int {
	want := map[string]struct{}{}
	for _, c := range cands {
		want[filepath.ToSlash(c)] = struct{}{}
	}
	var hits []int
	for i, file := range files {
		p := filepath.ToSlash(file.Path)
		if _, ok := want[p]; ok {
			hits = append(hits, i)
			continue
		}
		for c := range want {
			if strings.HasSuffix(p, "/"+c) {
				hits = append(hits, i)
				break
			}
		}
	}
	return hits
}

func languageFor(p string) *gotreesitter.Language {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".go":
		return golang.Language()
	case ".js", ".mjs", ".cjs", ".jsx":
		return javascript.Language()
	case ".ts":
		return typescript.Language()
	case ".tsx":
		return tsx.Language()
	case ".py":
		return python.Language()
	case ".java":
		return java.Language()
	case ".kt", ".kts":
		return kotlin.Language()
	case ".rs":
		return rust.Language()
	case ".php":
		return php.Language()
	case ".cs":
		return csharp.Language()
	case ".swift":
		return swift.Language()
	case ".vue":
		return vue.Language()
	default:
		return nil
	}
}

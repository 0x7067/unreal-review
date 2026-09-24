package review

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

type ChangedFile struct {
	Path    string
	Added   int
	Deleted int
	Binary  bool
}

func (f ChangedFile) Lines() int {
	if f.Binary {
		return 0
	}
	return f.Added + f.Deleted
}

type FileGroup struct {
	Title     string
	Pathspecs []string
	Files     []ChangedFile
}

func (g FileGroup) Lines() int {
	n := 0
	for _, file := range g.Files {
		n += file.Lines()
	}
	return n
}

type GroupResult struct {
	Spec   Spec
	From   string
	To     string
	Groups []FileGroup
}

func Groups(ctx context.Context, workspace string, spec Spec, paths, exclude []string) (GroupResult, error) {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return GroupResult{}, fmt.Errorf("workspace: %w", err)
	}
	r, err := resolveSpec(ctx, workspace, spec)
	if err != nil {
		return GroupResult{}, err
	}
	files, err := collectNumstat(ctx, workspace, r, pathspecScope(paths, exclude))
	if err != nil {
		return GroupResult{}, err
	}
	return GroupResult{Spec: spec, From: r.base, To: r.head, Groups: clusterFiles(files)}, nil
}

func parseNumstat(raw string) ([]ChangedFile, error) {
	var files []ChangedFile
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		added, rest, ok := strings.Cut(line, "\t")
		if !ok {
			return nil, fmt.Errorf("numstat: %q", line)
		}
		deleted, path, ok := strings.Cut(rest, "\t")
		if !ok {
			return nil, fmt.Errorf("numstat: %q", line)
		}
		path = parseNumstatPath(path)
		if path == "" {
			continue
		}
		file := ChangedFile{Path: path}
		if added == "-" && deleted == "-" {
			file.Binary = true
			files = append(files, file)
			continue
		}
		a, err := strconv.Atoi(added)
		if err != nil {
			return nil, fmt.Errorf("numstat added %q: %w", added, err)
		}
		d, err := strconv.Atoi(deleted)
		if err != nil {
			return nil, fmt.Errorf("numstat deleted %q: %w", deleted, err)
		}
		file.Added = a
		file.Deleted = d
		files = append(files, file)
	}
	return files, nil
}

func parseNumstatPath(path string) string {
	path = unquoteGit(path)
	path = renameNewPath(path)
	return filepath.ToSlash(path)
}

func unquoteGit(path string) string {
	if len(path) >= 2 && path[0] == '"' {
		if s, err := strconv.Unquote(path); err == nil {
			return s
		}
	}
	return path
}

func renameNewPath(path string) string {
	if i := strings.Index(path, "{"); i >= 0 {
		j := strings.Index(path, " => ")
		k := strings.Index(path, "}")
		if j > i && k > j {
			return path[:i] + path[j+4:k] + path[k+1:]
		}
	}
	if i := strings.Index(path, " => "); i >= 0 {
		return path[i+4:]
	}
	return path
}

const (
	maxGroupFiles = 25
	maxGroupLines = 2000
)

func clusterFiles(files []ChangedFile) []FileGroup {
	if len(files) == 0 {
		return nil
	}
	files = append([]ChangedFile(nil), files...)
	slices.SortFunc(files, func(a, b ChangedFile) int {
		return strings.Compare(a.Path, b.Path)
	})
	groupKey := make([]string, len(files))
	for i, file := range files {
		dir := filepath.ToSlash(filepath.Dir(file.Path))
		if dir == "." {
			groupKey[i] = "file:" + file.Path
			continue
		}
		groupKey[i] = dir
	}
	attachTests(files, groupKey)
	mergeFamilies(files, groupKey, localeFamilyKey, 0)
	mergeFamilies(files, groupKey, headerFamilyKey, 4)
	buckets := make(map[string][]ChangedFile)
	for i, file := range files {
		key := groupKey[i]
		buckets[key] = append(buckets[key], file)
	}
	groups := groupsFromBuckets(buckets, files)
	groups = splitOversized(groups, files)
	slices.SortFunc(groups, func(a, b FileGroup) int {
		return strings.Compare(a.Files[0].Path, b.Files[0].Path)
	})
	return groups
}

func attachTests(files []ChangedFile, groupKey []string) {
	type implRef struct {
		index int
		dup   bool
	}
	impl := make(map[string]*implRef)
	for i, file := range files {
		stem, ext, isTest := splitTestName(file.Path)
		if isTest || stem == "" {
			continue
		}
		key := stem + "\x00" + ext
		if existing, ok := impl[key]; ok {
			existing.dup = true
			continue
		}
		impl[key] = &implRef{index: i}
	}
	for i, file := range files {
		stem, ext, isTest := splitTestName(file.Path)
		if !isTest || stem == "" {
			continue
		}
		ref, ok := impl[stem+"\x00"+ext]
		if !ok || ref.dup {
			continue
		}
		groupKey[i] = groupKey[ref.index]
	}
}

func mergeFamilies(files []ChangedFile, groupKey []string, keyFn func(string) (string, bool), maxN int) {
	buckets := make(map[string][]int)
	for i, file := range files {
		key, ok := keyFn(file.Path)
		if !ok {
			continue
		}
		buckets[key] = append(buckets[key], i)
	}
	for key, idxs := range buckets {
		if len(idxs) < 2 {
			continue
		}
		if maxN > 0 && len(idxs) > maxN {
			continue
		}
		owner := "family:" + key
		for _, i := range idxs {
			groupKey[i] = owner
		}
	}
}

func groupsFromBuckets(buckets map[string][]ChangedFile, all []ChangedFile) []FileGroup {
	groups := make([]FileGroup, 0, len(buckets))
	for _, grouped := range buckets {
		specs := groupPathspecs(grouped, all)
		groups = append(groups, FileGroup{
			Title:     groupTitle(grouped, specs),
			Pathspecs: specs,
			Files:     grouped,
		})
	}
	return groups
}

func splitOversized(groups []FileGroup, all []ChangedFile) []FileGroup {
	var out []FileGroup
	for _, group := range groups {
		if len(group.Files) <= maxGroupFiles && group.Lines() <= maxGroupLines {
			out = append(out, group)
			continue
		}
		parts := splitByNextDir(group.Files)
		if len(parts) <= 1 {
			out = append(out, group)
			continue
		}
		childBuckets := make(map[string][]ChangedFile, len(parts))
		for k, files := range parts {
			childBuckets[k] = files
		}
		out = append(out, splitOversized(groupsFromBuckets(childBuckets, all), all)...)
	}
	return out
}

func splitByNextDir(files []ChangedFile) map[string][]ChangedFile {
	prefix := commonDirPrefix(files)
	parts := make(map[string][]ChangedFile)
	for _, file := range files {
		dir := filepath.ToSlash(filepath.Dir(file.Path))
		key := ""
		if prefix != "" && prefix != "." {
			if dir == prefix {
				key = ""
			} else if strings.HasPrefix(dir, prefix+"/") {
				key, _, _ = strings.Cut(dir[len(prefix)+1:], "/")
			} else {
				key, _, _ = strings.Cut(dir, "/")
			}
		} else if dir != "." {
			key, _, _ = strings.Cut(dir, "/")
		}
		parts[key] = append(parts[key], file)
	}
	return parts
}

func commonDirPrefix(files []ChangedFile) string {
	if len(files) == 0 {
		return ""
	}
	prefix := strings.Split(filepath.ToSlash(filepath.Dir(files[0].Path)), "/")
	for _, file := range files[1:] {
		dir := strings.Split(filepath.ToSlash(filepath.Dir(file.Path)), "/")
		n := len(prefix)
		if len(dir) < n {
			n = len(dir)
		}
		i := 0
		for i < n && prefix[i] == dir[i] {
			i++
		}
		prefix = prefix[:i]
	}
	return strings.Join(prefix, "/")
}

func groupPathspecs(files, all []ChangedFile) []string {
	dirs := make(map[string]struct{})
	for _, file := range files {
		dirs[filepath.ToSlash(filepath.Dir(file.Path))] = struct{}{}
	}
	if len(dirs) == 1 {
		var dir string
		for d := range dirs {
			dir = d
		}
		if dir != "." && exclusivePrefix(dir, files, all) {
			return []string{dir}
		}
	}
	specs := make([]string, len(files))
	for i, file := range files {
		specs[i] = file.Path
	}
	return specs
}

func exclusivePrefix(prefix string, files, all []ChangedFile) bool {
	inGroup := make(map[string]struct{}, len(files))
	for _, file := range files {
		inGroup[file.Path] = struct{}{}
	}
	matched := 0
	for _, file := range all {
		if file.Path != prefix && !strings.HasPrefix(file.Path, prefix+"/") {
			continue
		}
		if _, ok := inGroup[file.Path]; !ok {
			return false
		}
		matched++
	}
	return matched == len(files)
}

func groupTitle(files []ChangedFile, pathspecs []string) string {
	if len(pathspecs) == 1 {
		return pathspecs[0]
	}
	names := make([]string, len(files))
	seen := map[string]int{}
	dup := false
	for i, file := range files {
		base := filepath.Base(file.Path)
		names[i] = base
		seen[base]++
		if seen[base] > 1 {
			dup = true
		}
	}
	if dup {
		for i, file := range files {
			names[i] = file.Path
		}
	}
	if len(names) <= 3 {
		return strings.Join(names, ", ")
	}
	return fmt.Sprintf("%s, %s, +%d", names[0], names[1], len(names)-2)
}

func splitTestName(path string) (stem, ext string, isTest bool) {
	base := filepath.Base(path)
	ext = filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	switch {
	case strings.HasSuffix(name, "_test"):
		return strings.TrimSuffix(name, "_test"), ext, true
	case strings.HasSuffix(name, ".test"):
		return strings.TrimSuffix(name, ".test"), ext, true
	case strings.HasSuffix(name, ".spec"):
		return strings.TrimSuffix(name, ".spec"), ext, true
	case strings.HasPrefix(name, "test_"):
		return strings.TrimPrefix(name, "test_"), ext, true
	default:
		return name, ext, false
	}
}

func localeFamilyKey(path string) (string, bool) {
	if key, ok := localeDirKey(path); ok {
		return key, true
	}
	ext := strings.ToLower(filepath.Ext(path))
	if !localeExts[ext] {
		return "", false
	}
	dir := filepath.ToSlash(filepath.Dir(path))
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	base := stripLocaleSuffix(stem)
	if base == "" {
		return "", false
	}
	return "locale\x00" + dir + "\x00" + strings.ToLower(base) + "\x00" + ext, true
}

func localeDirKey(path string) (string, bool) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) < 3 {
		return "", false
	}
	i18nAt := -1
	for i, part := range parts[:len(parts)-1] {
		if i18nDirs[strings.ToLower(part)] {
			i18nAt = i
			break
		}
	}
	if i18nAt < 0 {
		return "", false
	}
	for i := i18nAt + 1; i < len(parts)-1; i++ {
		if !isLocalePathTag(parts[i]) {
			continue
		}
		replaced := append([]string(nil), parts...)
		replaced[i] = "*"
		return "localedir\x00" + strings.Join(replaced, "/"), true
	}
	return "", false
}

func headerFamilyKey(path string) (string, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	stem := strings.ToLower(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)))
	if stem == "" {
		return "", false
	}
	switch ext {
	case ".c", ".h":
		return "c\x00" + stem, true
	case ".cc", ".hh", ".cpp", ".hpp", ".cxx", ".hxx":
		return "cxx\x00" + stem, true
	default:
		return "", false
	}
}

func stripLocaleSuffix(stem string) string {
	s := stem
	for {
		next, ok := cutOneLocaleSuffix(s)
		if !ok {
			return s
		}
		s = next
	}
}

func cutOneLocaleSuffix(stem string) (string, bool) {
	i := strings.LastIndexAny(stem, "_-")
	if i <= 0 {
		return "", false
	}
	tag := stem[i+1:]
	low := strings.ToLower(tag)
	if localeLangs[low] || localeScripts[low] {
		return stem[:i], true
	}
	if len(low) == 2 && isAlpha(low) {
		prefix := stem[:i]
		j := strings.LastIndexAny(prefix, "_-")
		lang := prefix
		if j >= 0 {
			lang = prefix[j+1:]
		}
		if localeLangs[strings.ToLower(lang)] {
			return prefix, true
		}
	}
	return "", false
}

func isLocalePathTag(part string) bool {
	low := strings.ToLower(part)
	if localeLangs[low] || localeScripts[low] {
		return true
	}
	lang, rest, ok := strings.Cut(low, "_")
	if !ok {
		lang, rest, ok = strings.Cut(low, "-")
	}
	if !ok {
		return false
	}
	if !localeLangs[lang] {
		return false
	}
	region, script, ok := strings.Cut(rest, "_")
	if !ok {
		region, script, ok = strings.Cut(rest, "-")
	}
	if !ok {
		return len(rest) == 2 && isAlpha(rest) || localeScripts[rest]
	}
	return (len(region) == 2 && isAlpha(region) || localeScripts[region]) &&
		(localeScripts[script] || len(script) == 2 && isAlpha(script))
}

func isAlpha(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i] | 32
		if c < 'a' || c > 'z' {
			return false
		}
	}
	return true
}

var localeExts = map[string]bool{
	".properties": true,
	".json":       true,
	".po":         true,
	".pot":        true,
	".strings":    true,
	".resx":       true,
	".xliff":      true,
	".xlf":        true,
	".arb":        true,
}

var i18nDirs = map[string]bool{
	"locales":      true,
	"locale":       true,
	"i18n":         true,
	"l10n":         true,
	"lang":         true,
	"langs":        true,
	"language":     true,
	"languages":    true,
	"translations": true,
	"translation":  true,
	"intl":         true,
}

var localeScripts = map[string]bool{
	"hans": true,
	"hant": true,
	"latn": true,
	"cyrl": true,
	"arab": true,
}

var localeLangs = localeSet("aa ab ae af ak am an ar as av ay az ba be bg bh bi bm bn bo br bs ca ce ch co cr cs cu cv cy da de dv dz ee el en eo es et eu fa ff fi fj fo fr fy ga gd gl gn gu gv ha he hi ho hr ht hu hy hz ia id ie ig ii ik io is it iu ja jv ka kg ki kj kk kl km kn ko kr ks ku kv kw ky la lb lg li ln lo lt lu lv mg mh mi mk ml mn mr ms mt my na nb nd ne ng nl nn no nr nv ny oc oj om or os pa pi pl ps pt qu rm rn ro ru rw sa sc sd se sg si sk sl sm sn so sq sr ss st su sv sw ta te tg th ti tk tl tn to tr ts tt tw ty ug uk ur uz ve vi vo wa wo xh yi yo za zh zu")

func localeSet(s string) map[string]bool {
	m := make(map[string]bool)
	for _, w := range strings.Fields(s) {
		m[w] = true
	}
	return m
}

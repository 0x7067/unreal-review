package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/settings"

	"github.com/0x7067/unreal-review/findings"
)

const (
	// compactionMinCutoff is the lowest accepted cutoff. A lower cutoff
	// compacted on nearly every other turn in a real review.
	//
	// At least one regular turn between compactions is guaranteed. unreal-agent
	// v0.3.1 Compact clears prefixTokens, so NeedsCompaction is false until a
	// regular response arrives. The post-compaction context is normally well
	// under the floor but not guaranteed: the summary is uncapped model output,
	// and the retained tail copies whole turns and the staged suffix verbatim.
	compactionMinCutoff = 150_000
	// The handoff lists the newest findings from disk. When the current
	// findings path is a child file in this run's planned or focused stage
	// directory, sibling stage files in that directory are included. Path
	// and claim are collapsed to one line and truncated so the block stays small.
	maxRecordedFindings = 20
	maxClaimBytes       = 160
	// The handoff read is bounded: at most this many stage files, the names
	// that sort last, and at most this many bytes from each file. A longer
	// file contributes its tail. The current stage file stays if the file
	// cap would drop it. Nothing outside the stage directory is opened.
	maxStageFindingFiles = 128
	maxStageFindingBytes = 256 << 10
)

// CompactionThreshold resolves the harness cutoff in tokens of the latest
// model response (input tokens plus output tokens). An empty spec leaves
// compaction off and returns no note. An explicit percent with an unknown
// window is an error. The returned threshold is never zero.
func CompactionThreshold(model, spec, window string) (int64, string, error) {
	spec = strings.TrimSpace(spec)
	parsed, err := parseCompactionSpec(spec)
	if err != nil {
		return 0, "", err
	}
	switch parsed.mode {
	case compactionOff:
		if spec == "" {
			return compactionDisabled, "", nil
		}
		return compactionDisabled, "compaction off", nil
	case compactionTokens:
		if parsed.tokens < compactionMinCutoff {
			return 0, "", cutoffFloorError(parsed.tokens)
		}
		return parsed.tokens, cutoffNote(parsed.tokens), nil
	default:
		size, known, err := contextWindow(model, window)
		if err != nil {
			return 0, "", err
		}
		if !known {
			return 0, "", fmt.Errorf("compaction %q: context window unknown for %q", spec, model)
		}
		return applyPercent(size, parsed.percent)
	}
}

func cutoffNote(tokens int64) string {
	return fmt.Sprintf("compaction cutoff %d tokens", tokens)
}

func cutoffFloorError(tokens int64) error {
	return fmt.Errorf("compaction cutoff %d tokens is below %d", tokens, compactionMinCutoff)
}

func applyPercent(window int64, percent int) (int64, string, error) {
	// percent is 1..100, so this is exact and cannot overflow int64.
	threshold := window/100*int64(percent) + (window%100)*int64(percent)/100
	if threshold < compactionMinCutoff {
		return 0, "", cutoffFloorError(threshold)
	}
	return threshold, cutoffNote(threshold), nil
}

type compactionMode int

const (
	compactionOff compactionMode = iota
	compactionTokens
	compactionPercent
)

type compactionSpec struct {
	mode    compactionMode
	percent int
	tokens  int64
}

func parseCompactionSpec(spec string) (compactionSpec, error) {
	switch {
	case spec == "":
		return compactionSpec{mode: compactionOff}, nil
	case strings.EqualFold(spec, "off"):
		return compactionSpec{mode: compactionOff}, nil
	case strings.HasSuffix(spec, "%"):
		percent, err := strconv.Atoi(strings.TrimSpace(strings.TrimSuffix(spec, "%")))
		if err != nil || percent < 1 || percent > 100 {
			return compactionSpec{}, fmt.Errorf("compaction %q: want off, a positive token count, or a percent from 1%% to 100%%", spec)
		}
		return compactionSpec{mode: compactionPercent, percent: percent}, nil
	default:
		tokens, err := strconv.ParseInt(spec, 10, 64)
		if err != nil || tokens <= 0 {
			return compactionSpec{}, fmt.Errorf("compaction %q: want off, a positive token count, or a percent from 1%% to 100%%", spec)
		}
		return compactionSpec{mode: compactionTokens, tokens: tokens}, nil
	}
}

func contextWindow(model, window string) (int64, bool, error) {
	window = strings.TrimSpace(window)
	if window != "" {
		size, err := strconv.ParseInt(window, 10, 64)
		if err != nil || size <= 0 {
			return 0, false, fmt.Errorf("context window %q: want a positive token count", window)
		}
		return size, true, nil
	}
	size, ok := builtinContextWindow(model)
	return size, ok, nil
}

func builtinContextWindow(model string) (int64, bool) {
	provider, id, ok := strings.Cut(strings.TrimSpace(model), "/")
	if !ok || provider == "" || id == "" {
		return 0, false
	}
	// settings.Load returns the built-in table when the file is missing.
	// os.ReadFile("") is ErrNotExist, so no sentinel path is required.
	configured, err := settings.Load("")
	if err != nil {
		return 0, false
	}
	window := configured.Model(provider, id).ContextWindow
	if window <= 0 {
		return 0, false
	}
	return window, true
}

// compactionUserInstruction is the full text of the user message BuildCompaction
// appends as the last input item in unreal-agent v0.3.1
// harness/contextbuilder/compaction.go. llm.Request has no compaction flag.
const compactionUserInstruction = "This is system message. You are performing context compaction. Return only the text of a handoff summary for another LLM assistant to resume the original task."

const compactionHypotheses = "List hypotheses already checked and rejected, one per line, each with a one-line reason."

// guardCompaction rejects an unusable compaction response and stamps findings
// from disk onto the handoff. It does not submit settings or keep state.
func guardCompaction(inner llm.Adapter, findingsPath string) *compactionGuard {
	return &compactionGuard{inner: inner, findingsPath: findingsPath}
}

type compactionGuard struct {
	inner        llm.Adapter
	findingsPath string
	// recordUsage keeps billed tokens when the coordinator will drop the
	// response. An error from Respond is not stored as a model response.
	recordUsage func(llm.Response)
}

func (g *compactionGuard) Respond(ctx context.Context, req llm.Request, opts llm.RequestOptions) (llm.Response, error) {
	compacting := compactionRequest(req)
	if compacting {
		req = annotateCompactionRequest(req)
	}
	response, err := g.inner.Respond(ctx, req, opts)
	if err != nil {
		return response, err
	}
	if !compacting {
		return response, nil
	}
	if response.Stop != llm.StopComplete || compactionSummary(response) == "" {
		if g.recordUsage != nil {
			g.recordUsage(response)
		}
		return llm.Response{}, fmt.Errorf("compaction response stop %q is not a usable summary; resuming with the same --out replays the compaction; --compaction off avoids it", response.Stop)
	}
	return stampSummary(response, recordedFindingsBlock(g.findingsPath)), nil
}

func annotateCompactionRequest(req llm.Request) llm.Request {
	input := append([]llm.Item(nil), req.Input...)
	last := input[len(input)-1]
	message := last.Data.(llm.Message)
	message.Text += "\n\n" + compactionHypotheses
	last.Data = message
	input[len(input)-1] = last
	req.Input = input
	return req
}

func stampSummary(response llm.Response, block string) llm.Response {
	for i := len(response.Output) - 1; i >= 0; i-- {
		message, ok := response.Output[i].Data.(llm.Message)
		if response.Output[i].Type == llm.ItemMessage && ok && message.Role == llm.RoleAssistant && strings.TrimSpace(message.Text) != "" {
			message.Text = strings.TrimSpace(message.Text) + "\n\n" + block
			response.Output[i].Data = message
			return response
		}
	}
	return response
}

func recordedFindingsBlock(path string) string {
	if paths, stage, listed := stageFindingPaths(path); stage {
		if !listed {
			return "Findings already recorded: unavailable (findings file unreadable)"
		}
		return mergeStageFindings(paths)
	}
	return oneFileFindingsBlock(path)
}

func oneFileFindingsBlock(path string) string {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "Findings already recorded: none."
	}
	if err != nil {
		return "Findings already recorded: unreadable."
	}
	report, err := parseFindingsBytes(raw)
	if err != nil {
		return "Findings already recorded: unavailable (findings file unreadable)"
	}
	return formatRecordedFindings(report.Findings)
}

// Stage files are merged in sorted base-name order. Within a file, findings
// stay in the order they were recorded, which is that stage's order. A later
// file, then a later line, is the newer occurrence of an id.
func mergeStageFindings(paths []string) string {
	var all []findings.Finding
	failed := 0
	for _, path := range paths {
		report, status := readStageFindings(path)
		switch status {
		case stageReadOK:
			all = append(all, report.Findings...)
		case stageReadMissing, stageReadSkip:
		default:
			failed++
		}
	}
	if len(all) == 0 {
		if failed > 0 {
			return "Findings already recorded: unavailable (findings file unreadable)"
		}
		return "Findings already recorded: none."
	}
	return formatRecordedFindings(all)
}

func formatRecordedFindings(all []findings.Finding) string {
	if len(all) == 0 {
		return "Findings already recorded: none."
	}
	shown, omitted := newestFindings(all)
	var b strings.Builder
	b.WriteString("Findings already recorded:\n")
	for _, finding := range shown {
		fmt.Fprintf(&b, "- %s:%d-%d %s\n", trimField(finding.Path), finding.StartLine, finding.EndLine, trimField(finding.Body))
	}
	if omitted > 0 {
		fmt.Fprintf(&b, "omitted %d older findings\n", omitted)
	}
	return strings.TrimSpace(b.String())
}

type stageRead int

const (
	stageReadOK stageRead = iota
	stageReadMissing
	stageReadSkip
	stageReadBad
)

func readStageFindings(path string) (findings.Report, stageRead) {
	raw, err := readStageBytes(path)
	if errors.Is(err, os.ErrNotExist) {
		return findings.Report{}, stageReadMissing
	}
	if errors.Is(err, errNotStageFile) {
		return findings.Report{}, stageReadSkip
	}
	if err != nil {
		return findings.Report{}, stageReadBad
	}
	report, err := parseFindingsBytes(raw)
	if err != nil {
		return findings.Report{}, stageReadBad
	}
	return report, stageReadOK
}

// A torn trailing line must not drop findings already written. A corrupt
// line that is not last stays unreadable, so one file does not claim that
// nothing was recorded.
func parseFindingsBytes(raw []byte) (findings.Report, error) {
	report, err := findings.Parse(bytes.NewReader(raw))
	if err == nil {
		return report, nil
	}
	return findings.Parse(bytes.NewReader(withoutLastLine(raw)))
}

var errNotStageFile = errors.New("not a regular stage findings file")

func readStageBytes(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotStageFile
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	limit := maxStageFindingBytes
	if info.Size() > int64(limit) {
		if _, err := file.Seek(-int64(limit), io.SeekEnd); err != nil {
			return nil, err
		}
	}
	raw, err := io.ReadAll(io.LimitReader(file, int64(limit)))
	if err != nil {
		return nil, err
	}
	if info.Size() > int64(limit) {
		if i := bytes.IndexByte(raw, '\n'); i >= 0 {
			raw = raw[i+1:]
		} else {
			raw = nil
		}
	}
	return raw, nil
}

// stageFindingPaths lists child findings files for the same planned or
// focused run. ok is false when path is not inside that run's stage
// directory, and the caller then reads only path. Manifests, receipts, the
// lock, and any other name are not stage findings files.
func stageFindingPaths(current string) (paths []string, ok bool, listed bool) {
	dir, kind, ok := stageDirectoryOf(current)
	if !ok {
		return nil, false, false
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, true, false
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name != filepath.Base(name) || !stageFindingName(kind, name) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	names = capStageNames(names, filepath.Base(current))
	paths = make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		if !insideDir(dir, path) {
			continue
		}
		paths = append(paths, path)
	}
	return paths, true, true
}

func capStageNames(names []string, current string) []string {
	if len(names) <= maxStageFindingFiles {
		return names
	}
	tail := append([]string(nil), names[len(names)-maxStageFindingFiles:]...)
	for _, name := range tail {
		if name == current {
			return tail
		}
	}
	present := false
	for _, name := range names {
		if name == current {
			present = true
			break
		}
	}
	if !present {
		return tail
	}
	tail = append([]string{current}, tail[1:]...)
	sort.Strings(tail)
	return tail
}

func stageDirectoryOf(current string) (dir, kind string, ok bool) {
	abs, err := filepath.Abs(current)
	if err != nil {
		return "", "", false
	}
	abs = filepath.Clean(abs)
	dir = filepath.Dir(abs)
	if !insideDir(dir, abs) {
		return "", "", false
	}
	root, err := sessionDirectory()
	if err != nil {
		return "", "", false
	}
	root = filepath.Clean(root)
	parent := filepath.Dir(dir)
	kind = filepath.Base(parent)
	if kind != "planned" && kind != "focused" {
		return "", "", false
	}
	if filepath.Clean(filepath.Dir(parent)) != root || !hex64(filepath.Base(dir)) {
		return "", "", false
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", false
	}
	return dir, kind, true
}

func insideDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != "" && rel == filepath.Base(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func stageFindingName(kind, base string) bool {
	switch kind {
	case "focused":
		return focusedStageFile(base)
	case "planned":
		return plannedChildFile(base)
	default:
		return false
	}
}

func focusedStageFile(base string) bool {
	name, ok := strings.CutSuffix(base, ".jsonl")
	if !ok {
		return false
	}
	if name == "verification" {
		return true
	}
	for _, lens := range focusedLenses {
		if name == lens {
			return true
		}
	}
	return false
}

// plannedChildFile matches the child files planned.go writes:
// focusedHash(stage)+".jsonl" and focusedHash(stage)+"-"+generation+".jsonl".
func plannedChildFile(base string) bool {
	name, ok := strings.CutSuffix(base, ".jsonl")
	if !ok || name == "" || strings.Count(name, "-") > 1 {
		return false
	}
	hash, gen, found := strings.Cut(name, "-")
	if !found {
		return hex64(name)
	}
	if !hex64(hash) || gen == "" {
		return false
	}
	for _, c := range gen {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func hex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func newestFindings(all []findings.Finding) ([]findings.Finding, int) {
	seen := make(map[string]struct{}, len(all))
	newest := make([]findings.Finding, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		finding := all[i]
		id := finding.ID
		if id == "" {
			id = findings.Fingerprint(finding)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		newest = append(newest, finding)
	}
	omitted := 0
	if len(newest) > maxRecordedFindings {
		omitted = len(newest) - maxRecordedFindings
		newest = newest[:maxRecordedFindings]
	}
	for i, j := 0, len(newest)-1; i < j; i, j = i+1, j-1 {
		newest[i], newest[j] = newest[j], newest[i]
	}
	return newest, omitted
}

func withoutLastLine(raw []byte) []byte {
	raw = bytes.TrimRight(raw, "\r\n")
	if i := bytes.LastIndexByte(raw, '\n'); i >= 0 {
		return raw[:i]
	}
	return nil
}

func trimField(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= maxClaimBytes {
		return text
	}
	cut := maxClaimBytes
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "..."
}

func compactionRequest(req llm.Request) bool {
	if len(req.Input) == 0 {
		return false
	}
	item := req.Input[len(req.Input)-1]
	message, ok := item.Data.(llm.Message)
	return item.Type == llm.ItemMessage && ok && message.Role == llm.RoleUser && message.Text == compactionUserInstruction
}

func compactionSummary(response llm.Response) string {
	for i := len(response.Output) - 1; i >= 0; i-- {
		item := response.Output[i]
		message, ok := item.Data.(llm.Message)
		if item.Type == llm.ItemMessage && ok && message.Role == llm.RoleAssistant && strings.TrimSpace(message.Text) != "" {
			return message.Text
		}
	}
	return ""
}

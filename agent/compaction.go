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
	"golang.org/x/sys/unix"

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
	// The handoff lists findings from disk. Path and claim are collapsed to
	// one line and truncated so the block stays small. Only a discovery
	// stage also reads other discovery files in the same stage directory.
	// Own findings take the first of these slots; other tasks fill the rest.
	maxRecordedFindings = 20
	ownFindingsLabel    = "Findings YOU recorded in this task:"
	otherFindingsLabel  = `Findings recorded by OTHER tasks of this review (not yours). Do not describe them in your summary. If you independently confirmed the same issue, still call record_finding for it; duplicates are merged later in verification. If you recorded nothing yourself, your summary must start with "No material issues".`
	maxClaimBytes       = 160
	// Sibling discovery files opened, besides the current file. Names that
	// sort first are dropped. That order is not chronological.
	maxStageFindingFiles = 128
	// Bytes read from one discovery file. A longer file contributes its tail.
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
	paths, discovery, err := discoverySiblingPaths(path)
	if !discovery {
		return oneFileFindingsBlock(path)
	}
	return mergeStageFindings(paths, err)
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

// paths lists sibling discovery files in sorted base-name order, then the
// current file. Sibling name order is deterministic, not chronological. The
// current file is last and wins an id clash. Its findings take the newest
// slots; siblings fill whatever remains of the 20.
func mergeStageFindings(paths []string, enumErr error) string {
	var own, others []findings.Finding
	failed := enumErr != nil
	current := ""
	if len(paths) > 0 {
		current = paths[len(paths)-1]
	}
	for _, path := range paths {
		raw, err := readStageBytes(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if errors.Is(err, errNotStageFile) {
			if path == current {
				failed = true
			}
			continue
		}
		if err != nil {
			failed = true
			continue
		}
		report, err := parseFindingsBytes(raw)
		if err != nil {
			failed = true
			continue
		}
		if path == current {
			own = append(own, report.Findings...)
		} else {
			others = append(others, report.Findings...)
		}
	}
	if len(own) == 0 && len(others) == 0 {
		if failed {
			return "Findings already recorded: unavailable (findings file unreadable)"
		}
	}
	// An unlistable directory did not prove that other tasks recorded nothing.
	block := formatTaskFindings(own, withoutFindingIDs(others, own), enumErr != nil && len(others) == 0)
	// A partial list must not look complete. The current file being a symlink
	// or other non-regular file counts; sibling symlinks stay skipped.
	if failed {
		block += "\n(some findings files were unreadable)"
	}
	return block
}

func formatTaskFindings(own, others []findings.Finding, othersUnreadable bool) string {
	ownShown, ownOmitted := newestFindings(own, maxRecordedFindings)
	otherShown, otherOmitted := newestFindings(others, maxRecordedFindings-len(ownShown))
	ownText := formatFindingGroup(ownFindingsLabel, ownShown, ownOmitted, true)
	otherText := formatFindingGroup(otherFindingsLabel, otherShown, otherOmitted, false)
	if othersUnreadable && len(otherShown) == 0 && otherOmitted == 0 {
		otherText = otherFindingsLabel + "\nunavailable (findings file unreadable)"
	}
	return ownText + "\n\n" + otherText
}

func formatFindingGroup(header string, shown []findings.Finding, omitted int, colonNone bool) string {
	if len(shown) == 0 && omitted == 0 {
		if colonNone {
			return header + " none."
		}
		return header + "\nnone."
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	for _, finding := range shown {
		fmt.Fprintf(&b, "- %s:%d-%d %s\n", trimField(finding.Path), finding.StartLine, finding.EndLine, trimField(finding.Body))
	}
	// The count is a lower bound: the tail window and the sibling file cap
	// can drop findings before this cap is applied.
	if omitted > 0 {
		fmt.Fprintf(&b, "omitted %d older findings\n", omitted)
	}
	return strings.TrimSpace(b.String())
}

func formatRecordedFindings(all []findings.Finding) string {
	if len(all) == 0 {
		return "Findings already recorded: none."
	}
	shown, omitted := newestFindings(all, maxRecordedFindings)
	var b strings.Builder
	b.WriteString("Findings already recorded:\n")
	for _, finding := range shown {
		fmt.Fprintf(&b, "- %s:%d-%d %s\n", trimField(finding.Path), finding.StartLine, finding.EndLine, trimField(finding.Body))
	}
	// The count is a lower bound: the tail window and the sibling file cap
	// can drop findings before this cap is applied.
	if omitted > 0 {
		fmt.Fprintf(&b, "omitted %d older findings\n", omitted)
	}
	return strings.TrimSpace(b.String())
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
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
	if err != nil {
		if errors.Is(err, unix.ELOOP) || errors.Is(err, unix.ENXIO) || errors.Is(err, unix.EAGAIN) {
			return nil, errNotStageFile
		}
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errNotStageFile
	}
	limit := int64(maxStageFindingBytes)
	if info.Size() <= limit {
		return io.ReadAll(io.LimitReader(file, limit))
	}
	// The byte just before the window says whether the window starts on a
	// record boundary. Drop a partial first line only when it does not.
	if _, err := file.Seek(info.Size()-limit-1, io.SeekStart); err != nil {
		return nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, fmt.Errorf("findings record exceeds read window")
	}
	window := raw[1:]
	if raw[0] != '\n' {
		i := bytes.IndexByte(window, '\n')
		if i < 0 {
			return nil, fmt.Errorf("findings record exceeds read window")
		}
		window = window[i+1:]
	}
	if len(bytes.TrimSpace(window)) == 0 {
		return nil, fmt.Errorf("findings record exceeds read window")
	}
	return window, nil
}

// stageReadDir lists a stage directory. Tests replace it to force an
// enumeration failure while the current file stays readable.
var stageReadDir = os.ReadDir

// discoverySiblingPaths lists other discovery files for a discovery stage.
// Verifier, consolidator, and verification stages are not discovery: planned
// attempt files are plannedAttemptFile (a "-<gen>" suffix) and focused
// verification is focusedFindingsFile("verification"). Those stages are
// left to the single-file reader. A missing directory or any other path that
// is not a stage directory returns discovery=false and a nil error, so the
// caller reads that one file and does not add an unreadable note. A ReadDir
// error on a real stage directory is returned with the current path: it is
// an unreadable-files failure, never a silent none. Sibling names are sorted;
// that order is not chronological. The current path is always appended last,
// whether or not a sibling filter would have matched it.
func discoverySiblingPaths(current string) ([]string, bool, error) {
	dir, kind, ok := stageDirectoryOf(current)
	if !ok || !discoveryFindingsFile(kind, filepath.Base(current)) {
		return nil, false, nil
	}
	entries, err := stageReadDir(dir)
	if err != nil {
		return []string{current}, true, err
	}
	base := filepath.Base(current)
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name == base || !discoveryFindingsFile(kind, name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) > maxStageFindingFiles {
		names = names[len(names)-maxStageFindingFiles:]
	}
	paths := make([]string, 0, len(names)+1)
	for _, name := range names {
		paths = append(paths, filepath.Join(dir, name))
	}
	return append(paths, current), true, nil
}

func stageDirectoryOf(current string) (dir, kind string, ok bool) {
	abs, err := filepath.Abs(current)
	if err != nil {
		return "", "", false
	}
	dir = filepath.Dir(filepath.Clean(abs))
	root, err := sessionDirectory()
	if err != nil {
		return "", "", false
	}
	parent := filepath.Dir(dir)
	kind = filepath.Base(parent)
	if (kind != "planned" && kind != "focused") || filepath.Clean(filepath.Dir(parent)) != filepath.Clean(root) || !hex64(filepath.Base(dir)) {
		return "", "", false
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", "", false
	}
	return dir, kind, true
}

func discoveryFindingsFile(kind, base string) bool {
	switch kind {
	case "focused":
		if base == focusedFindingsFile("verification") {
			return false
		}
		for _, lens := range focusedLenses {
			if base == focusedFindingsFile(lens) {
				return true
			}
		}
		return false
	case "planned":
		name, ok := strings.CutSuffix(base, ".jsonl")
		return ok && hex64(name)
	default:
		return false
	}
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

func withoutFindingIDs(all, drop []findings.Finding) []findings.Finding {
	if len(all) == 0 || len(drop) == 0 {
		return all
	}
	seen := make(map[string]struct{}, len(drop))
	for _, finding := range drop {
		seen[findingKey(finding)] = struct{}{}
	}
	kept := make([]findings.Finding, 0, len(all))
	for _, finding := range all {
		if _, ok := seen[findingKey(finding)]; ok {
			continue
		}
		kept = append(kept, finding)
	}
	return kept
}

func findingKey(finding findings.Finding) string {
	if finding.ID != "" {
		return finding.ID
	}
	return findings.Fingerprint(finding)
}

func newestFindings(all []findings.Finding, limit int) ([]findings.Finding, int) {
	seen := make(map[string]struct{}, len(all))
	newest := make([]findings.Finding, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		finding := all[i]
		id := findingKey(finding)
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		newest = append(newest, finding)
	}
	omitted := 0
	if len(newest) > limit {
		omitted = len(newest) - limit
		newest = newest[:limit]
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

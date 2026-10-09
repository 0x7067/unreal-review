package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/settings"

	"github.com/0x7067/unreal-review/findings"
)

const (
	defaultCompactionPercent = 75
	// compactionMinCutoff is the lowest accepted cutoff. A lower cutoff
	// compacted on nearly every other turn in a real review.
	//
	// The minimum gap after a compaction is this floor plus upstream's rebuild,
	// not runtime state. unreal-agent v0.3.1 Compact starts a new builder with
	// an empty token prefix, so the next request is the system prompt, tools,
	// the summary, and at most a 20,000-token tail, plus the findings block
	// below. That total cannot cross 150,000. Upstream copies the assistant
	// summary with no size cap; the block is capped instead.
	compactionMinCutoff = 150_000
	// The handoff lists the newest findings from disk. Path and claim are
	// collapsed to one line and truncated so the block stays small.
	maxRecordedFindings = 20
	maxClaimBytes       = 160
)

// CompactionThreshold resolves the harness cutoff in tokens of the latest
// model response (input tokens plus output tokens). An empty spec uses
// defaultCompactionPercent of a known context window. An unknown window
// disables compaction and returns a note. An explicit percent with an unknown
// window is an error. The returned threshold is never zero.
func CompactionThreshold(model, spec, window string) (int64, string, error) {
	spec = strings.TrimSpace(spec)
	parsed, err := parseCompactionSpec(spec)
	if err != nil {
		return 0, "", err
	}
	switch parsed.mode {
	case compactionOff:
		return compactionDisabled, "compaction off", nil
	case compactionTokens:
		if parsed.tokens < compactionMinCutoff {
			return 0, "", cutoffFloorError(parsed.tokens)
		}
		return parsed.tokens, cutoffNote(parsed.tokens), nil
	}
	size, known, err := contextWindow(model, window)
	if err != nil {
		return 0, "", err
	}
	if parsed.mode == compactionPercent {
		if !known {
			return 0, "", fmt.Errorf("compaction %q: context window unknown for %q", spec, model)
		}
		return applyPercent(size, parsed.percent, true)
	}
	if !known {
		return compactionDisabled, fmt.Sprintf("compaction off: context window unknown for %q; set --context-window or %s", model, ContextWindowEnv), nil
	}
	return applyPercent(size, defaultCompactionPercent, false)
}

func cutoffNote(tokens int64) string {
	return fmt.Sprintf("compaction cutoff %d tokens", tokens)
}

func cutoffFloorError(tokens int64) error {
	return fmt.Errorf("compaction cutoff %d tokens is below %d", tokens, compactionMinCutoff)
}

func applyPercent(window int64, percent int, explicit bool) (int64, string, error) {
	// percent is 1..100, so this is exact and cannot overflow int64.
	threshold := window/100*int64(percent) + (window%100)*int64(percent)/100
	if threshold < compactionMinCutoff {
		if !explicit {
			return compactionDisabled, fmt.Sprintf("compaction off: context window %d tokens is too small to compact", window), nil
		}
		return 0, "", cutoffFloorError(threshold)
	}
	return threshold, cutoffNote(threshold), nil
}

type compactionMode int

const (
	compactionAuto compactionMode = iota
	compactionOff
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
		return compactionSpec{mode: compactionAuto}, nil
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
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "Findings already recorded: none."
	}
	if err != nil {
		return "Findings already recorded: unreadable."
	}
	report, err := findings.Parse(bytes.NewReader(raw))
	if err != nil {
		// A torn trailing line must not drop findings already written.
		// Unreadable is only for a file that cannot be read at all.
		report, err = findings.Parse(bytes.NewReader(withoutLastLine(raw)))
	}
	if err != nil || len(report.Findings) == 0 {
		return "Findings already recorded: none."
	}
	shown, omitted := newestFindings(report.Findings)
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

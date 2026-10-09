package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
	"uuid"

	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/settings"

	"github.com/0x7067/unreal-review/findings"
)

const (
	defaultCompactionPercent = 75
	// compactionMinCutoff is the lowest accepted cutoff. A 40,000 cutoff
	// compacted on nearly every other turn in a real review.
	compactionMinCutoff = 150_000
	// After a compaction, the cutoff stays off until both of these have passed.
	compactionGapTurns  = 4
	compactionGapTokens = 50_000
	// The handoff lists findings from disk, capped so the block stays small.
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
	if window > math.MaxInt64/int64(percent) {
		return 0, "", fmt.Errorf("compaction threshold overflows")
	}
	threshold := window * int64(percent) / 100
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

// guardCompaction rejects an unusable compaction response, stamps findings from
// disk onto the handoff, and holds the cutoff up via inbox UpdateSettings.
// NeedsCompaction reads the threshold stored by SetModel; inbox.Settings
// carries CompactionThreshold and AddControlMessage applies it.
func guardCompaction(inner llm.Adapter, floor int64, findingsPath string) *compactionGuard {
	return &compactionGuard{inner: inner, floor: floor, findingsPath: findingsPath}
}

type compactionGuard struct {
	inner        llm.Adapter
	box          inbox.Writer
	floor        int64
	findingsPath string

	held         bool
	regularTurns int
	baseline     int64
	haveBaseline bool
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
		if err := g.observeRegular(ctx, response); err != nil {
			return response, err
		}
		return response, nil
	}
	if response.Stop != llm.StopComplete || compactionSummary(response) == "" {
		return llm.Response{}, fmt.Errorf("compaction response stop %q is not a usable summary; resuming with the same --out replays the compaction; --compaction off avoids it", response.Stop)
	}
	response = stampSummary(response, recordedFindingsBlock(g.findingsPath))
	if err := g.hold(ctx); err != nil {
		return llm.Response{}, err
	}
	return response, nil
}

func (g *compactionGuard) hold(ctx context.Context) error {
	g.held = true
	g.regularTurns = 0
	g.haveBaseline = false
	g.baseline = 0
	return g.setThreshold(ctx, compactionDisabled)
}

func (g *compactionGuard) observeRegular(ctx context.Context, response llm.Response) error {
	if !g.held {
		return nil
	}
	g.regularTurns++
	tokens := response.Usage.InputTokens + response.Usage.OutputTokens
	if !g.haveBaseline {
		g.baseline = tokens
		g.haveBaseline = true
	}
	if g.regularTurns >= compactionGapTurns && tokens-g.baseline >= compactionGapTokens {
		g.held = false
		return g.setThreshold(ctx, g.floor)
	}
	return nil
}

func (g *compactionGuard) setThreshold(ctx context.Context, threshold int64) error {
	payload, err := json.Marshal(inbox.ControlMessage{
		Mode:       inbox.UpdateSettings,
		Parameters: inbox.Settings{CompactionThreshold: &threshold},
	})
	if err != nil {
		return fmt.Errorf("encode compaction threshold: %w", err)
	}
	if g.box == nil {
		return fmt.Errorf("compaction inbox is not open")
	}
	if err := g.box.Submit(ctx, inbox.Input{
		ID:      inbox.ID(uuid.New().String()),
		Kind:    inbox.InputControl,
		Payload: payload,
	}); err != nil {
		return fmt.Errorf("update compaction threshold: %w", err)
	}
	return nil
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
	report, err := findings.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "Findings already recorded: unreadable."
	}
	if err != nil || len(report.Findings) == 0 {
		return "Findings already recorded: none."
	}
	shown := len(report.Findings)
	if shown > maxRecordedFindings {
		shown = maxRecordedFindings
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Findings already recorded (at most %d):\n", maxRecordedFindings)
	for _, finding := range report.Findings[:shown] {
		fmt.Fprintf(&b, "- %s:%d-%d %s\n", finding.Path, finding.StartLine, finding.EndLine, trimClaim(finding.Body))
	}
	if extra := len(report.Findings) - shown; extra > 0 {
		fmt.Fprintf(&b, "- and %d more\n", extra)
	}
	return strings.TrimSpace(b.String())
}

func trimClaim(text string) string {
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

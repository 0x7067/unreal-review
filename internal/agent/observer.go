package agent

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"sync"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"

	"unreal-review/internal/findings"
)

type recordKind int

const (
	kindFinding recordKind = iota
	kindSummary
)

type stashedRecord struct {
	kind    recordKind
	finding findings.Finding
	summary string
}

type sessionObserver struct {
	sessionID    session.ID
	findingsPath string
	log          io.Writer
	cancel       context.CancelFunc

	mu          sync.Mutex
	err         error
	cost        findings.Cost
	responseIDs []string
	stash       map[string]stashedRecord
}

func newSessionObserver(sessionID session.ID, findingsPath string, log io.Writer, cancel context.CancelFunc) *sessionObserver {
	return &sessionObserver{
		sessionID:    sessionID,
		findingsPath: findingsPath,
		log:          log,
		cancel:       cancel,
		cost:         findings.Cost{Currency: "USD"},
		stash:        make(map[string]stashedRecord),
	}
}

func (o *sessionObserver) stashFinding(callID string, finding findings.Finding) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stash[callID] = stashedRecord{kind: kindFinding, finding: finding}
}

func (o *sessionObserver) stashSummary(callID string, body string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stash[callID] = stashedRecord{kind: kindSummary, summary: body}
}

func (o *sessionObserver) Observe(id session.ID, item sessionstore.Item) {
	if id != o.sessionID {
		return
	}
	if o.Err() != nil {
		return
	}
	if o.log != nil {
		if err := writeSessionItem(o.log, item); err != nil {
			o.fail(fmt.Errorf("write session log: %w", err))
			return
		}
	}
	switch item.Kind {
	case sessionstore.ItemModelResponse:
		if response, ok := item.Data.(sessionstore.ModelResponse); ok {
			o.observeModelResponse(response.Response)
		}
	case sessionstore.ItemToolCallStatus:
		if status, ok := item.Data.(sessionstore.ToolCallStatus); ok {
			o.observeToolCallStatus(status)
		}
	}
}

func (o *sessionObserver) observeModelResponse(response llm.Response) {
	o.mu.Lock()
	o.cost.Requests++
	o.cost.InputTokens += response.Usage.InputTokens
	o.cost.OutputTokens += response.Usage.OutputTokens
	o.cost.ReasoningTokens += response.Usage.ReasoningTokens
	o.cost.CachedInputTokens += response.Usage.CachedInputTokens
	if len(response.Usage.Raw) > 0 {
		var raw map[string]any
		if err := json.Unmarshal([]byte(response.Usage.Raw), &raw); err == nil {
			if amount, ok := floatVal(raw, "cost", "total_cost"); ok {
				o.cost.AmountUSD += amount
			}
		}
	}
	if response.ID != "" {
		o.responseIDs = append(o.responseIDs, response.ID)
	}
	o.mu.Unlock()
}

func (o *sessionObserver) observeToolCallStatus(status sessionstore.ToolCallStatus) {
	if status.Status.Error != "" {
		return
	}
	if !allOperationsTerminal(status.Operations) {
		return
	}
	o.mu.Lock()
	record, ok := o.stash[status.CallID]
	if ok {
		delete(o.stash, status.CallID)
	}
	o.mu.Unlock()
	if !ok {
		return
	}
	var err error
	switch record.kind {
	case kindFinding:
		_, err = findings.AppendFinding(o.findingsPath, record.finding)
	case kindSummary:
		err = findings.AppendSummary(o.findingsPath, record.summary)
	}
	if err != nil {
		o.fail(fmt.Errorf("write findings: %w", err))
	}
}

func allOperationsTerminal(operations []operation.Operation) bool {
	if len(operations) == 0 {
		return false
	}
	for _, op := range operations {
		switch op.Status {
		case operation.StatusCompleted, operation.StatusFailed, operation.StatusCanceled:
		default:
			return false
		}
	}
	return true
}

func (o *sessionObserver) fail(err error) {
	o.mu.Lock()
	if o.err == nil {
		o.err = err
	}
	o.mu.Unlock()
	o.cancel()
}

func (o *sessionObserver) Err() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

func (o *sessionObserver) Cost() findings.Cost {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cost
}

func (o *sessionObserver) ResponseIDs() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make([]string, len(o.responseIDs))
	copy(out, o.responseIDs)
	return out
}

func writeSessionItem(output io.Writer, item sessionstore.Item) error {
	encoded, err := jsonv2.Marshal(item)
	if err != nil {
		return fmt.Errorf("encode session item %d: %w", item.Sequence, err)
	}
	if _, err := fmt.Fprintf(output, "%s\n", encoded); err != nil {
		return fmt.Errorf("write session item %d: %w", item.Sequence, err)
	}
	return nil
}

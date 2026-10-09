package agent

import (
	"context"
	"encoding/json"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"

	"github.com/0x7067/unreal-review/internal/findings"
)

type sessionObserver struct {
	sessionID    session.ID
	findingsPath string
	log          io.Writer
	cancel       context.CancelFunc

	mu        sync.Mutex
	err       error
	cost      findings.Cost
	finalText string
}

func newSessionObserver(sessionID session.ID, findingsPath string, log io.Writer, cancel context.CancelFunc) *sessionObserver {
	return &sessionObserver{
		sessionID:    sessionID,
		findingsPath: findingsPath,
		log:          log,
		cancel:       cancel,
		cost:         findings.Cost{Currency: "USD"},
	}
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
	var usage struct {
		Cost float64 `json:"cost"`
	}
	if json.Unmarshal(response.Usage.Raw, &usage) == nil {
		o.cost.AmountUSD += usage.Cost
	}
	for _, item := range response.Output {
		if message, ok := item.Data.(llm.Message); ok && message.Role == llm.RoleAssistant && strings.TrimSpace(message.Text) != "" {
			o.finalText = message.Text
		}
	}
	o.mu.Unlock()
}

func (o *sessionObserver) observeToolCallStatus(status sessionstore.ToolCallStatus) {
	if status.Status.Error != "" || len(status.Operations) != 1 || status.Operations[0].Status != operation.StatusCompleted {
		return
	}
	encoded, err := operation.DecodeValue(status.Operations[0])
	if err != nil {
		return
	}
	var finding findings.Finding
	if err := json.Unmarshal(encoded, &finding); err != nil {
		o.fail(fmt.Errorf("decode record: %w", err))
		return
	}
	if _, err := findings.AppendFinding(o.findingsPath, finding); err != nil {
		o.fail(fmt.Errorf("write findings: %w", err))
	}
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

func (o *sessionObserver) takeFinalText() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	text := o.finalText
	o.finalText = ""
	return text
}

func (o *sessionObserver) Cost() findings.Cost {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.cost
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

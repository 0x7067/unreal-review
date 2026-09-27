package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"

	"github.com/unreallabsai/unreal-agent/harness/contextbuilder"
	"github.com/unreallabsai/unreal-agent/harness/coordinator"
	"github.com/unreallabsai/unreal-agent/harness/inbox"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openrouter"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore/localfile"
	"github.com/unreallabsai/unreal-agent/harness/tool"
	"github.com/unreallabsai/unreal-agent/harness/tool/bash"
	"github.com/unreallabsai/unreal-agent/harness/tool/viewimage"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

const (
	maxSummaryCorrections = 2
	openRouterBaseURL     = "https://openrouter.ai/api/v1"
	toolHeartbeatInterval = 10 * time.Minute
	sessionDirectoryName  = ".local/state/unreal-agent/sessions"
)

type Harness struct {
	APIKey        string
	ThinkingLevel string
	Log           io.Writer
	Timeout       time.Duration
}

var _ review.Agent = Harness{}

func (h Harness) Run(ctx context.Context, req review.AgentRequest) (review.AgentResult, error) {
	if h.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.Timeout)
		defer cancel()
	}

	client, err := openrouter.NewClient(openrouter.Config{APIKey: h.APIKey, BaseURL: openRouterBaseURL})
	if err != nil {
		return review.AgentResult{}, fmt.Errorf("create openrouter client: %w", err)
	}
	defer func() { _ = client.Close() }()

	storeDirectory, err := sessionDirectory()
	if err != nil {
		return review.AgentResult{}, fmt.Errorf("resolve session directory: %w", err)
	}
	store, err := localfile.New(storeDirectory)
	if err != nil {
		return review.AgentResult{}, fmt.Errorf("open session store: %w", err)
	}

	sessionID := session.ID(strings.TrimSpace(req.ReviewID))
	if sessionID == "" {
		return review.AgentResult{}, fmt.Errorf("review ID must be set")
	}
	if req.Identify == nil {
		return review.AgentResult{}, fmt.Errorf("identify is required")
	}
	if req.Resolve == nil {
		return review.AgentResult{}, fmt.Errorf("resolve is required")
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	operationDirectory := filepath.Join(storeDirectory, "operations", string(sessionID))
	if err := os.MkdirAll(operationDirectory, 0o700); err != nil {
		return review.AgentResult{}, fmt.Errorf("create operation directory: %w", err)
	}
	registry := newRecordRegistry(tool.NewRegistry(tool.StaticTranslators{
		Bash:      bash.New(bash.Config{Shell: "/bin/sh", Directory: req.Workspace, BaseDirectory: operationDirectory}),
		ViewImage: viewimage.New(viewimage.Config{Directory: req.Workspace}),
	}, tool.BashName, tool.ViewImageName), req.Identify, req.Resolve)

	observer := newSessionObserver(sessionID, req.FindingsPath, h.Log, cancel)
	observerID := store.AddObserver(observer.Observe)
	defer store.RemoveObserver(observerID)

	s := harnessSession{
		id:           sessionID,
		store:        store,
		llm:          client,
		registry:     registry,
		model:        llm.Model{ID: req.Model, ReasoningEffort: llm.ReasoningEffort(h.ThinkingLevel)},
		systemPrompt: req.SystemPrompt,
	}
	coordinatorErr := s.turn(runCtx, inbox.ID(sessionID), req.Prompt)
	for attempt := 0; coordinatorErr == nil; attempt++ {
		count, err := findingCount(req.FindingsPath)
		if err != nil {
			coordinatorErr = err
			break
		}
		summary, err := findings.CheckSummary(observer.takeFinalText(), count)
		if err == nil {
			coordinatorErr = findings.AppendSummary(req.FindingsPath, summary)
			break
		}
		if attempt == maxSummaryCorrections {
			coordinatorErr = fmt.Errorf("summary breaks the contract: %w", err)
			break
		}
		coordinatorErr = s.turn(runCtx, inbox.ID(uuid.New().String()), summaryCorrection(err))
	}
	if observerErr := observer.Err(); observerErr != nil {
		coordinatorErr = observerErr
	}

	return review.AgentResult{Cost: observer.Cost()}, coordinatorErr
}

type harnessSession struct {
	id           session.ID
	store        *localfile.Store
	llm          llm.Adapter
	registry     tool.Registry
	model        llm.Model
	systemPrompt string
}

func (s harnessSession) turn(ctx context.Context, messageID inbox.ID, message string) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	restored, err := openSession(ctx, s.store, s.id)
	if err != nil {
		return err
	}
	inputs, err := inbox.New(ctx, restored.ExternalInputIDs)
	if err != nil {
		return fmt.Errorf("open inbox: %w", err)
	}
	settings, err := json.Marshal(inbox.ControlMessage{
		Mode:       inbox.UpdateSettings,
		Parameters: inbox.Settings{ReasoningEffort: s.model.ReasoningEffort},
	})
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	stop, err := json.Marshal(inbox.ControlMessage{Mode: inbox.StopWhenIdle})
	if err != nil {
		return fmt.Errorf("encode stop request: %w", err)
	}
	for _, input := range []inbox.Input{
		{ID: inbox.ID(uuid.New().String()), Kind: inbox.InputControl, Payload: settings},
		{ID: messageID, Kind: inbox.InputExternal, Payload: payload},
		{ID: inbox.ID(uuid.New().String()), Kind: inbox.InputControl, Payload: stop},
	} {
		if err := inputs.Submit(ctx, input); err != nil {
			return fmt.Errorf("submit input: %w", err)
		}
	}
	builder := contextbuilder.NewBuilder()
	builder.SetModel(s.model)
	builder.SetSystemPrompt(s.systemPrompt)
	for _, definition := range s.registry.StaticDefinitions() {
		builder.AddTool(definition.Tool)
	}
	err = coordinator.New(coordinator.Dependencies{
		ToolHeartbeatInterval: toolHeartbeatInterval,
		SessionID:             s.id,
		Inbox:                 inputs,
		Restored:              restored,
		Sessions:              s.store,
		ContextBuilder:        builder,
		LLM:                   s.llm,
		Tools:                 s.registry,
		Operations:            operation.NewLocalOperationManager(ctx),
	}).Run(ctx)
	if err != nil {
		return fmt.Errorf("run coordinator: %w", err)
	}
	return nil
}

func findingCount(findingsPath string) (int, error) {
	report, err := findings.ReadFile(findingsPath)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	return len(report.Findings), err
}

func summaryCorrection(err error) string {
	return fmt.Sprintf("Your final message is the review summary, and it breaks the summary contract: %v. Reply with only the corrected summary.", err)
}

func openSession(ctx context.Context, store *localfile.Store, id session.ID) (sessionstore.ResumeState, error) {
	restored, err := store.Resume(ctx, id)
	if err == nil {
		return restored, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return sessionstore.ResumeState{}, fmt.Errorf("open session %q: %w", id, err)
	}
	if _, err := store.Create(ctx, id); err != nil {
		return sessionstore.ResumeState{}, fmt.Errorf("create session %q: %w", id, err)
	}
	return sessionstore.ResumeState{}, nil
}

func sessionDirectory() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("find home directory: %w", err)
	}
	return filepath.Join(home, sessionDirectoryName), nil
}

func SanitizeLevel(level string) (string, error) {
	level = strings.TrimSpace(level)
	if level == "" {
		return "high", nil
	}
	switch level {
	case "low", "medium", "high", "xhigh", "max":
		return level, nil
	default:
		return "", fmt.Errorf("thinking level %q: want low, medium, high, xhigh, or max", level)
	}
}

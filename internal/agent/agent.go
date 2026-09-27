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

	"unreal-review/internal/review"
)

const (
	openRouterBaseURL     = "https://openrouter.ai/api/v1"
	toolHeartbeatInterval = 10 * time.Minute
	sessionDirectoryName  = "unreal-agent/sessions"
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
	restored, err := openSession(ctx, store, sessionID)
	if err != nil {
		return review.AgentResult{}, err
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	operationDirectory := filepath.Join(storeDirectory, "operations", string(sessionID))
	if err := os.MkdirAll(operationDirectory, 0o700); err != nil {
		return review.AgentResult{}, fmt.Errorf("create operation directory: %w", err)
	}
	shell := strings.TrimSpace(os.Getenv("SHELL"))
	if shell == "" {
		shell = "/bin/sh"
	}

	inner := tool.NewRegistry(tool.StaticTranslators{
		Bash:      bash.New(bash.Config{Shell: shell, Directory: req.Workspace, BaseDirectory: operationDirectory}),
		ViewImage: viewimage.New(viewimage.Config{Directory: req.Workspace}),
	}, tool.BashName, tool.ViewImageName)

	observer := newSessionObserver(sessionID, req.FindingsPath, h.Log, cancel)
	registry := newRecordRegistry(inner)

	operations := operation.NewLocalOperationManager(runCtx)
	inputs, err := inbox.New(runCtx, restored.ExternalInputIDs)
	if err != nil {
		return review.AgentResult{}, fmt.Errorf("open inbox: %w", err)
	}

	reasoningEffort := llm.ReasoningEffort(h.ThinkingLevel)
	settingsPayload, err := json.Marshal(inbox.ControlMessage{
		Mode:       inbox.UpdateSettings,
		Parameters: inbox.Settings{ReasoningEffort: reasoningEffort},
	})
	if err != nil {
		return review.AgentResult{}, fmt.Errorf("encode settings: %w", err)
	}
	if err := inputs.Submit(runCtx, inbox.Input{
		ID: inbox.ID(uuid.New().String()), Kind: inbox.InputControl, Payload: settingsPayload,
	}); err != nil {
		return review.AgentResult{}, fmt.Errorf("submit settings: %w", err)
	}

	promptPayload, err := json.Marshal(req.Prompt)
	if err != nil {
		return review.AgentResult{}, fmt.Errorf("encode prompt: %w", err)
	}
	if err := inputs.Submit(runCtx, inbox.Input{
		ID: inbox.ID(sessionID), Kind: inbox.InputExternal, Payload: promptPayload,
	}); err != nil {
		return review.AgentResult{}, fmt.Errorf("submit prompt: %w", err)
	}

	stopPayload, err := json.Marshal(inbox.ControlMessage{Mode: inbox.StopWhenIdle})
	if err != nil {
		return review.AgentResult{}, fmt.Errorf("encode stop request: %w", err)
	}
	if err := inputs.Submit(runCtx, inbox.Input{
		ID: inbox.ID(uuid.New().String()), Kind: inbox.InputControl, Payload: stopPayload,
	}); err != nil {
		return review.AgentResult{}, fmt.Errorf("submit stop request: %w", err)
	}

	builder := contextbuilder.NewBuilder()
	builder.SetModel(llm.Model{ID: req.Model, ReasoningEffort: reasoningEffort})
	builder.SetSystemPrompt(req.SystemPrompt)
	for _, definition := range registry.StaticDefinitions() {
		builder.AddTool(definition.Tool)
	}

	observerID := store.AddObserver(observer.Observe)
	defer store.RemoveObserver(observerID)

	current := coordinator.New(coordinator.Dependencies{
		ToolHeartbeatInterval: toolHeartbeatInterval,
		SessionID:             sessionID,
		Inbox:                 inputs,
		Restored:              restored,
		Sessions:              store,
		ContextBuilder:        builder,
		LLM:                   client,
		Tools:                 registry,
		Operations:            operations,
	})
	coordinatorErr := current.Run(runCtx)
	if observerErr := observer.Err(); observerErr != nil {
		coordinatorErr = observerErr
	} else if coordinatorErr != nil {
		coordinatorErr = fmt.Errorf("run coordinator: %w", coordinatorErr)
	}

	cost := observer.Cost()
	if cost.AmountUSD == 0 {
		if responseIDs := observer.ResponseIDs(); len(responseIDs) > 0 {
			interrupted := coordinatorErr != nil &&
				(errors.Is(coordinatorErr, context.Canceled) || errors.Is(coordinatorErr, context.DeadlineExceeded) || ctx.Err() != nil)
			fetched, fetchErr := FetchOpenRouterCost(ctx, h.APIKey, responseIDs)
			if fetchErr != nil && !interrupted && ctx.Err() == nil {
				return review.AgentResult{Cost: cost}, errors.Join(coordinatorErr, fmt.Errorf("track review cost: %w", fetchErr))
			}
			if fetchErr == nil {
				cost.AmountUSD = fetched.AmountUSD
			}
		}
	}
	return review.AgentResult{Cost: cost}, coordinatorErr
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
	stateHome := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(stateHome) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("find home directory: %w", err)
		}
		stateHome = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(stateHome, sessionDirectoryName), nil
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

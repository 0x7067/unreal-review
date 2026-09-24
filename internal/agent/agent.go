package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"unreal-review/internal/review"
)

const defaultRunner = "unreal-agent-runner"

type Runner struct {
	Bin           string
	ThinkingLevel string
	Provider      string
	APIKey        string
	Log           io.Writer
	Stderr        io.Writer
	Timeout       time.Duration
}

var _ review.Agent = Runner{}

func (r Runner) Run(ctx context.Context, req review.AgentRequest) (review.AgentResult, error) {
	runner := r.Bin
	if runner == "" {
		runner = defaultRunner
	}
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}
	payload := map[string]any{
		"system_prompt": req.SystemPrompt,
		"prompt":        req.Prompt,
	}
	if req.ReviewID != "" {
		payload["session_id"] = req.ReviewID
		payload["messages"] = []any{map[string]any{
			"role":       "user",
			"content":    req.Prompt,
			"message_id": req.ReviewID,
		}}
		delete(payload, "prompt")
	}
	if req.Model != "" {
		payload["model"] = req.Model
	}
	if r.ThinkingLevel != "" {
		payload["thinking_level"] = r.ThinkingLevel
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return review.AgentResult{}, fmt.Errorf("encode agent request: %w", err)
	}
	cmd := exec.CommandContext(ctx, runner, "-workspace", req.Workspace)
	cmd.Stdin = bytes.NewReader(encoded)
	var logBuf bytes.Buffer
	logWriter := io.Writer(&logBuf)
	if r.Log != nil {
		logWriter = io.MultiWriter(&logBuf, r.Log)
	}
	cmd.Stdout = logWriter
	if r.Stderr != nil {
		cmd.Stderr = r.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	cmd.Env = r.environ(req.Model)
	runErr := cmd.Run()
	if runErr != nil {
		runErr = fmt.Errorf("run %s: %w", runner, runErr)
	}
	cost, generationIDs, err := ParseLogCost(bytes.NewReader(logBuf.Bytes()))
	if err != nil {
		return review.AgentResult{}, errors.Join(runErr, err)
	}
	interrupted := runErr != nil && (errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) || ctx.Err() != nil)
	if !cost.Recorded() && r.APIKey != "" && len(generationIDs) > 0 {
		fetched, fetchErr := FetchOpenRouterCost(ctx, r.APIKey, generationIDs)
		if fetchErr != nil && !interrupted && ctx.Err() == nil {
			return review.AgentResult{Cost: cost}, errors.Join(runErr, fmt.Errorf("track review cost: %w", fetchErr))
		}
		if fetchErr == nil {
			cost = fetched
		}
	}
	return review.AgentResult{Cost: cost}, runErr
}

func (r Runner) environ(model string) []string {
	keep := map[string]string{}
	for _, name := range []string{
		"PATH", "HOME", "USER", "SHELL", "TMPDIR", "LANG", "LC_ALL", "TERM",
		"SSL_CERT_FILE", "SSL_CERT_DIR", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY",
		"http_proxy", "https_proxy", "no_proxy",
		"UNREAL_HARNESS_LLM_BASE_URL", "UNREAL_HARNESS_LLM_MAX_ATTEMPTS",
		"XDG_STATE_HOME",
	} {
		if value, ok := os.LookupEnv(name); ok {
			keep[name] = value
		}
	}
	provider := r.Provider
	if provider == "" {
		provider = os.Getenv("UNREAL_HARNESS_LLM_PROVIDER")
	}
	if provider == "" {
		provider = "openrouter"
	}
	keep["UNREAL_HARNESS_LLM_PROVIDER"] = provider
	if model != "" {
		keep["UNREAL_HARNESS_LLM_MODEL"] = model
	} else if value := os.Getenv("UNREAL_HARNESS_LLM_MODEL"); value != "" {
		keep["UNREAL_HARNESS_LLM_MODEL"] = value
	}
	if r.APIKey != "" {
		keep["OPENROUTER_API_KEY"] = r.APIKey
	} else if key := os.Getenv("OPENROUTER_API_KEY"); key != "" {
		keep["OPENROUTER_API_KEY"] = key
	} else if key := os.Getenv("UNREAL_HARNESS_LLM_API_KEY"); key != "" {
		keep["OPENROUTER_API_KEY"] = key
	}
	env := make([]string, 0, len(keep))
	for name, value := range keep {
		env = append(env, name+"="+value)
	}
	return env
}

func LookPath(runner string) (string, error) {
	if runner == "" {
		runner = defaultRunner
	}
	if filepath.IsAbs(runner) {
		return runner, nil
	}
	path, err := exec.LookPath(runner)
	if err != nil {
		return "", fmt.Errorf("find %s: %w; install github.com/unreallabsai/unreal-agent/cmd/unreal-agent-runner", runner, err)
	}
	return path, nil
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

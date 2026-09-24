package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"unreal-review/internal/findings"
)

const defaultRunner = "unreal-agent-runner"

type Request struct {
	Workspace     string
	Runner        string
	Model         string
	ThinkingLevel string
	SystemPrompt  string
	Prompt        string
	Provider      string
	OpenRouterKey string
	Log           io.Writer
	Stderr        io.Writer
	Timeout       time.Duration
}

func Run(ctx context.Context, req Request) error {
	runner := req.Runner
	if runner == "" {
		runner = defaultRunner
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	payload := map[string]any{
		"prompt":        req.Prompt,
		"system_prompt": req.SystemPrompt,
	}
	if req.Model != "" {
		payload["model"] = req.Model
	}
	if req.ThinkingLevel != "" {
		payload["thinking_level"] = req.ThinkingLevel
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode agent request: %w", err)
	}
	cmd := exec.CommandContext(ctx, runner, "-workspace", req.Workspace)
	cmd.Stdin = bytes.NewReader(encoded)
	if req.Log != nil {
		cmd.Stdout = req.Log
	} else {
		cmd.Stdout = io.Discard
	}
	if req.Stderr != nil {
		cmd.Stderr = req.Stderr
	} else {
		cmd.Stderr = os.Stderr
	}
	cmd.Env = runnerEnv(req)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run %s: %w", runner, err)
	}
	return nil
}

func ReadFindings(path string) (findings.Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return findings.Report{}, fmt.Errorf("open findings: %w", err)
	}
	defer func() { _ = file.Close() }()
	report, err := findings.Parse(file)
	if err != nil {
		return findings.Report{}, err
	}
	return report, nil
}

func runnerEnv(req Request) []string {
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
	provider := req.Provider
	if provider == "" {
		provider = os.Getenv("UNREAL_HARNESS_LLM_PROVIDER")
	}
	if provider == "" {
		provider = "openrouter"
	}
	keep["UNREAL_HARNESS_LLM_PROVIDER"] = provider
	if req.Model != "" {
		keep["UNREAL_HARNESS_LLM_MODEL"] = req.Model
	} else if model := os.Getenv("UNREAL_HARNESS_LLM_MODEL"); model != "" {
		keep["UNREAL_HARNESS_LLM_MODEL"] = model
	}
	key := req.OpenRouterKey
	if key == "" {
		key = os.Getenv("OPENROUTER_API_KEY")
	}
	if key == "" {
		key = os.Getenv("UNREAL_HARNESS_LLM_API_KEY")
	}
	if key != "" {
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

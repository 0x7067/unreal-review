package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/operation"
	"github.com/unreallabsai/unreal-agent/harness/tool"
	"github.com/unreallabsai/unreal-agent/harness/tool/bash"
)

func TestBoundedBashRejectsRootWideFindBeforeExecution(t *testing.T) {
	workspace := t.TempDir()
	for _, command := range []string{
		`find / -path '*/i18n-0.7.0/lib/i18n/backend/fallbacks.rb'`,
		`find -- / -name fallbacks.rb`,
		`find -D search / -name fallbacks.rb`,
		`find -X / -name fallbacks.rb`,
		`find -name fallbacks.rb`,
		`f\ind / -name fallbacks.rb`,
		`find . -exec /usr/bin/find / -name fallbacks.rb ;`,
		`cd / && find . -name fallbacks.rb`,
	} {
		t.Run(command, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "executed")
			translator := testBoundedBash(t, workspace, marker)
			ctx := &recordingContext{}

			status := translator.Translate(ctx, bashCall(command))
			if status.Error == "" {
				t.Fatal("root-wide find was accepted")
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("root-wide find executed: marker stat error = %v", err)
			}
		})
	}
}

func TestBoundedBashStopsExecutingAfterCallLimit(t *testing.T) {
	workspace := t.TempDir()
	translator := newBoundedBash(workspace, 2, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
	for _, name := range []string{"first", "second"} {
		ctx := &recordingContext{}
		status := translator.Translate(ctx, bashCall("touch "+name))
		if status.Error != "" {
			t.Fatalf("create %s: %s", name, status.Error)
		}
		if _, err := runSubmittedShell(t.Context(), ctx); err != nil {
			t.Fatalf("run %s: %v", name, err)
		}
		if _, err := os.Stat(filepath.Join(workspace, name)); err != nil {
			t.Fatalf("%s was not created: %v", name, err)
		}
	}
	blocked := filepath.Join(workspace, "blocked")
	status := translator.Translate(&recordingContext{}, bashCall("touch blocked"))
	if status.Error == "" {
		t.Fatal("command beyond the configured limit was accepted")
	}
	if _, err := os.Stat(blocked); !os.IsNotExist(err) {
		t.Fatalf("blocked command executed: marker stat error = %v", err)
	}
}

func TestBoundedBashNegativeLimitDisablesExecution(t *testing.T) {
	workspace := t.TempDir()
	translator := newBoundedBash(workspace, -1, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
	marker := filepath.Join(workspace, "blocked")
	status := translator.Translate(&recordingContext{}, bashCall("touch blocked"))
	if status.Error == "" {
		t.Fatal("command was accepted while execution was disabled")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("disabled command executed: marker stat error = %v", err)
	}
}

func TestBoundedBashAllowsWorkspaceLocalFind(t *testing.T) {
	workspace := t.TempDir()
	want := filepath.Join(workspace, "vendor", "i18n-0.7.0", "lib", "i18n", "backend", "fallbacks.rb")
	if err := os.MkdirAll(filepath.Dir(want), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(want, []byte("fallbacks"), 0o644); err != nil {
		t.Fatal(err)
	}
	translator := newBoundedBash(workspace, 0, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
	ctx := &recordingContext{}

	status := translator.Translate(ctx, bashCall(`find . -path '*/i18n-0.7.0/lib/i18n/backend/fallbacks.rb'`))
	if status.Error != "" {
		t.Fatalf("translate: %s", status.Error)
	}
	out, err := runSubmittedShell(t.Context(), ctx)
	if err != nil {
		t.Fatalf("run workspace find: %v", err)
	}
	if strings.TrimSpace(out) != "./vendor/i18n-0.7.0/lib/i18n/backend/fallbacks.rb" {
		t.Fatalf("output = %q", out)
	}
}

func TestBoundedBashAllowsAbsoluteWorkspaceRoot(t *testing.T) {
	workspace := t.TempDir()
	want := filepath.Join(workspace, "dependency.rb")
	if err := os.WriteFile(want, []byte("dependency"), 0o644); err != nil {
		t.Fatal(err)
	}
	translator := newBoundedBash(workspace, 0, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
	ctx := &recordingContext{}

	status := translator.Translate(ctx, bashCall("find "+workspace+" -name dependency.rb"))
	if status.Error != "" {
		t.Fatalf("translate: %s", status.Error)
	}
	out, err := runSubmittedShell(t.Context(), ctx)
	if err != nil {
		t.Fatalf("run workspace find: %v", err)
	}
	if strings.TrimSpace(out) != want {
		t.Fatalf("output = %q, want %q", strings.TrimSpace(out), want)
	}
}

func TestBoundedBashRejectsSymlinkRootOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "outside")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	translator := newBoundedBash(workspace, 0, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
	ctx := &recordingContext{}

	status := translator.Translate(ctx, bashCall("find outside -name dependency.rb"))
	if status.Error == "" {
		t.Fatal("symlink root outside workspace was accepted")
	}
}

func TestBoundedBashKeepsCancellationOnAllowedFind(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell process test")
	}
	workspace := t.TempDir()
	bin := t.TempDir()
	find := filepath.Join(bin, "find")
	if err := os.WriteFile(find, []byte("#!/bin/sh\nparent=$PPID\nwhile kill -0 \"$parent\" 2>/dev/null; do :; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	translator := newBoundedBash(workspace, 0, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
	ctx := &recordingContext{}
	status := translator.Translate(ctx, bashCall("find . -name dependency.rb"))
	if status.Error != "" {
		t.Fatalf("translate: %s", status.Error)
	}

	runCtx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runSubmittedShell(runCtx, ctx)
	if err == nil || runCtx.Err() != context.DeadlineExceeded {
		t.Fatalf("run error = %v, context error = %v", err, runCtx.Err())
	}
	if elapsed := time.Since(started); elapsed > 3*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
}

func testBoundedBash(t *testing.T, workspace, marker string) interface {
	Translate(tool.Context, llm.ToolCall) tool.CallStatus
} {
	t.Helper()
	bin := t.TempDir()
	find := filepath.Join(bin, "find")
	if err := os.WriteFile(find, []byte("#!/bin/sh\ntouch "+marker+"\nsleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return newBoundedBash(workspace, 0, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
}

func bashCall(command string) llm.ToolCall {
	arguments, _ := json.Marshal(map[string]any{"command": command, "max_output_length": 4096})
	return llm.ToolCall{CallID: "bash-1", Name: "bash", Arguments: string(arguments)}
}

func runSubmittedShell(ctx context.Context, submitted *recordingContext) (string, error) {
	if len(submitted.specs) != 1 {
		return "", &operationCountError{got: len(submitted.specs)}
	}
	var state operation.ShellState
	if err := json.Unmarshal(submitted.specs[0].State, &state); err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, state.Input.Shell, "-c", state.Input.Command)
	cmd.Dir = state.Input.Directory
	out, err := cmd.CombinedOutput()
	return string(out), err
}

type operationCountError struct{ got int }

func (e *operationCountError) Error() string { return "submitted shell operation count is not one" }

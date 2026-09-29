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
			if status.Error == "" || !strings.Contains(status.Error, "find .") {
				t.Fatalf("error = %q, want a bounded-search correction", status.Error)
			}
			if len(ctx.specs) != 0 {
				t.Fatalf("submitted %d shell operations, want none", len(ctx.specs))
			}
			if _, err := os.Stat(marker); !os.IsNotExist(err) {
				t.Fatalf("root-wide find executed: marker stat error = %v", err)
			}
		})
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
	translator := newBoundedBash(workspace, bash.New(bash.Config{
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
	translator := newBoundedBash(workspace, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
	ctx := &recordingContext{}

	status := translator.Translate(ctx, bashCall("find "+workspace+" -name dependency.rb"))
	if status.Error != "" {
		t.Fatalf("translate: %s", status.Error)
	}
	if len(ctx.specs) != 1 {
		t.Fatalf("submitted %d shell operations, want one", len(ctx.specs))
	}
}

func TestBoundedBashRejectsSymlinkRootOutsideWorkspace(t *testing.T) {
	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(workspace, "outside")); err != nil {
		t.Skipf("create symlink: %v", err)
	}
	translator := newBoundedBash(workspace, bash.New(bash.Config{
		Shell:         "/bin/sh",
		Directory:     workspace,
		BaseDirectory: t.TempDir(),
	}))
	ctx := &recordingContext{}

	status := translator.Translate(ctx, bashCall("find outside -name dependency.rb"))
	if status.Error == "" {
		t.Fatal("symlink root outside workspace was accepted")
	}
	if len(ctx.specs) != 0 {
		t.Fatalf("submitted %d shell operations, want none", len(ctx.specs))
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
	translator := newBoundedBash(workspace, bash.New(bash.Config{
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
	return newBoundedBash(workspace, bash.New(bash.Config{
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

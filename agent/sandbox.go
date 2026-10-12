package agent

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/0x7067/unreal-review/review"
)

const (
	sandboxCommandTimeout       = 60 * time.Second
	sandboxStaleAge             = 24 * time.Hour
	sandboxAddressSpace   int64 = 4 << 30
	sandboxFileSize       int64 = 1 << 30
	sandboxScratchLimit   int64 = 2 << 30
	sandboxTmpfsSize      int64 = 256 << 20
	sandboxFallbackSuffix       = "verifier tasks run with Bash unchanged except a scrubbed environment."
)

// sandboxNotices receives the one-line sandbox status. Tests replace it.
var sandboxNotices io.Writer = os.Stderr

// sandboxConfig overrides limits. Zero values use the production defaults.
type sandboxConfig struct {
	commandTimeout time.Duration
	scratchLimit   int64
}

func (c sandboxConfig) timeout() time.Duration {
	if c.commandTimeout > 0 {
		return c.commandTimeout
	}
	return sandboxCommandTimeout
}

func (c sandboxConfig) limitBytes() int64 {
	if c.scratchLimit > 0 {
		return c.scratchLimit
	}
	return sandboxScratchLimit
}

// verifierSandbox is the scratch copy and Bash wrapper for one review's
// verifier tasks. close removes the scratch directory.
type verifierSandbox struct {
	root           string
	tree           string
	shell          string
	active         bool
	commandTimeout time.Duration
	notice         string
}

func (s *verifierSandbox) close() {
	if s == nil || s.root == "" {
		return
	}
	removeSandboxTree(s.root)
	s.root = ""
}

func (s *verifierSandbox) abandon(err error) (*verifierSandbox, error) {
	s.close()
	return nil, err
}

func (s *verifierSandbox) announce() {
	if s == nil || s.notice == "" || sandboxNotices == nil {
		return
	}
	_, _ = fmt.Fprintln(sandboxNotices, s.notice)
}

func verifierSandboxDir(reviewID string) (string, error) {
	base, err := sessionDirectory()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(reviewID) == "" {
		return "", fmt.Errorf("review ID must be set")
	}
	return filepath.Join(base, "sandbox", focusedHash(reviewID)), nil
}

// prepareVerifierSandbox builds a scratch copy and an offline bubblewrap
// wrapper for verifier tasks. Provisioning failures fall back to a scrubbed
// Bash and announce one stderr line. Cancellation still aborts the review.
func prepareVerifierSandbox(ctx context.Context, workspace, headSHA, reviewID string, cfg sandboxConfig) (*verifierSandbox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := verifierSandboxDir(reviewID)
	if err != nil {
		return nil, err
	}
	sweepStaleSandboxes(filepath.Dir(root))
	sb := &verifierSandbox{commandTimeout: cfg.timeout()}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	sb.root = root
	if err := os.Chmod(root, 0o700); err != nil {
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	if err := os.WriteFile(filepath.Join(root, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	if strings.TrimSpace(headSHA) == "" {
		return sb.fallback(sandboxUnavailable("no head SHA"))
	}
	if runtime.GOOS != "linux" {
		return sb.fallback(sandboxUnavailable("not Linux"))
	}
	tool, err := sandboxTools()
	if err != nil {
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	if _, err := exec.LookPath("go"); err != nil {
		return sb.fallback(sandboxUnavailable("no go binary"))
	}
	if !headHasGoMod(ctx, workspace, headSHA) {
		if err := ctx.Err(); err != nil {
			return sb.abandon(err)
		}
		return sb.fallback(sandboxSkippedNoGoMod)
	}
	toolchain, err := readGoToolchain(ctx, workspace)
	if err != nil {
		return sb.fail(ctx, err)
	}
	tree := filepath.Join(root, "tree")
	if err := extractGitArchive(ctx, workspace, headSHA, tree); err != nil {
		return sb.fail(ctx, err)
	}
	gitBinds, err := gitReadonlyBinds(ctx, workspace, tree)
	if err != nil {
		return sb.fail(ctx, err)
	}
	args, envPairs, err := sandboxMounts(root, tree, toolchain, gitBinds)
	if err != nil {
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	limitKB := cfg.limitBytes() / 1024
	if limitKB < 1 {
		limitKB = 1
	}
	shell := filepath.Join(root, "shell")
	body := renderSandboxScript(sandboxScript{
		root: root, timeout: int(cfg.timeout() / time.Second), limitKB: limitKB,
		envBin: tool.env, prlimit: tool.prlimit, timeoutBin: tool.timeout, bwrap: tool.bwrap,
		wc: tool.wc, du: tool.du, envPairs: envPairs, bwrapArgs: joinQuoted(args),
		as: sandboxAddressSpace, fsize: sandboxFileSize,
	})
	if err := writeExecutable(shell, body); err != nil {
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	if err := ctx.Err(); err != nil {
		return sb.abandon(err)
	}
	probeOut, probeErr := exec.CommandContext(ctx, shell, "--probe").CombinedOutput()
	if probeErr != nil {
		if ctx.Err() != nil {
			return sb.abandon(ctx.Err())
		}
		reason := firstLine(string(probeOut))
		if reason == "" {
			reason = "probe failed"
		}
		return sb.fallback(sandboxUnavailable(reason))
	}
	listOut, listErr := exec.CommandContext(ctx, shell, "-c", "go list -m").CombinedOutput()
	if listErr != nil {
		if ctx.Err() != nil {
			return sb.abandon(ctx.Err())
		}
		line := firstLine(string(listOut))
		if line == "" {
			line = "go list -m failed"
		}
		return sb.fallback(sandboxUnavailable("toolchain cannot build the reviewed head: " + line))
	}
	sb.tree = tree
	sb.shell = shell
	sb.active = true
	sb.notice = fmt.Sprintf("unreal-review: verifier sandbox active (bwrap %s, offline, %d s per command, scratch %s).", bwrapVersion(ctx, tool.bwrap), int(cfg.timeout()/time.Second), tree)
	sb.announce()
	return sb, nil
}

func (s *verifierSandbox) fail(ctx context.Context, err error) (*verifierSandbox, error) {
	if ctx.Err() != nil {
		return s.abandon(ctx.Err())
	}
	return s.fallback(sandboxUnavailable(err.Error()))
}

func (s *verifierSandbox) fallback(notice string) (*verifierSandbox, error) {
	s.active = false
	s.tree = ""
	s.notice = notice
	shell, err := installScrubShell(s.root)
	if err != nil {
		dir, mkErr := os.MkdirTemp("", "unreal-review-verifier-")
		if mkErr == nil {
			old := s.root
			s.root = dir
			shell, err = installScrubShell(dir)
			if err == nil && old != "" && old != dir {
				removeSandboxTree(old)
			}
		}
	}
	if err == nil {
		s.shell = shell
	}
	s.announce()
	return s, nil
}

const sandboxSkippedNoGoMod = "unreal-review: verifier sandbox skipped (no go.mod at reviewed head); " + sandboxFallbackSuffix

func sandboxUnavailable(reason string) string {
	reason = firstLine(reason)
	if reason == "" {
		reason = "unknown"
	}
	return fmt.Sprintf("unreal-review: verifier sandbox unavailable (%s); %s", reason, sandboxFallbackSuffix)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

type sandboxBins struct {
	bwrap, prlimit, timeout, env, wc, du string
}

func sandboxTools() (sandboxBins, error) {
	var bins sandboxBins
	for _, item := range []struct {
		name string
		dst  *string
	}{
		{"bwrap", &bins.bwrap},
		{"prlimit", &bins.prlimit},
		{"timeout", &bins.timeout},
		{"env", &bins.env},
		{"wc", &bins.wc},
		{"du", &bins.du},
	} {
		path, err := exec.LookPath(item.name)
		if err != nil {
			return sandboxBins{}, fmt.Errorf("%s not found", item.name)
		}
		*item.dst = path
	}
	return bins, nil
}

type goToolchain struct {
	goroot, modcache, gopath string
}

func readGoToolchain(ctx context.Context, workspace string) (goToolchain, error) {
	out, err := commandOutput(ctx, workspace, "go", "env", "GOROOT", "GOMODCACHE", "GOPATH")
	if err != nil {
		return goToolchain{}, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 3 || lines[0] == "" {
		return goToolchain{}, fmt.Errorf("go env GOROOT returned nothing")
	}
	info, err := os.Stat(lines[0])
	if err != nil || !info.IsDir() {
		return goToolchain{}, fmt.Errorf("GOROOT %s is not a directory", lines[0])
	}
	return goToolchain{goroot: lines[0], modcache: lines[1], gopath: lines[2]}, nil
}

func headHasGoMod(ctx context.Context, workspace, sha string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", workspace, "cat-file", "-e", sha+":go.mod")
	return cmd.Run() == nil
}

func extractGitArchive(ctx context.Context, workspace, sha, dest string) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", `git -C "$1" archive --format=tar "$2" | tar -x -C "$3"`, "extract", workspace, sha, dest)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git archive: %s", firstLine(string(out)+" "+err.Error()))
	}
	return nil
}

// gitReadonlyBinds exposes objects, refs, HEAD, and shallow so git show works
// inside the scratch tree without the host config or hooks.
func gitReadonlyBinds(ctx context.Context, workspace, tree string) ([]string, error) {
	gitDir := filepath.Join(tree, ".git")
	for _, dir := range []string{filepath.Join(gitDir, "objects"), filepath.Join(gitDir, "refs")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}
	for _, name := range []string{"HEAD", "config"} {
		if err := os.WriteFile(filepath.Join(gitDir, name), nil, 0o600); err != nil {
			return nil, err
		}
	}
	var args []string
	for _, name := range []string{"objects", "refs", "HEAD"} {
		src, err := absoluteGitPath(ctx, workspace, name)
		if err != nil {
			return nil, err
		}
		args = append(args, "--ro-bind", src, filepath.Join(gitDir, name))
	}
	shallow, err := absoluteGitPath(ctx, workspace, "shallow")
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(shallow); statErr == nil && !info.IsDir() {
		if err := os.WriteFile(filepath.Join(gitDir, "shallow"), nil, 0o600); err != nil {
			return nil, err
		}
		args = append(args, "--ro-bind", shallow, filepath.Join(gitDir, "shallow"))
	}
	return args, nil
}

func absoluteGitPath(ctx context.Context, workspace, name string) (string, error) {
	out, err := commandOutput(ctx, workspace, "git", "rev-parse", "--git-path", name)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(out) {
		return filepath.Clean(out), nil
	}
	return filepath.Clean(filepath.Join(workspace, out)), nil
}

func sandboxMounts(root, tree string, tc goToolchain, gitBinds []string) ([]string, string, error) {
	home := filepath.Join(root, "home")
	tmp := filepath.Join(root, "tmp")
	gopath := filepath.Join(root, "gopath")
	cache := filepath.Join(root, "gocache")
	mod := filepath.Join(root, "modcache")
	for _, dir := range []string{home, tmp, gopath, cache, mod} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, "", err
		}
	}
	modSrc := tc.modcache
	if info, err := os.Stat(modSrc); err != nil || !info.IsDir() {
		modSrc = mod
	}
	args := []string{"--unshare-all", "--die-with-parent", "--new-session", "--proc", "/proc", "--dev", "/dev"}
	for _, path := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		args = append(args, "--ro-bind", path, path)
	}
	args = append(args, "--ro-bind", tc.goroot, tc.goroot, "--ro-bind", modSrc, mod, "--bind", tree, tree)
	args = append(args, gitBinds...)
	args = append(args, "--bind", gopath, gopath, "--bind", cache, cache)
	size := strconv.FormatInt(sandboxTmpfsSize, 10)
	args = append(args, "--size", size, "--tmpfs", home, "--size", size, "--tmpfs", tmp, "--chdir", tree)
	pairs := shellAssignments([][2]string{
		{"PATH", filepath.Join(tc.goroot, "bin") + ":/usr/bin:/bin"},
		{"GOROOT", tc.goroot},
		{"GOTOOLCHAIN", "local"},
		{"GOPROXY", "off"},
		{"GOFLAGS", "-buildvcs=false"},
		{"GOMODCACHE", mod},
		{"GOCACHE", cache},
		{"GOPATH", gopath},
		{"HOME", home},
		{"TMPDIR", tmp},
	})
	return args, pairs, nil
}

type sandboxScript struct {
	root, envBin, prlimit, timeoutBin, bwrap, wc, du string
	envPairs, bwrapArgs                              string
	timeout                                          int
	limitKB, as, fsize                               int64
}

func renderSandboxScript(s sandboxScript) string {
	return strings.NewReplacer(
		"@@ROOT@@", shQuote(s.root),
		"@@TIMEOUT@@", strconv.Itoa(s.timeout),
		"@@LIMIT@@", strconv.FormatInt(s.limitKB, 10),
		"@@ENV@@", shQuote(s.envBin),
		"@@ENV_RAW@@", s.envBin,
		"@@PRLIMIT@@", shQuote(s.prlimit),
		"@@TIMEOUTBIN@@", shQuote(s.timeoutBin),
		"@@BWRAP@@", shQuote(s.bwrap),
		"@@WC_RAW@@", s.wc,
		"@@DU@@", shQuote(s.du),
		"@@AS@@", strconv.FormatInt(s.as, 10),
		"@@FSIZE@@", strconv.FormatInt(s.fsize, 10),
		"@@ENV_PAIRS@@", s.envPairs,
		"@@BWRAP_ARGS@@", s.bwrapArgs,
	).Replace(sandboxScriptSource)
}

const sandboxScriptSource = `#!/bin/sh
set -eu
root=@@ROOT@@
timeout_secs=@@TIMEOUT@@
cap_kb=@@LIMIT@@
run() {
  @@ENV@@ -i @@ENV_PAIRS@@ \
    @@PRLIMIT@@ --as=@@AS@@ --fsize=@@FSIZE@@ \
    @@TIMEOUTBIN@@ -k 2 "$timeout_secs" \
    @@BWRAP@@ @@BWRAP_ARGS@@ \
    /bin/sh -c "$1"
}
if [ "${1:-}" = "--probe" ]; then
  exec @@ENV@@ -i @@PRLIMIT@@ --as=@@AS@@ --fsize=@@FSIZE@@ @@TIMEOUTBIN@@ -k 2 "$timeout_secs" @@BWRAP@@ @@BWRAP_ARGS@@ /bin/sh -c 'unset PWD; test "$(@@WC_RAW@@ -l </proc/net/dev)" -eq 3 && test -z "$(@@ENV_RAW@@)"'
fi
if [ "${1:-}" != "-c" ]; then
  echo "unreal-review: verifier shell expects -c" >&2
  exit 2
fi
used=0
while IFS= read -r line; do
  kb=${line%%[!0-9]*}
  [ -n "$kb" ] || continue
  used=$((used + kb))
done <<EOF
$(@@DU@@ -skx "$root/tree" "$root/gocache" "$root/gopath")
EOF
if [ "$used" -ge "$cap_kb" ]; then
  echo "unreal-review: verifier scratch exceeds ${cap_kb} KiB; refusing further commands" >&2
  exit 1
fi
run "${2-}"
`

func installScrubShell(root string) (string, error) {
	if root == "" {
		return "", fmt.Errorf("sandbox directory is missing")
	}
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		return "", err
	}
	envBin, err := exec.LookPath("env")
	if err != nil {
		return "", err
	}
	path := os.Getenv("PATH")
	if strings.TrimSpace(path) == "" {
		path = "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
	}
	lang := os.Getenv("LANG")
	if strings.TrimSpace(lang) == "" || strings.ContainsAny(lang, "\n\r") {
		lang = "C.UTF-8"
	}
	body := strings.NewReplacer(
		"@@ENV@@", shQuote(envBin),
		"@@PATH@@", shQuote(path),
		"@@HOME@@", shQuote(home),
		"@@LANG@@", shQuote(lang),
	).Replace("#!/bin/sh\nset -eu\nif [ \"${1:-}\" != \"-c\" ]; then\n  echo \"unreal-review: verifier shell expects -c\" >&2\n  exit 2\nfi\nexec @@ENV@@ -i PATH=@@PATH@@ HOME=@@HOME@@ LANG=@@LANG@@ /bin/sh -c \"${2-}\"\n")
	shell := filepath.Join(root, "shell")
	if err := writeExecutable(shell, body); err != nil {
		return "", err
	}
	return shell, nil
}

func bwrapVersion(ctx context.Context, path string) string {
	out, err := commandOutput(ctx, "", path, "--version")
	if err != nil {
		return "unknown"
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "unknown"
	}
	return fields[len(fields)-1]
}

func commandOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := firstLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", errors.New(msg)
	}
	return strings.TrimSpace(string(out)), nil
}

func writeExecutable(path, body string) error {
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		return err
	}
	return os.Chmod(path, 0o700)
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func joinQuoted(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shQuote(arg)
	}
	return strings.Join(quoted, " ")
}

func shellAssignments(pairs [][2]string) string {
	parts := make([]string, len(pairs))
	for i, kv := range pairs {
		parts[i] = kv[0] + "=" + shQuote(kv[1])
	}
	return strings.Join(parts, " ")
}

func removeSandboxTree(root string) {
	if root == "" {
		return
	}
	_ = os.Chmod(root, 0o700)
	_ = filepath.Walk(root, func(path string, _ os.FileInfo, _ error) error {
		_ = os.Chmod(path, 0o700)
		return nil
	})
	_ = os.RemoveAll(root)
}

func sweepStaleSandboxes(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		if sandboxStale(dir) {
			removeSandboxTree(dir)
		}
	}
}

func sandboxStale(dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	if time.Since(info.ModTime()) > sandboxStaleAge {
		return true
	}
	return !processAlive(readPID(dir))
}

func readPID(dir string) int {
	b, err := os.ReadFile(filepath.Join(dir, "pid"))
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}

func processAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	err := unix.Kill(pid, 0)
	return err == nil || errors.Is(err, unix.EPERM)
}

func verifierSandboxAddendum(active bool, perCommand time.Duration) string {
	if !active {
		return ""
	}
	if perCommand <= 0 {
		perCommand = sandboxCommandTimeout
	}
	seconds := int(perCommand / time.Second)
	return fmt.Sprintf("\nVerifier sandbox: the workspace is a scratch copy of the reviewed commit and code may be run offline here (go build, go vet, go test of the changed packages, or a repro written in this scratch copy). Each command is killed after %d s; the network is off. This severity rule applies only to claims about runtime behavior. A finding that claims runtime behavior and is demonstrated by a command you ran in this session keeps its severity; cite that command and a short output excerpt in the body. A runtime-behavior claim you could not demonstrate is re-emitted with severity note, not dropped. A finding settled by reading the code alone (exact quoted lines at the cited range, such as a compile error or an always-false condition) may keep its severity. Re-emit each candidate with the same id, path, lines, and anchor; only severity and body may change.", seconds)
}

func applyVerifierSandbox(child *review.AgentRequest, agent review.Agent, sb *verifierSandbox) review.Agent {
	if sb == nil {
		return agent
	}
	child.SystemPrompt += verifierSandboxAddendum(sb.active, sb.commandTimeout)
	if sb.active {
		child.Workspace = sb.tree
	}
	if sb.shell == "" {
		return agent
	}
	child.Shell = sb.shell
	return withShell(agent, sb.shell)
}

func withShell(agent review.Agent, shell string) review.Agent {
	switch wrapped := agent.(type) {
	case Harness:
		wrapped.Shell = shell
		return wrapped
	default:
		return agent
	}
}

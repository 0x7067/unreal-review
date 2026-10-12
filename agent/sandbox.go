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
	sandboxCommandTimeout = 60 * time.Second
	sandboxTaskBudget     = 5 * time.Minute
	sandboxStaleAge       = 24 * time.Hour
	// sandboxAddressSpace is the RLIMIT_AS cap the design ran Go under.
	sandboxAddressSpace int64 = 4 << 30
	// sandboxFileSize caps one write. The design confirmed RLIMIT_FSIZE and
	// did not name the byte cap.
	sandboxFileSize  int64 = 1 << 30
	sandboxTmpfsSize int64 = 1 << 30
)

// sandboxNotices receives the one-line sandbox status. Tests replace it.
var sandboxNotices io.Writer = os.Stderr

// sandboxConfig overrides limits. Zero values use the production defaults.
type sandboxConfig struct {
	commandTimeout time.Duration
	taskBudget     time.Duration
}

func (c sandboxConfig) timeout() time.Duration {
	if c.commandTimeout > 0 {
		return c.commandTimeout
	}
	return sandboxCommandTimeout
}

func (c sandboxConfig) budget() time.Duration {
	if c.taskBudget > 0 {
		return c.taskBudget
	}
	return sandboxTaskBudget
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
	_ = os.RemoveAll(s.root)
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
// wrapper for verifier tasks. Provisioning failures fall back to verification
// without code execution and announce one stderr line.
func prepareVerifierSandbox(ctx context.Context, workspace, headSHA, reviewID string, cfg sandboxConfig) (*verifierSandbox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root, err := verifierSandboxDir(reviewID)
	if err != nil {
		return nil, err
	}
	sweepStaleSandboxes(filepath.Dir(root))
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(root, 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, "pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600); err != nil {
		return nil, err
	}
	sb := &verifierSandbox{root: root, commandTimeout: cfg.timeout()}
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
		if ctx.Err() != nil {
			return sb.abandon(ctx.Err())
		}
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	tree := filepath.Join(root, "tree")
	if err := extractGitArchive(ctx, workspace, headSHA, tree); err != nil {
		if ctx.Err() != nil {
			return sb.abandon(ctx.Err())
		}
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	gitBinds, err := gitReadonlyBinds(ctx, workspace, tree)
	if err != nil {
		if ctx.Err() != nil {
			return sb.abandon(ctx.Err())
		}
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	version, overlay := bwrapInfo(ctx, tool.bwrap)
	args, envPairs, err := sandboxMounts(root, tree, toolchain, gitBinds, overlay)
	if err != nil {
		return sb.fallback(sandboxUnavailable(err.Error()))
	}
	shell := filepath.Join(root, "shell")
	body := renderSandboxScript(sandboxScript{
		root: root, budget: int(cfg.budget() / time.Second), timeout: int(cfg.timeout() / time.Second),
		envBin: tool.env, prlimit: tool.prlimit, timeoutBin: tool.timeout, bwrap: tool.bwrap, wc: tool.wc,
		envPairs: envPairs, bwrapArgs: joinQuoted(args),
		as: sandboxAddressSpace, fsize: sandboxFileSize,
	})
	if err := writeExecutable(shell, body); err != nil {
		return sb.abandon(err)
	}
	probe := exec.CommandContext(ctx, shell, "--probe")
	out, probeErr := probe.CombinedOutput()
	if probeErr != nil {
		if ctx.Err() != nil {
			return sb.abandon(ctx.Err())
		}
		reason := firstLine(string(out))
		if reason == "" {
			reason = "probe failed"
		}
		return sb.fallback(sandboxUnavailable(reason))
	}
	sb.tree = tree
	sb.shell = shell
	sb.active = true
	sb.notice = fmt.Sprintf("unreal-review: verifier sandbox active (bwrap %s, offline, %d s per command, scratch %s).", version, int(cfg.timeout()/time.Second), tree)
	sb.announce()
	return sb, nil
}

func (s *verifierSandbox) fallback(notice string) (*verifierSandbox, error) {
	s.active = false
	s.tree = ""
	s.notice = notice
	shell, err := installScrubShell(s.root)
	if err != nil {
		return s.abandon(err)
	}
	s.shell = shell
	s.announce()
	return s, nil
}

const sandboxSkippedNoGoMod = "unreal-review: verifier sandbox skipped (no go.mod at reviewed head); verifier tasks run without code execution."

func sandboxUnavailable(reason string) string {
	reason = firstLine(reason)
	if reason == "" {
		reason = "unknown"
	}
	return fmt.Sprintf("unreal-review: verifier sandbox unavailable (%s); verifier tasks run without code execution.", reason)
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

type sandboxBins struct {
	bwrap, prlimit, timeout, env, wc string
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
	goroot    string
	modcache  string
	gopath    string
	cacheHost string
}

func readGoToolchain(ctx context.Context, workspace string) (goToolchain, error) {
	out, err := commandOutput(ctx, workspace, "go", "env", "GOROOT", "GOMODCACHE", "GOPATH", "GOCACHE")
	if err != nil {
		return goToolchain{}, err
	}
	lines := strings.Split(out, "\n")
	if len(lines) < 4 || lines[0] == "" {
		return goToolchain{}, fmt.Errorf("go env GOROOT returned nothing")
	}
	tc := goToolchain{goroot: lines[0], modcache: lines[1], gopath: lines[2], cacheHost: lines[3]}
	info, err := os.Stat(tc.goroot)
	if err != nil || !info.IsDir() {
		return goToolchain{}, fmt.Errorf("GOROOT %s is not a directory", tc.goroot)
	}
	return tc, nil
}

func headHasGoMod(ctx context.Context, workspace, sha string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", workspace, "cat-file", "-e", sha+":go.mod")
	return cmd.Run() == nil
}

func extractGitArchive(ctx context.Context, workspace, sha, dest string) error {
	if err := os.MkdirAll(dest, 0o700); err != nil {
		return err
	}
	archive := exec.CommandContext(ctx, "git", "-C", workspace, "archive", "--format=tar", sha)
	tarCmd := exec.CommandContext(ctx, "tar", "-x", "-C", dest)
	var archiveErr, tarErr bytes.Buffer
	reader, writer := io.Pipe()
	archive.Stdout = writer
	archive.Stderr = &archiveErr
	tarCmd.Stdin = reader
	tarCmd.Stderr = &tarErr
	if err := archive.Start(); err != nil {
		return err
	}
	if err := tarCmd.Start(); err != nil {
		_ = archive.Process.Kill()
		_ = writer.Close()
		_ = archive.Wait()
		return err
	}
	errArchive := archive.Wait()
	_ = writer.Close()
	errTar := tarCmd.Wait()
	if errArchive != nil {
		return fmt.Errorf("git archive: %s", firstLine(strings.TrimSpace(archiveErr.String()+" "+errArchive.Error())))
	}
	if errTar != nil {
		return fmt.Errorf("tar: %s", firstLine(strings.TrimSpace(tarErr.String()+" "+errTar.Error())))
	}
	return nil
}

// gitReadonlyBinds exposes objects, refs, HEAD, and shallow so git show works
// inside the scratch tree without the host config or hooks.
func gitReadonlyBinds(ctx context.Context, workspace, tree string) ([]string, error) {
	gitDir := filepath.Join(tree, ".git")
	if err := os.MkdirAll(filepath.Join(gitDir, "objects"), 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(gitDir, "refs"), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(gitDir, "HEAD"), []byte(""), 0o600); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), nil, 0o600); err != nil {
		return nil, err
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
		if err := os.WriteFile(filepath.Join(gitDir, "shallow"), []byte(""), 0o600); err != nil {
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

func sandboxMounts(root, tree string, tc goToolchain, gitBinds []string, overlay bool) ([]string, string, error) {
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
	// tmpfs /tmp before binds. A later tmpfs would hide a scratch path under /tmp.
	args := []string{
		"--unshare-all", "--die-with-parent",
		"--proc", "/proc", "--dev", "/dev",
		"--size", strconv.FormatInt(sandboxTmpfsSize, 10), "--tmpfs", "/tmp",
	}
	for _, path := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		args = append(args, "--ro-bind", path, path)
	}
	args = append(args,
		"--ro-bind", tc.goroot, tc.goroot,
		"--ro-bind", modSrc, mod,
		"--bind", tree, tree,
	)
	args = append(args, gitBinds...)
	args = append(args, "--bind", tmp, tmp, "--bind", home, home, "--bind", gopath, gopath)
	if overlay && tc.cacheHost != "" {
		if err := os.MkdirAll(tc.cacheHost, 0o700); err != nil {
			return nil, "", err
		}
		args = append(args, "--overlay-src", tc.cacheHost, "--tmp-overlay", cache)
	} else {
		args = append(args, "--size", strconv.FormatInt(sandboxTmpfsSize, 10), "--tmpfs", cache)
	}
	args = append(args, "--chdir", tree)
	pairs := shellAssignments([][2]string{
		{"PATH", filepath.Join(tc.goroot, "bin") + ":/usr/bin:/bin"},
		{"GOROOT", tc.goroot},
		{"GOTOOLCHAIN", "local"},
		{"GOPROXY", "off"},
		{"GOFLAGS", "-mod=readonly -buildvcs=false"},
		{"GOMODCACHE", mod},
		{"GOCACHE", cache},
		{"GOPATH", gopath},
		{"HOME", home},
		{"TMPDIR", tmp},
	})
	return args, pairs, nil
}

type sandboxScript struct {
	root, envBin, prlimit, timeoutBin, bwrap, wc string
	envPairs, bwrapArgs                          string
	budget, timeout                              int
	as, fsize                                    int64
}

func renderSandboxScript(s sandboxScript) string {
	return strings.NewReplacer(
		"@@ROOT@@", shQuote(s.root),
		"@@BUDGET@@", strconv.Itoa(s.budget),
		"@@TIMEOUT@@", strconv.Itoa(s.timeout),
		"@@ENV@@", shQuote(s.envBin),
		"@@ENV_RAW@@", s.envBin,
		"@@PRLIMIT@@", shQuote(s.prlimit),
		"@@TIMEOUTBIN@@", shQuote(s.timeoutBin),
		"@@BWRAP@@", shQuote(s.bwrap),
		"@@WC_RAW@@", s.wc,
		"@@AS@@", strconv.FormatInt(s.as, 10),
		"@@FSIZE@@", strconv.FormatInt(s.fsize, 10),
		"@@ENV_PAIRS@@", s.envPairs,
		"@@BWRAP_ARGS@@", s.bwrapArgs,
	).Replace(sandboxScriptSource)
}

const sandboxScriptSource = `#!/bin/sh
set -eu
root=@@ROOT@@
budget=@@BUDGET@@
timeout_secs=@@TIMEOUT@@
run() {
  @@ENV@@ -i @@ENV_PAIRS@@ \
    @@PRLIMIT@@ --as=@@AS@@ --fsize=@@FSIZE@@ \
    @@TIMEOUTBIN@@ -k 2 "$timeout_secs" \
    @@BWRAP@@ @@BWRAP_ARGS@@ \
    /bin/sh -c "$1"
}
if [ "${1:-}" = "--probe" ]; then
  # dash sets PWD; unset it so the empty-environment assertion sees only leaks.
  exec @@ENV@@ -i @@PRLIMIT@@ --as=@@AS@@ --fsize=@@FSIZE@@ @@TIMEOUTBIN@@ -k 2 "$timeout_secs" @@BWRAP@@ @@BWRAP_ARGS@@ /bin/sh -c 'unset PWD; test "$(@@WC_RAW@@ -l </proc/net/dev)" -eq 3 && test -z "$(@@ENV_RAW@@)"'
fi
if [ "${1:-}" != "-c" ]; then
  echo "unreal-review: verifier shell expects -c" >&2
  exit 2
fi
task=$(cat "$root/active-task" 2>/dev/null || echo task)
case "$task" in
  *[!A-Za-z0-9._-]*) task=task ;;
esac
mkdir -p "$root/budgets"
ledger="$root/budgets/$task"
secs=0
if [ -f "$ledger" ]; then
  secs=$(tr -cd '0-9' < "$ledger" || true)
  secs=${secs:-0}
fi
if [ "$secs" -ge "$budget" ]; then
  echo "unreal-review: verifier command time budget exhausted" >&2
  exit 1
fi
start=$(date +%s)
set +e
run "${2-}"
code=$?
set -e
end=$(date +%s)
elapsed=$((end - start))
if [ "$elapsed" -lt 0 ]; then elapsed=0; fi
cur=0
if [ -f "$ledger" ]; then
  cur=$(tr -cd '0-9' < "$ledger" || true)
  cur=${cur:-0}
fi
echo $((cur + elapsed)) > "$ledger"
exit "$code"
`

func installScrubShell(root string) (string, error) {
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
	).Replace(`#!/bin/sh
set -eu
if [ "${1:-}" != "-c" ]; then
  echo "unreal-review: verifier shell expects -c" >&2
  exit 2
fi
exec @@ENV@@ -i PATH=@@PATH@@ HOME=@@HOME@@ LANG=@@LANG@@ /bin/sh -c "${2-}"
`)
	shell := filepath.Join(root, "shell")
	if err := writeExecutable(shell, body); err != nil {
		return "", err
	}
	return shell, nil
}

func bwrapInfo(ctx context.Context, path string) (string, bool) {
	out, err := commandOutput(ctx, "", path, "--version")
	if err != nil {
		return "unknown", false
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return "unknown", false
	}
	ver := fields[len(fields)-1]
	return ver, versionAtLeast(ver, 0, 11)
}

func versionAtLeast(ver string, major, minor int) bool {
	parts := strings.Split(ver, ".")
	if len(parts) < 2 {
		return false
	}
	maj, err1 := strconv.Atoi(parts[0])
	min, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	if maj != major {
		return maj > major
	}
	return min >= minor
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
		return "", fmt.Errorf("%s", msg)
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
			_ = os.RemoveAll(dir)
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

func sandboxTaskName(stage string) string {
	var b strings.Builder
	for _, r := range stage {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "task"
	}
	return b.String()
}

func verifierSandboxAddendum(active bool, perCommand time.Duration) string {
	if !active {
		return ""
	}
	if perCommand <= 0 {
		perCommand = sandboxCommandTimeout
	}
	seconds := int(perCommand / time.Second)
	return fmt.Sprintf("\nVerifier sandbox: the workspace is a scratch copy of the reviewed commit and code may be run offline here (go build, go vet, go test of the changed packages, or a repro written in this scratch copy). Each command is killed after %d s; the network is off. A finding that claims runtime behavior and is demonstrated by a command you ran in this session keeps its severity; cite that command and a short output excerpt in the body. A runtime-behavior claim you could not demonstrate is re-emitted with severity note, not dropped. A finding settled by reading the code alone (exact quoted lines at the cited range, such as a compile error or an always-false condition) may keep its severity. Re-emit each candidate with the same id, path, lines, and anchor; only severity and body may change.", seconds)
}

func applyVerifierSandbox(child *review.AgentRequest, agent review.Agent, sb *verifierSandbox, stage string) review.Agent {
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
	_ = os.WriteFile(filepath.Join(sb.root, "active-task"), []byte(sandboxTaskName(stage)), 0o600)
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

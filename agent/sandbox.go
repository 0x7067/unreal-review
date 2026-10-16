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

	"github.com/0x7067/unreal-review/review"
)

const (
	sandboxCommandTimeout       = 60 * time.Second
	sandboxStaleAge             = 24 * time.Hour
	sandboxAddressSpace   int64 = 4 << 30
	sandboxFileSize       int64 = 1 << 30
	sandboxScratchLimit   int64 = 2 << 30
	sandboxTmpfsSize      int64 = 256 << 20
	sandboxTaskWall             = 3 * time.Minute
	verifierTaskWall            = 6 * time.Minute
	sandboxScrubbed             = "verifier tasks run with Bash unchanged except a scrubbed environment."
	sandboxBashOff              = "verifier Bash disabled because a scrubbed shell could not be installed."
	sandboxBudgetSpent          = "unreal-review: verifier sandbox budget spent; decide from the evidence you have"
)

var sandboxNotices io.Writer = os.Stderr

type sandboxConfig struct {
	commandTimeout time.Duration
	scratchLimit   int64
	fileSize       int64
	tmpfsSize      int64
	addressSpace   int64
}
type verifierSandbox struct {
	root           string
	tree           string
	shell          string
	active         bool
	disableBash    bool
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
func prepareVerifierSandbox(ctx context.Context, workspace, headSHA, reviewID string, cfg sandboxConfig) (*verifierSandbox, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timeout := cfg.commandTimeout
	if timeout <= 0 {
		timeout = sandboxCommandTimeout
	}
	root, err := verifierSandboxDir(reviewID)
	if err != nil {
		return (&verifierSandbox{commandTimeout: timeout}).refuse(err.Error())
	}
	sweepStaleSandboxes(filepath.Dir(root))
	sb := &verifierSandbox{commandTimeout: timeout}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return sb.refuse(err.Error())
	}
	sb.root = root
	if strings.TrimSpace(headSHA) == "" {
		return sb.refuse("no head SHA")
	}
	if runtime.GOOS != "linux" {
		return sb.refuse("not Linux")
	}
	if !headHasGoMod(ctx, workspace, headSHA) {
		if err := ctx.Err(); err != nil {
			return sb.abandon(err)
		}
		return sb.fallback("verifier sandbox skipped (no go.mod at reviewed head)")
	}
	toolchain, err := readGoToolchain(ctx, workspace)
	if err != nil {
		return sb.fail(ctx, err)
	}
	bin, err := sandboxBins()
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
	limit, fsize, tmpfs, as := cfg.scratchLimit, cfg.fileSize, cfg.tmpfsSize, cfg.addressSpace
	if limit <= 0 {
		limit = sandboxScratchLimit
	}
	if fsize <= 0 {
		fsize = sandboxFileSize
	}
	if tmpfs <= 0 {
		tmpfs = sandboxTmpfsSize
	}
	if as <= 0 {
		as = sandboxAddressSpace
	}
	args, envPairs, err := sandboxMounts(root, tree, toolchain, gitBinds, tmpfs)
	if err != nil {
		return sb.fail(ctx, err)
	}
	limitKB := limit / 1024
	if limitKB < 1 {
		limitKB = 1
	}
	shell := filepath.Join(root, "shell")
	body := sandboxScript(root, int(timeout/time.Second), limitKB, bin, envPairs, joinQuoted(args), fsize, as)
	if err := os.WriteFile(shell, []byte(body), 0o700); err != nil {
		return sb.fail(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return sb.abandon(err)
	}
	probeOut, probeErr := exec.CommandContext(ctx, shell, "--probe").CombinedOutput()
	if probeErr != nil {
		if ctx.Err() != nil {
			return sb.abandon(ctx.Err())
		}
		line := firstLine(string(probeOut))
		var exitErr *exec.ExitError
		if errors.As(probeErr, &exitErr) && exitErr.ExitCode() == 2 && line != "" {
			return sb.refuse("toolchain cannot build the reviewed head: " + line)
		}
		if line == "" {
			if errors.As(probeErr, &exitErr) && exitErr.ExitCode() == 124 {
				return sb.refuse("probe timed out")
			}
			return sb.refuse("probe produced no output")
		}
		return sb.refuse(line)
	}
	sb.tree = tree
	sb.shell = shell
	sb.active = true
	note := ""
	if entries, err := os.ReadDir(toolchain.modcache); err != nil || len(entries) == 0 {
		note = " (module cache not found; builds that need downloaded modules will fail)"
	}
	sb.notice = fmt.Sprintf("unreal-review: verifier sandbox active (offline, %d s per command, scratch %s)%s.", int(timeout/time.Second), tree, note)
	sb.announce()
	return sb, nil
}
func (s *verifierSandbox) fail(ctx context.Context, err error) (*verifierSandbox, error) {
	if ctx.Err() != nil {
		return s.abandon(ctx.Err())
	}
	return s.refuse(err.Error())
}
func (s *verifierSandbox) refuse(reason string) (*verifierSandbox, error) {
	reason = firstLine(reason)
	if reason == "" {
		reason = "unknown"
	}
	return s.fallback("verifier sandbox unavailable (" + reason + ")")
}
func (s *verifierSandbox) fallback(prefix string) (*verifierSandbox, error) {
	s.active, s.tree, s.shell = false, "", ""
	suffix := sandboxBashOff
	if shell, err := installScrubShell(s.root); err == nil {
		s.shell, suffix = shell, sandboxScrubbed
	} else {
		s.disableBash = true
	}
	s.notice = "unreal-review: " + prefix + "; " + suffix
	s.announce()
	return s, nil
}
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
func sandboxBins() ([]string, error) {
	bin := make([]string, 6)
	for i, name := range []string{"env", "prlimit", "timeout", "bwrap", "wc", "du"} {
		path, err := exec.LookPath(name)
		if err != nil {
			return nil, fmt.Errorf("%s not found", name)
		}
		bin[i] = path
	}
	for _, name := range []string{"flock", "date"} {
		if _, err := exec.LookPath(name); err != nil {
			return nil, fmt.Errorf("%s not found", name)
		}
	}
	return bin, nil
}

type goToolchain struct {
	goroot, modcache, gopath string
}

func readGoToolchain(ctx context.Context, workspace string) (goToolchain, error) {
	out, err := commandOutput(ctx, workspace, "go", "env", "GOROOT", "GOMODCACHE", "GOPATH")
	if err != nil {
		return goToolchain{}, err
	}
	lines := strings.Split(out+"\n\n", "\n")
	goroot, err := safeMount("GOROOT", lines[0])
	if err != nil {
		return goToolchain{}, err
	}
	modcache, err := safeMount("GOMODCACHE", lines[1])
	if err != nil {
		return goToolchain{}, err
	}
	info, err := os.Stat(goroot)
	if err != nil || !info.IsDir() {
		return goToolchain{}, fmt.Errorf("GOROOT %s is not a directory", goroot)
	}
	return goToolchain{goroot: goroot, modcache: modcache, gopath: lines[2]}, nil
}
func safeMount(name, path string) (string, error) {
	clean := filepath.Clean(path)
	if clean == "." || clean == "/" || !filepath.IsAbs(clean) {
		return "", fmt.Errorf("refusing to mount %s %q", name, path)
	}
	resolved, err := resolveMount(clean)
	if err != nil || resolved == "/" || !filepath.IsAbs(resolved) {
		shown := path
		if err == nil {
			shown = resolved
		}
		return "", fmt.Errorf("refusing to mount %s %q", name, shown)
	}
	return resolved, nil
}
func resolveMount(path string) (string, error) {
	var missing []string
	cur := path
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved), nil
		}
		if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return "", err
		}
		missing = append(missing, filepath.Base(cur))
		cur = parent
	}
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
func gitReadonlyBinds(ctx context.Context, workspace, tree string) ([]string, error) {
	gitDir := filepath.Join(tree, ".git")
	for _, name := range []string{"objects", "refs"} {
		if err := os.MkdirAll(filepath.Join(gitDir, name), 0o700); err != nil {
			return nil, err
		}
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config"), nil, 0o600); err != nil {
		return nil, err
	}
	var args []string
	for _, name := range []string{"objects", "refs", "HEAD", "shallow"} {
		src, err := absoluteGitPath(ctx, workspace, name)
		if err != nil {
			return nil, err
		}
		info, statErr := os.Stat(src)
		if name == "shallow" && (statErr != nil || info.IsDir()) {
			continue
		}
		dest := filepath.Join(gitDir, name)
		if statErr == nil && !info.IsDir() {
			if err := os.WriteFile(dest, nil, 0o600); err != nil {
				return nil, err
			}
		}
		args = append(args, "--ro-bind", src, dest)
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
func sandboxMounts(root, tree string, tc goToolchain, gitBinds []string, tmpfs int64) ([]string, string, error) {
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
		if err == nil && info.IsDir() {
			args = append(args, "--ro-bind", path, path)
		}
	}
	args = append(args, "--ro-bind", tc.goroot, tc.goroot, "--ro-bind", modSrc, mod, "--bind", tree, tree)
	args = append(args, gitBinds...)
	args = append(args, "--bind", gopath, gopath, "--bind", cache, cache)
	size := strconv.FormatInt(tmpfs, 10)
	// HOME is a fresh empty tmpfs, then read-only, so an earlier command cannot leave .gitconfig or an rg config.
	args = append(args, "--size", size, "--tmpfs", home, "--remount-ro", home, "--size", size, "--tmpfs", tmp, "--chdir", tree, "--remount-ro", "/")
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
func sandboxScript(root string, timeoutSec int, limitKB int64, bin []string, envPairs, bwrapArgs string, fsize, as int64) string {
	run := fmt.Sprintf("%s -i %s %s --as=%d --fsize=%d %s -k 2 \"$timeout_secs\" %s %s /bin/sh -f -c \"$1\"",
		shQuote(bin[0]), envPairs, shQuote(bin[1]), as, fsize, shQuote(bin[2]), shQuote(bin[3]), bwrapArgs)
	probe := fmt.Sprintf("%s -i %s --as=%d --fsize=%d %s -k 2 \"$timeout_secs\" %s %s /bin/sh -f -c 'unset PWD; test \"$(%s -l </proc/net/dev)\" -eq 3 && test -z \"$(%s)\"'",
		shQuote(bin[0]), shQuote(bin[1]), as, fsize, shQuote(bin[2]), shQuote(bin[3]), bwrapArgs, bin[4], bin[0])
	return fmt.Sprintf("#!/bin/sh\nset -eu\nroot=%s\ntimeout_secs=%d\ncap_kb=%d\nrun() {\n  %s\n}\nif [ \"${1:-}\" = \"--probe\" ]; then\n  %s || exit 1\n  run \"go list -m\" || exit 2\n  exit 0\nfi\nif [ \"${1:-}\" != \"-c\" ]; then\n  echo \"unreal-review: verifier shell expects -c\" >&2\n  exit 2\nfi\n", shQuote(root), timeoutSec, limitKB, run, probe) +
		"used=0\nwhile IFS= read -r line; do\n  kb=${line%%[!0-9]*}\n  [ -n \"$kb\" ] || continue\n  used=$((used + kb))\ndone <<EOF\n$(" + shQuote(bin[5]) + " -sk \"$root/tree\" \"$root/gocache\" \"$root/gopath\")\nEOF\nif [ \"$used\" -ge \"$cap_kb\" ]; then\n  echo \"unreal-review: verifier scratch exceeds ${cap_kb} KiB; refusing further commands\" >&2\n  exit 1\nfi\nrun \"${2-}\"\n"
}
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
	if strings.TrimSpace(lang) == "" {
		lang = "C.UTF-8"
	}
	body := fmt.Sprintf("#!/bin/sh\nset -eu\nif [ \"${1:-}\" != \"-c\" ]; then\n  echo \"unreal-review: verifier shell expects -c\" >&2\n  exit 2\nfi\nexec %s -i PATH=%s HOME=%s LANG=%s /bin/sh -f -c \"${2-}\"\n",
		shQuote(envBin), shQuote(path), shQuote(home), shQuote(lang))
	shell := filepath.Join(root, "shell")
	if err := os.WriteFile(shell, []byte(body), 0o700); err != nil {
		return "", err
	}
	return shell, nil
}
func commandOutput(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// GOTELEMETRY=off still writes $HOME/.config/go/telemetry.
	cmd.Env = append(os.Environ(),
		"GOTELEMETRY=off",
		"XDG_CONFIG_HOME="+filepath.Join(os.TempDir(), "unreal-review-go-config"),
	)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := firstLine(stderr.String()); msg != "" {
			err = errors.New(msg)
		}
		return "", err
	}
	return strings.TrimRight(string(out), "\r\n"), nil
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
	chmodDirs(root)
	_ = os.RemoveAll(root)
}
func chmodDirs(path string) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return
	}
	_ = os.Chmod(path, 0o700)
	entries, err := os.ReadDir(path)
	if err != nil {
		return
	}
	for _, entry := range entries {
		chmodDirs(filepath.Join(path, entry.Name()))
	}
}
func sweepStaleSandboxes(root string) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !entry.IsDir() || time.Since(info.ModTime()) <= sandboxStaleAge {
			continue
		}
		removeSandboxTree(filepath.Join(root, entry.Name()))
	}
}
func execVerifierAddendum(active bool, perCommand time.Duration) string {
	if !active {
		return ""
	}
	if perCommand <= 0 {
		perCommand = sandboxCommandTimeout
	}
	seconds := int(perCommand / time.Second)
	minutes := int(sandboxTaskWall / time.Minute)
	return fmt.Sprintf("\nVerifier sandbox: the workspace is a scratch copy of the reviewed commit and code may be run offline here (go build, go vet, go test of the changed packages, or a repro written in this scratch copy). Each command is killed after %d s; the network is off. Reads are free; anything that runs code counts against the %d-minute budget. After that budget, decide from the evidence you have and do not run more code. Run code only for a low-confidence candidate: severity error whose cited lines do not include a changed line of the diff. Do not run code for a warning, a note, or a diff-visible candidate. This severity rule applies only to claims about runtime behavior. A finding that claims runtime behavior and is demonstrated by a command you ran in this session keeps its severity; cite that command and a short output excerpt in the body. A runtime-behavior claim you could not demonstrate is re-emitted with severity note, not dropped. A finding settled by reading the code alone (exact quoted lines at the cited range, such as a compile error or an always-false condition) may keep its severity. A wrong or missing argument, option, or condition visible in the diff is settled by reading the code and keeps its severity. Re-emit each candidate with the same id, path, lines, and anchor; only severity and body may change.", seconds, minutes)
}

const readOnlyBashTool = "Read-only shell command. Pass paths and patterns as bare words, or use rg -F. Quotes, backslashes, ~, $, |, ;, &, <, >, backticks, newlines and parentheses are refused. Globs are not expanded."

const readOnlyVerifierAddendum = "\nVerifier sandbox: this batch is read-only. Every candidate is high-confidence (severity warning or note) or diff-visible (its cited lines include a changed line of the selected diff: an added line when anchor is new, a removed line when anchor is old). Code execution has no budget. cat, ls, head, nl, wc, tail, rg, and git show, grep, log, and diff still run. Any other command is refused. " + readOnlyBashTool + " Decide from the code you read. A finding settled by exact quoted lines may keep its severity. Re-emit each candidate with the same id, path, lines, and anchor; only severity and body may change."

const scrubReadOnlyAddendum = "\nVerifier sandbox is unavailable, so this batch is read-only and no shell command will run. Decide from the code you read. Re-emit each candidate with the same id, path, lines, and anchor; only severity and body may change."

func verifierSandboxAddendum(active bool, perCommand time.Duration, readOnly bool) string {
	if readOnly && !active {
		return scrubReadOnlyAddendum
	}
	if readOnly {
		return readOnlyVerifierAddendum
	}
	return execVerifierAddendum(active, perCommand)
}
func installCommandBudget(dir, inner string, wall time.Duration, freeReads bool) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	secs := int(wall / time.Second)
	if wall > 0 && secs < 1 {
		secs = 1
	}
	if secs < 0 {
		secs = 0
	}
	free := 0
	if freeReads {
		free = 1
	}
	state := filepath.Join(dir, "state")
	body := fmt.Sprintf(`#!/bin/sh
set -eu
if [ "${1:-}" != "-c" ]; then
  echo "unreal-review: verifier shell expects -c" >&2
  exit 2
fi
state=%s
limit=%d
free=%d
inner=%s
msg=%s
lock_open() { exec 9>"$state.lock"; flock 9; }
lock_close() { exec 9>&-; }
load_spent() {
  SPENT=0
  CORRUPT=0
  if [ ! -f "$state" ]; then
    return 0
  fi
  IFS= read -r line < "$state" || line=x
  case $line in
    ''|*[!0-9]*) CORRUPT=1 ;;
    *) SPENT=$line ;;
  esac
}
git_flag_ok() {
  sub=$1
  arg=$2
  case $arg in
    -n*)
      case $sub in log|grep) ;; *) return 1 ;; esac
      rest=${arg#-n}
      case $rest in *[!0-9]*) return 1 ;; esac
      return 0 ;;
    --max-count)
      case $sub in show|log) return 0 ;; esac
      return 1 ;;
    --max-count=*)
      case $sub in show|log) ;; *) return 1 ;; esac
      rest=${arg#--max-count=}
      case $rest in ''|*[!0-9]*) return 1 ;; esac
      return 0 ;;
    -U*)
      case $sub in show|log|diff) ;; *) return 1 ;; esac
      rest=${arg#-U}
      case $rest in ''|*[!0-9]*) return 1 ;; esac
      return 0 ;;
    --unified)
      case $sub in show|log|diff) return 0 ;; esac
      return 1 ;;
    --unified=*)
      case $sub in show|log|diff) ;; *) return 1 ;; esac
      rest=${arg#--unified=}
      case $rest in ''|*[!0-9]*) return 1 ;; esac
      return 0 ;;
  esac
  case $sub in
    show)
      case $arg in
        --|--stat|--name-only|--name-status|--patch|-p|-1|--no-color|--color=never|--format|--format=*|--pretty|--pretty=*) return 0 ;;
      esac ;;
    log)
      case $arg in
        --|--oneline|--stat|--name-only|--name-status|--patch|-p|-1|--no-color|--color=never|--format|--format=*|--pretty|--pretty=*) return 0 ;;
      esac ;;
    diff)
      case $arg in
        --|--stat|--name-only|--name-status|--patch|-p|--no-color|--color=never) return 0 ;;
      esac ;;
    grep)
      case $arg in
        --|-i|--ignore-case|-w|--word-regexp|-F|--fixed-strings|-e|--regexp|--regexp=*|--no-color|--color=never) return 0 ;;
      esac ;;
  esac
  return 1
}
rg_blocked() {
  arg=$1
  case $arg in
    --) return 1 ;;
    --*) ;;
    -*z*) return 0 ;;
    *) return 1 ;;
  esac
  name=$arg
  case $name in
    *=*) name=${name%%%%=*} ;;
  esac
  for opt in --pre --pre-glob --search-zip; do
    case $opt in
      "$name"*) return 0 ;;
    esac
  done
  return 1
}
charges() {
  cmd=$1
  if [ "$free" -eq 0 ]; then
    return 0
  fi
  case $cmd in
    *'$'*|*'|'*|*';'*|*'&'*|*'>'*|*'<'*|*'('*|*')'*|*'
'*) return 0 ;;
  esac
  tick=$(printf '\140')
  sq=$(printf '\047')
  dq=$(printf '\042')
  bs=$(printf '\134')
  # Quotes, backslashes, and tilde are interpreted by the inner shell. Globs are not: it runs under set -f.
  case $cmd in
    *$tick*|*"$sq"*|*"$dq"*|*"$bs"*|*'~'*) return 0 ;;
  esac
  set -f
  set -- $cmd
  set +f
  [ $# -gt 0 ] || return 0
  case $1 in
    *=*|*/*) return 0 ;;
  esac
  case $1 in
    cat|ls|head|nl|wc) return 1 ;;
    tail)
      for arg do
        case $arg in
          -f|--follow|--follow=*) return 0 ;;
        esac
      done
      return 1 ;;
    rg)
      shift
      for arg do
        rg_blocked "$arg" && return 0
      done
      return 1 ;;
    git)
      case ${2-} in
        show|grep|log|diff) ;;
        *) return 0 ;;
      esac
      sub=$2
      shift 2
      for arg do
        case $arg in
          -*) git_flag_ok "$sub" "$arg" || return 0 ;;
        esac
      done
      return 1 ;;
  esac
  return 0
}
run=$2
if charges "$run"; then
  lock_open
  load_spent
  over=0
  if [ "$CORRUPT" -eq 1 ] || [ "$SPENT" -ge "$limit" ]; then
    over=1
  fi
  lock_close
  if [ "$over" -eq 1 ]; then
    printf '%%s\n' "$msg"
    exit 0
  fi
fi
start=$(date +%%s)
set +e
"$inner" -c "set -f; $run" 9>&-
code=$?
set -e
if charges "$run"; then
  now=$(date +%%s)
  elapsed=$((now - start))
  if [ "$elapsed" -lt 0 ]; then elapsed=0; fi
  lock_open
  load_spent
  if [ "$CORRUPT" -eq 1 ]; then
    lock_close
    printf '%%s\n' "$msg"
    exit 0
  fi
  printf '%%s\n' "$((SPENT + elapsed))" > "$state"
  lock_close
fi
exit "$code"
`, shQuote(state), secs, free, shQuote(inner), shQuote(sandboxBudgetSpent))
	path := filepath.Join(dir, "shell")
	if err := os.WriteFile(path, []byte(body), 0o700); err != nil {
		return "", err
	}
	return path, nil
}
func applyVerifierSandbox(child *review.AgentRequest, agent review.Agent, sb *verifierSandbox, execWall time.Duration) review.Agent {
	if sb == nil {
		return agent
	}
	c := *sb
	sb = &c
	readOnly := execWall <= 0
	if sb.shell != "" && !sb.disableBash && (sb.active || readOnly) {
		wall := execWall
		if !readOnly {
			wall = sandboxTaskWall
		}
		budget, err := installCommandBudget(filepath.Join(filepath.Dir(sb.shell), "budget", focusedHash(child.ReviewID)), sb.shell, wall, sb.active)
		if err != nil {
			if readOnly {
				sb.disableBash = true
				sb.shell = ""
			} else {
				sb, _ = sb.fallback("verifier sandbox unavailable (" + firstLine(err.Error()) + ")")
			}
		} else {
			sb.shell = budget
			if sb.active {
				child.Workspace = sb.tree
			}
		}
	}
	child.SystemPrompt += verifierSandboxAddendum(sb.active, sb.commandTimeout, readOnly)
	child.ReadOnlyBash = readOnly && sb.active
	h, ok := agent.(Harness)
	if sb.disableBash {
		child.Shell = ""
		if ok {
			h.MaxBashCalls, h.Shell = -1, ""
			return h
		}
		return agent
	}
	if sb.shell == "" {
		return agent
	}
	child.Shell = sb.shell
	if ok {
		h.Shell = sb.shell
		return h
	}
	return agent
}

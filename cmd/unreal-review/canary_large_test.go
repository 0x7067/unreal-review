//go:build canary

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

// Real compiled CLI/Harness coverage tests, with a scripted model stand-in.
// No assertion here establishes real-model recall or verification quality.
type largeAnchor struct {
	path string
	line int
	old  bool
}
type largeFixture struct {
	dir, base, head, diff, sha string
	anchors                    map[string]largeAnchor
	contract                   findings.Finding
}

func largeNoTracingEnv(env []string) []string {
	out := make([]string, 0, len(env)+2)
	for _, entry := range env {
		if !strings.HasPrefix(entry, "GIT_TRACE2_EVENT=") && !strings.HasPrefix(entry, "GIT_AI_SKIP_ALL_HOOKS=") {
			out = append(out, entry)
		}
	}
	return append(out, "GIT_TRACE2_EVENT=0", "GIT_AI_SKIP_ALL_HOOKS=1")
}

func largeCanaryEnv(t *testing.T, url string) []string {
	t.Helper()
	return largeNoTracingEnv(focusedCanaryEnv(t, url))
}

func largeGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = largeNoTracingEnv(os.Environ())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}
func largeWrite(t *testing.T, dir, path, text string) {
	t.Helper()
	full := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
func newLargeFixture(t *testing.T, singleton bool) largeFixture {
	t.Helper()
	// Native Git can be watched by a user-configured Trace2 checkpoint daemon.
	// Disable its event stream before even the shared fixture's first commit,
	// so detached Git AI writes cannot race removal of disposable repositories.
	t.Setenv("GIT_TRACE2_EVENT", "0")
	t.Setenv("GIT_AI_SKIP_ALL_HOOKS", "1")
	dir, _, _, _ := pullHistory(t)
	f := largeFixture{dir: dir, anchors: map[string]largeAnchor{}}
	oldFiles, newFiles := map[string]string{}, map[string]string{}
	if singleton {
		var old, changed strings.Builder
		for i := 0; i < 500; i++ {
			before, after := fmt.Sprintf("LARGE_OLD_SINGLE_%04d", i), fmt.Sprintf("LARGE_NEW_SINGLE_%04d", i)
			fmt.Fprintf(&old, "%s %s\n", before, strings.Repeat("a", 160))
			fmt.Fprintf(&changed, "%s %s\n", after, strings.Repeat("b", 160))
			f.anchors[before] = largeAnchor{"huge.txt", i*8 + 1, true}
			f.anchors[after] = largeAnchor{"huge.txt", i*8 + 1, false}
			for j := 0; j < 7; j++ {
				line := fmt.Sprintf("unchanged context %04d %d\n", i, j)
				old.WriteString(line)
				changed.WriteString(line)
			}
		}
		oldFiles["huge.txt"], newFiles["huge.txt"] = old.String(), changed.String()
	} else {
		oldFiles["go.mod"], newFiles["go.mod"] = "module fixture\n\ngo 1.27\n", "module fixture\n\ngo 1.27\n"
		for i := 0; i < 10; i++ {
			path := fmt.Sprintf("pkg%02d/code.go", i)
			prefix := fmt.Sprintf("package pkg%02d\n\n", i)
			before, after := prefix, prefix
			if i == 0 {
				before += "func Contract() string { return \"old\" } // LARGE_OLD_PRODUCER\n"
				after += "func Contract() int { return 0 } // LARGE_NEW_PRODUCER\n"
				f.anchors["LARGE_OLD_PRODUCER"] = largeAnchor{path, 3, true}
				f.anchors["LARGE_NEW_PRODUCER"] = largeAnchor{path, 3, false}
			} else if i == 9 {
				before += "import \"fixture/pkg00\"\n\nfunc Consumer() int { return len(pkg00.Contract()) } // LARGE_OLD_CONSUMER\n"
				after += "import \"fixture/pkg00\"\n\nfunc Consumer() int { return len(pkg00.Contract()) + 1 } // LARGE_NEW_CONSUMER\n"
				f.anchors["LARGE_OLD_CONSUMER"] = largeAnchor{path, 5, true}
				f.anchors["LARGE_NEW_CONSUMER"] = largeAnchor{path, 5, false}
			}
			// The import-linked pair exceeds one prompt together, so its caller
			// and callee necessarily occupy different local scopes.
			lines := 25
			if i == 0 || i == 9 {
				lines = 400
			}
			for j := 0; j < lines; j++ {
				marker := fmt.Sprintf("LARGE_NEW_P%02d_L%04d", i, j)
				f.anchors[marker] = largeAnchor{path, strings.Count(after, "\n") + 1, false}
				after += "// " + marker + " " + strings.Repeat("x", 220) + "\n"
			}
			oldFiles[path], newFiles[path] = before, after
		}
		f.contract = findings.Finding{Path: "pkg00/code.go", StartLine: 3, EndLine: 3, Anchor: findings.AnchorNew, Severity: findings.SeverityError, Body: "Contract returns an integer but the cross-scope caller still applies len to its result."}
	}
	for path, text := range oldFiles {
		largeWrite(t, dir, path, text)
	}
	largeGit(t, dir, "add", ".")
	largeGit(t, dir, "commit", "-q", "-m", "large baseline")
	f.base = strings.TrimSpace(largeGit(t, dir, "rev-parse", "HEAD"))
	for path, text := range newFiles {
		largeWrite(t, dir, path, text)
	}
	if !singleton {
		diff := largeGit(t, dir, "diff", "--no-color", "--no-ext-diff", f.base, "--")
		padding := 252701 - len(diff) - len("+// LARGE_NEW_PAD \n")
		if padding < 0 {
			t.Fatalf("fixture exceeds target: %d", len(diff))
		}
		path := "pkg09/code.go"
		f.anchors["LARGE_NEW_PAD"] = largeAnchor{path, strings.Count(newFiles[path], "\n") + 1, false}
		largeWrite(t, dir, path, newFiles[path]+"// LARGE_NEW_PAD "+strings.Repeat("p", padding)+"\n")
	}
	largeGit(t, dir, "add", ".")
	largeGit(t, dir, "commit", "-q", "-m", "large change")
	f.head = strings.TrimSpace(largeGit(t, dir, "rev-parse", "HEAD"))
	f.diff = largeGit(t, dir, "diff", "--no-color", "--no-ext-diff", "--merge-base", f.base, f.head, "--")
	if len(f.diff) <= 200000 || (!singleton && len(f.diff) != 252701) {
		t.Fatalf("fixture size %d", len(f.diff))
	}
	h := sha256.Sum256([]byte(f.diff))
	f.sha = hex.EncodeToString(h[:])
	return f
}

type largeSession struct {
	root, stage, kind   string
	requests, responses int
	tool                bool
}
type largeProvider struct {
	mu                              sync.Mutex
	fixture                         largeFixture
	out                             string
	sessions                        map[string]*largeSession
	covered                         map[string]map[string]largeAnchor
	spans                           map[string][]review.DiffSpan
	roots                           []string
	problems                        []string
	candidateStages, contractStages map[string]bool
	extraCandidates                 int
	blockOnce                       bool
	blocked                         chan struct{}
}

func newLargeProvider(f largeFixture, out string) *largeProvider {
	return &largeProvider{fixture: f, out: out, sessions: map[string]*largeSession{}, covered: map[string]map[string]largeAnchor{}, spans: map[string][]review.DiffSpan{}, candidateStages: map[string]bool{}, contractStages: map[string]bool{}, blocked: make(chan struct{})}
}
func largePrompt(raw []byte) (string, error) {
	var req struct {
		Input []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"input"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return "", err
	}
	for _, input := range req.Input {
		if input.Role != "user" {
			continue
		}
		var text string
		if json.Unmarshal(input.Content, &text) != nil {
			var parts []struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(input.Content, &parts); err != nil {
				return "", err
			}
			for _, part := range parts {
				text += part.Text
			}
		}
		if strings.HasPrefix(text, "Planned ") {
			return text, nil
		}
	}
	return "", fmt.Errorf("request has no planned user prompt")
}

var largeHunk = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)
var largeMarker = regexp.MustCompile(`LARGE_(?:OLD|NEW)_[A-Z0-9_]+`)

func (p *largeProvider) observeLocal(root, prompt string) error {
	if p.covered[root] == nil {
		p.covered[root] = map[string]largeAnchor{}
	}
	path, oldLine, newLine := "", 0, 0
	inHunk, gotSpans := false, false
	for _, line := range strings.Split(prompt, "\n") {
		if strings.HasPrefix(line, "primary_owned_spans:") {
			var spans []review.DiffSpan
			if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "primary_owned_spans:"))), &spans); err != nil {
				return err
			}
			p.spans[root] = append(p.spans[root], spans...)
			gotSpans = true
		}
		if strings.HasPrefix(line, "diff --git ") {
			inHunk = false
		}
		if strings.HasPrefix(line, "+++ b/") {
			path = strings.TrimPrefix(line, "+++ b/")
		}
		if m := largeHunk.FindStringSubmatch(line); m != nil {
			oldLine, _ = strconv.Atoi(m[1])
			newLine, _ = strconv.Atoi(m[2])
			inHunk = true
			continue
		}
		if !inHunk || len(line) == 0 {
			continue
		}
		if marker := largeMarker.FindString(line); marker != "" {
			if want, ok := p.fixture.anchors[marker]; ok {
				got := largeAnchor{path, newLine, line[0] == '-'}
				if got.old {
					got.line = oldLine
				}
				if got != want {
					return fmt.Errorf("anchor %s=%+v want %+v", marker, got, want)
				}
				p.covered[root][marker] = got
			}
		}
		switch line[0] {
		case '-':
			oldLine++
		case '+':
			newLine++
		case ' ':
			oldLine++
			newLine++
		default:
			inHunk = false
		}
	}
	if !gotSpans {
		return fmt.Errorf("local task has no ownership metadata")
	}
	return nil
}
func (p *largeProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, 4<<20))
	prompt, promptErr := largePrompt(raw)
	p.mu.Lock()
	fail := func(msg string) {
		p.problems = append(p.problems, msg)
		p.mu.Unlock()
		http.Error(w, msg, http.StatusBadRequest)
	}
	if err != nil || promptErr != nil {
		fail(fmt.Sprintf("request: %v %v", err, promptErr))
		return
	}
	if r.Method != http.MethodPost || r.URL.Path != "/responses" || r.Header.Get("Authorization") != "Bearer dummy" {
		fail("unexpected request/credentials")
		return
	}
	if len(prompt) > review.MaxPlanPromptBytes {
		fail(fmt.Sprintf("unbounded prompt: %d", len(prompt)))
		return
	}
	header := strings.SplitN(prompt, "\n", 3)
	stage, kind := header[0], "verification"
	if strings.HasPrefix(stage, "Planned discovery task: ") {
		if len(header) < 3 {
			fail("missing kind")
			return
		}
		kind = strings.TrimPrefix(header[1], "Task kind: ")
		if kind != "local" && kind != "boundary" {
			fail("invalid kind")
			return
		}
		if !strings.Contains(prompt, p.fixture.sha) || !strings.Contains(prompt, "full_diff_bytes: "+strconv.Itoa(len(p.fixture.diff))) {
			fail("task lost full source")
			return
		}
	} else if !strings.HasPrefix(stage, "Planned verification batch: ") && !strings.HasPrefix(stage, "Planned consolidation batch: ") {
		fail("unexpected stage")
		return
	}
	public, err := findings.ReadFile(p.out)
	if err != nil || public.Run == nil || public.Run.Status != findings.StatusRunning {
		fail(fmt.Sprintf("missing running root: %v", err))
		return
	}
	if public.Run.Source.BaseSHA != p.fixture.base || public.Run.Source.HeadSHA != p.fixture.head || public.Run.Source.DiffSHA != p.fixture.sha {
		fail("root source is not full selected diff")
		return
	}
	if len(public.Findings) != 0 || public.Summary != "" {
		fail("partial candidates/summary published")
		return
	}
	root := public.Run.ID
	if len(p.roots) == 0 || p.roots[len(p.roots)-1] != root {
		p.roots = append(p.roots, root)
	}
	session := r.Header.Get("x-session-id")
	if session == "" {
		fail("missing session header")
		return
	}
	s := p.sessions[session]
	if s == nil {
		s = &largeSession{root: root, stage: stage, kind: kind}
		p.sessions[session] = s
		if kind == "local" {
			if err := p.observeLocal(root, prompt); err != nil {
				fail(err.Error())
				return
			}
		}
	} else if s.root != root || s.stage != stage {
		fail("session migrated across root/stage")
		return
	}
	s.requests++
	if p.blockOnce && kind == "boundary" {
		p.blockOnce = false
		close(p.blocked)
		p.mu.Unlock()
		<-r.Context().Done()
		return
	}
	var output []map[string]any
	toolResults := strings.Contains(string(raw), "function_call_output")
	var items []findings.Finding
	if kind == "local" && !p.candidateStages[root] && p.extraCandidates > 0 && !toolResults {
		p.candidateStages[root] = true
		for i := 0; i < p.extraCandidates; i++ {
			items = append(items, findings.Finding{Path: "pkg00/code.go", StartLine: 3, EndLine: 3, Anchor: findings.AnchorNew, Severity: findings.SeverityNote, Body: fmt.Sprintf("Unverified private candidate %d. %s", i, strings.Repeat("x", 3000))})
		}
	} else if kind == "boundary" && p.fixture.contract.Path != "" && !p.contractStages[root] && !toolResults && strings.Contains(prompt, "LARGE_NEW_PRODUCER") && strings.Contains(prompt, "LARGE_NEW_CONSUMER") {
		p.contractStages[root] = true
		items = []findings.Finding{p.fixture.contract} // Actual record tool creates its ID.
	} else if kind == "verification" && !toolResults {
		_, data, found := strings.Cut(prompt, "\nCandidate JSON:\n")
		if !found || len(data) > 100000 {
			fail("unbounded candidate batch")
			return
		}
		var candidates []findings.Finding
		if err := json.Unmarshal([]byte(data), &candidates); err != nil {
			fail(err.Error())
			return
		}
		for _, item := range candidates {
			if item.ID == "" {
				fail("candidate lacks canonical ID")
				return
			}
			if item.Body == p.fixture.contract.Body && p.fixture.contract.Path != "" {
				if item.ID != findings.Fingerprint(p.fixture.contract) {
					fail("hashed ID changed")
					return
				}
				items = append(items, item)
			}
		}
	}
	id := fmt.Sprintf("large-%s-%d", session, s.requests)
	if len(items) > 0 {
		s.tool = true
		for i, item := range items {
			args, err := json.Marshal(item)
			if err != nil {
				fail(err.Error())
				return
			}
			output = append(output, map[string]any{"id": fmt.Sprintf("%s-fc-%d", id, i), "type": "function_call", "call_id": fmt.Sprintf("%s-call-%d", id, i), "name": "record_finding", "arguments": string(args), "status": "completed"})
		}
	} else {
		text := "No material issues: the assigned scope and premises were checked."
		if s.tool {
			text = "The contract change sends an integer to a caller requiring a string."
		}
		output = []map[string]any{{"id": id + "-msg", "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []map[string]any{{"type": "output_text", "text": text}}}}
	}
	s.responses++
	p.mu.Unlock()
	response := map[string]any{"id": id, "object": "response", "status": "completed", "output": output, "usage": map[string]any{"input_tokens": 12, "output_tokens": 8, "input_tokens_details": map[string]any{"cached_tokens": 5}, "output_tokens_details": map[string]any{"reasoning_tokens": 4}, "cost": 0.001}}
	encoded, err := json.Marshal(map[string]any{"type": "response.completed", "response": response})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}
func (p *largeProvider) check(t *testing.T, root string, complete bool) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.problems) > 0 {
		t.Fatalf("provider: %v", p.problems)
	}
	report, err := findings.ReadFile(p.out)
	if err != nil || report.Run == nil {
		t.Fatalf("report: %v %+v", err, report)
	}
	if report.Run.ID != root || report.Run.Source.DiffSHA != p.fixture.sha {
		t.Fatal("root identity/source changed")
	}
	responses, locals, boundaries, verification := 0, 0, 0, 0
	for _, s := range p.sessions {
		if s.root == root {
			responses += s.responses
			switch s.kind {
			case "local":
				locals++
			case "boundary":
				boundaries++
			case "verification":
				verification++
			}
		}
	}
	c := report.Run.Cost
	if c.Requests != responses || c.InputTokens != int64(responses*12) || c.OutputTokens != int64(responses*8) || c.ReasoningTokens != int64(responses*4) || c.CachedInputTokens != int64(responses*5) || math.Abs(c.AmountUSD-float64(responses)*0.001) > 1e-9 {
		t.Fatalf("cost not exactly returned responses %d: %+v", responses, c)
	}
	if !complete {
		if report.Complete() || len(report.Findings) != 0 || report.Summary != "" {
			t.Fatal("interrupted plan became public/complete")
		}
		return
	}
	minimumLocals := 1
	if len(p.fixture.diff) > 200000 {
		minimumLocals = 2
	}
	if report.Run.Status != findings.StatusComplete || locals < minimumLocals || boundaries == 0 {
		t.Fatalf("incomplete plan status=%s local=%d boundary=%d", report.Run.Status, locals, boundaries)
	}
	if len(p.covered[root]) != len(p.fixture.anchors) {
		t.Fatalf("local anchors covered %d/%d", len(p.covered[root]), len(p.fixture.anchors))
	}
	spans := append([]review.DiffSpan(nil), p.spans[root]...)
	sort.Slice(spans, func(i, j int) bool { return spans[i].Start < spans[j].Start })
	end := 0
	for _, span := range spans {
		if span.Start != end || span.End <= span.Start {
			t.Fatalf("lost/overlapping span after %d: %+v", end, span)
		}
		end = span.End
	}
	if end != len(p.fixture.diff) {
		t.Fatalf("raw coverage=%d want %d", end, len(p.fixture.diff))
	}
	if p.fixture.contract.Path != "" {
		if !p.contractStages[root] || len(report.Findings) != 1 || report.Findings[0].ID != findings.Fingerprint(p.fixture.contract) || report.Findings[0].Body != p.fixture.contract.Body {
			t.Fatalf("boundary-only hashed finding lost: %+v", report.Findings)
		}
		if p.extraCandidates > 0 && verification < 2 {
			t.Fatalf("did not exercise bounded candidate batches: %d", verification)
		}
	} else if len(report.Findings) != 0 {
		t.Fatal("clean singleton published findings")
	}
	if _, err := findings.CheckSummary(report.Summary, len(report.Findings)); err != nil {
		t.Fatal(err)
	}
}
func largeArgs(f largeFixture, out string) []string {
	return []string{"run", "--model", "canary-model", "--timeout", "45s", "--workspace", f.dir, "--from", f.base, "--to", f.head, "--out", out}
}
func TestCanaryLargeMultifileAndSingleton(t *testing.T) {
	bin := canaryBinary(t)
	for _, singleton := range []bool{false, true} {
		name := "multifile-252701"
		if singleton {
			name = "singleton-all-hunks"
		}
		t.Run(name, func(t *testing.T) {
			f := newLargeFixture(t, singleton)
			out := filepath.Join(t.TempDir(), "findings.jsonl")
			p := newLargeProvider(f, out)
			if !singleton {
				p.extraCandidates = 36
			}
			server := httptest.NewServer(p)
			defer server.Close()
			env := largeCanaryEnv(t, server.URL)
			args := largeArgs(f, out)
			if singleton {
				args = append(args, "--decompose")
			}
			_, msg, code := runCLI(t, bin, env, args...)
			if code != 0 {
				t.Fatalf("large exit=%d stderr=%s", code, msg)
			}
			report, err := findings.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			root := report.Run.ID
			p.check(t, root, true)
			before, err := os.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			_, msg, code = runCLI(t, bin, env, args...)
			if code != 1 || !strings.Contains(msg, "complete review") {
				t.Fatalf("complete refusal exit=%d stderr=%s", code, msg)
			}
			after, err := os.ReadFile(out)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("refusal changed checkpoint")
			}
			_, msg, code = runCLI(t, bin, env, append(args, "--fresh")...)
			if code != 0 {
				t.Fatalf("fresh exit=%d stderr=%s", code, msg)
			}
			fresh, err := findings.ReadFile(out)
			if err != nil {
				t.Fatal(err)
			}
			if fresh.Run.ID == root {
				t.Fatal("fresh reused root")
			}
			p.check(t, fresh.Run.ID, true)
		})
	}
}
func largeManifest(t *testing.T, env []string) string {
	t.Helper()
	home := ""
	for _, entry := range env {
		if strings.HasPrefix(entry, "HOME=") {
			home = strings.TrimPrefix(entry, "HOME=")
		}
	}
	paths, err := filepath.Glob(filepath.Join(home, ".local", "state", "unreal-agent", "sessions", "planned", "*", "manifest.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("manifest paths=%v err=%v", paths, err)
	}
	return paths[0]
}
func waitLargeCompleted(t *testing.T, env []string) map[string]string {
	t.Helper()
	deadline := time.After(20 * time.Second)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("tasks did not settle before interrupt")
		case <-tick.C:
			raw, err := os.ReadFile(largeManifest(t, env))
			if err != nil {
				t.Fatal(err)
			}
			var m struct {
				Next     map[string]int    `json:"next"`
				Complete map[string]string `json:"complete"`
			}
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			if len(m.Next) > 1 && len(m.Complete) == len(m.Next)-1 {
				return m.Complete
			}
		}
	}
}
func TestCanaryLargeInterruptResumeAndRefusals(t *testing.T) {
	bin := canaryBinary(t)
	f := newLargeFixture(t, false)
	out := filepath.Join(t.TempDir(), "findings.jsonl")
	p := newLargeProvider(f, out)
	p.blockOnce = true
	server := httptest.NewServer(p)
	defer server.Close()
	env := largeCanaryEnv(t, server.URL)
	args := largeArgs(f, out)
	ctx, cancel := context.WithTimeout(t.Context(), 35*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	select {
	case <-p.blocked:
	case err := <-done:
		t.Fatalf("CLI exited before interrupt: %v %s", err, stderr.String())
	case <-ctx.Done():
		t.Fatal("no interrupt point")
	}
	completed := waitLargeCompleted(t, env)
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("interrupted CLI succeeded")
		}
	case <-ctx.Done():
		t.Fatal("interrupted CLI did not stop")
	}
	paused, err := findings.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if paused.Run.Status != findings.StatusRunning {
		t.Fatalf("paused status=%s stderr=%s", paused.Run.Status, stderr.String())
	}
	root := paused.Run.ID
	p.check(t, root, false)
	// An incomplete root cannot contact GitHub or publish a receipt.
	githubCalls := 0
	var githubMu sync.Mutex
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		githubMu.Lock()
		githubCalls++
		githubMu.Unlock()
		http.Error(w, "unexpected GitHub call", http.StatusUnauthorized)
	}))
	defer gh.Close()
	renderEnv := append(append([]string{}, env...), "GH_TOKEN=dummy", "UNREAL_REVIEW_GITHUB_API="+gh.URL)
	_, msg, code := runCLI(t, bin, renderEnv, "render", "github", "--pr", "canary/fixture#1", out)
	if code != 1 || !strings.Contains(msg, "resume the review before posting") {
		t.Fatalf("paused render exit=%d stderr=%s", code, msg)
	}
	githubMu.Lock()
	calls := githubCalls
	githubMu.Unlock()
	if calls != 0 {
		t.Fatalf("incomplete contacted GitHub %d times", calls)
	}
	manifest := largeManifest(t, env)
	original, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	refuse := func(a []string, want string) {
		t.Helper()
		_, msg, code := runCLI(t, bin, env, a...)
		if code != 1 || !strings.Contains(msg, want) {
			t.Fatalf("refusal exit=%d stderr=%s want=%s", code, msg, want)
		}
		p.check(t, root, false)
	}
	model := append([]string{}, args...)
	for i := range model {
		if model[i] == "canary-model" {
			model[i] = "changed-model"
		}
	}
	refuse(model, "configuration/source/plan mismatch")
	// The saved binding includes the entire immutable plan. Tampering must fail closed.
	var altered map[string]json.RawMessage
	if err := json.Unmarshal(original, &altered); err != nil {
		t.Fatal(err)
	}
	altered["config"] = json.RawMessage(`"changed-plan-binding"`)
	data, err := json.Marshal(altered)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifest, data, 0o600); err != nil {
		t.Fatal(err)
	}
	refuse(args, "configuration/source/plan mismatch")
	if err := os.WriteFile(manifest, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(manifest, manifest+".held"); err != nil {
		t.Fatal(err)
	}
	refuse(args, "missing manifest on resume")
	if err := os.Rename(manifest+".held", manifest); err != nil {
		t.Fatal(err)
	}
	before := map[string]int{}
	p.mu.Lock()
	for _, s := range p.sessions {
		before[s.stage] = s.requests
	}
	p.mu.Unlock()
	_, msg, code = runCLI(t, bin, env, args...)
	if code != 0 {
		t.Fatalf("resume exit=%d stderr=%s", code, msg)
	}
	p.check(t, root, true)
	p.mu.Lock()
	defer p.mu.Unlock()
	for stage := range completed {
		marker := "Planned discovery task: " + strings.TrimPrefix(stage, "task:")
		for _, s := range p.sessions {
			if s.stage == marker && s.requests != before[marker] {
				t.Fatalf("completed task reran: %s", stage)
			}
		}
	}
}

func TestCanaryLargeExplicitSmallDecomposition(t *testing.T) {
	bin := canaryBinary(t)
	f := newLargeFixture(t, false)
	path := "pkg01/code.go"
	f.diff = largeGit(t, f.dir, "diff", "--no-color", "--no-ext-diff", "--merge-base", f.base, f.head, "--", path)
	if len(f.diff) >= 200000 {
		t.Fatalf("explicit decomposition fixture is oversized: %d", len(f.diff))
	}
	h := sha256.Sum256([]byte(f.diff))
	f.sha = hex.EncodeToString(h[:])
	f.contract = findings.Finding{}
	for marker, anchor := range f.anchors {
		if anchor.path != path {
			delete(f.anchors, marker)
		}
	}
	out := filepath.Join(t.TempDir(), "findings.jsonl")
	p := newLargeProvider(f, out)
	server := httptest.NewServer(p)
	defer server.Close()
	args := append(largeArgs(f, out), "--decompose", path)
	_, msg, code := runCLI(t, bin, largeCanaryEnv(t, server.URL), args...)
	if code != 0 {
		t.Fatalf("explicit decomposition exit=%d stderr=%s", code, msg)
	}
	report, err := findings.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	p.check(t, report.Run.ID, true)
}

// Optional local evidence, never a network clone or a required CI dependency.
func TestCanaryLargeOfflineSentryCoverage(t *testing.T) {
	dir := os.Getenv("UNREAL_REVIEW_SENTRY_CANARY_WORKSPACE")
	if dir == "" {
		t.Skip("set UNREAL_REVIEW_SENTRY_CANARY_WORKSPACE to the existing offline sentry-greptile-5 checkout")
	}
	bin := canaryBinary(t)
	f := largeFixture{dir: dir, base: "49a275847631e231672d01b5dcd66558c069d25b", head: "ea188e2d736fd6ed27c1dba8aae63b7268e2a7a9", anchors: map[string]largeAnchor{}}
	f.diff = largeGit(t, dir, "diff", "--no-color", "--no-ext-diff", f.base, f.head, "--")
	if len(f.diff) != 252701 {
		t.Fatalf("offline baseline source changed: %d diff bytes", len(f.diff))
	}
	h := sha256.Sum256([]byte(f.diff))
	f.sha = hex.EncodeToString(h[:])
	// Baseline workspaces may contain only two shallow snapshots. Preserve the
	// checkout and never fetch to manufacture ancestry for an offline test.
	mergeBase := exec.CommandContext(t.Context(), "git", "-C", dir, "merge-base", f.base, f.head)
	if _, err := mergeBase.Output(); err != nil {
		t.Skipf("offline snapshots have no merge-base: full CLI range blocked; raw diff verified bytes=%d sha=%s", len(f.diff), f.sha)
	}
	out := filepath.Join(t.TempDir(), "findings.jsonl")
	p := newLargeProvider(f, out)
	server := httptest.NewServer(p)
	defer server.Close()
	_, msg, code := runCLI(t, bin, largeCanaryEnv(t, server.URL), largeArgs(f, out)...)
	if code != 0 {
		t.Fatalf("offline source coverage exit=%d stderr=%s", code, msg)
	}
	report, err := findings.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	p.check(t, report.Run.ID, true)
	t.Logf("offline source: bytes=%d sha=%s, all primary bytes covered without network or source edits", len(f.diff), f.sha)
}

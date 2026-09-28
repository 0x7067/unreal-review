//go:build canary

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const (
	canaryOwner = "canary"
	canaryRepo  = "fixture"
	emptyDiff   = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	// Nothing listens here. A run --pr that starts the agent dials this origin
	// and cannot reach openrouter.ai. make canary starts the shell stub in a
	// separate process, so this env has to be set on the test's own exec.
	canaryOpenRouterAPI = "http://127.0.0.1:9/api/v1"
)

func TestCanaryCLIEnvPinsOpenRouterLocal(t *testing.T) {
	var got string
	for _, entry := range cliEnv("http://127.0.0.1:1", "canary-token") {
		if strings.HasPrefix(entry, "UNREAL_REVIEW_OPENROUTER_API=") {
			got = strings.TrimPrefix(entry, "UNREAL_REVIEW_OPENROUTER_API=")
		}
	}
	if got != canaryOpenRouterAPI || !strings.HasPrefix(got, "http://127.0.0.1:") || strings.Contains(got, "openrouter.ai") {
		t.Fatalf("UNREAL_REVIEW_OPENROUTER_API=%q", got)
	}
}

func TestCanaryRunPRRequiresToken(t *testing.T) {
	bin := canaryBinary(t)
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	env := cliEnv("", "")
	for i, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			env[i] = "PATH=" + shim + string(os.PathListSeparator) + os.Getenv("PATH")
		}
	}
	out := filepath.Join(t.TempDir(), "findings.jsonl")
	_, stderr, code := runCLI(t, bin, env, "run", "--model", "x", "--pr", canaryOwner+"/"+canaryRepo+"#1", "--out", out, "--timeout", "1s")
	if code != 1 || !strings.Contains(stderr, "set GH_TOKEN to review a pull request") {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
}

func TestCanaryRunPRNarrowsToLatestSuccessfulCheck(t *testing.T) {
	bin := canaryBinary(t)
	dir, base, reviewed, head := pullHistory(t)
	fake := &fakeGH{
		base:    base,
		head:    head,
		commits: []string{reviewed, head},
		checks: map[string]string{
			head:     "failure",
			reviewed: "success",
		},
		comments: []map[string]any{
			{
				"path": "hello.txt",
				"body": "**error**\n\nAlready posted.\n\n<!-- unreal-review finding aaaaaaaaaaaaaaaa -->",
				"line": 1, "side": "RIGHT",
			},
		},
	}
	server := httptest.NewServer(fake)
	defer server.Close()

	out := filepath.Join(t.TempDir(), "findings.jsonl")
	_, stderr, code := runCLI(t, bin, cliEnv(server.URL, "canary-token"),
		"run", "--model", "x", "--timeout", "5s",
		"--workspace", dir,
		"--pr", canaryOwner+"/"+canaryRepo+"#1",
		"--out", out,
	)
	fake.check(t)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if strings.Contains(stderr, "401") {
		t.Fatalf("narrowed range started the agent:\n%s", stderr)
	}
	if !strings.Contains(stderr, "cost: USD 0.000000") {
		t.Fatalf("stderr=%s", stderr)
	}
	rec := readRun(t, out)
	src := rec.source
	if src["base"] != reviewed || src["head"] != head || src["base_sha"] != reviewed || src["head_sha"] != head {
		t.Fatalf("source=%v want base %s head %s", src, reviewed, head)
	}
	if src["diff_sha"] != emptyDiff {
		t.Fatalf("diff_sha=%v want empty; narrowing missed the receipt on %s", src["diff_sha"], reviewed)
	}
	if !strings.Contains(string(mustRead(t, out)), "No material issues: the selected range has no changes.") {
		t.Fatalf("summary missing from %s", out)
	}
	if posts := fake.count(http.MethodPost, "/check-runs"); posts != 0 {
		t.Fatalf("run --pr posted %d check runs", posts)
	}
	uris := fake.uris()
	headURI := "/commits/" + head + "/check-runs"
	reviewedURI := "/commits/" + reviewed + "/check-runs"
	if !strings.Contains(uris, headURI) || !strings.Contains(uris, reviewedURI) {
		t.Fatalf("check-run walk missing head or reviewed commit:\n%s", uris)
	}
	if strings.Index(uris, headURI) > strings.Index(uris, reviewedURI) {
		t.Fatalf("walk should see the failed head before the successful ancestor:\n%s", uris)
	}
	if !strings.Contains(uris, "/pulls/1/comments") {
		t.Fatalf("run --pr did not read posted comments:\n%s", uris)
	}
}

func TestCanaryRenderPostsStatusAndReceipt(t *testing.T) {
	bin := canaryBinary(t)
	const head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fake := newRenderFake(head, nil)
	server := httptest.NewServer(fake)
	defer server.Close()

	findings := writeFindings(t, head, finding{"1111111111111111", "hello.go", 3, "The new line races."})
	_, stderr, code := postFindings(t, bin, server.URL, findings)
	fake.check(t)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "posted 1 inline comment(s)") {
		t.Fatalf("stderr=%s", stderr)
	}
	reviews := fake.bodies(http.MethodPost, "/pulls/1/reviews")
	if len(reviews) != 1 || !strings.Contains(reviews[0], "1111111111111111") || !strings.Contains(reviews[0], `"line":3`) {
		t.Fatalf("review posts=%v", reviews)
	}
	status := fake.bodies(http.MethodPost, "/issues/1/comments")
	if len(status) != 1 || !strings.Contains(status[0], "unreal-review status "+head) {
		t.Fatalf("status posts=%v", status)
	}
	receipts := fake.bodies(http.MethodPost, "/check-runs")
	if len(receipts) != 1 || !strings.Contains(receipts[0], `"conclusion":"success"`) || !strings.Contains(receipts[0], head) {
		t.Fatalf("receipts=%v", receipts)
	}
}

func TestCanaryRenderSuppressesDuplicateAndStillReceipts(t *testing.T) {
	bin := canaryBinary(t)
	const head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	const id = "2222222222222222"
	fake := newRenderFake(head, []map[string]any{
		{
			"path": "hello.go",
			"body": "**error**\n\nAlready posted.\n\n<!-- unreal-review finding " + id + " -->",
			"line": 3, "side": "RIGHT",
		},
	})
	server := httptest.NewServer(fake)
	defer server.Close()

	findings := writeFindings(t, head, finding{id, "hello.go", 3, "Already posted."})
	_, stderr, code := postFindings(t, bin, server.URL, findings)
	fake.check(t)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "posted no review") || !strings.Contains(stderr, "1 already reported") {
		t.Fatalf("stderr=%s", stderr)
	}
	if reviews := fake.bodies(http.MethodPost, "/pulls/1/reviews"); len(reviews) != 0 {
		t.Fatalf("duplicate was posted: %v", reviews)
	}
	status := fake.bodies(http.MethodPost, "/issues/1/comments")
	if len(status) != 1 || !strings.Contains(status[0], "1 already reported") {
		t.Fatalf("status=%v", status)
	}
	// Nothing was dropped, so the receipt is still created.
	if receipts := fake.count(http.MethodPost, "/check-runs"); receipts != 1 {
		t.Fatalf("receipts=%d want 1", receipts)
	}
}

func TestCanaryRenderOutOfPatchFindingPostsOnceAndReceipts(t *testing.T) {
	bin := canaryBinary(t)
	const head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fake := newRenderFake(head, nil)
	server := httptest.NewServer(fake)
	defer server.Close()

	findings := writeFindings(t, head, finding{"3333333333333333", "hello.go", 90, "This line is outside the patch."})
	for run := 1; run <= 2; run++ {
		_, stderr, code := postFindings(t, bin, server.URL, findings)
		fake.check(t)
		if code != 0 {
			t.Fatalf("run %d: exit=%d stderr=%s", run, code, stderr)
		}
		want := "dropped 1"
		if run == 2 {
			want = "1 already reported"
		}
		if !strings.Contains(stderr, want) {
			t.Fatalf("run %d: want %q in stderr=%s", run, want, stderr)
		}
	}
	reviews := fake.bodies(http.MethodPost, "/pulls/1/reviews")
	if len(reviews) != 1 {
		t.Fatalf("two renders of one head posted %d reviews: %v", len(reviews), reviews)
	}
	var posted struct {
		Body string `json:"body"`
	}
	if err := json.Unmarshal([]byte(reviews[0]), &posted); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(posted.Body, "Not in the pull request diff, so not posted inline:\n- **error** `hello.go` L90: This line is outside the patch. <!-- unreal-review finding 3333333333333333 -->") {
		t.Fatalf("dropped finding has no marker in the review body:\n%s", posted.Body)
	}
	receipts := fake.bodies(http.MethodPost, "/check-runs")
	if len(receipts) != 1 || !strings.Contains(receipts[0], head) {
		t.Fatalf("receipts=%v want one on %s", receipts, head)
	}
	if reads := fake.count(http.MethodGet, "/pulls/1/reviews"); reads != 2 {
		t.Fatalf("two renders read the reviews %d times, want once each", reads)
	}
}

func TestCanaryRenderOverCapFindingSkipsReceipt(t *testing.T) {
	bin := canaryBinary(t)
	const head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fake := newRenderFake(head, nil)
	server := httptest.NewServer(fake)
	defer server.Close()

	var items []finding
	for i := range 51 {
		items = append(items, finding{fmt.Sprintf("%016x", i+1), "hello.go", 1 + i%3, fmt.Sprintf("Problem %d.", i)})
	}
	_, stderr, code := postFindings(t, bin, server.URL, writeFindings(t, head, items...))
	fake.check(t)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "posted 50 inline comment(s)") || !strings.Contains(stderr, "dropped 1") {
		t.Fatalf("stderr=%s", stderr)
	}
	if receipts := fake.count(http.MethodPost, "/check-runs"); receipts != 0 {
		t.Fatalf("a finding cut by the inline cap still created %d check runs", receipts)
	}
}

func TestCanaryRenderLGTMPostsOncePerHead(t *testing.T) {
	bin := canaryBinary(t)
	const head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fake := newRenderFake(head, nil)
	server := httptest.NewServer(fake)
	defer server.Close()

	findings := writeFindings(t, head)
	for run := 1; run <= 2; run++ {
		_, stderr, code := postFindings(t, bin, server.URL, findings)
		fake.check(t)
		if code != 0 {
			t.Fatalf("run %d: exit=%d stderr=%s", run, code, stderr)
		}
		want := "posted LGTM"
		if run == 2 {
			want = "LGTM already posted"
		}
		if !strings.Contains(stderr, want) {
			t.Fatalf("run %d: want %q in stderr=%s", run, want, stderr)
		}
	}
	reviews := fake.bodies(http.MethodPost, "/pulls/1/reviews")
	if len(reviews) != 1 || !strings.Contains(reviews[0], `"body":"LGTM - no findings in aaaaaaa..bbbbbbb."`) {
		t.Fatalf("want one LGTM review for the head, got %v", reviews)
	}
}

func TestCanaryRenderSkipsReceiptWhenHeadAlreadyReceipted(t *testing.T) {
	bin := canaryBinary(t)
	const head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fake := newRenderFake(head, nil)
	fake.checks = map[string]string{head: "success"}
	server := httptest.NewServer(fake)
	defer server.Close()

	findings := writeFindings(t, head, finding{"1111111111111111", "hello.go", 3, "The new line races."})
	_, stderr, code := postFindings(t, bin, server.URL, findings)
	fake.check(t)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if !strings.Contains(stderr, "posted 1 inline comment(s)") {
		t.Fatalf("stderr=%s", stderr)
	}
	if receipts := fake.count(http.MethodPost, "/check-runs"); receipts != 0 {
		t.Fatalf("existing receipt was repeated: %d", receipts)
	}
}

func TestCanaryRenderRefusesHeadMismatch(t *testing.T) {
	bin := canaryBinary(t)
	const head = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	fake := newRenderFake(head, nil)
	server := httptest.NewServer(fake)
	defer server.Close()

	other := strings.Repeat("c", 40)
	findings := writeFindings(t, other, finding{"1111111111111111", "hello.go", 3, "Stale."})
	_, stderr, code := postFindings(t, bin, server.URL, findings)
	fake.check(t)
	if code != 1 || !strings.Contains(stderr, other) || !strings.Contains(stderr, head) || !strings.Contains(stderr, "rerun the review before posting") {
		t.Fatalf("exit=%d stderr=%s", code, stderr)
	}
	if reviews := fake.count(http.MethodPost, "/pulls/1/reviews"); reviews != 0 {
		t.Fatalf("mismatch still posted %d reviews", reviews)
	}
}

func postFindings(t *testing.T, bin, api, findings string) (string, string, int) {
	t.Helper()
	return runCLI(t, bin, cliEnv(api, "canary-token"),
		"render", "github", "--pr", canaryOwner+"/"+canaryRepo+"#1", findings)
}

type finding struct {
	id, path string
	line     int
	body     string
}

func writeFindings(t *testing.T, head string, items ...finding) string {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, `{"v":1,"type":"run","id":"00000000-0000-0000-0000-000000000010","created_at":"2026-09-28T00:00:00Z","model":"x","status":"complete","source":{"kind":"git","base":"main","head":"HEAD","base_sha":"%s","head_sha":"%s","diff_sha":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"},"cost":{"amount_usd":0.001,"currency":"USD","input_tokens":1,"output_tokens":1,"requests":1}}`+"\n", strings.Repeat("a", 40), head)
	for _, item := range items {
		fmt.Fprintf(&b, `{"v":1,"type":"finding","id":"%s","path":"%s","start_line":%d,"end_line":%d,"anchor":"new","severity":"error","body":"%s"}`+"\n", item.id, item.path, item.line, item.line, item.body)
	}
	b.WriteString(`{"v":1,"type":"summary","body":"A real defect."}` + "\n")
	path := filepath.Join(t.TempDir(), "findings.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newRenderFake(head string, comments []map[string]any) *fakeGH {
	return &fakeGH{
		base:     strings.Repeat("a", 40),
		head:     head,
		commits:  []string{head},
		comments: comments,
		files: []map[string]any{
			{
				"filename": "hello.go",
				"patch":    "@@ -0,0 +1,3 @@\n+package main\n+\n+func main() {}\n",
			},
		},
	}
}

type ghCall struct {
	method, uri, body, auth string
}

type fakeGH struct {
	mu         sync.Mutex
	calls      []ghCall
	unexpected []string
	base       string
	head       string
	commits    []string
	checks     map[string]string
	comments   []map[string]any
	files      []map[string]any
}

func (f *fakeGH) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		f.note("read body: " + err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if got := r.Header.Get("Authorization"); got != "Bearer canary-token" {
		f.note("auth " + got)
	}
	f.mu.Lock()
	f.calls = append(f.calls, ghCall{method: r.Method, uri: r.URL.RequestURI(), body: string(raw), auth: r.Header.Get("Authorization")})
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	path := r.URL.Path
	prefix := "/repos/" + canaryOwner + "/" + canaryRepo
	switch {
	case r.Method == http.MethodGet && path == prefix+"/pulls/1":
		f.write(w, map[string]any{
			"number": 1,
			"base":   map[string]string{"ref": "main", "sha": f.base},
			"head":   map[string]string{"sha": f.head},
		})
	case r.Method == http.MethodGet && path == prefix+"/issues/1/comments":
		f.write(w, []any{})
	case r.Method == http.MethodGet && path == prefix+"/pulls/1/comments":
		if f.comments == nil {
			f.write(w, []any{})
			return
		}
		f.write(w, f.comments)
	case r.Method == http.MethodGet && path == prefix+"/pulls/1/commits":
		commits := make([]map[string]string, len(f.commits))
		for i, sha := range f.commits {
			commits[i] = map[string]string{"sha": sha}
		}
		f.write(w, commits)
	case r.Method == http.MethodGet && path == prefix+"/pulls/1/files":
		if f.files == nil {
			f.write(w, []any{})
			return
		}
		f.write(w, f.files)
	case r.Method == http.MethodGet && path == prefix+"/pulls/1/reviews":
		reviews := []map[string]string{}
		for _, body := range f.bodies(http.MethodPost, "/pulls/1/reviews") {
			var review struct {
				CommitID string `json:"commit_id"`
				Body     string `json:"body"`
			}
			if err := json.Unmarshal([]byte(body), &review); err != nil {
				f.note("decode posted review: " + err.Error())
			}
			reviews = append(reviews, map[string]string{"commit_id": review.CommitID, "body": review.Body})
		}
		f.write(w, reviews)
	case r.Method == http.MethodGet && strings.HasPrefix(path, prefix+"/commits/") && strings.HasSuffix(path, "/check-runs"):
		sha := strings.TrimSuffix(strings.TrimPrefix(path, prefix+"/commits/"), "/check-runs")
		runs := []map[string]string{}
		f.mu.Lock()
		conclusion, ok := f.checks[sha]
		f.mu.Unlock()
		if ok {
			runs = append(runs, map[string]string{
				"name": "unreal-review", "status": "completed", "conclusion": conclusion,
			})
		}
		f.write(w, map[string]any{"total_count": len(runs), "check_runs": runs})
	case r.Method == http.MethodPost && path == prefix+"/pulls/1/reviews":
		f.write(w, map[string]any{"id": 1})
	case r.Method == http.MethodPost && path == prefix+"/issues/1/comments":
		f.write(w, map[string]any{"id": 2})
	case r.Method == http.MethodPost && path == prefix+"/check-runs":
		var run struct {
			HeadSHA    string `json:"head_sha"`
			Conclusion string `json:"conclusion"`
		}
		if err := json.Unmarshal(raw, &run); err != nil {
			f.note("decode check run: " + err.Error())
		}
		f.mu.Lock()
		if f.checks == nil {
			f.checks = map[string]string{}
		}
		f.checks[run.HeadSHA] = run.Conclusion
		f.mu.Unlock()
		f.write(w, map[string]any{"id": 3})
	default:
		f.note("unexpected " + r.Method + " " + r.URL.RequestURI())
		http.NotFound(w, r)
	}
}

func (f *fakeGH) write(w http.ResponseWriter, value any) {
	if err := json.NewEncoder(w).Encode(value); err != nil {
		f.note("encode: " + err.Error())
	}
}

func (f *fakeGH) note(msg string) {
	f.mu.Lock()
	f.unexpected = append(f.unexpected, msg)
	f.mu.Unlock()
}

func (f *fakeGH) check(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, msg := range f.unexpected {
		t.Error(msg)
	}
}

func (f *fakeGH) snapshot() []ghCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]ghCall, len(f.calls))
	copy(out, f.calls)
	return out
}

func (f *fakeGH) uris() string {
	var b strings.Builder
	for _, call := range f.snapshot() {
		b.WriteString(call.method)
		b.WriteByte(' ')
		b.WriteString(call.uri)
		b.WriteByte('\n')
	}
	return b.String()
}

func (f *fakeGH) count(method, pathPart string) int {
	n := 0
	for _, call := range f.snapshot() {
		path := strings.Split(call.uri, "?")[0]
		if call.method == method && strings.HasSuffix(path, pathPart) {
			n++
		}
	}
	return n
}

func (f *fakeGH) bodies(method, pathPart string) []string {
	var out []string
	for _, call := range f.snapshot() {
		path := strings.Split(call.uri, "?")[0]
		if call.method == method && strings.HasSuffix(path, pathPart) {
			out = append(out, call.body)
		}
	}
	return out
}

type jsonRun struct {
	source map[string]string
}

func readRun(t *testing.T, path string) jsonRun {
	t.Helper()
	raw := mustRead(t, path)
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj struct {
			Type   string            `json:"type"`
			Source map[string]string `json:"source"`
		}
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatal(err)
		}
		if obj.Type == "run" {
			return jsonRun{source: obj.Source}
		}
	}
	t.Fatalf("no run record in %s", path)
	return jsonRun{}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func pullHistory(t *testing.T) (dir, base, reviewed, head string) {
	t.Helper()
	dir = t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "canary@example.com")
	git("config", "user.name", "Canary")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "hello.txt")
	git("commit", "-q", "-m", "base")
	base = revParse(t, dir, "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("reviewed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "hello.txt")
	git("commit", "-q", "-m", "reviewed")
	reviewed = revParse(t, dir, "HEAD")
	git("commit", "-q", "--allow-empty", "-m", "tip")
	head = revParse(t, dir, "HEAD")
	return dir, base, reviewed, head
}

func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", "-C", dir, "rev-parse", rev)
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func canaryBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("UNREAL_REVIEW_BIN")
	if bin == "" {
		t.Fatal("UNREAL_REVIEW_BIN must be bin/unreal-review built from this checkout")
	}
	info, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() || info.Mode()&0o111 == 0 {
		t.Fatalf("%s is not an executable", bin)
	}
	root := repoRoot(t)
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if !strings.HasPrefix(rel, "cmd"+string(os.PathSeparator)+"unreal-review"+string(os.PathSeparator)) && !strings.HasPrefix(rel, "internal"+string(os.PathSeparator)) {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		if st.ModTime().After(info.ModTime()) {
			return fmt.Errorf("%s is newer than %s; rebuild bin/unreal-review from this checkout", rel, bin)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return bin
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func requireLocalOpenRouter(t *testing.T, env []string) {
	t.Helper()
	var got string
	for _, entry := range env {
		if strings.HasPrefix(entry, "UNREAL_REVIEW_OPENROUTER_API=") {
			got = strings.TrimPrefix(entry, "UNREAL_REVIEW_OPENROUTER_API=")
		}
	}
	if !strings.HasPrefix(got, "http://127.0.0.1:") || strings.Contains(got, "openrouter.ai") {
		t.Fatalf("exec env UNREAL_REVIEW_OPENROUTER_API=%q can reach openrouter.ai", got)
	}
}

func cliEnv(api, token string) []string {
	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"OPENROUTER_API_KEY=dummy",
		"UNREAL_REVIEW_OPENROUTER_API=" + canaryOpenRouterAPI,
	}
	if v := os.Getenv("TMPDIR"); v != "" {
		env = append(env, "TMPDIR="+v)
	}
	if api != "" {
		env = append(env, "UNREAL_REVIEW_GITHUB_API="+api)
	}
	if token != "" {
		env = append(env, "GH_TOKEN="+token)
	}
	return env
}

func runCLI(t *testing.T, bin string, env []string, args ...string) (string, string, int) {
	t.Helper()
	requireLocalOpenRouter(t, env)
	cmd := exec.CommandContext(context.Background(), bin, args...)
	cmd.Env = env
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return stdout.String(), stderr.String(), 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return stdout.String(), stderr.String(), exit.ExitCode()
	}
	t.Fatalf("exec: %v\nstderr: %s", err, stderr.String())
	return "", "", -1
}

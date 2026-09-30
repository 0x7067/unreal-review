//go:build canary

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"unreal-review/internal/findings"
)

// This provider is a representative wire-level model stand-in. It exercises
// the release binary and real Harness, not model quality or improved recall.
type focusedCanaryProvider struct {
	mu       sync.Mutex
	out      string
	calls    map[string]int
	stages   map[string]string
	roots    map[string]string
	rootIDs  []string
	problems []string
}

var focusedCanaryLenses = []string{"correctness", "failures", "security-data", "contracts-tests"}

func (p *focusedCanaryProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fail := func(message string) {
		p.problems = append(p.problems, message)
		http.Error(w, message, http.StatusBadRequest)
	}
	if r.Method != http.MethodPost || r.URL.Path != "/responses" {
		fail("unexpected provider method/path")
		return
	}
	if r.Header.Get("Authorization") != "Bearer dummy" {
		fail("provider did not receive isolated dummy credential")
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		fail(err.Error())
		return
	}
	if !json.Valid(body) {
		fail("invalid Responses request JSON")
		return
	}
	session := r.Header.Get("x-session-id")
	if session == "" {
		fail("missing child session cache header")
		return
	}
	stage := ""
	for _, lens := range focusedCanaryLenses {
		if strings.Contains(string(body), "Discovery lens: "+lens+".") {
			stage = lens
		}
	}
	if strings.Contains(string(body), "Verification and semantic deduplication:") {
		stage = "verification"
	}
	if stage == "" {
		fail("missing focused stage prompt")
		return
	}
	if prior, exists := p.stages[session]; exists && prior != stage {
		fail("one child session was reused across stages")
		return
	}
	p.stages[session] = stage
	public, err := findings.ReadFile(p.out)
	if err != nil || public.Run == nil || public.Run.Status != findings.StatusRunning {
		fail(fmt.Sprintf("missing running root checkpoint: %v", err))
		return
	}
	if len(public.Findings) != 0 {
		fail("unverified discovery candidates reached public checkpoint")
		return
	}
	if prior, exists := p.roots[session]; exists && prior != public.Run.ID {
		fail("fresh root reused an earlier child session")
		return
	}
	p.roots[session] = public.Run.ID
	if len(p.rootIDs) == 0 || p.rootIDs[len(p.rootIDs)-1] != public.Run.ID {
		p.rootIDs = append(p.rootIDs, public.Run.ID)
	}
	if stage == "verification" {
		for _, lens := range focusedCanaryLenses {
			ready := false
			for id, name := range p.stages {
				ready = ready || (name == lens && p.roots[id] == public.Run.ID && p.calls[id] == 2)
			}
			if !ready {
				fail("verifier started before discovery summaries completed")
				return
			}
		}
	}
	p.calls[session]++
	n := p.calls[session]
	if n > 2 {
		fail("unexpected extra model request or summary correction")
		return
	}
	id := fmt.Sprintf("resp-%s-%d", session, n)
	var output []map[string]any
	if n == 1 {
		items := []findings.Finding{{ID: "canary-retained", Path: "hello.txt", StartLine: 1, EndLine: 1, Anchor: findings.AnchorNew, Severity: findings.SeverityWarning, Body: "Confirmed fixture issue."}}
		if stage != "verification" {
			items = append(items, findings.Finding{ID: "canary-rejected-" + stage, Path: "hello.txt", StartLine: 1, EndLine: 1, Anchor: findings.AnchorNew, Severity: findings.SeverityNote, Body: "Unsupported fixture hypothesis for " + stage + "."})
		} else {
			for _, lens := range focusedCanaryLenses {
				if !strings.Contains(string(body), "canary-rejected-"+lens) {
					fail("verifier did not receive every discovery candidate")
					return
				}
			}
		}
		for i, item := range items {
			args, marshalErr := json.Marshal(item)
			if marshalErr != nil {
				fail(marshalErr.Error())
				return
			}
			output = append(output, map[string]any{"id": fmt.Sprintf("%s-fc-%d", id, i), "type": "function_call", "call_id": fmt.Sprintf("%s-call-%d", id, i), "name": "record_finding", "arguments": string(args), "status": "completed"})
		}
	} else {
		if !strings.Contains(string(body), "function_call_output") {
			fail("Harness did not return record_finding tool results")
			return
		}
		output = []map[string]any{{"id": id + "-msg", "type": "message", "role": "assistant", "status": "completed", "phase": "final_answer", "content": []map[string]any{{"type": "output_text", "text": "The confirmed fixture issue weakens the changed behavior.", "annotations": []any{}, "logprobs": []any{}}}}}
	}
	response := map[string]any{"id": id, "object": "response", "status": "completed", "output": output, "usage": map[string]any{"input_tokens": 12, "output_tokens": 8, "input_tokens_details": map[string]any{"cached_tokens": 5}, "output_tokens_details": map[string]any{"reasoning_tokens": 4}, "cost": 0.01}}
	encoded, err := json.Marshal(map[string]any{"type": "response.completed", "response": response})
	if err != nil {
		fail(err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: %s\n\n", encoded)
}

func focusedCanaryEnv(t *testing.T, url string) []string {
	t.Helper()
	env := cliEnv("", "")
	for i, entry := range env {
		switch {
		case strings.HasPrefix(entry, "HOME="):
			env[i] = "HOME=" + t.TempDir()
		case strings.HasPrefix(entry, "UNREAL_REVIEW_OPENROUTER_API="):
			env[i] = "UNREAL_REVIEW_OPENROUTER_API=" + url
		}
	}
	return env
}

func TestCanaryFocusedHarnessGraph(t *testing.T) {
	bin := canaryBinary(t)
	dir, base, _, head := pullHistory(t)
	out := filepath.Join(t.TempDir(), "findings.jsonl")
	provider := &focusedCanaryProvider{out: out, calls: make(map[string]int), stages: make(map[string]string), roots: make(map[string]string)}
	server := httptest.NewServer(provider)
	defer server.Close()
	env := focusedCanaryEnv(t, server.URL)
	args := []string{"run", "--strategy", "focused", "--model", "canary-model", "--timeout", "30s", "--workspace", dir, "--from", base, "--to", head, "--out", out}
	check := func(graphs int) string {
		t.Helper()
		report, err := findings.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if report.Run == nil || report.Run.Status != findings.StatusComplete || !report.Complete() {
			t.Fatalf("incomplete report: %+v", report)
		}
		if len(report.Findings) != 1 || report.Findings[0].ID != "canary-retained" || report.Findings[0].Body != "Confirmed fixture issue." {
			t.Fatalf("unverified or lost findings: %+v", report.Findings)
		}
		if _, err := findings.CheckSummary(report.Summary, 1); err != nil {
			t.Fatal(err)
		}
		cost := report.Run.Cost
		if cost.Requests != 10 || cost.InputTokens != 120 || cost.OutputTokens != 80 || cost.ReasoningTokens != 40 || cost.CachedInputTokens != 50 || math.Abs(cost.AmountUSD-0.10) > 1e-9 {
			t.Fatalf("lost or duplicated stage cost: %+v", cost)
		}
		if report.Run.Source.BaseSHA != base || report.Run.Source.HeadSHA != head || report.Run.Source.DiffSHA == "" {
			t.Fatalf("root source binding: %+v", report.Run.Source)
		}
		provider.mu.Lock()
		defer provider.mu.Unlock()
		if len(provider.problems) != 0 {
			t.Fatalf("provider: %v", provider.problems)
		}
		if len(provider.calls) != graphs*5 || len(provider.rootIDs) != graphs || provider.rootIDs[graphs-1] != report.Run.ID {
			t.Fatalf("graph/root identity mismatch: sessions=%v roots=%v root=%s", provider.stages, provider.rootIDs, report.Run.ID)
		}
		for _, root := range provider.rootIDs {
			stageCounts := make(map[string]int)
			for session, stage := range provider.stages {
				if provider.roots[session] == root {
					stageCounts[stage]++
				}
			}
			for _, stage := range append(append([]string{}, focusedCanaryLenses...), "verification") {
				if stageCounts[stage] != 1 {
					t.Fatalf("root %s has unexpected stage sessions: %v", root, stageCounts)
				}
			}
		}
		for session, calls := range provider.calls {
			if calls != 2 {
				t.Fatalf("session %s made %d calls", session, calls)
			}
		}
		return report.Run.ID
	}
	_, stderr, code := runCLI(t, bin, env, args...)
	if code != 0 {
		t.Fatalf("focused exit=%d stderr=%s", code, stderr)
	}
	root := check(1)
	before, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	_, stderr, code = runCLI(t, bin, env, args...)
	if code != 1 || !strings.Contains(stderr, "complete review") {
		t.Fatalf("complete review was not refused: exit=%d stderr=%s", code, stderr)
	}
	after, err := os.ReadFile(out)
	if err != nil || string(before) != string(after) || check(1) != root {
		t.Fatalf("refusal changed root checkpoint: %v", err)
	}
	_, stderr, code = runCLI(t, bin, env, append(args, "--fresh")...)
	if code != 0 {
		t.Fatalf("fresh exit=%d stderr=%s", code, stderr)
	}
	if check(2) == root {
		t.Fatal("fresh reused root and child graph")
	}
}

func TestCanaryFocusedValidationAndEmptyDiff(t *testing.T) {
	bin := canaryBinary(t)
	dir, _, _, _ := pullHistory(t)
	requests := 0
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
		http.Error(w, "model must not be called", http.StatusUnauthorized)
	}))
	defer server.Close()
	env := focusedCanaryEnv(t, server.URL)
	out := filepath.Join(t.TempDir(), "empty.jsonl")
	args := []string{"run", "--model", "canary-model", "--workspace", dir, "--out", out, "--timeout", "2s"}
	_, stderr, code := runCLI(t, bin, env, append(args, "--strategy", "invalid")...)
	if code != 1 || !strings.Contains(stderr, "want single or focused") {
		t.Fatalf("invalid strategy exit=%d stderr=%s", code, stderr)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatalf("invalid strategy wrote checkpoint: %v", err)
	}
	_, stderr, code = runCLI(t, bin, env, append(args, "--strategy", "focused")...)
	if code != 0 {
		t.Fatalf("empty focused exit=%d stderr=%s", code, stderr)
	}
	report, err := findings.ReadFile(out)
	if err != nil || report.Run == nil || report.Run.Status != findings.StatusComplete || len(report.Findings) != 0 || report.Run.Cost.Recorded() || !strings.HasPrefix(report.Summary, findings.CleanVerdict) {
		t.Fatalf("empty focused report=%+v err=%v", report, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if requests != 0 {
		t.Fatalf("validation or empty range called model %d times", requests)
	}
}

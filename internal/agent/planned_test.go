package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

type plannedTestAgent func(context.Context, review.AgentRequest) (review.AgentResult, error)

func TestPlannedRestoreCandidateIDsFromBodyPrefix(t *testing.T) {
	candidate := plannedTestIssue("candidate-id")
	candidates := map[string]findings.Finding{candidate.ID: candidate}
	verified := candidate
	verified.ID = "model-generated-id"
	verified.Body = "[candidate-id] Confirmed production impact"
	restored, err := plannedRestoreCandidateIDs([]findings.Finding{verified}, candidates)
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 1 || restored[0].ID != candidate.ID || restored[0].Body != "Confirmed production impact" {
		t.Fatalf("restored = %+v", restored)
	}
	if err := plannedValidateOutput(findings.Report{Findings: restored}, candidates, nil); err != nil {
		t.Fatalf("validate restored output: %v", err)
	}

	changed := verified
	changed.StartLine++
	if _, err := plannedRestoreCandidateIDs([]findings.Finding{changed}, candidates); err == nil || !strings.Contains(err.Error(), "canonical location") {
		t.Fatalf("changed location error = %v", err)
	}
	unknown := verified
	unknown.Body = "[not-a-candidate] Confirmed production impact"
	if _, err := plannedRestoreCandidateIDs([]findings.Finding{unknown}, candidates); err == nil || !strings.Contains(err.Error(), "unknown verifier ID") {
		t.Fatalf("unknown prefix error = %v", err)
	}
}

func TestPlannedConsolidationTwoHundredCandidatesBoundedRounds(t *testing.T) {
	req := plannedTestRequest(t)
	issues := make([]findings.Finding, 200)
	for i := range issues {
		issues[i] = plannedTestIssue(fmt.Sprintf("issue-%03d", i))
		issues[i].Body = strings.Repeat("e", 1500) + issues[i].ID
	}
	initial := append([]findings.Finding{}, issues...)
	plannedOrder(initial, 0)
	groups, e := plannedBatches(initial)
	if e != nil {
		t.Fatal(e)
	}
	group0 := map[string]int{}
	for batch, items := range groups {
		for _, item := range items {
			group0[item.ID] = batch
		}
	}
	// One confirmed intra-batch merge creates progress. Another duplicate starts
	// across that partition and becomes adjacent in the deterministic next round.
	intraA, intraB := initial[0].ID, initial[1].ID
	next := []findings.Finding{}
	for _, item := range initial {
		if item.ID != intraB {
			next = append(next, item)
		}
	}
	plannedOrder(next, 1)
	nextGroups, e := plannedBatches(next)
	if e != nil {
		t.Fatal(e)
	}
	crossA, crossB := "", ""
	for _, group := range nextGroups {
		for i := 0; i < len(group) && crossA == ""; i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i].ID, group[j].ID
				if a != intraA && b != intraA && group0[a] != group0[b] {
					crossA, crossB = a, b
					break
				}
			}
		}
	}
	if crossA == "" {
		t.Fatal("fixture has no cross-batch reshuffle pair")
	}
	discover := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		if plannedTestTask(r) == "local-a" {
			return plannedTestEmit(r, issues)
		}
		return plannedTestEmit(r, nil)
	})
	calls := 0
	verifier := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		items, e := plannedTestItems(r)
		if e != nil {
			return review.AgentResult{}, e
		}
		if strings.HasPrefix(r.Prompt, "Planned consolidation") {
			calls++
			present := map[string]bool{}
			for _, item := range items {
				present[item.ID] = true
			}
			drop := map[string]bool{}
			if present[intraA] && present[intraB] {
				drop[intraB] = true
			}
			if present[crossA] && present[crossB] {
				drop[crossB] = true
			}
			kept := []findings.Finding{}
			for _, item := range items {
				if !drop[item.ID] {
					kept = append(kept, item)
				}
			}
			items = kept
		}
		return plannedTestEmit(r, items)
	})
	result, e := (Planned{Agent: discover, Verifier: verifier}).Run(t.Context(), req)
	if e != nil || result.Coverage == nil {
		t.Fatalf("bounded pipeline %+v %v", result, e)
	}
	if calls > 3*len(groups) || calls < 2*len(groups) {
		t.Fatalf("consolidation calls %d, batch bound %d", calls, 3*len(groups))
	}
	report, e := findings.ReadFile(req.FindingsPath)
	if e != nil || len(report.Findings) != 198 {
		t.Fatalf("confirmed duplicates/overlap distinct issues: %d %v", len(report.Findings), e)
	}
	for _, item := range report.Findings {
		if item.ID == intraB || item.ID == crossB {
			t.Fatalf("duplicate %s retained", item.ID)
		}
	}
}

func TestPlannedNestedFocusedDoesNotDoubleChargeFailedTask(t *testing.T) {
	req := plannedTestRequest(t)
	var mu sync.Mutex
	failures := 0
	inner := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		task := plannedTestTask(r)
		stage := strings.TrimSuffix(filepath.Base(r.FindingsPath), ".jsonl")
		if stage == "verification" {
			// Focused verification contains the original task prompt before its JSON.
			_, raw, ok := strings.Cut(r.Prompt, "Candidate JSON:\n")
			if !ok {
				return review.AgentResult{}, errors.New("focused candidates missing")
			}
			var items []findings.Finding
			if e := json.Unmarshal([]byte(raw), &items); e != nil {
				return review.AgentResult{}, e
			}
			return plannedTestEmit(r, items)
		}
		result, e := plannedTestEmit(r, []findings.Finding{plannedTestIssue(task)})
		if task == "local-b" && stage == "failures" {
			mu.Lock()
			failures++
			attempt := failures
			mu.Unlock()
			if attempt == 1 {
				return result, errors.New("nested failed child")
			}
		}
		return result, e
	})
	p := Planned{Agent: Focused{Agent: inner}, Verifier: plannedTestAgent(plannedTestVerifier), Config: "focused/high"}
	first, e := p.Run(t.Context(), req)
	if e == nil || first.Cost.Requests != 14 || first.Coverage != nil {
		t.Fatalf("nested first %+v %v", first, e)
	}
	req.Resuming = true
	req.PriorCost = first.Cost
	second, e := p.Run(t.Context(), req)
	if e != nil || second.Cost.Requests != 4 || second.Coverage == nil {
		t.Fatalf("nested retry %+v %v", second, e)
	}
	req.PriorCost = req.PriorCost.Add(second.Cost)
	third, e := p.Run(t.Context(), req)
	if e != nil || third.Cost.Recorded() {
		t.Fatalf("nested repeated cost %+v %v", third, e)
	}
}

func (fn plannedTestAgent) Run(ctx context.Context, r review.AgentRequest) (review.AgentResult, error) {
	return fn(ctx, r)
}
func plannedTestRequest(t *testing.T) review.AgentRequest {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	return review.AgentRequest{Workspace: dir, ReviewID: "planned-root", Model: "fake", Prompt: "Selected source: working tree from base abc\n```diff\nlarge root diff omitted by children", SystemPrompt: "Evidence only", FindingsPath: filepath.Join(dir, "public.jsonl"), Plan: &review.ReviewPlan{Version: "test", Digest: "plan-digest", DiffSHA: "source-sha", DiffBytes: 2, Tasks: []review.PlanTask{{ID: "local-a", Kind: "local", Prompt: "Review local a at base abc and working tree", Spans: []review.DiffSpan{{Start: 0, End: 1}}}, {ID: "local-b", Kind: "local", Prompt: "Review local b at base abc and working tree", Spans: []review.DiffSpan{{Start: 1, End: 2}}}, {ID: "boundary", Kind: "boundary", Prompt: "Review contracts across local a and b at base abc and working tree"}}}}
}
func plannedTestTask(r review.AgentRequest) string {
	line, _, _ := strings.Cut(r.Prompt, "\n")
	return strings.TrimPrefix(line, "Planned discovery task: ")
}
func plannedTestItems(r review.AgentRequest) ([]findings.Finding, error) {
	_, raw, ok := strings.Cut(r.Prompt, "\nCandidate JSON:\n")
	if !ok {
		return nil, errors.New("no candidates")
	}
	if len(raw) > plannedBatchBytes || len(r.Prompt) > review.MaxPlanPromptBytes {
		return nil, errors.New("oversized verifier input")
	}
	var items []findings.Finding
	e := json.Unmarshal([]byte(raw), &items)
	return items, e
}
func plannedTestIssue(id string) findings.Finding {
	return findings.Finding{ID: id, Path: "a.go", StartLine: 4, EndLine: 4, Anchor: findings.AnchorNew, Severity: findings.SeverityError, Body: "Confirmed defect " + id}
}
func plannedTestEmit(r review.AgentRequest, items []findings.Finding) (review.AgentResult, error) {
	summary := findings.CleanVerdict
	if len(items) > 0 {
		summary = "Confirmed a defect"
	}
	e := focusedWriteReport(r.FindingsPath, findings.Report{Findings: items, Summary: summary})
	return review.AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1, AmountUSD: 1, InputTokens: 10}}, e
}
func plannedTestVerifier(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
	items, e := plannedTestItems(r)
	if e != nil {
		return review.AgentResult{}, e
	}
	return plannedTestEmit(r, items)
}
func plannedTestState(t *testing.T, req review.AgentRequest) (string, plannedManifest) {
	t.Helper()
	base, e := sessionDirectory()
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(base, "planned", focusedHash(req.ReviewID), "manifest.json")
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	var m plannedManifest
	if e := json.Unmarshal(raw, &m); e != nil {
		t.Fatal(e)
	}
	return path, m
}
func plannedTestSave(t *testing.T, path string, m plannedManifest) {
	t.Helper()
	raw, e := json.Marshal(m)
	if e != nil {
		t.Fatal(e)
	}
	if e := focusedAtomic(path, raw); e != nil {
		t.Fatal(e)
	}
}

func TestPlannedPrivateBoundedConcurrencyCoverageAndOverlap(t *testing.T) {
	req := plannedTestRequest(t)
	var mu sync.Mutex
	active, maxActive := 0, 0
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	discover := plannedTestAgent(func(ctx context.Context, child review.AgentRequest) (review.AgentResult, error) {
		if child.Plan != nil || child.ReviewID == req.ReviewID || child.FindingsPath == req.FindingsPath || len(child.Prompt) > review.MaxPlanPromptBytes || strings.Contains(child.Prompt, "large root diff") {
			return review.AgentResult{}, errors.New("unbounded or nonisolated task")
		}
		if !strings.Contains(child.SystemPrompt, "UNVERIFIED") {
			return review.AgentResult{}, errors.New("public discovery")
		}
		if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
			return review.AgentResult{}, errors.New("published before coverage")
		}
		mu.Lock()
		active++
		if active > maxActive {
			maxActive = active
		}
		mu.Unlock()
		select {
		case arrived <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-ctx.Done():
			return review.AgentResult{}, ctx.Err()
		}
		mu.Lock()
		active--
		mu.Unlock()
		return plannedTestEmit(child, []findings.Finding{plannedTestIssue(plannedTestTask(child))})
	})
	go func() { <-arrived; <-arrived; close(release) }()
	verificationCalls := 0
	consolidationCalls := 0
	verify := plannedTestAgent(func(ctx context.Context, child review.AgentRequest) (review.AgentResult, error) {
		verificationCalls++
		if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
			return review.AgentResult{}, errors.New("premature publish")
		}
		if !strings.Contains(child.Prompt, "working tree from base abc") || strings.Contains(child.Prompt, "large root diff") {
			return review.AgentResult{}, errors.New("source context lost")
		}
		return plannedTestVerifier(ctx, child)
	})
	consolidate := plannedTestAgent(func(ctx context.Context, child review.AgentRequest) (review.AgentResult, error) {
		consolidationCalls++
		if !strings.HasPrefix(child.Prompt, "Planned consolidation") || !strings.Contains(child.Prompt, "different bugs on overlapping lines") || !strings.Contains(child.SystemPrompt, "Do not inspect the workspace or call tools") {
			return review.AgentResult{}, errors.New("consolidation contract missing")
		}
		return plannedTestVerifier(ctx, child)
	})
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	planned := Planned{Agent: discover, Verifier: verify, Consolidator: consolidate}
	result, e := planned.Run(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	if maxActive != 2 || result.Coverage == nil || len(result.Coverage.Completed) != 3 || len(result.Coverage.Verified) != 3 || result.Coverage.Digest != req.Plan.Digest {
		t.Fatalf("coverage/parallel %+v max=%d", result, maxActive)
	}
	report, e := findings.ReadFile(req.FindingsPath)
	if e != nil || len(report.Findings) != 3 {
		t.Fatalf("overlap bugs lost %+v %v", report, e)
	}
	if result.Cost.Requests != 5 {
		t.Fatalf("cost %+v", result.Cost)
	}
	if verificationCalls != 1 || consolidationCalls != 1 {
		t.Fatalf("verification calls=%d consolidation calls=%d", verificationCalls, consolidationCalls)
	}
	req.Resuming = true
	req.PriorCost = result.Cost
	again, e := planned.Run(ctx, req)
	if e != nil || again.Cost.Recorded() || again.Coverage == nil {
		t.Fatalf("completed resume %+v %v", again, e)
	}
}

func TestPlannedFailedCostRetrySkipAndReceiptGapRecovery(t *testing.T) {
	for _, persisted := range []bool{false, true} {
		t.Run(fmt.Sprint(persisted), func(t *testing.T) {
			req := plannedTestRequest(t)
			var mu sync.Mutex
			calls := map[string]int{}
			childIDs := map[string]string{}
			discover := plannedTestAgent(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
				id := plannedTestTask(child)
				mu.Lock()
				calls[id]++
				attempt := calls[id]
				old := childIDs[id]
				childIDs[id] = child.ReviewID
				mu.Unlock()
				if attempt > 1 {
					if old != child.ReviewID || !child.Resuming || child.PriorCost.Requests != 1 {
						return review.AgentResult{}, errors.New("child continuation/accounting lost")
					}
					report, e := findings.ReadFile(child.FindingsPath)
					if e != nil || len(report.Findings) != 1 || report.Summary != "" {
						return review.AgentResult{}, errors.Join(errors.New("partial candidates or stale summary"), e)
					}
				}
				r, e := plannedTestEmit(child, []findings.Finding{plannedTestIssue(id)})
				if id == "local-b" && attempt == 1 {
					return r, errors.New("retry this task")
				}
				return r, e
			})
			p := Planned{Agent: discover, Verifier: plannedTestAgent(plannedTestVerifier)}
			first, e := p.Run(t.Context(), req)
			if e == nil || first.Cost.Requests != 3 || first.Coverage != nil {
				t.Fatalf("first %+v %v", first, e)
			}
			path, m := plannedTestState(t, req)
			// Simulate receipt persisted but manifest entry absent after a crash.
			id := m.Complete["task:local-a"]
			delete(m.Complete, "task:local-a")
			delete(m.Receipts, id)
			plannedTestSave(t, path, m)
			req.Resuming = true
			if persisted {
				req.PriorCost = first.Cost
			}
			second, e := p.Run(t.Context(), req)
			want := 6
			if persisted {
				want = 3
			}
			if e != nil || second.Cost.Requests != want || second.Coverage == nil {
				t.Fatalf("recover %+v %v want%d", second, e, want)
			}
			if calls["local-a"] != 1 || calls["boundary"] != 1 || calls["local-b"] != 2 {
				t.Fatalf("repeated tasks %v", calls)
			}
			req.PriorCost = req.PriorCost.Add(second.Cost)
			third, e := p.Run(t.Context(), req)
			if e != nil || third.Cost.Recorded() {
				t.Fatalf("double count %+v %v", third, e)
			}
		})
	}
}

func TestPlannedPoisonedVerifierFreshRetriesAndNoCoverage(t *testing.T) {
	for _, mode := range []string{"unknown", "location", "summary", "cost", "error", "consolidation"} {
		t.Run(mode, func(t *testing.T) {
			req := plannedTestRequest(t)
			discover := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
				return plannedTestEmit(r, []findings.Finding{plannedTestIssue(plannedTestTask(r))})
			})
			attempts := 0
			firstID, firstPath := "", ""
			verify := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
				target := strings.HasPrefix(r.Prompt, "Planned verification")
				if mode == "consolidation" {
					target = strings.HasPrefix(r.Prompt, "Planned consolidation")
				}
				items, e := plannedTestItems(r)
				if e != nil {
					return review.AgentResult{}, e
				}
				if target {
					attempts++
					if attempts == 1 {
						firstID, firstPath = r.ReviewID, r.FindingsPath
						if mode == "unknown" {
							items[0].ID = "bad"
						}
						if mode == "location" {
							items[0].Path = "wrong.go"
						}
					}
				}
				if target && attempts == 2 {
					if r.ReviewID == firstID || r.FindingsPath == firstPath {
						return review.AgentResult{}, errors.New("poisoned session reused")
					}
					old, e := findings.ReadFile(r.FindingsPath)
					if e != nil || len(old.Findings) > 0 || old.Summary != "" {
						return review.AgentResult{}, errors.New("verifier candidates preseeded")
					}
				}
				result, e := plannedTestEmit(r, items)
				if target && attempts == 1 {
					switch mode {
					case "summary":
						e = focusedWriteReport(r.FindingsPath, findings.Report{Findings: items, Summary: findings.CleanVerdict})
					case "cost":
						result.Cost = findings.Cost{}
					case "error", "consolidation":
						e = errors.New("verification failed")
					}
				}
				return result, e
			})
			p := Planned{Agent: discover, Verifier: verify}
			first, e := p.Run(t.Context(), req)
			if e == nil || first.Coverage != nil {
				t.Fatal("poison accepted")
			}
			if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("hypotheses published")
			}
			req.Resuming = true
			req.PriorCost = first.Cost
			second, e := p.Run(t.Context(), req)
			if e != nil || second.Coverage == nil {
				t.Fatalf("retry %+v %v", second, e)
			}
		})
	}
}

func TestPlannedRejectsStateAndConfigurationChanges(t *testing.T) {
	for _, mode := range []string{"model", "plan", "config", "missing-manifest", "artifact", "receipt", "nil-plan"} {
		t.Run(mode, func(t *testing.T) {
			req := plannedTestRequest(t)
			fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
				return plannedTestEmit(r, nil)
			})
			p := Planned{Agent: fake, Verifier: plannedTestAgent(plannedTestVerifier), Config: "single/high"}
			first, e := p.Run(t.Context(), req)
			if e != nil {
				t.Fatal(e)
			}
			req.Resuming = true
			req.PriorCost = first.Cost
			path, m := plannedTestState(t, req)
			switch mode {
			case "model":
				req.Model = "changed"
			case "plan":
				req.Plan.Tasks[0].Prompt += " changed"
			case "config":
				p.Config = "focused/high"
			case "missing-manifest":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
			case "artifact":
				r := m.Receipts[m.Complete["task:local-a"]]
				if e := os.WriteFile(filepath.Join(filepath.Dir(path), r.Path), []byte("{}"), 0o600); e != nil {
					t.Fatal(e)
				}
			case "receipt":
				for id := range m.Receipts {
					if e := os.Remove(filepath.Join(filepath.Dir(path), "receipt-"+id+".json")); e != nil {
						t.Fatal(e)
					}
					break
				}
			case "nil-plan":
				req.Plan = nil
			}
			result, e := p.Run(t.Context(), req)
			if e == nil || result.Coverage != nil {
				t.Fatalf("invalid resume accepted %+v", result)
			}
		})
	}
}

func TestPlannedBatchBoundAndGlobalDifferentLocationDedup(t *testing.T) {
	req := plannedTestRequest(t)
	discover := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		item := plannedTestIssue(plannedTestTask(r))
		item.Body = strings.Repeat("e", 40000) + item.ID
		if item.ID == "local-b" {
			item.Path = "b.go"
		}
		return plannedTestEmit(r, []findings.Finding{item})
	})
	verifyCalls, consolidations := 0, 0
	verify := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		items, e := plannedTestItems(r)
		if e != nil {
			return review.AgentResult{}, e
		}
		if strings.HasPrefix(r.Prompt, "Planned verification") {
			verifyCalls++
			for i := range items {
				items[i].Body = "Confirmed issue " + items[i].ID
			}
		} else {
			consolidations++
			foundA, foundB := false, false
			for _, item := range items {
				foundA = foundA || item.ID == "local-a"
				foundB = foundB || item.ID == "local-b"
			}
			if foundA && foundB {
				kept := []findings.Finding{}
				for _, item := range items {
					if item.ID != "local-b" {
						kept = append(kept, item)
					}
				}
				items = kept
			}
		}
		return plannedTestEmit(r, items)
	})
	result, e := (Planned{Agent: discover, Verifier: verify}).Run(t.Context(), req)
	if e != nil {
		t.Fatal(e)
	}
	if verifyCalls != 2 || consolidations != 2 || result.Coverage == nil {
		t.Fatalf("batch/merge %d %d %+v", verifyCalls, consolidations, result)
	}
	report, e := findings.ReadFile(req.FindingsPath)
	if e != nil || len(report.Findings) != 2 {
		t.Fatalf("semantic dedup %+v %v", report, e)
	}
	if _, e := plannedBatches([]findings.Finding{{ID: "huge", Body: strings.Repeat("x", plannedBatchBytes)}}); e == nil {
		t.Fatal("oversized candidate truncated/accepted")
	}
}

func TestPlannedNilForwardingAndWholeCallTimeout(t *testing.T) {
	for _, planned := range []bool{false, true} {
		t.Run(fmt.Sprint(planned), func(t *testing.T) {
			req := plannedTestRequest(t)
			if !planned {
				req.Plan = nil
			}
			fake := plannedTestAgent(func(ctx context.Context, r review.AgentRequest) (review.AgentResult, error) {
				<-ctx.Done()
				result, _ := plannedTestEmit(r, nil)
				return result, ctx.Err()
			})
			result, e := (Planned{Agent: fake, Verifier: fake, Timeout: 20 * time.Millisecond}).Run(t.Context(), req)
			if !errors.Is(e, context.DeadlineExceeded) || result.Coverage != nil {
				t.Fatalf("deadline %+v %v", result, e)
			}
			if planned {
				if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("published before coverage")
				}
			}
		})
	}
}

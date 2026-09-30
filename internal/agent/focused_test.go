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

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

type focusedTestModel struct{ responses []llm.Response }

func (m *focusedTestModel) Respond(_ context.Context, _ llm.Request, _ llm.RequestOptions) (llm.Response, error) {
	if len(m.responses) == 0 {
		return llm.Response{}, errors.New("focused model exhausted")
	}
	r := m.responses[0]
	m.responses = m.responses[1:]
	return r, nil
}

func TestFocusedRealHarnessRetriesPoisonedVerifier(t *testing.T) {
	for _, mode := range []string{"unknown", "location"} {
		t.Run(mode, func(t *testing.T) {
			req := focusedTestRequest(t)
			var mu sync.Mutex
			calls := map[string]int{}
			var initialVerifierID string
			runner := focusedTestRunner(func(ctx context.Context, child review.AgentRequest) (review.AgentResult, error) {
				stage := focusedTestStage(child)
				mu.Lock()
				calls[stage]++
				attempt := calls[stage]
				mu.Unlock()
				item := focusedTestIssue(stage)
				if stage == "verification" {
					items, e := focusedTestCandidates(child)
					if e != nil {
						return review.AgentResult{}, e
					}
					item = items[0]
					if attempt == 1 {
						initialVerifierID = child.ReviewID
						if mode == "unknown" {
							item.ID = "poison"
						} else {
							item.Path = "wrong.go"
						}
					}
					if attempt == 2 {
						if child.ReviewID == initialVerifierID {
							return review.AgentResult{}, errors.New("poisoned verifier session reused")
						}
						report, e := findings.ReadFile(child.FindingsPath)
						if e != nil {
							return review.AgentResult{}, e
						}
						if len(report.Findings) != 0 || report.Summary != "" {
							return review.AgentResult{}, errors.New("poisoned verifier file reused")
						}
					}
				}
				args, e := json.Marshal(item)
				if e != nil {
					return review.AgentResult{}, e
				}
				model := &focusedTestModel{responses: []llm.Response{
					{ID: "focused-call", Stop: llm.StopComplete, Output: []llm.Item{{ProviderID: "focused-tool", Type: llm.ItemToolCall, Data: llm.ToolCall{CallID: "focused-record", Name: review.RecordFindingTool, Arguments: string(args)}}}, Usage: llm.Usage{InputTokens: 10, OutputTokens: 2}},
					{ID: "focused-summary", Stop: llm.StopComplete, Output: []llm.Item{{ProviderID: "focused-text", Type: llm.ItemMessage, Data: llm.Message{Role: llm.RoleAssistant, Text: "Confirmed defect in a.go"}}}, Usage: llm.Usage{InputTokens: 10, OutputTokens: 2}},
				}}
				return (Harness{ThinkingLevel: "high"}).run(ctx, model, child)
			})
			a := Focused{Agent: runner}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			first, e := a.Run(ctx, req)
			if e == nil || first.Cost.Requests != 10 {
				t.Fatalf("first %+v %v", first, e)
			}
			if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("poison published")
			}
			req.Resuming = true
			req.PriorCost = first.Cost
			second, e := a.Run(ctx, req)
			if e != nil || second.Cost.Requests != 2 {
				t.Fatalf("retry %+v %v", second, e)
			}
			for _, lens := range focusedLenses {
				if calls[lens] != 1 {
					t.Fatalf("discovery reran: %v", calls)
				}
			}
			report, e := findings.ReadFile(req.FindingsPath)
			if e != nil || len(report.Findings) != 1 || report.Findings[0].ID == "poison" || report.Findings[0].Path != "a.go" {
				t.Fatalf("verified %+v %v", report, e)
			}
		})
	}
}

func TestFocusedLedgerValidationAndPriorBaseCost(t *testing.T) {
	req := focusedTestRequest(t)
	req.PriorCost = findings.Cost{Currency: "USD", Requests: 3, AmountUSD: 2}
	fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
		if child.PriorCost.Recorded() {
			return review.AgentResult{}, errors.New("root prior cost leaked to child")
		}
		return focusedTestEmit(child, nil)
	})
	a := Focused{Agent: fake}
	first, e := a.Run(t.Context(), req)
	if e != nil || first.Cost.Requests != 5 {
		t.Fatalf("base cost %+v %v", first, e)
	}
	req.Resuming = true
	req.PriorCost = req.PriorCost.Add(first.Cost)
	again, e := a.Run(t.Context(), req)
	if e != nil || again.Cost.Recorded() {
		t.Fatalf("persisted %+v %v", again, e)
	}
	req.PriorCost.Requests++
	if _, e := a.Run(t.Context(), req); e == nil {
		t.Fatal("over-accounted prior cost accepted")
	}
	if _, e := focusedCostDelta(findings.Cost{AmountUSD: 1}, findings.Cost{AmountUSD: 1 + 1e-10}); e != nil {
		t.Fatal(e)
	}
	if _, e := focusedCostDelta(findings.Cost{Requests: -1}, findings.Cost{}); e == nil {
		t.Fatal("negative cost accepted")
	}
}

func TestFocusedManifestSaveFailureReturnsIncurredCost(t *testing.T) {
	req := focusedTestRequest(t)
	fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
		r, e := focusedTestEmit(child, nil)
		if focusedTestStage(child) == "verification" {
			path := filepath.Join(filepath.Dir(child.FindingsPath), "manifest.json")
			if err := os.Remove(path); err != nil {
				return r, err
			}
			if err := os.Mkdir(path, 0o700); err != nil {
				return r, err
			}
		}
		return r, e
	})
	r, e := (Focused{Agent: fake}).Run(t.Context(), req)
	if e == nil || r.Cost.Requests != 5 || r.Cost.AmountUSD != 5 {
		t.Fatalf("save failure lost cost: %+v %v", r, e)
	}
	if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("published after failed manifest save")
	}
}

type focusedTestRunner func(context.Context, review.AgentRequest) (review.AgentResult, error)

func (fn focusedTestRunner) Run(ctx context.Context, r review.AgentRequest) (review.AgentResult, error) {
	return fn(ctx, r)
}
func focusedTestRequest(t *testing.T) review.AgentRequest {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	return review.AgentRequest{Workspace: dir, ReviewID: "focused-test-root", Model: "fake", Prompt: "Review diff source-sha-1", SystemPrompt: "Review safely", FindingsPath: filepath.Join(dir, "findings.jsonl")}
}
func focusedTestIssue(id string) findings.Finding {
	return findings.Finding{ID: id, Path: "a.go", StartLine: 4, EndLine: 4, Anchor: findings.AnchorNew, Severity: findings.SeverityError, Body: "Defect hypothesis " + id}
}
func focusedTestStage(req review.AgentRequest) string {
	return strings.TrimSuffix(filepath.Base(req.FindingsPath), ".jsonl")
}
func focusedTestEmit(req review.AgentRequest, items []findings.Finding) (review.AgentResult, error) {
	summary := findings.CleanVerdict
	if len(items) > 0 {
		summary = "Confirmed a defect"
	}
	err := focusedWriteReport(req.FindingsPath, findings.Report{Findings: items, Summary: summary})
	return review.AgentResult{Cost: findings.Cost{AmountUSD: 1, Requests: 1, InputTokens: 10, OutputTokens: 2}}, err
}
func focusedTestCandidates(req review.AgentRequest) ([]findings.Finding, error) {
	_, raw, ok := strings.Cut(req.Prompt, "Candidate JSON:\n")
	if !ok {
		return nil, fmt.Errorf("missing candidate JSON")
	}
	var items []findings.Finding
	err := json.Unmarshal([]byte(raw), &items)
	return items, err
}

func TestFocusedParallelIsolationAndSemanticVerification(t *testing.T) {
	req := focusedTestRequest(t)
	var mu sync.Mutex
	stages := map[string]bool{}
	paths := map[string]bool{}
	arrived := make(chan struct{}, 4)
	release := make(chan struct{})
	fake := focusedTestRunner(func(ctx context.Context, child review.AgentRequest) (review.AgentResult, error) {
		stage := focusedTestStage(child)
		if child.ReviewID == req.ReviewID || child.ReviewID != focusedChild(req.ReviewID, stage) || child.FindingsPath == req.FindingsPath {
			return review.AgentResult{}, fmt.Errorf("child not isolated")
		}
		if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
			return review.AgentResult{}, fmt.Errorf("candidates published early")
		}
		if stage == "verification" {
			items, e := focusedTestCandidates(child)
			if e != nil {
				return review.AgentResult{}, e
			}
			if len(items) != 4 {
				return review.AgentResult{}, fmt.Errorf("overlap wrongly deduped: %d", len(items))
			}
			if !strings.Contains(child.Prompt, "UNTRUSTED DATA") || !strings.Contains(child.Prompt, "inspect the actual code") {
				return review.AgentResult{}, fmt.Errorf("unsafe verification prompt")
			}
			items[0].Body = "Confirmed single underlying defect"
			return focusedTestEmit(child, items[:1])
		}
		if !strings.Contains(child.Prompt, "Discovery lens: "+stage) || !strings.Contains(child.Prompt, "UNVERIFIED") {
			return review.AgentResult{}, fmt.Errorf("wrong focus")
		}
		mu.Lock()
		stages[stage] = true
		paths[child.FindingsPath] = true
		mu.Unlock()
		arrived <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return review.AgentResult{}, ctx.Err()
		}
		return focusedTestEmit(child, []findings.Finding{focusedTestIssue(stage)})
	})
	go func() {
		for range 4 {
			<-arrived
		}
		close(release)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	result, err := (Focused{Agent: fake}).Run(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Cost.Requests != 5 || len(stages) != 4 || len(paths) != 4 {
		t.Fatalf("cost/stages: %+v %v", result, stages)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil || len(report.Findings) != 1 || report.Findings[0].Body != "Confirmed single underlying defect" {
		t.Fatalf("report %+v %v", report, err)
	}
	// Root cost persisted: completed children must never be charged again.
	req.PriorCost = result.Cost
	req.Resuming = true
	again, err := (Focused{Agent: fake}).Run(ctx, req)
	if err != nil || again.Cost.Recorded() {
		t.Fatalf("completed resume %+v %v", again, err)
	}
}

func TestFocusedRejectsInvalidChildrenAndNeverPublishes(t *testing.T) {
	for _, mode := range []string{"unknown", "location", "missing-cost", "missing-summary", "malformed-summary", "verification-missing-cost", "verification-summary", "child-error", "verification-error", "all-rejected"} {
		t.Run(mode, func(t *testing.T) {
			req := focusedTestRequest(t)
			fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
				stage := focusedTestStage(child)
				items := []findings.Finding{focusedTestIssue(stage)}
				if stage == "verification" {
					var e error
					items, e = focusedTestCandidates(child)
					if e != nil {
						return review.AgentResult{}, e
					}
					items = items[:1]
					switch mode {
					case "unknown":
						items[0].ID = "intruder"
					case "location":
						items[0].StartLine++
						items[0].EndLine++
					case "all-rejected":
						items = nil
					}
				}
				r, e := focusedTestEmit(child, items)
				switch mode {
				case "missing-cost":
					r.Cost = findings.Cost{}
				case "missing-summary":
					e = focusedWriteReport(child.FindingsPath, findings.Report{Findings: items})
				case "verification-missing-cost":
					if stage == "verification" {
						r.Cost = findings.Cost{}
					}
				case "verification-summary":
					if stage == "verification" {
						e = focusedWriteReport(child.FindingsPath, findings.Report{Findings: items, Summary: findings.CleanVerdict})
					}
				case "malformed-summary":
					e = focusedWriteReport(child.FindingsPath, findings.Report{Findings: items, Summary: findings.CleanVerdict})
				case "child-error":
					e = errors.New("child failed")
				case "verification-error":
					if stage == "verification" {
						e = errors.New("verification failed")
					}
				}
				return r, e
			})
			r, e := (Focused{Agent: fake}).Run(context.Background(), req)
			if mode == "all-rejected" {
				if e != nil {
					t.Fatal(e)
				}
				report, e := findings.ReadFile(req.FindingsPath)
				if e != nil || len(report.Findings) != 0 || report.Summary != findings.CleanVerdict {
					t.Fatalf("clean %+v %v", report, e)
				}
				return
			}
			if e == nil {
				t.Fatal("invalid child accepted")
			}
			if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("private candidates published")
			}
			want := 4
			if mode == "unknown" || mode == "location" || mode == "verification-error" || mode == "verification-summary" {
				want = 5
			}
			if mode == "missing-cost" {
				want = 0
			}
			if r.Cost.Requests != want {
				t.Fatalf("failing cost %+v want %d", r.Cost, want)
			}
		})
	}
}

func TestFocusedRetryKeepsFindingsClearsSummaryAndRecoversCost(t *testing.T) {
	req := focusedTestRequest(t)
	var mu sync.Mutex
	calls := map[string]int{}
	fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
		stage := focusedTestStage(child)
		mu.Lock()
		calls[stage]++
		attempt := calls[stage]
		mu.Unlock()
		if stage == "verification" {
			items, e := focusedTestCandidates(child)
			if e != nil {
				return review.AgentResult{}, e
			}
			return focusedTestEmit(child, items[:1])
		}
		if stage == "failures" && attempt == 2 {
			old, e := findings.ReadFile(child.FindingsPath)
			if e != nil || old.Summary != "" || len(old.Findings) != 1 {
				return review.AgentResult{}, errors.Join(fmt.Errorf("resume lost findings or stale summary: %+v", old), e)
			}
		}
		r, e := focusedTestEmit(child, []findings.Finding{focusedTestIssue(stage)})
		if stage == "failures" && attempt == 1 {
			return r, errors.New("retry me")
		}
		return r, e
	})
	adapter := Focused{Agent: fake}
	first, e := adapter.Run(context.Background(), req)
	if e == nil || first.Cost.Requests != 4 {
		t.Fatalf("first %+v %v", first, e)
	}
	// Simulate crash before caller persists root cost: recover the ledger too.
	req.Resuming = true
	second, e := adapter.Run(context.Background(), req)
	if e != nil || second.Cost.Requests != 6 {
		t.Fatalf("recovery %+v %v", second, e)
	}
	for _, stage := range focusedLenses {
		want := 1
		if stage == "failures" {
			want = 2
		}
		if calls[stage] != want {
			t.Fatalf("calls %v", calls)
		}
	}
	req.PriorCost = second.Cost
	third, e := adapter.Run(context.Background(), req)
	if e != nil || third.Cost.Recorded() {
		t.Fatalf("double cost %+v %v", third, e)
	}
}

func TestFocusedCancellationAndTotalDeadline(t *testing.T) {
	for _, verification := range []bool{false, true} {
		t.Run(fmt.Sprint(verification), func(t *testing.T) {
			req := focusedTestRequest(t)
			fake := focusedTestRunner(func(ctx context.Context, child review.AgentRequest) (review.AgentResult, error) {
				if verification && focusedTestStage(child) != "verification" {
					return focusedTestEmit(child, []findings.Finding{focusedTestIssue(focusedTestStage(child))})
				}
				<-ctx.Done()
				r, _ := focusedTestEmit(child, nil)
				if verification {
					return r, nil
				} // A child ignoring cancellation cannot authorize publication.
				return r, ctx.Err()
			})
			r, e := (Focused{Agent: fake, Timeout: 30 * time.Millisecond}).Run(context.Background(), req)
			if !errors.Is(e, context.DeadlineExceeded) || r.Cost.Requests < 4 {
				t.Fatalf("deadline %+v %v", r, e)
			}
			if _, e := os.Stat(req.FindingsPath); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("published on timeout")
			}
		})
	}
	t.Run("cancel", func(t *testing.T) {
		req := focusedTestRequest(t)
		ctx, cancel := context.WithCancel(context.Background())
		fake := focusedTestRunner(func(ctx context.Context, child review.AgentRequest) (review.AgentResult, error) {
			cancel()
			<-ctx.Done()
			r, _ := focusedTestEmit(child, nil)
			return r, ctx.Err()
		})
		r, e := (Focused{Agent: fake}).Run(ctx, req)
		if !errors.Is(e, context.Canceled) || r.Cost.Requests != 4 {
			t.Fatalf("cancel %+v %v", r, e)
		}
	})
}

func TestFocusedResumeConfigurationAndStageValidation(t *testing.T) {
	for _, mode := range []string{"workspace", "model", "prompt", "system", "config", "tamper", "missing-manifest", "fresh"} {
		t.Run(mode, func(t *testing.T) {
			req := focusedTestRequest(t)
			fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
				return focusedTestEmit(child, nil)
			})
			a := Focused{Agent: fake, Config: "high"}
			first, e := a.Run(context.Background(), req)
			if e != nil {
				t.Fatal(e)
			}
			req.PriorCost = first.Cost
			req.Resuming = true
			base, _ := sessionDirectory()
			dir := filepath.Join(base, "focused", focusedHash(req.ReviewID))
			switch mode {
			case "workspace":
				req.Workspace += "-other"
			case "model":
				req.Model += "-other"
			case "prompt":
				req.Prompt += " source-sha-2"
			case "system":
				req.SystemPrompt += " changed"
			case "config":
				a.Config = "low"
			case "tamper":
				if e := os.WriteFile(filepath.Join(dir, "correctness.jsonl"), []byte("{}\n"), 0o600); e != nil {
					t.Fatal(e)
				}
			case "missing-manifest":
				if e := os.Remove(filepath.Join(dir, "manifest.json")); e != nil {
					t.Fatal(e)
				}
			case "fresh":
				req.ReviewID = "fresh-root"
				req.PriorCost = findings.Cost{}
				req.Resuming = false
			}
			r, e := a.Run(context.Background(), req)
			if mode == "fresh" {
				if e != nil || r.Cost.Requests != 5 {
					t.Fatalf("fresh %+v %v", r, e)
				}
			} else if e == nil {
				t.Fatal("bad resume accepted")
			}
		})
	}
}

func TestFocusedCarriesPreviouslyVerifiedIDsAndGuardsLargeInput(t *testing.T) {
	for _, mode := range []string{"preserve", "drop", "large", "exact"} {
		t.Run(mode, func(t *testing.T) {
			req := focusedTestRequest(t)
			prior := focusedTestIssue("prior")
			if mode == "preserve" || mode == "drop" {
				if e := focusedWriteReport(req.FindingsPath, findings.Report{Findings: []findings.Finding{prior}, Summary: "Prior verified issue"}); e != nil {
					t.Fatal(e)
				}
			}
			fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
				stage := focusedTestStage(child)
				if stage == "verification" {
					old, e := findings.ReadFile(child.FindingsPath)
					if e != nil && !errors.Is(e, os.ErrNotExist) {
						return review.AgentResult{}, e
					}
					if len(old.Findings) != 0 {
						return review.AgentResult{}, fmt.Errorf("verifier preseeded")
					}
					items, e := focusedTestCandidates(child)
					if e != nil {
						return review.AgentResult{}, e
					}
					if mode == "drop" {
						return focusedTestEmit(child, nil)
					}
					if mode == "exact" && len(items) != 1 {
						return review.AgentResult{}, fmt.Errorf("exact duplicates not collapsed")
					}
					return focusedTestEmit(child, items[:1])
				}
				item := focusedTestIssue(stage)
				if mode == "large" {
					item.Body = strings.Repeat("x", 51000) + stage
				}
				if mode == "exact" {
					item = focusedTestIssue("same")
				}
				return focusedTestEmit(child, []findings.Finding{item})
			})
			_, e := (Focused{Agent: fake}).Run(context.Background(), req)
			if mode == "drop" || mode == "large" {
				if e == nil {
					t.Fatal("unsafe output accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			report, e := findings.ReadFile(req.FindingsPath)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "preserve" && report.Findings[0].ID != "prior" {
				t.Fatal("prior ID lost")
			}
		})
	}
}

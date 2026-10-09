package review

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/internal/findings"
)

func plannedRootFixture(t *testing.T) (string, string) {
	t.Helper()
	dir := gitRepo(t)
	gitRun(t, dir, "config", "core.hooksPath", os.DevNull)
	gitRun(t, dir, "config", "commit.gpgsign", "false")
	gitRun(t, dir, "config", "maintenance.auto", "false")
	gitRun(t, dir, "config", "gc.auto", "0")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(t.TempDir(), "findings.jsonl")
}

func acknowledgePlan(plan *ReviewPlan) *PlanCoverage {
	ack := &PlanCoverage{Digest: plan.Digest}
	for _, task := range plan.Tasks {
		ack.Completed = append(ack.Completed, task.ID)
		ack.Verified = append(ack.Verified, task.ID)
	}
	return ack
}

func TestPlannedRootRequiresCompleteVerifiedCoverage(t *testing.T) {
	for _, invalid := range []string{"nil", "missing-local", "missing-boundary", "missing-verification", "duplicate", "unknown", "wrong-digest"} {
		t.Run(invalid, func(t *testing.T) {
			dir, out := plannedRootFixture(t)
			backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
				if req.Plan == nil {
					t.Fatal("decomposition did not supply source plan")
				}
				ack := acknowledgePlan(req.Plan)
				switch invalid {
				case "nil":
					ack = nil
				case "missing-local", "missing-boundary":
					kind := strings.TrimPrefix(invalid, "missing-")
					for i, task := range req.Plan.Tasks {
						if task.Kind == kind {
							ack.Completed = append(ack.Completed[:i], ack.Completed[i+1:]...)
							break
						}
					}
				case "missing-verification":
					ack.Verified = ack.Verified[:len(ack.Verified)-1]
				case "duplicate":
					ack.Completed = append(ack.Completed, ack.Completed[0])
				case "unknown":
					ack.Completed[0] = "unknown"
				case "wrong-digest":
					ack.Digest = "different-plan"
				}
				if err := findings.AppendSummary(req.FindingsPath, "No material issues in the fixture."); err != nil {
					return AgentResult{}, err
				}
				return AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1}, Coverage: ack}, nil
			})
			result, err := Run(t.Context(), Options{Workspace: dir, Out: out, Model: "test", Decompose: true, Agent: backend})
			if err == nil || result.Report.Complete() || result.Report.Run.Status != findings.StatusFailed || result.Report.Run.Cost.Requests != 1 {
				t.Fatalf("incomplete coverage produced publishable root: %+v err=%v", result, err)
			}
			stored, err := findings.ReadFile(out)
			if err != nil || stored.Complete() {
				t.Fatalf("stored partial root became complete: %+v err=%v", stored, err)
			}
		})
	}
}

func TestPlannedRootCompletesOnlyForUnchangedSource(t *testing.T) {
	for _, mutate := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[mutate], func(t *testing.T) {
			dir, out := plannedRootFixture(t)
			before, err := loadGitDiff(t.Context(), dir, Spec{}, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
				if req.Plan == nil || req.Plan.DiffSHA != before.source.DiffSHA {
					t.Fatal("plan did not bind full source")
				}
				if mutate {
					if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("changed during review\n"), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				if err := findings.AppendSummary(req.FindingsPath, "No material issues in the fixture."); err != nil {
					return AgentResult{}, err
				}
				return AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1}, Coverage: acknowledgePlan(req.Plan)}, nil
			})
			result, err := Run(t.Context(), Options{Workspace: dir, Out: out, Model: "test", Decompose: true, Agent: backend})
			if mutate {
				if err == nil || result.Report.Complete() {
					t.Fatalf("changed source completed: %+v err=%v", result, err)
				}
			} else if err != nil || !result.Report.Complete() || !before.source.SameDiff(result.Report.Run.Source) {
				t.Fatalf("complete planned root lost source: %+v err=%v", result, err)
			}
		})
	}
}

func TestOversizedRootAutomaticallySuppliesBoundedPlan(t *testing.T) {
	dir, out := plannedRootFixture(t)
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte(strings.Repeat("ordinary changed line with surrounding review context\n", 6000)), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		if req.Plan == nil || req.Plan.DiffBytes <= maxBriefDiff {
			t.Fatal("large diff was not planned")
		}
		for _, task := range req.Plan.Tasks {
			if len(task.Prompt) > MaxPlanPromptBytes {
				t.Fatal("task exceeds prompt byte limit")
			}
		}
		if err := findings.AppendSummary(req.FindingsPath, "No material issues in the fixture."); err != nil {
			return AgentResult{}, err
		}
		return AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1}, Coverage: acknowledgePlan(req.Plan)}, nil
	})
	result, err := Run(t.Context(), Options{Workspace: dir, Out: out, Model: "test", Agent: backend})
	if err != nil || !result.Report.Complete() {
		t.Fatalf("large plan did not complete: %+v err=%v", result, err)
	}
}

func TestPlannedRootInterruptedCostAndIdentitySurviveResume(t *testing.T) {
	dir, out := plannedRootFixture(t)
	calls := 0
	id, digest := "", ""
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		calls++
		cost := findings.Cost{Currency: "USD", Requests: 1}
		if calls == 1 {
			id, digest = req.ReviewID, req.Plan.Digest
			return AgentResult{Cost: cost}, context.DeadlineExceeded
		}
		if !req.Resuming || req.ReviewID != id || req.Plan.Digest != digest || req.PriorCost.Requests != 1 {
			t.Fatal("resume changed binding or lost cost")
		}
		if err := findings.AppendSummary(req.FindingsPath, "No material issues in the fixture."); err != nil {
			return AgentResult{}, err
		}
		return AgentResult{Cost: cost, Coverage: acknowledgePlan(req.Plan)}, nil
	})
	opts := Options{Workspace: dir, Out: out, Model: "test", Decompose: true, Agent: backend}
	paused, err := Run(t.Context(), opts)
	if err == nil || paused.Report.Run.Status != findings.StatusRunning || paused.Report.Complete() {
		t.Fatalf("expected running partial review: %+v err=%v", paused, err)
	}
	complete, err := Run(t.Context(), opts)
	if err != nil || !complete.Report.Complete() || complete.Report.Run.ID != id || complete.Report.Run.Cost.Requests != 2 {
		t.Fatalf("resumed planned review: %+v err=%v", complete, err)
	}
}

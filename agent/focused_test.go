package agent

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/review"
)

type focusedTestRunner func(context.Context, review.AgentRequest) (review.AgentResult, error)

func (fn focusedTestRunner) Run(ctx context.Context, r review.AgentRequest) (review.AgentResult, error) {
	return fn(ctx, r)
}

func focusedTestRequest(t *testing.T) review.AgentRequest {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	return review.AgentRequest{
		Workspace:    dir,
		ReviewID:     "focused-test-root",
		Model:        "fake",
		Prompt:       "Review diff source-sha-1",
		SystemPrompt: "Review safely",
		FindingsPath: filepath.Join(dir, "findings.jsonl"),
	}
}

func focusedTestIssue(id string) findings.Finding {
	return findings.Finding{
		ID:        id,
		Path:      "a.go",
		StartLine: 4,
		EndLine:   4,
		Anchor:    findings.AnchorNew,
		Severity:  findings.SeverityError,
		Body:      "Defect hypothesis " + id,
	}
}

func focusedTestEmit(req review.AgentRequest, items []findings.Finding) (review.AgentResult, error) {
	summary := findings.CleanVerdict
	if len(items) > 0 {
		summary = "Confirmed a defect"
	}
	err := focusedWriteReport(req.FindingsPath, findings.Report{Findings: items, Summary: summary})
	return review.AgentResult{Cost: findings.Cost{AmountUSD: 1, Requests: 1, InputTokens: 10, OutputTokens: 2}}, err
}

func TestFocusedPassesMergeAndDedupe(t *testing.T) {
	logic := focusedTestIssue("logic-bug")
	restated := logic
	restated.ID = "logic-restated"
	race := focusedTestIssue("race-bug")
	race.Path = "b.go"
	race.Body = "Two goroutines write the counter"
	dropped := focusedTestIssue("dropped-err")
	dropped.Path = "c.go"
	dropped.StartLine = 8
	dropped.EndLine = 8
	dropped.Body = "The error from Sync is discarded"
	escape := focusedTestIssue("path-escape")
	escape.Path = "d.go"
	escape.StartLine = 2
	escape.EndLine = 3
	escape.Body = "Cleaned path escapes the workspace"

	newFake := func(verified *[]findings.Finding) focusedTestRunner {
		var mu sync.Mutex
		return func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
			switch {
			case strings.Contains(child.Prompt, "Candidate JSON:"):
				items, err := plannedTestCandidates(child.Prompt)
				if err != nil {
					return review.AgentResult{}, err
				}
				mu.Lock()
				*verified = append([]findings.Finding{}, items...)
				mu.Unlock()
				kept := make([]findings.Finding, 0, len(items))
				for _, item := range items {
					if item.ID == "dropped-err" {
						continue
					}
					kept = append(kept, item)
				}
				return focusedTestEmit(child, kept)
			case strings.Contains(child.Prompt, "Discovery lens: logic"):
				return focusedTestEmit(child, []findings.Finding{logic, restated})
			case strings.Contains(child.Prompt, "Discovery lens: concurrency"):
				return focusedTestEmit(child, []findings.Finding{logic, race})
			case strings.Contains(child.Prompt, "Discovery lens: errors"):
				return focusedTestEmit(child, []findings.Finding{dropped})
			case strings.Contains(child.Prompt, "Discovery lens: security"):
				return focusedTestEmit(child, []findings.Finding{escape})
			default:
				return review.AgentResult{}, context.Canceled
			}
		}
	}
	ids := func(report findings.Report) map[string]int {
		got := map[string]int{}
		for _, item := range report.Findings {
			got[item.ID]++
		}
		return got
	}

	t.Run("candidates", func(t *testing.T) {
		req := focusedTestRequest(t)
		var verified []findings.Finding
		adapter := Focused{Agent: newFake(&verified), DiscoveryOnly: true}
		if _, err := adapter.Run(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		if len(verified) != 0 {
			t.Fatal("discovery published through verification")
		}
		report, err := findings.ReadFile(req.FindingsPath)
		if err != nil {
			t.Fatal(err)
		}
		got := ids(report)
		if len(report.Findings) != 4 || got["logic-bug"] != 1 || got["race-bug"] != 1 || got["dropped-err"] != 1 || got["path-escape"] != 1 || got["logic-restated"] != 0 {
			t.Fatalf("merged candidates = %+v", report.Findings)
		}
	})

	t.Run("verified", func(t *testing.T) {
		req := focusedTestRequest(t)
		var verified []findings.Finding
		adapter := Focused{Agent: newFake(&verified)}
		if _, err := adapter.Run(t.Context(), req); err != nil {
			t.Fatal(err)
		}
		seen := map[string]bool{}
		for _, item := range verified {
			seen[item.ID] = true
		}
		if len(verified) != 4 || !seen["logic-bug"] || !seen["race-bug"] || !seen["dropped-err"] || !seen["path-escape"] || seen["logic-restated"] {
			t.Fatalf("verifier candidates = %+v", verified)
		}
		report, err := findings.ReadFile(req.FindingsPath)
		if err != nil {
			t.Fatal(err)
		}
		got := ids(report)
		if len(report.Findings) != 3 || got["logic-bug"] != 1 || got["race-bug"] != 1 || got["path-escape"] != 1 || got["dropped-err"] != 0 || got["logic-restated"] != 0 {
			t.Fatalf("public findings = %+v", report.Findings)
		}
	})
}

func TestFocusedDiscoveryOnlyModeIsImmutableOnResume(t *testing.T) {
	req := focusedTestRequest(t)
	fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
		return focusedTestEmit(child, []findings.Finding{focusedTestIssue("same")})
	})
	adapter := Focused{Agent: fake, DiscoveryOnly: true}
	first, err := adapter.Run(t.Context(), req)
	if err != nil || !first.Cost.Recorded() {
		t.Fatalf("private run: result=%+v err=%v", first, err)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil || len(report.Findings) != 1 || report.Findings[0].ID != "same" {
		t.Fatalf("private report: report=%+v err=%v", report, err)
	}

	req.Resuming = true
	req.PriorCost = first.Cost
	again, err := adapter.Run(t.Context(), req)
	if err != nil || again.Cost.Recorded() {
		t.Fatalf("private resume: result=%+v err=%v", again, err)
	}

	adapter.DiscoveryOnly = false
	if _, err := adapter.Run(t.Context(), req); err == nil {
		t.Fatal("resume accepted a changed discovery mode")
	}
}

func TestFocusedLedgerValidationAndPriorBaseCost(t *testing.T) {
	req := focusedTestRequest(t)
	req.PriorCost = findings.Cost{Currency: "USD", Requests: 3, AmountUSD: 2}
	fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
		return focusedTestEmit(child, nil)
	})
	a := Focused{Agent: fake}
	first, err := a.Run(t.Context(), req)
	if err != nil || !first.Cost.Recorded() {
		t.Fatalf("base cost: result=%+v err=%v", first, err)
	}

	req.Resuming = true
	req.PriorCost = req.PriorCost.Add(first.Cost)
	again, err := a.Run(t.Context(), req)
	if err != nil || again.Cost.Recorded() {
		t.Fatalf("persisted cost: result=%+v err=%v", again, err)
	}

	req.PriorCost.Requests++
	if _, err := a.Run(t.Context(), req); err == nil {
		t.Fatal("over-accounted prior cost accepted")
	}
}

func TestFocusedResumeConfigurationValidation(t *testing.T) {
	for _, mode := range []string{"workspace", "model", "prompt", "system", "config", "fresh"} {
		t.Run(mode, func(t *testing.T) {
			req := focusedTestRequest(t)
			fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
				return focusedTestEmit(child, nil)
			})
			a := Focused{Agent: fake, Config: "high"}
			first, err := a.Run(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			req.PriorCost = first.Cost
			req.Resuming = true
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
			case "fresh":
				req.ReviewID = "fresh-root"
				req.PriorCost = findings.Cost{}
				req.Resuming = false
			}

			result, err := a.Run(context.Background(), req)
			if mode == "fresh" {
				if err != nil || !result.Cost.Recorded() {
					t.Fatalf("fresh run: result=%+v err=%v", result, err)
				}
			} else if err == nil {
				t.Fatal("invalid resume accepted")
			}
		})
	}
}

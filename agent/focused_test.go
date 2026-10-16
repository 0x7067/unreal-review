package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestFocusedRestoresCanonicalLocationForKnownVerifierID(t *testing.T) {
	candidate := focusedTestIssue("known-span")
	moved := candidate
	moved.StartLine = 6
	moved.EndLine = 7
	moved.Anchor = findings.AnchorOld
	moved.Severity = findings.SeverityWarning
	moved.Body = "Sharper confirmed body"
	req := focusedTestRequest(t)
	fake := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
		switch {
		case strings.Contains(child.Prompt, "Verification and semantic deduplication"):
			return focusedTestEmit(child, []findings.Finding{moved})
		case strings.Contains(child.Prompt, "Discovery lens: correctness"):
			return focusedTestEmit(child, []findings.Finding{candidate})
		}
		return focusedTestEmit(child, nil)
	})
	if _, err := (Focused{Agent: fake}).Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 {
		t.Fatalf("published %+v", report.Findings)
	}
	got := report.Findings[0]
	if got.ID != "known-span" || got.Path != "a.go" || got.StartLine != 4 || got.EndLine != 4 || got.Anchor != findings.AnchorNew || got.Severity != findings.SeverityWarning || got.Body != "Sharper confirmed body" {
		t.Fatalf("published %+v", got)
	}

	req = focusedTestRequest(t)
	stray := focusedTestIssue("not-a-candidate")
	stray.Path = "elsewhere.go"
	stray.StartLine = 2
	stray.EndLine = 3
	stray.Anchor = findings.AnchorOld
	invent := focusedTestRunner(func(_ context.Context, child review.AgentRequest) (review.AgentResult, error) {
		switch {
		case strings.Contains(child.Prompt, "Verification and semantic deduplication"):
			return focusedTestEmit(child, []findings.Finding{stray})
		case strings.Contains(child.Prompt, "Discovery lens: correctness"):
			return focusedTestEmit(child, []findings.Finding{candidate})
		}
		return focusedTestEmit(child, nil)
	})
	if _, err := (Focused{Agent: invent}).Run(t.Context(), req); err == nil {
		t.Fatal("published a verifier id at an unknown location")
	}
	if _, err := os.Stat(req.FindingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published a verifier id at an unknown location: %v", err)
	}
}

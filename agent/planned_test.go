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
	"sync/atomic"
	"testing"
	"time"

	"github.com/0x7067/unreal-review/findings"
	"github.com/0x7067/unreal-review/review"
)

type plannedTestAgent func(context.Context, review.AgentRequest) (review.AgentResult, error)

func (fn plannedTestAgent) Run(ctx context.Context, r review.AgentRequest) (review.AgentResult, error) {
	return fn(ctx, r)
}

func plannedTestRequest(t *testing.T) review.AgentRequest {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	return review.AgentRequest{
		Workspace:    dir,
		ReviewID:     "planned-root",
		Model:        "fake",
		Prompt:       "Selected source: working tree from base abc",
		SystemPrompt: "Evidence only",
		FindingsPath: filepath.Join(dir, "public.jsonl"),
		Plan: &review.ReviewPlan{
			Version:   "test",
			Digest:    "plan-digest",
			DiffSHA:   "source-sha",
			DiffBytes: 2,
			Tasks: []review.PlanTask{
				{ID: "local-a", Kind: "local", Prompt: "Review local a", Spans: []review.DiffSpan{{Start: 0, End: 1}}},
				{ID: "local-b", Kind: "local", Prompt: "Review local b", Spans: []review.DiffSpan{{Start: 1, End: 2}}},
				{ID: "boundary", Kind: "boundary", Prompt: "Review contracts across local a and b"},
			},
		},
	}
}

func plannedTestIssue(id string) findings.Finding {
	return findings.Finding{
		ID:        id,
		Path:      "a.go",
		StartLine: 4,
		EndLine:   4,
		Anchor:    findings.AnchorNew,
		Severity:  findings.SeverityError,
		Body:      "Confirmed defect " + id,
	}
}

func plannedTestEmit(r review.AgentRequest, items []findings.Finding) (review.AgentResult, error) {
	summary := findings.CleanVerdict
	if len(items) > 0 {
		summary = "Confirmed a defect"
	}
	err := focusedWriteReport(r.FindingsPath, findings.Report{Findings: items, Summary: summary})
	return review.AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1, AmountUSD: 1, InputTokens: 10}}, err
}

func TestPlannedRejectsConfigurationChangesOnResume(t *testing.T) {
	for _, mode := range []string{"model", "plan", "config", "nil-plan"} {
		t.Run(mode, func(t *testing.T) {
			req := plannedTestRequest(t)
			fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
				return plannedTestEmit(r, nil)
			})
			p := Planned{Agent: fake, Verifier: fake, Consolidator: fake, Config: "single/high"}
			first, err := p.Run(t.Context(), req)
			if err != nil {
				t.Fatal(err)
			}
			req.Resuming = true
			req.PriorCost = first.Cost
			switch mode {
			case "model":
				req.Model = "changed"
			case "plan":
				req.Plan.Tasks[0].Prompt += " changed"
			case "config":
				p.Config = "focused/high"
			case "nil-plan":
				req.Plan = nil
			}

			result, err := p.Run(t.Context(), req)
			if err == nil || result.Coverage != nil {
				t.Fatalf("invalid resume accepted: %+v", result)
			}
		})
	}
}

func TestPlannedWholeCallTimeout(t *testing.T) {
	req := plannedTestRequest(t)
	fake := plannedTestAgent(func(ctx context.Context, r review.AgentRequest) (review.AgentResult, error) {
		<-ctx.Done()
		result, _ := plannedTestEmit(r, nil)
		return result, ctx.Err()
	})

	result, err := (Planned{Agent: fake, Verifier: fake, Consolidator: fake, Timeout: 20 * time.Millisecond}).Run(t.Context(), req)
	if !errors.Is(err, context.DeadlineExceeded) || result.Coverage != nil {
		t.Fatalf("deadline result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(req.FindingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("published before complete coverage")
	}
}

func TestVerifierWallCapPublishesCandidates(t *testing.T) {
	kept := plannedTestIssue("kept")
	also := plannedTestIssue("also")
	also.StartLine, also.EndLine = 8, 8
	also.Body = "Second defect"
	weak := plannedTestIssue("weak")
	weak.StartLine, weak.EndLine = 12, 12
	weak.Severity = findings.SeverityWarning
	weak.Body = "Weak candidate"
	candidates := []findings.Finding{kept, also, weak}
	consolidator := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		items, err := plannedTestCandidates(r.Prompt)
		if err != nil {
			return review.AgentResult{}, err
		}
		return plannedTestEmit(r, items)
	})
	run := func(t *testing.T, verifier plannedTestAgent) findings.Report {
		t.Helper()
		req := plannedTestRequest(t)
		discovery := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
			if strings.Contains(r.Prompt, "local a") {
				return plannedTestEmit(r, candidates)
			}
			return plannedTestEmit(r, nil)
		})
		result, err := (Planned{
			Agent: discovery, Verifier: verifier, Consolidator: consolidator,
			verifierWall: 200 * time.Millisecond,
		}).Run(t.Context(), req)
		if err != nil || result.Coverage == nil {
			t.Fatalf("wall result=%+v err=%v", result, err)
		}
		report, err := findings.ReadFile(req.FindingsPath)
		if err != nil {
			t.Fatal(err)
		}
		return report
	}
	t.Run("partial", func(t *testing.T) {
		report := run(t, func(ctx context.Context, r review.AgentRequest) (review.AgentResult, error) {
			items, err := plannedTestCandidates(r.Prompt)
			if err != nil {
				return review.AgentResult{}, err
			}
			for _, item := range items {
				if item.ID != "kept" {
					continue
				}
				verified := item
				verified.Body = "verified kept"
				if err := focusedWriteReport(r.FindingsPath, findings.Report{Findings: []findings.Finding{verified}, Summary: "verified one"}); err != nil {
					return review.AgentResult{}, err
				}
			}
			<-ctx.Done()
			return review.AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1}}, ctx.Err()
		})
		got := map[string]findings.Finding{}
		for _, item := range report.Findings {
			got[item.ID] = item
		}
		if len(report.Findings) != 1 || got["kept"].Body != "verified kept" || got["kept"].Severity != findings.SeverityError || got["also"].ID != "" || got["weak"].ID != "" {
			t.Fatalf("published findings = %+v", report.Findings)
		}
	})
	t.Run("none", func(t *testing.T) {
		report := run(t, func(ctx context.Context, _ review.AgentRequest) (review.AgentResult, error) {
			<-ctx.Done()
			return review.AgentResult{}, ctx.Err()
		})
		assertWallNotes(t, report)
	})
	t.Run("corrupt", func(t *testing.T) {
		report := run(t, func(ctx context.Context, r review.AgentRequest) (review.AgentResult, error) {
			if err := os.WriteFile(r.FindingsPath, []byte("{\"type\":\"finding\",\"id\":\"kept\"\n"), 0o600); err != nil {
				return review.AgentResult{}, err
			}
			<-ctx.Done()
			return review.AgentResult{}, ctx.Err()
		})
		assertWallNotes(t, report)
	})
}

func assertWallNotes(t *testing.T, report findings.Report) {
	t.Helper()
	got := map[string]findings.Finding{}
	for _, item := range report.Findings {
		got[item.ID] = item
	}
	if len(report.Findings) != 2 || got["kept"].Severity != findings.SeverityNote || got["kept"].Body != "Confirmed defect kept" || got["also"].Severity != findings.SeverityNote || got["also"].Body != "Second defect" || got["weak"].ID != "" {
		t.Fatalf("published findings = %+v", report.Findings)
	}
}

func TestVerifierWallStopResumes(t *testing.T) {
	req := plannedTestRequest(t)
	issue := plannedTestIssue("kept")
	var calls atomic.Int32
	discovery := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		calls.Add(1)
		if strings.Contains(r.Prompt, "local a") {
			return plannedTestEmit(r, []findings.Finding{issue})
		}
		return plannedTestEmit(r, nil)
	})
	verifier := plannedTestAgent(func(ctx context.Context, _ review.AgentRequest) (review.AgentResult, error) {
		calls.Add(1)
		<-ctx.Done()
		return review.AgentResult{}, ctx.Err()
	})
	consolidator := plannedTestAgent(func(_ context.Context, _ review.AgentRequest) (review.AgentResult, error) {
		t.Errorf("consolidator ran for a single finding")
		return review.AgentResult{}, fmt.Errorf("consolidator ran")
	})
	p := Planned{Agent: discovery, Verifier: verifier, Consolidator: consolidator, verifierWall: 200 * time.Millisecond}
	first, err := p.Run(t.Context(), req)
	if err != nil || first.Coverage == nil {
		t.Fatalf("wall result=%+v err=%v", first, err)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].ID != "kept" || report.Findings[0].Severity != findings.SeverityNote || report.Findings[0].Body != issue.Body {
		t.Fatalf("published findings = %+v", report.Findings)
	}
	after := calls.Load()
	req.Resuming = true
	req.PriorCost = first.Cost
	again, err := p.Run(t.Context(), req)
	if err != nil || again.Coverage == nil {
		t.Fatalf("resume result=%+v err=%v", again, err)
	}
	if calls.Load() != after {
		t.Fatalf("resume reran children: calls %d -> %d", after, calls.Load())
	}
	resumed, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(resumed.Findings) != 1 || resumed.Findings[0].ID != "kept" || resumed.Findings[0].Severity != findings.SeverityNote || resumed.Findings[0].Body != issue.Body {
		t.Fatalf("resumed findings = %+v", resumed.Findings)
	}
}

func TestPlannedForwardsUnplannedReview(t *testing.T) {
	req := plannedTestRequest(t)
	req.Plan = nil
	fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		return plannedTestEmit(r, []findings.Finding{plannedTestIssue("direct")})
	})

	result, err := (Planned{Agent: fake}).Run(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage != nil || !result.Cost.Recorded() {
		t.Fatalf("unplanned result = %+v", result)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Findings) != 1 || report.Findings[0].ID != "direct" || report.Summary != "Confirmed a defect" {
		t.Fatalf("unplanned report = %+v", report)
	}
}

func TestPlannedConsolidationReshufflesCrossBatchDuplicates(t *testing.T) {
	req := plannedTestRequest(t)
	duplicateA := plannedTestIssue("dup-a")
	duplicateB := plannedTestIssue("dup-b")
	duplicateB.Body = "Duplicate candidate for issue dup-a on the same line"
	pad := func(tag string, n int) string {
		return strings.Repeat(fmt.Sprintf("Distinct confirmed defect %s padding token%d ", tag, n), 40)
	}
	localA := []findings.Finding{duplicateA, duplicateB}
	for i := range 260 {
		filler := plannedTestIssue(fmt.Sprintf("filler-a%03d", i))
		filler.Body = pad("a", i)
		localA = append(localA, filler)
	}
	localB := []findings.Finding{}
	for i := range 200 {
		filler := plannedTestIssue(fmt.Sprintf("filler-b%03d", i))
		filler.Body = pad("b", i)
		localB = append(localB, filler)
	}
	union := append(append([]findings.Finding{}, localA...), localB...)
	groups, err := plannedBatches(union)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) < 2 {
		t.Fatal("test fixture: fillers should split the union into multiple batches")
	}
	var mergeMu sync.Mutex
	var stageMu sync.Mutex
	stage := 0
	merges := 0
	fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		switch {
		case strings.Contains(r.Prompt, "verify:"):
			items, err := plannedTestCandidates(r.Prompt)
			if err != nil {
				return review.AgentResult{}, err
			}
			return plannedTestEmit(r, items)
		case strings.Contains(r.Prompt, "consolidate:"):
			items, err := plannedTestCandidates(r.Prompt)
			if err != nil {
				return review.AgentResult{}, err
			}
			hasA, hasB := false, false
			for _, item := range items {
				if item.ID == "dup-a" {
					hasA = true
				}
				if item.ID == "dup-b" {
					hasB = true
				}
			}
			kept := []findings.Finding{}
			for _, item := range items {
				if hasA && hasB && item.ID == "dup-b" {
					mergeMu.Lock()
					merges++
					mergeMu.Unlock()
					continue
				}
				kept = append(kept, item)
			}
			return plannedTestEmit(r, kept)
		}
		stageMu.Lock()
		stage++
		discovery := stage
		stageMu.Unlock()
		if discovery == 1 {
			return plannedTestEmit(r, localA)
		}
		return plannedTestEmit(r, localB)
	})

	if _, err := (Planned{Agent: fake, Verifier: fake, Consolidator: fake}).Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	bodies := map[string]int{}
	for _, item := range report.Findings {
		bodies[strings.Join(strings.Fields(item.Body), " ")]++
	}
	if n := bodies["Confirmed defect dup-a"]; n != 1 {
		t.Fatalf("cross-batch duplicate reached the public report %d times: %+v", n, report.Findings)
	}
	if merges == 0 {
		t.Fatal("no consolidation batch ever merged")
	}
}

func plannedTestCandidates(prompt string) ([]findings.Finding, error) {
	_, raw, ok := strings.Cut(prompt, "Candidate JSON:\n")
	if !ok {
		return nil, fmt.Errorf("test: no candidate JSON in prompt")
	}
	items := []findings.Finding{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &items); err != nil {
		return nil, err
	}
	return items, nil
}

func TestPlannedPublishesVerifierRestatementsWithoutCandidateIDs(t *testing.T) {
	req := plannedTestRequest(t)
	race := plannedTestIssue("race-candidate")
	leak := plannedTestIssue("leak-candidate")
	leak.Path, leak.StartLine, leak.EndLine = "b.go", 9, 9
	leak.Body = "Confirmed leak"
	fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		if !strings.Contains(r.Prompt, "verify:") && !strings.Contains(r.Prompt, "consolidate:") {
			if strings.Contains(r.Prompt, "local a") {
				return plannedTestEmit(r, []findings.Finding{race, leak})
			}
			return plannedTestEmit(r, nil)
		}
		items, err := plannedTestCandidates(r.Prompt)
		if err != nil {
			return review.AgentResult{}, err
		}
		out := append([]findings.Finding{}, items...)
		if strings.Contains(r.Prompt, "verify:") {
			for _, item := range items {
				restated := item
				restated.ID = "derived-" + item.ID
				out = append(out, restated)
			}
		}
		return plannedTestEmit(r, out)
	})
	if _, err := (Planned{Agent: fake, Verifier: fake, Consolidator: fake}).Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, item := range report.Findings {
		got[item.Path+":"+item.Body]++
	}
	if len(report.Findings) != 2 || got["a.go:Confirmed defect race-candidate"] != 1 || got["b.go:Confirmed leak"] != 1 {
		t.Fatalf("published %+v", report.Findings)
	}
}

func TestPlannedRebindsVerifierRecordThatDroppedItsCandidateID(t *testing.T) {
	candidate := plannedTestIssue("only-candidate")
	for _, body := range []string{"Sharper confirmed body", "[derived] Sharper confirmed body"} {
		t.Run(body, func(t *testing.T) {
			req := plannedTestRequest(t)
			fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
				switch {
				case strings.Contains(r.Prompt, "verify:"):
					kept := candidate
					kept.ID = "model-derived"
					kept.Body = body
					return plannedTestEmit(r, []findings.Finding{kept})
				case strings.Contains(r.Prompt, "local a"):
					return plannedTestEmit(r, []findings.Finding{candidate})
				}
				return plannedTestEmit(r, nil)
			})
			if _, err := (Planned{Agent: fake, Verifier: fake, Consolidator: fake}).Run(t.Context(), req); err != nil {
				t.Fatal(err)
			}
			report, err := findings.ReadFile(req.FindingsPath)
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Findings) != 1 || report.Findings[0].ID != "only-candidate" || report.Findings[0].Body != body || report.Findings[0].Path != "a.go" {
				t.Fatalf("published %+v", report.Findings)
			}
		})
	}

	req := plannedTestRequest(t)
	stray := plannedTestIssue("stray")
	stray.Path = "elsewhere.go"
	invent := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		switch {
		case strings.Contains(r.Prompt, "verify:"):
			return plannedTestEmit(r, []findings.Finding{stray})
		case strings.Contains(r.Prompt, "local a"):
			return plannedTestEmit(r, []findings.Finding{candidate})
		}
		return plannedTestEmit(r, nil)
	})
	if _, err := (Planned{Agent: invent, Verifier: invent, Consolidator: invent}).Run(t.Context(), req); err == nil {
		t.Fatal("published a verifier issue outside the candidate set")
	}
}

func TestPlannedRebindsTwoCandidatesAtTheSameLocation(t *testing.T) {
	req := plannedTestRequest(t)
	alpha := plannedTestIssue("alpha")
	beta := plannedTestIssue("beta")
	fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		switch {
		case strings.Contains(r.Prompt, "verify:"):
			items, err := plannedTestCandidates(r.Prompt)
			if err != nil {
				return review.AgentResult{}, err
			}
			out := make([]findings.Finding, 0, len(items))
			for _, item := range items {
				item.ID = "derived-" + item.ID
				out = append(out, item)
			}
			return plannedTestEmit(r, out)
		case strings.Contains(r.Prompt, "consolidate:"):
			items, err := plannedTestCandidates(r.Prompt)
			if err != nil {
				return review.AgentResult{}, err
			}
			return plannedTestEmit(r, items)
		case strings.Contains(r.Prompt, "local a"):
			return plannedTestEmit(r, []findings.Finding{alpha, beta})
		}
		return plannedTestEmit(r, nil)
	})
	if _, err := (Planned{Agent: fake, Verifier: fake, Consolidator: fake}).Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	report, err := findings.ReadFile(req.FindingsPath)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range report.Findings {
		if item.Path != "a.go" || item.StartLine != 4 || item.EndLine != 4 {
			t.Fatalf("published %+v", report.Findings)
		}
		got[item.ID] = item.Body
	}
	if len(report.Findings) != 2 || got["alpha"] != "Confirmed defect alpha" || got["beta"] != "Confirmed defect beta" {
		t.Fatalf("published %+v", report.Findings)
	}
}

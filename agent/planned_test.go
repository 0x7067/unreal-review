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

func TestPlannedRestoresCanonicalLocationForKnownVerifierID(t *testing.T) {
	candidate := plannedTestIssue("known-span")
	moved := candidate
	moved.StartLine = 6
	moved.EndLine = 7
	moved.Anchor = findings.AnchorOld
	moved.Severity = findings.SeverityWarning
	moved.Body = "Sharper confirmed body"

	req := plannedTestRequest(t)
	fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		switch {
		case strings.Contains(r.Prompt, "verify:"):
			return plannedTestEmit(r, []findings.Finding{moved})
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
	if len(report.Findings) != 1 {
		t.Fatalf("published %+v", report.Findings)
	}
	got := report.Findings[0]
	if got.ID != "known-span" || got.Path != "a.go" || got.StartLine != 4 || got.EndLine != 4 || got.Anchor != findings.AnchorNew || got.Severity != findings.SeverityWarning || got.Body != "Sharper confirmed body" {
		t.Fatalf("published %+v", got)
	}

	req = plannedTestRequest(t)
	stray := plannedTestIssue("not-a-candidate")
	stray.Path = "elsewhere.go"
	stray.StartLine = 2
	stray.EndLine = 3
	stray.Anchor = findings.AnchorOld
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
		t.Fatal("published a verifier id at an unknown location")
	}
	if _, err := os.Stat(req.FindingsPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("published a verifier id at an unknown location: %v", err)
	}
}

func plannedRunVerified(t *testing.T, discovered, verified []findings.Finding) (string, error) {
	t.Helper()
	req := plannedTestRequest(t)
	fake := plannedTestAgent(func(_ context.Context, r review.AgentRequest) (review.AgentResult, error) {
		switch {
		case strings.Contains(r.Prompt, "verify:"):
			return plannedTestEmit(r, verified)
		case strings.Contains(r.Prompt, "local a"):
			return plannedTestEmit(r, discovered)
		}
		return plannedTestEmit(r, nil)
	})
	_, err := (Planned{Agent: fake, Verifier: fake, Consolidator: fake}).Run(t.Context(), req)
	return req.FindingsPath, err
}

func TestPlannedRejectsFarOrOtherFileVerifierSpan(t *testing.T) {
	candidate := plannedTestIssue("known-span")
	far := candidate
	far.StartLine, far.EndLine = 20, 22
	other := candidate
	other.Path = "other.go"
	for _, moved := range []findings.Finding{far, other} {
		path, err := plannedRunVerified(t, []findings.Finding{candidate}, []findings.Finding{moved})
		if err == nil {
			t.Fatalf("published span %+v", moved)
		}
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("published %+v: %v", moved, statErr)
		}
	}
}

func TestPlannedRejectsDuplicateKnownIDAtDifferentRawLocations(t *testing.T) {
	candidate := plannedTestIssue("known-span")
	nudged := candidate
	nudged.StartLine, nudged.EndLine = 6, 7
	path, err := plannedRunVerified(t, []findings.Finding{candidate}, []findings.Finding{candidate, nudged})
	if err == nil || !strings.Contains(err.Error(), "duplicate verifier ID") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("published merged duplicate: %v", statErr)
	}
}

func TestPlannedRejectsCrossCandidateRelabel(t *testing.T) {
	alpha := plannedTestIssue("alpha")
	beta := plannedTestIssue("beta")
	beta.StartLine, beta.EndLine = 6, 6
	relabel := beta
	relabel.ID = alpha.ID
	relabel.Body = "Written on the other candidate"
	path, err := plannedRunVerified(t, []findings.Finding{alpha, beta}, []findings.Finding{relabel})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("published relabeled candidate: %v", statErr)
	}
}

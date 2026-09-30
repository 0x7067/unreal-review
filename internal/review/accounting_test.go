package review

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"unreal-review/internal/findings"
)

type accountingAgentFunc func(context.Context, AgentRequest) (AgentResult, error)

func (f accountingAgentFunc) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	return f(ctx, req)
}

func TestRunPassesRootCostAndResumeStateToAdapter(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "findings.jsonl")
	firstCost := findings.Cost{AmountUSD: 0.01, Currency: "USD", InputTokens: 10, Requests: 1}
	secondCost := findings.Cost{AmountUSD: 0.02, Currency: "USD", InputTokens: 20, Requests: 2}
	var originalID string
	calls := 0
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		calls++
		switch calls {
		case 1:
			originalID = req.ReviewID
			if req.Resuming || req.PriorCost.Recorded() {
				t.Fatalf("fresh request: resuming=%v cost=%+v", req.Resuming, req.PriorCost)
			}
			return AgentResult{Cost: firstCost}, errors.New("stage failed")
		case 2:
			if !req.Resuming || req.ReviewID != originalID || req.PriorCost != firstCost {
				t.Fatalf("resumed request: %+v", req)
			}
		case 3:
			if req.Resuming || req.ReviewID == originalID || req.PriorCost.Recorded() {
				t.Fatalf("--fresh request: %+v", req)
			}
		default:
			t.Fatalf("unexpected call %d", calls)
		}
		if err := findings.AppendSummary(req.FindingsPath, "No material issues in the changed text."); err != nil {
			return AgentResult{}, err
		}
		return AgentResult{Cost: secondCost}, nil
	})
	opts := Options{Workspace: dir, Out: out, Model: "test", Agent: backend}
	first, err := Run(t.Context(), opts)
	if err == nil || first.Report.Run.Status != findings.StatusFailed || first.Report.Run.Cost != firstCost {
		t.Fatalf("first run: %+v err=%v", first, err)
	}
	second, err := Run(t.Context(), opts)
	if err != nil || second.Report.Run.Status != findings.StatusComplete || second.Report.Run.Cost != firstCost.Add(secondCost) {
		t.Fatalf("second run: %+v err=%v", second, err)
	}
	opts.Fresh = true
	fresh, err := Run(t.Context(), opts)
	if err != nil || fresh.Report.Run.Cost != secondCost {
		t.Fatalf("fresh run: %+v err=%v", fresh, err)
	}
}

package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

type resumeFixtureAgent struct {
	t     *testing.T
	calls int
	id    string
}

func (a *resumeFixtureAgent) Run(_ context.Context, req review.AgentRequest) (review.AgentResult, error) {
	a.t.Helper()
	a.calls++
	cost := review.AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1}}
	if strings.Contains(req.Prompt, "findings.jsonl") {
		a.t.Fatal("eval-owned checkpoint leaked into reviewed diff")
	}
	if a.calls == 1 {
		if req.Resuming {
			a.t.Fatal("fresh fixture unexpectedly resuming")
		}
		a.id = req.ReviewID
		return cost, context.DeadlineExceeded
	}
	if !req.Resuming || req.ReviewID != a.id || req.PriorCost.Requests != 1 {
		a.t.Fatalf("continuation lost root binding/accounting: %+v", req)
	}
	if err := os.WriteFile(req.FindingsPath, []byte("{\"v\":1,\"type\":\"summary\",\"body\":\"No material issues: fixture continuation completed.\"}\n"), 0o600); err != nil {
		a.t.Fatal(err)
	}
	return cost, nil
}

func TestPlantedCheckpointDoesNotChangeSourceOnResume(t *testing.T) {
	dir := t.TempDir()
	if err := setup(t.Context(), Corpus[0], dir); err != nil {
		t.Fatal(err)
	}
	agent := &resumeFixtureAgent{t: t}
	opts := review.Options{Workspace: dir, Out: filepath.Join(dir, "findings.jsonl"), Model: "test", Agent: agent}
	paused, err := review.Run(t.Context(), opts)
	if err == nil || paused.Report.Run == nil || paused.Report.Run.Status != findings.StatusRunning {
		t.Fatalf("expected paused checkpoint, got %+v, %v", paused, err)
	}
	for _, path := range []string{opts.Out, opts.Out + ".work"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	completed, err := review.Run(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if !completed.Report.Complete() || agent.calls != 2 || !paused.Report.Run.Source.SameDiff(completed.Report.Run.Source) || completed.Report.Run.Cost.Requests != 2 {
		t.Fatalf("continuation changed source or cost: paused=%+v complete=%+v calls=%d", paused, completed, agent.calls)
	}
}

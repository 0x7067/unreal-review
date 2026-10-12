package review

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/findings"
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

func TestRunPassesHeadSHAOnlyForACommittedRange(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var head string
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		head = req.HeadSHA
		return AgentResult{}, errors.New("stop")
	})
	if _, err := Run(t.Context(), Options{Workspace: dir, Out: filepath.Join(t.TempDir(), "work.jsonl"), Model: "test", Agent: backend}); err == nil {
		t.Fatal("working-tree run returned no error")
	}
	if head != "" {
		t.Fatalf("working-tree HeadSHA = %q", head)
	}
	gitRun(t, dir, "add", "change.txt")
	gitRun(t, dir, "commit", "-q", "-m", "head")
	shaOut, err := exec.CommandContext(t.Context(), "git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(shaOut))
	if _, err := Run(t.Context(), Options{Workspace: dir, Spec: Spec{Commit: "HEAD"}, Out: filepath.Join(t.TempDir(), "commit.jsonl"), Model: "test", Agent: backend}); err == nil {
		t.Fatal("commit run returned no error")
	}
	if head != sha {
		t.Fatalf("commit HeadSHA = %q, want %s", head, sha)
	}
}

func TestStdoutCancelKeepsRunningAndReturnsCanceled(t *testing.T) {
	dir := gitRepo(t)
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backend := accountingAgentFunc(func(context.Context, AgentRequest) (AgentResult, error) {
		return AgentResult{}, context.Canceled
	})
	result, err := Run(t.Context(), Options{Workspace: dir, Out: "-", Model: "test", Agent: backend})
	if result.Report.Run == nil || result.Report.Run.Status != findings.StatusRunning || !errors.Is(err, context.Canceled) {
		t.Fatalf("stdout cancel: %+v err=%v", result, err)
	}
}

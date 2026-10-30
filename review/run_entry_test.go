package review

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/findings"
)

const emptyDiffSHA = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

func TestRunEmptySelectionWritesCleanFindings(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "note.txt", "same\n")
	commitAll(t, dir, "base")
	out := filepath.Join(t.TempDir(), "findings.jsonl")

	report, err := Run(t.Context(), Options{Workspace: dir, Out: out, Model: "test-model"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != "No material issues: the selected range has no changes." {
		t.Fatalf("summary=%q", report.Summary)
	}
	if report.Run == nil || report.Run.Status != findings.StatusComplete || report.Run.Model != "test-model" {
		t.Fatalf("run=%+v", report.Run)
	}
	if report.Run.Source.Base != "HEAD" || report.Run.Source.Head != "" || report.Run.Source.BaseSHA != report.Run.Source.HeadSHA || report.Run.Source.DiffSHA != emptyDiffSHA {
		t.Fatalf("source=%+v", report.Run.Source)
	}
	head := strings.TrimSpace(gitOutput(t, dir, "rev-parse", "HEAD"))
	if report.Run.Source.BaseSHA != head {
		t.Fatalf("base sha=%s head=%s", report.Run.Source.BaseSHA, head)
	}
	stored, err := findings.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Summary != report.Summary || stored.Run == nil || stored.Run.ID != report.Run.ID || stored.Run.Status != findings.StatusComplete || stored.Run.Source.DiffSHA != emptyDiffSHA {
		t.Fatalf("stored=%+v", stored)
	}
}

func TestRunKeepsTheSessionUnderTheInjectedDirectory(t *testing.T) {
	dir := gitRepo(t)
	writeRepoFile(t, dir, "note.txt", "before\n")
	commitAll(t, dir, "base")
	writeRepoFile(t, dir, "note.txt", "after\n")
	out := filepath.Join(t.TempDir(), "findings.jsonl")
	session := filepath.Join(t.TempDir(), "sessions")
	const api = "http://127.0.0.1:9/api/v1"
	backend := accountingAgentFunc(func(_ context.Context, req AgentRequest) (AgentResult, error) {
		if err := os.MkdirAll(req.SessionDir, 0o700); err != nil {
			return AgentResult{}, err
		}
		if err := os.WriteFile(filepath.Join(req.SessionDir, "review-id"), []byte(req.ReviewID), 0o600); err != nil {
			return AgentResult{}, err
		}
		if err := os.WriteFile(filepath.Join(req.SessionDir, "openrouter-api"), []byte(req.OpenRouterAPI), 0o600); err != nil {
			return AgentResult{}, err
		}
		if err := findings.AppendSummary(req.FindingsPath, "No material issues in the changed note."); err != nil {
			return AgentResult{}, err
		}
		return AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1, AmountUSD: 0.01}}, nil
	})

	report, err := Run(t.Context(), Options{
		Workspace:     dir,
		Out:           out,
		Model:         "test-model",
		Agent:         backend,
		SessionDir:    session,
		OpenRouterAPI: api,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary != "No material issues in the changed note." || report.Run == nil || report.Run.Status != findings.StatusComplete {
		t.Fatalf("report=%+v", report)
	}
	id, err := os.ReadFile(filepath.Join(session, "review-id"))
	if err != nil {
		t.Fatal(err)
	}
	if string(id) != report.Run.ID {
		t.Fatalf("session review id %q, findings %q", id, report.Run.ID)
	}
	gotAPI, err := os.ReadFile(filepath.Join(session, "openrouter-api"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotAPI) != api {
		t.Fatalf("openrouter api %q", gotAPI)
	}
	stored, err := findings.ReadFile(out)
	if err != nil || stored.Summary != report.Summary || stored.Run == nil || stored.Run.ID != report.Run.ID || stored.Run.Status != findings.StatusComplete {
		t.Fatalf("stored=%+v err=%v", stored, err)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
	return string(out)
}

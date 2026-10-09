package eval

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/0x7067/unreal-review/internal/findings"
	"github.com/0x7067/unreal-review/internal/review"
)

type resumeAgent struct {
	calls int
}

func (a *resumeAgent) Run(ctx context.Context, req review.AgentRequest) (review.AgentResult, error) {
	a.calls++
	if a.calls == 1 {
		return review.AgentResult{}, context.Canceled
	}
	summary := findings.CleanVerdict + ": planted defect confirmed."
	finding := findings.Finding{
		ID:        "resume-1",
		Path:      "cache.go",
		StartLine: 1,
		EndLine:   2,
		Anchor:    findings.AnchorNew,
		Severity:  findings.SeverityError,
		Body:      "planted defect",
	}
	if err := findings.WriteFile(req.FindingsPath, findings.Report{Findings: []findings.Finding{finding}, Summary: summary}); err != nil {
		return review.AgentResult{}, err
	}
	return review.AgentResult{Cost: findings.Cost{Currency: "USD", Requests: 1, AmountUSD: 1, InputTokens: 10}}, nil
}

func TestRunResumesInterruptedPlantedCase(t *testing.T) {
	c := Case{
		Name:   "resume-case",
		Class:  "test",
		Base:   map[string]string{"cache.go": "package main\n\nfunc main() {}\n"},
		Change: map[string]string{"cache.go": "package main\n\nfunc main() { panic(\"bug\") }\n"},
		Gold:   []Gold{{Path: "cache.go", StartLine: 1, EndLine: 2, Severity: findings.SeverityError}},
	}
	agent := &resumeAgent{}
	root := t.TempDir()
	first, err := Run(t.Context(), c, root, Options{Agent: agent})
	if err == nil && first.Err == "" {
		t.Fatal("first run should fail like an interrupted review")
	}
	score, err := Run(t.Context(), c, root, Options{Agent: agent})
	if err != nil {
		t.Fatal(err)
	}
	if score.Matched != 1 {
		t.Fatalf("matched=%d, want 1 after resume", score.Matched)
	}
	if _, err := os.Stat(filepath.Join(root, c.Name, "findings.jsonl")); err != nil {
		t.Fatal(err)
	}
}

func TestPlantedCaseExcludesFindingsOnce(t *testing.T) {
	c := Case{
		Name:   "exclude-once",
		Base:   map[string]string{"cache.go": "package main\n\nfunc main() {}\n"},
		Change: map[string]string{"cache.go": "package main\n\nfunc main() { panic(\"bug\") }\n"},
	}
	root := t.TempDir()
	for i := 0; i < 2; i++ {
		if _, err := Run(t.Context(), c, root, Options{}); err != nil {
			t.Fatal(err)
		}
	}
	fixture := filepath.Join(root, c.Name)
	body, err := os.ReadFile(filepath.Join(fixture, ".git", "info", "exclude"))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, line := range strings.Split(string(body), "\n") {
		if line == "/findings.jsonl" || line == "/findings.jsonl.work" {
			counts[line]++
		}
	}
	if counts["/findings.jsonl"] != 1 || counts["/findings.jsonl.work"] != 1 {
		t.Fatalf("exclude contains /findings.jsonl %d times and /findings.jsonl.work %d times:\n%s",
			counts["/findings.jsonl"], counts["/findings.jsonl.work"], body)
	}
	gitCmd(t, fixture, "check-ignore", "-q", "findings.jsonl")
}

func TestFixturesIgnoreInheritedGitDir(t *testing.T) {
	sentinel := t.TempDir()
	gitCmd(t, sentinel, "init", "--template=", "-q")
	if err := os.WriteFile(filepath.Join(sentinel, "keep.txt"), []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, sentinel, "add", "keep.txt")
	gitCmd(t, sentinel, "-c", "user.name=eval", "-c", "user.email=eval@invalid", "commit", "-q", "-m", "sentinel")
	t.Setenv("GIT_DIR", filepath.Join(sentinel, ".git"))

	c := Case{
		Name:   "git-dir",
		Base:   map[string]string{"cache.go": "package main\n\nfunc main() {}\n"},
		Change: map[string]string{"cache.go": "package main\n\nfunc main() { panic(\"bug\") }\n"},
	}
	root := t.TempDir()
	if _, err := Run(t.Context(), c, root, Options{}); err != nil {
		t.Fatal(err)
	}
	fixture := filepath.Join(root, c.Name)
	if _, err := os.Stat(filepath.Join(fixture, ".git", "HEAD")); err != nil {
		t.Errorf("planted fixture .git/HEAD: %v", err)
	} else if !gitIndexHas(t, fixture, "cache.go") {
		t.Errorf("planted fixture index does not list cache.go")
	}
	if gitIndexHas(t, sentinel, "cache.go") {
		t.Errorf("sentinel index gained cache.go")
	}

	dir := t.TempDir()
	if _, err := martianGit(t.Context(), dir, "init", "--template=", "-q"); err != nil {
		t.Fatal(err)
	}
	got, err := martianGit(t.Context(), dir, "rev-parse", "--git-dir")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, ".git")
	gotPath := got
	if !filepath.IsAbs(gotPath) {
		gotPath = filepath.Join(dir, gotPath)
	}
	if filepath.Clean(gotPath) != want {
		t.Fatalf("rev-parse --git-dir = %q, want %s", got, want)
	}
}

func gitCmd(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %s", strings.Join(args, " "), out)
	}
}

func gitIndexHas(t *testing.T, dir, name string) bool {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", "--git-dir", filepath.Join(dir, ".git"), "ls-files", "-z")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}
	for _, path := range strings.Split(string(out), "\x00") {
		if path == name {
			return true
		}
	}
	return false
}

func TestMatchOverlappingLines(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 21, EndLine: 21}}
	matched, severityHits, extra := match(gold, produced)
	if matched != 1 || severityHits != 0 || extra != 0 {
		t.Fatalf("matched=%d severityHits=%d extra=%d, want 1 0 0", matched, severityHits, extra)
	}
}

func TestMatchIgnoresDisjointLines(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 5, EndLine: 8}}
	matched, _, extra := match(gold, produced)
	if matched != 0 {
		t.Fatalf("matched=%d, want 0", matched)
	}
	if extra != 1 {
		t.Fatalf("extra=%d, want 1: an unrelated finding in the same file is not a hit", extra)
	}
}

func TestMatchRequiresSamePath(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{{Path: "other.go", StartLine: 20, EndLine: 23}}
	matched, _, extra := match(gold, produced)
	if matched != 0 || extra != 1 {
		t.Fatalf("matched=%d extra=%d, want 0 1", matched, extra)
	}
}

func TestMatchSpanningFindingCountsOnce(t *testing.T) {
	gold := []Gold{
		{Path: "cache.go", StartLine: 10, EndLine: 12, Severity: findings.SeverityError},
		{Path: "cache.go", StartLine: 20, EndLine: 22, Severity: findings.SeverityError},
	}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 10, EndLine: 22}}
	matched, severityHits, extra := match(gold, produced)
	if matched != 1 || severityHits != 0 || extra != 0 {
		t.Fatalf("matched=%d severityHits=%d extra=%d, want 1 0 0", matched, severityHits, extra)
	}
}

func TestMatchDuplicateFindingsCountAsExtra(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{
		{Path: "cache.go", StartLine: 21, EndLine: 21, Severity: findings.SeverityError},
		{Path: "cache.go", StartLine: 22, EndLine: 22, Severity: findings.SeverityError},
		{Path: "cache.go", StartLine: 22, EndLine: 23, Severity: findings.SeverityError},
	}
	matched, _, extra := match(gold, produced)
	if matched != 1 || extra != 2 {
		t.Fatalf("matched=%d extra=%d, want 1 2", matched, extra)
	}
}

func TestMatchCountsUnmatchedProducedAsExtra(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{
		{Path: "cache.go", StartLine: 20, EndLine: 23},
		{Path: "cache.go", StartLine: 27, EndLine: 31},
	}
	matched, _, extra := match(gold, produced)
	if matched != 1 || extra != 1 {
		t.Fatalf("matched=%d extra=%d, want 1 1", matched, extra)
	}
}

func TestMatchSeverityAgreesWhenEqual(t *testing.T) {
	gold := []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}
	produced := []findings.Finding{{Path: "cache.go", StartLine: 21, EndLine: 21, Severity: findings.SeverityError}}
	matched, severityHits, extra := match(gold, produced)
	if matched != 1 || severityHits != 1 || extra != 0 {
		t.Fatalf("matched=%d severityHits=%d extra=%d, want 1 1 0", matched, severityHits, extra)
	}
}

func TestMatchSeverityMissesWhenWeaker(t *testing.T) {
	gold := []Gold{{Path: "server.go", StartLine: 15, EndLine: 19, Severity: findings.SeverityWarning}}
	produced := []findings.Finding{{Path: "server.go", StartLine: 16, EndLine: 18, Severity: findings.SeverityError}}
	matched, severityHits, _ := match(gold, produced)
	if matched != 1 || severityHits != 0 {
		t.Fatalf("matched=%d severityHits=%d, want 1 0: finding the leak but grading it error is not agreement", matched, severityHits)
	}
}

func TestMatchSeverityGradesTheConsumingFinding(t *testing.T) {
	gold := []Gold{{Path: "server.go", StartLine: 15, EndLine: 19, Severity: findings.SeverityWarning}}
	produced := []findings.Finding{
		{Path: "server.go", StartLine: 15, EndLine: 19, Severity: findings.SeverityError},
		{Path: "server.go", StartLine: 16, EndLine: 16, Severity: findings.SeverityWarning},
	}
	matched, severityHits, extra := match(gold, produced)
	if matched != 1 || severityHits != 0 || extra != 1 {
		t.Fatalf("matched=%d severityHits=%d extra=%d, want 1 0 1", matched, severityHits, extra)
	}
}

func TestRecallIsOneWithoutGold(t *testing.T) {
	score := Score{Name: "clean"}
	if score.Recall() != 1 || score.SeverityAgreement() != 1 {
		t.Fatalf("recall=%f severity=%f, want 1 1 when nothing was planted", score.Recall(), score.SeverityAgreement())
	}
}

func TestPrecisionIsOneWithoutFindings(t *testing.T) {
	score := Score{Name: "clean", Gold: 0}
	if score.Precision() != 1 {
		t.Fatalf("precision=%f, want 1 when nothing was produced", score.Precision())
	}
}

func TestScoreReportReadsRunStatus(t *testing.T) {
	c := Case{Name: "race", Class: "concurrency", Gold: []Gold{{Path: "cache.go", StartLine: 20, EndLine: 23, Severity: findings.SeverityError}}}
	report := findings.Report{
		Run: &findings.Run{Status: findings.StatusComplete, Cost: findings.Cost{AmountUSD: 0.5, Requests: 2}},
		Findings: []findings.Finding{
			{Path: "cache.go", StartLine: 21, EndLine: 21, Severity: findings.SeverityError},
		},
	}
	score := scoreReport(c, report)
	if !score.Completed() {
		t.Fatal("want completed")
	}
	if score.Class != "concurrency" {
		t.Fatalf("class=%q", score.Class)
	}
	if score.Matched != 1 || score.SeverityHits != 1 || score.Produced != 1 || score.Requests != 2 || score.CostUSD != 0.5 {
		t.Fatalf("score=%+v", score)
	}
	if score.Status != "complete" {
		t.Fatalf("status=%q", score.Status)
	}
}

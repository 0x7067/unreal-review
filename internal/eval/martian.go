package eval

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"unreal-review/internal/findings"
	"unreal-review/internal/review"
)

//go:embed martian.json
var martianJSON []byte

type MartianComment struct {
	Comment  string `json:"comment"`
	Severity string `json:"severity"`
	Category string `json:"category"`
}

type MartianCase struct {
	Name     string           `json:"name"`
	Title    string           `json:"title"`
	URL      string           `json:"url"`
	Repo     string           `json:"repo"`
	Base     string           `json:"base"`
	Head     string           `json:"head"`
	Comments []MartianComment `json:"comments"`
}

var MartianSeverities = []string{"Critical", "High", "Medium", "Low"}

var MartianProfiles = []string{"strict", "core", "all"}

var martianProfiles = map[string][]string{
	"strict": {"bug", "security", "concurrency", "data", "api"},
	"core":   {"bug", "security", "concurrency", "data", "api", "perf", "test_gap", "doc_defect"},
	"all":    {"bug", "security", "concurrency", "data", "api", "perf", "test_gap", "doc_defect", "style", "speculative"},
}

func MartianCorpus() ([]MartianCase, error) {
	var cases []MartianCase
	if err := json.Unmarshal(martianJSON, &cases); err != nil {
		return nil, fmt.Errorf("martian corpus: %w", err)
	}
	return cases, nil
}

func MartianProfile(profile string) (map[string]bool, error) {
	categories, ok := martianProfiles[profile]
	if !ok {
		return nil, fmt.Errorf("unknown profile %q: use core, strict, or all", profile)
	}
	keep := map[string]bool{}
	for _, category := range categories {
		keep[category] = true
	}
	return keep, nil
}

func MartianSeverity(severity string) findings.Severity {
	switch severity {
	case "Critical", "High":
		return findings.SeverityError
	case "Medium":
		return findings.SeverityWarning
	default:
		return findings.SeverityNote
	}
}

type Pair struct {
	Golden  int `json:"golden"`
	Finding int `json:"finding"`
}

func ParsePairs(content string, golden, produced int) ([]Pair, error) {
	start, end := strings.Index(content, "{"), strings.LastIndex(content, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("judge reply has no JSON object")
	}
	var reply struct {
		Matches []Pair `json:"matches"`
	}
	if err := json.Unmarshal([]byte(content[start:end+1]), &reply); err != nil {
		return nil, fmt.Errorf("judge reply: %w", err)
	}
	usedGolden := map[int]bool{}
	usedFinding := map[int]bool{}
	var pairs []Pair
	for _, p := range reply.Matches {
		if p.Golden < 0 || p.Golden >= golden || p.Finding < 0 || p.Finding >= produced {
			continue
		}
		if usedGolden[p.Golden] || usedFinding[p.Finding] {
			continue
		}
		usedGolden[p.Golden], usedFinding[p.Finding] = true, true
		pairs = append(pairs, p)
	}
	return pairs, nil
}

type Tally struct {
	Gold         int `json:"gold"`
	Matched      int `json:"matched"`
	SeverityHits int `json:"severity_hits"`
}

type Counts struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	FN int `json:"fn"`
}

func (c Counts) Add(o Counts) Counts {
	return Counts{TP: c.TP + o.TP, FP: c.FP + o.FP, FN: c.FN + o.FN}
}

func (c Counts) Precision() float64 {
	return ratio(c.TP, c.TP+c.FP)
}

func (c Counts) Recall() float64 {
	return ratio(c.TP, c.TP+c.FN)
}

func (c Counts) F1() float64 {
	p, r := c.Precision(), c.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func ratio(hits, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

type MartianScore struct {
	Score
	Repo            string            `json:"repo"`
	URL             string            `json:"url"`
	MatchedExcluded int               `json:"matched_excluded"`
	BySeverity      map[string]Tally  `json:"by_severity"`
	ByProfile       map[string]Counts `json:"by_profile"`
	Pairs           []Pair            `json:"pairs"`
	JudgeCostUSD    float64           `json:"judge_cost_usd"`
	JudgeErr        string            `json:"judge_err,omitempty"`
}

func ScoreMartian(c MartianCase, report findings.Report, pairs []Pair, profile string) MartianScore {
	score := MartianScore{
		Score:      Score{Name: c.Name, Class: c.Repo, Produced: len(report.Findings), Extra: len(report.Findings) - len(pairs)},
		Repo:       c.Repo,
		URL:        c.URL,
		BySeverity: map[string]Tally{},
		ByProfile:  map[string]Counts{},
		Pairs:      pairs,
	}
	if report.Run != nil {
		score.Status = string(report.Run.Status)
		score.CostUSD = report.Run.Cost.AmountUSD
		score.Requests = report.Run.Cost.Requests
	}
	matched := map[int]int{}
	for _, p := range pairs {
		matched[p.Golden] = p.Finding
	}
	for _, name := range MartianProfiles {
		keep, _ := MartianProfile(name)
		counts := Counts{FP: score.Extra}
		for i, comment := range c.Comments {
			_, hit := matched[i]
			switch {
			case !keep[comment.Category]:
			case hit:
				counts.TP++
			default:
				counts.FN++
			}
		}
		score.ByProfile[name] = counts
	}
	keep, _ := MartianProfile(profile)
	for i, comment := range c.Comments {
		finding, hit := matched[i]
		if !keep[comment.Category] {
			if hit {
				score.MatchedExcluded++
			}
			continue
		}
		tally := score.BySeverity[comment.Severity]
		tally.Gold++
		score.Gold++
		if hit {
			tally.Matched++
			score.Matched++
			if report.Findings[finding].Severity == MartianSeverity(comment.Severity) {
				tally.SeverityHits++
				score.SeverityHits++
			}
		}
		score.BySeverity[comment.Severity] = tally
	}
	return score
}

type Judge struct {
	APIKey string
	Model  string
	Base   string
}

type Verdict struct {
	Pairs   []Pair
	CostUSD float64
}

const judgeQuestion = "For each golden comment, decide whether a candidate finding identifies the SAME underlying issue as the golden comment. Wording, line numbers, and severity may differ; the defect must be the same. Each finding matches at most one golden comment and each golden comment at most one finding. Reply with only JSON: {\"matches\":[{\"golden\":<index>,\"finding\":<index>}]}."

func judgePrompt(c MartianCase, produced []findings.Finding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nPull request: %s\n\nGolden comments:\n", judgeQuestion, c.Title)
	for i, comment := range c.Comments {
		fmt.Fprintf(&b, "[%d] %s\n", i, comment.Comment)
	}
	b.WriteString("\nCandidate findings:\n")
	for i, f := range produced {
		fmt.Fprintf(&b, "[%d] %s:%d-%d (%s) %s\n", i, f.Path, f.StartLine, f.EndLine, f.Severity, f.Body)
	}
	return b.String()
}

func (j Judge) Match(ctx context.Context, c MartianCase, produced []findings.Finding) (Verdict, error) {
	if len(c.Comments) == 0 || len(produced) == 0 {
		return Verdict{}, nil
	}
	body, err := json.Marshal(map[string]any{
		"model":    j.Model,
		"messages": []map[string]string{{"role": "user", "content": judgePrompt(c, produced)}},
		"usage":    map[string]bool{"include": true},
	})
	if err != nil {
		return Verdict{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(j.Base, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Verdict{}, err
	}
	req.Header.Set("Authorization", "Bearer "+j.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Verdict{}, fmt.Errorf("judge: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return Verdict{}, fmt.Errorf("judge: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Verdict{}, fmt.Errorf("judge: %s: %s", resp.Status, strings.TrimSpace(string(raw)))
	}
	var reply struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Cost float64 `json:"cost"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		return Verdict{}, fmt.Errorf("judge: %w", err)
	}
	if len(reply.Choices) == 0 {
		return Verdict{CostUSD: reply.Usage.Cost}, fmt.Errorf("judge: no choices")
	}
	pairs, err := ParsePairs(reply.Choices[0].Message.Content, len(c.Comments), len(produced))
	return Verdict{Pairs: pairs, CostUSD: reply.Usage.Cost}, err
}

func RunMartian(ctx context.Context, c MartianCase, root string, opts Options, judge Judge, profile string) (MartianScore, error) {
	dir := filepath.Join(root, c.Name)
	head, err := CheckoutMartian(ctx, c, dir)
	if err != nil {
		return MartianScore{Score: Score{Name: c.Name}}, fmt.Errorf("set up %s: %w", c.Name, err)
	}
	findingsPath := filepath.Join(root, c.Name+".findings.jsonl")
	start := time.Now()
	result, runErr := review.Run(ctx, review.Options{
		Workspace: dir,
		Spec:      review.Spec{Commit: head},
		Out:       findingsPath,
		Fresh:     true,
		Model:     opts.Model,
		Agent:     opts.Agent,
	})
	verdict, judgeErr := judge.Match(ctx, c, result.Report.Findings)
	score := ScoreMartian(c, result.Report, verdict.Pairs, profile)
	score.JudgeCostUSD = verdict.CostUSD
	score.DurationMS = time.Since(start).Milliseconds()
	score.FindingsPath = findingsPath
	if runErr != nil {
		score.Err = runErr.Error()
	}
	if judgeErr != nil {
		score.JudgeErr = judgeErr.Error()
	}
	return score, nil
}

func CheckoutMartian(ctx context.Context, c MartianCase, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	git := func(args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "user.name=eval", "-c", "user.email=eval@invalid"}, args...)...)
		cmd.Dir = dir
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		out, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(stderr.String()))
		}
		return strings.TrimSpace(string(out)), nil
	}
	steps := [][]string{
		{"init", "-q"},
		{"fetch", "-q", "--depth=1", "https://github.com/" + c.Repo + ".git", c.Head, c.Base},
	}
	for _, step := range steps {
		if _, err := git(step...); err != nil {
			return "", err
		}
	}
	base, err := git("commit-tree", c.Base+"^{tree}", "-m", "base")
	if err != nil {
		return "", err
	}
	head, err := git("commit-tree", c.Head+"^{tree}", "-p", base, "-m", c.Title)
	if err != nil {
		return "", err
	}
	if _, err := git("checkout", "-q", "--detach", head); err != nil {
		return "", err
	}
	return head, nil
}

func SumBySeverity(scores []MartianScore) map[string]Tally {
	sum := map[string]Tally{}
	for _, score := range scores {
		for severity, tally := range score.BySeverity {
			total := sum[severity]
			total.Gold += tally.Gold
			total.Matched += tally.Matched
			total.SeverityHits += tally.SeverityHits
			sum[severity] = total
		}
	}
	return sum
}

func SumProfiles(scores []MartianScore) map[string]Counts {
	sum := map[string]Counts{}
	for _, score := range scores {
		for profile, counts := range score.ByProfile {
			sum[profile] = sum[profile].Add(counts)
		}
	}
	return sum
}

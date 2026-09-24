package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"unreal-review/internal/diffmap"
	"unreal-review/internal/findings"
	"unreal-review/internal/github"
	"unreal-review/internal/render"
)

func cmdRender(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("render target required: github or markdown")
	}
	switch args[0] {
	case "github":
		return renderGitHub(args[1:])
	case "markdown":
		return renderMarkdown(args[1:])
	case "-h", "--help", "help":
		_, _ = fmt.Fprint(os.Stdout, `Usage:
  unreal-review render github [--pr owner/repo#n] [--dry-run] [findings.jsonl]
  unreal-review render markdown [findings.jsonl]
`)
		return nil
	default:
		return fmt.Errorf("unknown render target %q", args[0])
	}
}

func renderMarkdown(args []string) error {
	fs := flag.NewFlagSet("render markdown", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	outPath := fs.String("out", "-", "output path, or - for stdout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	report, err := loadReport(fs.Arg(0))
	if err != nil {
		return err
	}
	out, err := openOut(*outPath)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	return render.Markdown(out, report)
}

func renderGitHub(args []string) error {
	fs := flag.NewFlagSet("render github", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	prSpec := fs.String("pr", "", "pull request: owner/repo#n, a URL, or a number")
	repo := fs.String("repo", os.Getenv("GITHUB_REPOSITORY"), "owner/repo when --pr is a number")
	commit := fs.String("commit", "", "commit SHA for inline comments (default: PR head)")
	tokenFlag := fs.String("token", "", "GitHub token (default GH_TOKEN or GITHUB_TOKEN)")
	dryRun := fs.Bool("dry-run", false, "print the review payload instead of posting")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	report, err := loadReport(fs.Arg(0))
	if err != nil {
		return err
	}
	if !*dryRun && !report.Complete() {
		status := findings.StatusRunning
		if report.Run != nil && report.Run.Status != "" {
			status = report.Run.Status
		}
		return fmt.Errorf("findings are %s; resume the review before posting", status)
	}
	owner, name, number, err := resolvePR(*prSpec, *repo)
	if err != nil {
		return err
	}
	token := firstNonEmpty(*tokenFlag, os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"))
	if token == "" && !*dryRun {
		if out, authErr := exec.CommandContext(context.Background(), "gh", "auth", "token").Output(); authErr == nil {
			token = strings.TrimSpace(string(out))
		}
	}
	client := &github.Client{Token: token, HTTP: github.NewHTTPClient()}
	opts := render.GitHubOptions{
		Owner:      owner,
		Repo:       name,
		PullNumber: number,
		CommitID:   *commit,
	}
	if token != "" {
		ctx := context.Background()
		pr, err := client.GetPullRequest(ctx, owner, name, number)
		if err != nil {
			return err
		}
		if opts.CommitID == "" {
			opts.CommitID = pr.HeadSHA
		}
		files, err := client.ListPullFiles(ctx, owner, name, number)
		if err != nil {
			return err
		}
		lines := diffmap.New()
		for _, file := range files {
			if file.Patch == "" {
				continue
			}
			if err := diffmap.MergePatch(lines, file.Path, file.Patch); err != nil {
				return fmt.Errorf("parse patch for %s: %w", file.Path, err)
			}
		}
		opts.Lines = lines
		opts.HasLines = true
	}
	result := render.GitHub(report, opts)
	if *dryRun {
		enc := json.NewEncoder(os.Stdout)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		return enc.Encode(result.Payload)
	}
	if token == "" {
		return fmt.Errorf("set GH_TOKEN, GITHUB_TOKEN, or --token to post a review")
	}
	if err := client.CreateReview(context.Background(), result.Payload); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "posted %d inline comment(s)", len(result.Payload.Review.Comments))
	if len(result.Dropped) > 0 {
		fmt.Fprintf(os.Stderr, ", dropped %d", len(result.Dropped))
	}
	fmt.Fprintln(os.Stderr)
	if report.Run != nil {
		fmt.Fprintf(os.Stderr, "cost: %s\n", report.Run.Cost.Format())
	}
	return nil
}

func loadReport(path string) (findings.Report, error) {
	var (
		r   io.ReadCloser
		err error
	)
	if path == "" || path == "-" {
		r = io.NopCloser(os.Stdin)
	} else {
		r, err = os.Open(path)
		if err != nil {
			return findings.Report{}, fmt.Errorf("open %s: %w", path, err)
		}
	}
	defer func() { _ = r.Close() }()
	return findings.Parse(r)
}

func resolvePR(spec, repo string) (string, string, int, error) {
	defaultOwner, defaultRepo := "", ""
	if repo != "" {
		var err error
		defaultOwner, defaultRepo, err = github.SplitRepo(repo)
		if err != nil {
			return "", "", 0, err
		}
	}
	if spec == "" {
		spec = actionsPR()
	}
	if spec == "" {
		return "", "", 0, fmt.Errorf("set --pr")
	}
	return github.ParsePR(spec, defaultOwner, defaultRepo)
}

func actionsPR() string {
	path := os.Getenv("GITHUB_EVENT_PATH")
	if path == "" {
		return ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var event struct {
		Number int `json:"number"`
		PR     struct {
			Number int `json:"number"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(raw, &event); err != nil {
		return ""
	}
	n := event.Number
	if n == 0 {
		n = event.PR.Number
	}
	if n == 0 {
		return ""
	}
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return fmt.Sprintf("%d", n)
	}
	return fmt.Sprintf("%s#%d", repo, n)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"unreal-review/internal/github"
	"unreal-review/internal/render"
	"unreal-review/internal/review"
)

const checkName = "unreal-review"

type pullResolver struct {
	client       *github.Client
	defaultOwner string
	defaultRepo  string
}

func (p pullResolver) ResolvePull(ctx context.Context, spec string) (review.Pull, error) {
	owner, repo, number, err := github.ParsePR(spec, p.defaultOwner, p.defaultRepo)
	if err != nil {
		return review.Pull{}, err
	}
	state, err := p.client.PullState(ctx, owner, repo, number)
	if err != nil {
		return review.Pull{}, err
	}
	reviewed, err := p.reviewedHead(ctx, owner, repo, number, state.Commits)
	if err != nil {
		return review.Pull{}, err
	}
	return review.Pull{
		BaseRef:      state.BaseRef,
		BaseSHA:      state.BaseSHA,
		HeadSHA:      state.HeadSHA,
		ReviewedHead: reviewed,
		Reported:     render.ReportedFindings(state.Comments),
	}, nil
}

const maxReceiptWalk = 50

func (p pullResolver) reviewedHead(ctx context.Context, owner, repo string, number int, commits []string) (string, error) {
	walked := 0
	for _, sha := range commits {
		if walked == maxReceiptWalk {
			break
		}
		walked++
		ok, err := p.client.HasSuccessfulCheck(ctx, owner, repo, sha, checkName)
		if err != nil {
			return "", err
		}
		if ok {
			return sha, nil
		}
	}
	return "", nil
}

func newPullResolver(token, repo string) (review.PullResolver, error) {
	if token == "" {
		return nil, fmt.Errorf("set GH_TOKEN, GITHUB_TOKEN, or --token to review a pull request")
	}
	owner, name := "", ""
	if repo != "" {
		var err error
		owner, name, err = github.SplitRepo(repo)
		if err != nil {
			return nil, err
		}
	}
	return pullResolver{
		client:       &github.Client{Token: token, HTTP: github.NewHTTPClient()},
		defaultOwner: owner,
		defaultRepo:  name,
	}, nil
}

func resolveToken(flag string, allowGH bool) string {
	token := firstNonEmpty(flag, os.Getenv("GH_TOKEN"), os.Getenv("GITHUB_TOKEN"))
	if token == "" && allowGH {
		if out, err := exec.CommandContext(context.Background(), "gh", "auth", "token").Output(); err == nil {
			token = strings.TrimSpace(string(out))
		}
	}
	return token
}

func missingComments(ctx context.Context, client *github.Client, owner, repo string, number int, result render.GitHubResult) ([]string, error) {
	comments, err := client.ListReviewComments(ctx, owner, repo, number)
	if err != nil {
		return nil, err
	}
	landed := make(map[string]bool, len(comments))
	for _, comment := range comments {
		if id, ok := github.ParseFinding(comment.Body); ok {
			landed[id] = true
		}
	}
	var missing []string
	for _, comment := range result.Payload.Review.Comments {
		id, ok := github.ParseFinding(comment.Body)
		if ok && !landed[id] {
			missing = append(missing, fmt.Sprintf("%s %d", comment.Path, comment.Line))
		}
	}
	return missing, nil
}

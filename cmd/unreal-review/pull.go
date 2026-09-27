package main

import (
	"context"
	"fmt"
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
	for i := len(commits) - 1; i >= 0 && walked < maxReceiptWalk; i-- {
		walked++
		ok, err := p.client.HasSuccessfulCheck(ctx, owner, repo, commits[i], checkName)
		if err != nil {
			return "", err
		}
		if ok {
			return commits[i], nil
		}
	}
	return "", nil
}

func newPullResolver(token, repo string) (review.PullResolver, error) {
	if token == "" {
		return nil, fmt.Errorf("set GH_TOKEN or GITHUB_TOKEN to review a pull request")
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

func resolveToken(allowGH bool) string {
	token := firstNonEmpty(secret("GH_TOKEN"), secret("GITHUB_TOKEN"))
	if token == "" && allowGH {
		if out, err := exec.CommandContext(context.Background(), "gh", "auth", "token").Output(); err == nil {
			token = strings.TrimSpace(string(out))
		}
	}
	return token
}

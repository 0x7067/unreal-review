package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"unreal-review/internal/agent"
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
		BaseSHA:      state.BaseSHA,
		HeadSHA:      state.HeadSHA,
		ReviewedHead: reviewed,
		Title:        state.Title,
		Description:  state.Body,
		Reported:     render.HistoryOf(state).Reported(),
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
		return nil, fmt.Errorf("set GH_TOKEN to review a pull request")
	}
	owner, name := "", ""
	if repo != "" {
		var err error
		owner, name, err = github.SplitRepo(repo)
		if err != nil {
			return nil, err
		}
	}
	client, err := githubClient(token)
	if err != nil {
		return nil, err
	}
	return pullResolver{
		client:       client,
		defaultOwner: owner,
		defaultRepo:  name,
	}, nil
}

// githubClient talks to api.github.com unless UNREAL_REVIEW_GITHUB_API names
// a loopback origin (127.0.0.1, ::1, or localhost). The offline canary points
// that variable at a local server. An empty value keeps the public API. A
// non-loopback value is an error. The variable is not a credential, so secret
// reexec leaves it in the environment.
func githubClient(token string) (*github.Client, error) {
	base, err := agent.LoopbackBaseURL("UNREAL_REVIEW_GITHUB_API", os.Getenv("UNREAL_REVIEW_GITHUB_API"))
	if err != nil {
		return nil, err
	}
	httpClient := github.NewHTTPClient()
	if base != "" {
		httpClient = agent.LoopbackHTTPClient()
		httpClient.Timeout = 30 * time.Second
	}
	return &github.Client{
		Token:   token,
		BaseURL: base,
		HTTP:    httpClient,
	}, nil
}

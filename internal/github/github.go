package github

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultAPI = "https://api.github.com"

type Client struct {
	Token   string
	BaseURL string
	HTTP    *http.Client
}

type PullRequest struct {
	Number  int
	Owner   string
	Repo    string
	BaseRef string
	BaseSHA string
	HeadSHA string
	Title   string
}

type PullFile struct {
	Path     string
	Patch    string
	Status   string
	PrevPath string
}

type PostedComment struct {
	Path      string
	StartLine int
	EndLine   int
	Side      string
	Body      string
}

type IssueComment struct {
	ID   int64
	Body string
}

type PullState struct {
	BaseRef         string
	BaseSHA         string
	HeadSHA         string
	Status          Status
	StatusCommentID int64
	Comments        []PostedComment
	Commits         []string
}

type ReviewComment struct {
	Path      string `json:"path"`
	Body      string `json:"body"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
}

type Review struct {
	CommitID string          `json:"commit_id,omitempty"`
	Event    string          `json:"event"`
	Body     string          `json:"body"`
	Comments []ReviewComment `json:"comments,omitempty"`
}

type Payload struct {
	Owner      string `json:"owner"`
	Repo       string `json:"repo"`
	PullNumber int    `json:"pull_number"`
	Review     Review `json:"review"`
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimRight(c.BaseURL, "/")
	}
	return defaultAPI
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return http.DefaultClient
}

func (c *Client) GetPullRequest(ctx context.Context, owner, repo string, number int) (PullRequest, error) {
	var raw struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Base   struct {
			Ref string `json:"ref"`
			SHA string `json:"sha"`
		} `json:"base"`
		Head struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", owner, repo, number)
	if err := c.get(ctx, path, &raw); err != nil {
		return PullRequest{}, err
	}
	return PullRequest{
		Number:  raw.Number,
		Owner:   owner,
		Repo:    repo,
		BaseRef: raw.Base.Ref,
		BaseSHA: raw.Base.SHA,
		HeadSHA: raw.Head.SHA,
		Title:   raw.Title,
	}, nil
}

func (c *Client) PullState(ctx context.Context, owner, repo string, number int) (PullState, error) {
	pull, err := c.GetPullRequest(ctx, owner, repo, number)
	if err != nil {
		return PullState{}, err
	}
	state := PullState{BaseRef: pull.BaseRef, BaseSHA: pull.BaseSHA, HeadSHA: pull.HeadSHA}
	issue, err := c.ListIssueComments(ctx, owner, repo, number)
	if err != nil {
		return PullState{}, err
	}
	for _, comment := range issue {
		if status, ok := ParseStatus(comment.Body); ok {
			state.Status = status
			state.StatusCommentID = comment.ID
		}
	}
	comments, err := c.ListReviewComments(ctx, owner, repo, number)
	if err != nil {
		return PullState{}, err
	}
	state.Comments = comments
	commits, err := c.ListPullCommits(ctx, owner, repo, number)
	if err != nil {
		return PullState{}, err
	}
	state.Commits = commits
	return state, nil
}

func (c *Client) UpsertStatusComment(ctx context.Context, owner, repo string, number int, id int64, body string) error {
	if id != 0 {
		return c.patch(ctx, fmt.Sprintf("/repos/%s/%s/issues/comments/%d", owner, repo, id), commentBody{Body: body})
	}
	return c.post(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d/comments", owner, repo, number), commentBody{Body: body}, nil)
}

type commentBody struct {
	Body string `json:"body"`
}

func (c *Client) ListIssueComments(ctx context.Context, owner, repo string, number int) ([]IssueComment, error) {
	var out []IssueComment
	page := 1
	for {
		var raw []struct {
			ID   int64  `json:"id"`
			Body string `json:"body"`
		}
		path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100&page=%d", owner, repo, number, page)
		if err := c.get(ctx, path, &raw); err != nil {
			return nil, err
		}
		for _, item := range raw {
			out = append(out, IssueComment{ID: item.ID, Body: item.Body})
		}
		if len(raw) < 100 {
			return out, nil
		}
		page++
	}
}

func (c *Client) ListReviewComments(ctx context.Context, owner, repo string, number int) ([]PostedComment, error) {
	var out []PostedComment
	page := 1
	for {
		var raw []struct {
			Path              string  `json:"path"`
			Body              string  `json:"body"`
			Side              *string `json:"side"`
			OriginalSide      *string `json:"original_side"`
			Line              *int    `json:"line"`
			OriginalLine      *int    `json:"original_line"`
			StartLine         *int    `json:"start_line"`
			OriginalStartLine *int    `json:"original_start_line"`
		}
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/comments?per_page=100&page=%d", owner, repo, number, page)
		if err := c.get(ctx, path, &raw); err != nil {
			return nil, err
		}
		for _, item := range raw {
			end := firstInt(item.Line, item.OriginalLine)
			start := firstInt(item.StartLine, item.OriginalStartLine)
			if start == 0 {
				start = end
			}
			out = append(out, PostedComment{
				Path:      item.Path,
				StartLine: start,
				EndLine:   end,
				Side:      firstString(item.Side, item.OriginalSide),
				Body:      item.Body,
			})
		}
		if len(raw) < 100 {
			return out, nil
		}
		page++
	}
}

func firstInt(values ...*int) int {
	for _, value := range values {
		if value != nil && *value != 0 {
			return *value
		}
	}
	return 0
}

func firstString(values ...*string) string {
	for _, value := range values {
		if value != nil && *value != "" {
			return *value
		}
	}
	return ""
}

func (c *Client) ListPullFiles(ctx context.Context, owner, repo string, number int) ([]PullFile, error) {
	var files []PullFile
	page := 1
	for {
		var raw []struct {
			Filename         string `json:"filename"`
			PreviousFilename string `json:"previous_filename"`
			Status           string `json:"status"`
			Patch            string `json:"patch"`
		}
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/files?per_page=100&page=%d", owner, repo, number, page)
		if err := c.get(ctx, path, &raw); err != nil {
			return nil, err
		}
		for _, item := range raw {
			files = append(files, PullFile{
				Path:     item.Filename,
				PrevPath: item.PreviousFilename,
				Status:   item.Status,
				Patch:    item.Patch,
			})
		}
		if len(raw) < 100 {
			return files, nil
		}
		page++
	}
}

func (c *Client) CreateReview(ctx context.Context, payload Payload) error {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews", payload.Owner, payload.Repo, payload.PullNumber)
	return c.post(ctx, path, payload.Review, nil)
}

func (c *Client) get(ctx context.Context, path string, dest any) error {
	return c.do(ctx, http.MethodGet, path, nil, dest)
}

func (c *Client) post(ctx context.Context, path string, payload, dest any) error {
	return c.do(ctx, http.MethodPost, path, payload, dest)
}

func (c *Client) patch(ctx context.Context, path string, payload any) error {
	return c.do(ctx, http.MethodPatch, path, payload, nil)
}

func (c *Client) do(ctx context.Context, method, path string, payload, dest any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("github %s %s: encode: %w", method, path, err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base()+path, body)
	if err != nil {
		return fmt.Errorf("github %s %s: %w", method, path, err)
	}
	c.headers(req)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("github %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github %s %s: read body: %w", method, path, err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github %s %s: %s: %s", method, path, resp.Status, truncate(raw))
	}
	if dest == nil || len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("github %s %s: decode: %w", method, path, err)
	}
	return nil
}

func (c *Client) headers(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "unreal-review")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

func truncate(body []byte) string {
	s := strings.TrimSpace(string(body))
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

func ParsePR(spec, defaultOwner, defaultRepo string) (owner, repo string, number int, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return "", "", 0, fmt.Errorf("pull request is empty")
	}
	if u, parseErr := url.Parse(spec); parseErr == nil && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) >= 4 && parts[2] == "pull" {
			n, convErr := strconv.Atoi(parts[3])
			if convErr != nil {
				return "", "", 0, fmt.Errorf("pull request number: %w", convErr)
			}
			return parts[0], parts[1], n, nil
		}
	}
	if ownerRepo, num, ok := strings.Cut(spec, "#"); ok {
		n, convErr := strconv.Atoi(strings.TrimPrefix(num, "#"))
		if convErr != nil {
			return "", "", 0, fmt.Errorf("pull request number: %w", convErr)
		}
		owner, repo, err = SplitRepo(ownerRepo)
		if err != nil {
			return "", "", 0, err
		}
		return owner, repo, n, nil
	}
	n, convErr := strconv.Atoi(strings.TrimPrefix(spec, "#"))
	if convErr != nil {
		return "", "", 0, fmt.Errorf("pull request %q: want owner/repo#n, a URL, or a number", spec)
	}
	if defaultOwner == "" || defaultRepo == "" {
		return "", "", 0, fmt.Errorf("pull request number %d needs owner/repo", n)
	}
	return defaultOwner, defaultRepo, n, nil
}

func SplitRepo(name string) (string, string, error) {
	owner, repo, ok := strings.Cut(strings.TrimSpace(name), "/")
	if !ok || owner == "" || repo == "" {
		return "", "", fmt.Errorf("repository %q: want owner/repo", name)
	}
	return owner, strings.TrimSuffix(repo, ".git"), nil
}

func ParseRemoteURL(remote string) (string, string, error) {
	remote = strings.TrimSpace(remote)
	remote = strings.TrimSuffix(remote, ".git")
	if strings.HasPrefix(remote, "git@") {
		_, path, ok := strings.Cut(remote, ":")
		if !ok {
			return "", "", fmt.Errorf("git remote %q: missing path", remote)
		}
		return SplitRepo(path)
	}
	u, err := url.Parse(remote)
	if err != nil {
		return "", "", fmt.Errorf("git remote %q: %w", remote, err)
	}
	return SplitRepo(strings.Trim(u.Path, "/"))
}

func NewHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) ListPullCommits(ctx context.Context, owner, repo string, number int) ([]string, error) {
	var out []string
	page := 1
	for {
		var raw []struct {
			SHA string `json:"sha"`
		}
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/commits?per_page=100&page=%d", owner, repo, number, page)
		if err := c.get(ctx, path, &raw); err != nil {
			return nil, err
		}
		for _, item := range raw {
			out = append(out, item.SHA)
		}
		if len(raw) < 100 {
			return out, nil
		}
		page++
	}
}

func (c *Client) HasSuccessfulCheck(ctx context.Context, owner, repo, sha, checkName string) (bool, error) {
	var raw struct {
		CheckRuns []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"check_runs"`
	}
	path := fmt.Sprintf("/repos/%s/%s/commits/%s/check-runs?check_name=%s&per_page=100", owner, repo, sha, checkName)
	if err := c.get(ctx, path, &raw); err != nil {
		return false, err
	}
	for _, run := range raw.CheckRuns {
		if run.Name == checkName && run.Status == "completed" && run.Conclusion == "success" {
			return true, nil
		}
	}
	return false, nil
}

func (c *Client) CreateCheckRun(ctx context.Context, owner, repo, sha, name, title, summary string) error {
	body := struct {
		Name       string `json:"name"`
		HeadSHA    string `json:"head_sha"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
		Output     struct {
			Title   string `json:"title"`
			Summary string `json:"summary"`
		} `json:"output"`
	}{
		Name:       name,
		HeadSHA:    sha,
		Status:     "completed",
		Conclusion: "success",
	}
	body.Output.Title = title
	body.Output.Summary = summary
	return c.post(ctx, fmt.Sprintf("/repos/%s/%s/check-runs", owner, repo), body, nil)
}

func (c *Client) HasLGTMReview(ctx context.Context, owner, repo string, number int, sha string) (bool, error) {
	page := 1
	for {
		var raw []struct {
			CommitID string `json:"commit_id"`
			Body     string `json:"body"`
		}
		path := fmt.Sprintf("/repos/%s/%s/pulls/%d/reviews?per_page=100&page=%d", owner, repo, number, page)
		if err := c.get(ctx, path, &raw); err != nil {
			return false, err
		}
		for _, item := range raw {
			if item.CommitID == sha && strings.HasPrefix(strings.TrimSpace(item.Body), "LGTM") {
				return true, nil
			}
		}
		if len(raw) < 100 {
			return false, nil
		}
		page++
	}
}

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
	HeadSHA string
	Title   string
}

type PullFile struct {
	Path     string
	Patch    string
	Status   string
	PrevPath string
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
		Head   struct {
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
		HeadSHA: raw.Head.SHA,
		Title:   raw.Title,
	}, nil
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
	return c.post(ctx, path, payload.Review)
}

func (c *Client) get(ctx context.Context, path string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+path, nil)
	if err != nil {
		return fmt.Errorf("github GET %s: %w", path, err)
	}
	c.headers(req)
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("github GET %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github GET %s: read body: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github GET %s: %s: %s", path, resp.Status, truncate(body))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("github GET %s: decode: %w", path, err)
	}
	return nil
}

func (c *Client) post(ctx context.Context, path string, payload any) error {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("github POST %s: encode: %w", path, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+path, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("github POST %s: %w", path, err)
	}
	c.headers(req)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("github POST %s: %w", path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("github POST %s: read body: %w", path, err)
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("github POST %s: %s: %s", path, resp.Status, truncate(body))
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

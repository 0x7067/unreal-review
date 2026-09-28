package main

import (
	"strings"
	"testing"
)

func TestGitHubAPIOrigin(t *testing.T) {
	t.Setenv("UNREAL_REVIEW_GITHUB_API", "")
	client, err := githubClient("token")
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL != "" {
		t.Fatalf("empty override base %q", client.BaseURL)
	}

	t.Setenv("UNREAL_REVIEW_GITHUB_API", " http://127.0.0.1:9 ")
	client, err = githubClient("token")
	if err != nil {
		t.Fatal(err)
	}
	if client.BaseURL != "http://127.0.0.1:9" {
		t.Fatalf("loopback base %q", client.BaseURL)
	}

	for _, raw := range []string{
		"https://api.github.com",
		"http://127.0.0.2:9",
		"not a url",
	} {
		t.Setenv("UNREAL_REVIEW_GITHUB_API", raw)
		if _, err = githubClient("token"); err == nil || !strings.Contains(err.Error(), "UNREAL_REVIEW_GITHUB_API") {
			t.Fatalf("%q: got %v", raw, err)
		}
	}
}

package main

import (
	"net/http"
	"net/http/httptest"
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

	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/steal", http.StatusFound)
	}))
	t.Cleanup(stub.Close)
	t.Setenv("UNREAL_REVIEW_GITHUB_API", stub.URL)
	client, err = githubClient("secret")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, stub.URL+"/repos/o/r", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer secret")
	resp, err := client.HTTP.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("github client followed an off-host redirect")
	}
	if !strings.Contains(err.Error(), "not loopback") {
		t.Fatalf("err=%v", err)
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

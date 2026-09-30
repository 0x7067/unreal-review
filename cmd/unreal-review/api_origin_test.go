package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
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
	if err == nil {
		t.Fatal("github client followed an off-host redirect")
	}

	for _, raw := range []string{
		"https://api.github.com",
		"http://127.0.0.2:9",
		"not a url",
	} {
		t.Setenv("UNREAL_REVIEW_GITHUB_API", raw)
		if _, err = githubClient("token"); err == nil {
			t.Fatalf("%q: got %v", raw, err)
		}
	}
}

func TestIsolateSecretsRemovesCredentialsFromChildEnvironment(t *testing.T) {
	const helper = "UNREAL_REVIEW_TEST_ISOLATE_SECRETS"
	values := map[string]string{
		"OPENROUTER_API_KEY": "openrouter-secret",
		"GH_TOKEN":           "github-secret",
		"GITHUB_TOKEN":       "inherited-github-secret",
	}
	if os.Getenv(helper) == "1" {
		if err := isolateSecrets(); err != nil {
			t.Fatal(err)
		}
		for name, want := range values {
			if _, ok := os.LookupEnv(name); ok {
				t.Fatalf("%s remains in the child environment", name)
			}
			if got := secret(name); got != want {
				t.Fatalf("secret(%q) = %q, want %q", name, got, want)
			}
		}
		return
	}

	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestIsolateSecretsRemovesCredentialsFromChildEnvironment$")
	cmd.Env = []string{helper + "=1"}
	for name, value := range values {
		cmd.Env = append(cmd.Env, name+"="+value)
	}
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("secret isolation subprocess: %v\n%s", err, output)
	}
}

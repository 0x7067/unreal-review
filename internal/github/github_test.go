package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusMarkerRoundTrip(t *testing.T) {
	want := Status{Head: "42227a3148c46baf3636706415dd01840062297b", Runs: 3, CostUSD: 0.021134}
	body := "Some prose.\n\n" + StatusMarker(want)
	got, ok := ParseStatus(body)
	if !ok {
		t.Fatalf("ParseStatus did not find the marker in %q", body)
	}
	if got != want {
		t.Fatalf("status: %+v want %+v", got, want)
	}
}

func TestStatusMarkerRejectsOtherBodies(t *testing.T) {
	for name, body := range map[string]string{
		"no marker":         "just a review body",
		"another tool":      `<!-- devin-review-comment {"id": "BUG_x_0001"} -->`,
		"finding marker":    FindingMarker("a1b2c3d4e5f60708"),
		"truncated":         "<!-- unreal-review status abc123",
		"missing head":      "<!-- unreal-review status  3 0.5 -->",
		"runs not a number": "<!-- unreal-review status abc123 three 0.5 -->",
	} {
		if _, ok := ParseStatus(body); ok {
			t.Errorf("%s: ParseStatus accepted %q", name, body)
		}
	}
}

func TestFindingMarkerRoundTrip(t *testing.T) {
	body := "**warning**\n\nThis map write races with the reader.\n\n" + FindingMarker("a1b2c3d4e5f60708")
	id, ok := ParseFinding(body)
	if !ok || id != "a1b2c3d4e5f60708" {
		t.Fatalf("ParseFinding = %q, %v", id, ok)
	}
	if _, ok := ParseFinding(StatusMarker(Status{Head: "abc123", Runs: 1})); ok {
		t.Fatal("ParseFinding accepted a status marker")
	}
	if _, ok := ParseFinding("**warning**\n\nno marker here"); ok {
		t.Fatal("ParseFinding accepted a body without a marker")
	}
}

func TestParseDroppedReadsEveryDroppedMarkerInABody(t *testing.T) {
	body := "2 finding(s) were not posted as inline comments:\n" +
		"- `a.go` line 9: outside " + DroppedMarker("cGF5bG9hZC1h") + "\n" +
		"- `b.go` line 4: over cap " + FindingMarker("bbbb2222") + "\n" +
		StatusMarker(Status{Head: "abc123", Runs: 1}) + "\n" +
		"- `c.go` line 2: outside " + DroppedMarker("cGF5bG9hZC1j") + "\n"
	got := ParseDropped(body)
	if len(got) != 2 || got[0] != "cGF5bG9hZC1h" || got[1] != "cGF5bG9hZC1j" {
		t.Fatalf("ParseDropped = %q", got)
	}
	if got := ParseDropped("LGTM"); len(got) != 0 {
		t.Fatalf("ParseDropped(LGTM) = %q", got)
	}
}

func TestPullStateReadsStatusAndPostedComments(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/pulls/3", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"number": 3,
			"title":  "Review the repository's own pull requests",
			"base":   map[string]string{"ref": "main", "sha": "8ad9a39"},
			"head":   map[string]string{"sha": "42227a3"},
		})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/commits", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []map[string]any{
			{"sha": "42227a3"},
			{"sha": "dab3e1c"},
			{"sha": "7c81b2c"},
		})
	})
	mux.HandleFunc("/repos/o/r/issues/3/comments", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []map[string]any{
			{"id": 11, "body": "a human comment"},
			{"id": 12, "body": StatusMarker(Status{Head: "7c81b2c", Runs: 1, CostUSD: 0.5})},
			{"id": 13, "body": StatusMarker(Status{Head: "dab3e1c", Runs: 2, CostUSD: 1.25})},
		})
	})
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Variables map[string]any `json:"variables"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		writeJSON(t, w, map[string]any{
			"data": map[string]any{
				"repository": map[string]any{
					"pullRequest": map[string]any{
						"reviewThreads": map[string]any{
							"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""},
							"nodes": []map[string]any{
								{
									"id": "thread_1", "isResolved": false,
									"path": "internal/eval/eval.go",
									"line": 155, "originalLine": 155, "startLine": nil, "originalStartLine": nil,
									"diffSide": "RIGHT",
									"comments": map[string]any{
										"nodes": []map[string]any{
											{"databaseId": 501, "body": "**warning**\n\nFirst.\n\n" + FindingMarker("aaaa1111")},
										},
									},
								},
								{
									"id": "thread_2", "isResolved": true,
									"path": ".github/workflows/review.yml",
									"line": nil, "originalLine": 37, "startLine": nil, "originalStartLine": 35,
									"diffSide": "RIGHT",
									"comments": map[string]any{
										"nodes": []map[string]any{
											{"databaseId": 502, "body": "**error**\n\nOutdated one.\n\n" + FindingMarker("bbbb2222")},
											{"databaseId": 503, "body": "Fixed in `dab3e1c`: patched.\n\n" + ResolvedMarker("bbbb2222")},
										},
									},
								},
								{
									"id": "thread_3", "isResolved": false,
									"path": "cmd/unreal-review/eval.go",
									"line": 43, "originalLine": 43, "startLine": nil, "originalStartLine": nil,
									"diffSide": "RIGHT",
									"comments": map[string]any{
										"nodes": []map[string]any{
											{"databaseId": 504, "body": "a human inline comment"},
										},
									},
								},
							},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("/repos/o/r/pulls/3/reviews", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, []map[string]any{
			{"commit_id": "42227a3", "body": "Summary.\n\n- `a.go` line 9: outside " + DroppedMarker("cGF5bG9hZA")},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{Token: "t", BaseURL: server.URL, HTTP: server.Client()}
	state, err := client.PullState(context.Background(), "o", "r", 3)
	if err != nil {
		t.Fatal(err)
	}
	if state.BaseSHA != "8ad9a39" || state.HeadSHA != "42227a3" {
		t.Fatalf("pull request: %+v", state)
	}
	if state.StatusCommentID != 13 {
		t.Fatalf("status comment id = %d, want the newest marker (13)", state.StatusCommentID)
	}
	if state.Status.Head != "dab3e1c" || state.Status.Runs != 2 || state.Status.CostUSD != 1.25 {
		t.Fatalf("status: %+v", state.Status)
	}
	if len(state.Comments) != 3 {
		t.Fatalf("comments: %+v", state.Comments)
	}
	current, outdated, human := state.Comments[0], state.Comments[1], state.Comments[2]
	if current.StartLine != 155 || current.EndLine != 155 || current.Side != "RIGHT" {
		t.Fatalf("single-line comment should start where it ends: %+v", current)
	}
	if id, ok := ParseFinding(current.Body); !ok || id != "aaaa1111" {
		t.Fatalf("marker: %q %v", id, ok)
	}
	if current.ThreadID != "thread_1" || current.ThreadResolved || current.CommentID != 501 {
		t.Fatalf("current thread metadata: %+v", current)
	}
	if outdated.StartLine != 35 || outdated.EndLine != 37 || outdated.Side != "RIGHT" {
		t.Fatalf("outdated comment lost its original position: %+v", outdated)
	}
	if outdated.ThreadID != "thread_2" || !outdated.ThreadResolved || outdated.CommentID != 502 {
		t.Fatalf("outdated thread metadata: %+v", outdated)
	}
	if len(outdated.Replied) != 1 || outdated.Replied[0] != "bbbb2222" {
		t.Fatalf("outdated thread should carry our reply id: %+v", outdated.Replied)
	}
	if _, ok := ParseFinding(human.Body); ok {
		t.Fatal("a human comment must not parse as one of ours")
	}
	if human.ThreadID != "thread_3" || human.ThreadResolved {
		t.Fatalf("human thread metadata: %+v", human)
	}
	if len(state.Reviews) != 1 || state.Reviews[0].CommitID != "42227a3" {
		t.Fatalf("reviews: %+v", state.Reviews)
	}
	if payloads := ParseDropped(state.Reviews[0].Body); len(payloads) != 1 || payloads[0] != "cGF5bG9hZA" {
		t.Fatalf("review body markers: %q", payloads)
	}
}

func TestReviewThreadsGraphQLErrorsReturnedAsError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, map[string]any{
			"errors": []map[string]any{{"message": "Could not resolve to a Repository"}},
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{Token: "t", BaseURL: server.URL, HTTP: server.Client()}
	if _, err := client.ReviewThreads(context.Background(), "o", "r", 3); err == nil {
		t.Fatal("a graphql errors array should be returned as an error")
	}
}

func TestUpsertStatusCommentCreatesThenEdits(t *testing.T) {
	var (
		methods []string
		paths   []string
		bodies  []string
	)
	mux := http.NewServeMux()
	record := func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Body string `json:"body"`
		}
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		methods = append(methods, r.Method)
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, payload.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":99}`))
	}
	mux.HandleFunc("/repos/o/r/issues/3/comments", record)
	mux.HandleFunc("/repos/o/r/issues/comments/99", record)
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{Token: "t", BaseURL: server.URL, HTTP: server.Client()}
	ctx := context.Background()
	if err := client.UpsertStatusComment(ctx, "o", "r", 3, 0, "first"); err != nil {
		t.Fatal(err)
	}
	if err := client.UpsertStatusComment(ctx, "o", "r", 3, 99, "second"); err != nil {
		t.Fatal(err)
	}
	if len(methods) != 2 || methods[0] != http.MethodPost || methods[1] != http.MethodPatch {
		t.Fatalf("methods: %v", methods)
	}
	if paths[0] != "/repos/o/r/issues/3/comments" || paths[1] != "/repos/o/r/issues/comments/99" {
		t.Fatalf("paths: %v", paths)
	}
	if bodies[0] != "first" || bodies[1] != "second" {
		t.Fatalf("bodies: %v", bodies)
	}
}

func writeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Fatal(err)
	}
}

func TestCheckRunReceiptRoundTrip(t *testing.T) {
	type call struct {
		method string
		path   string
		body   string
	}
	var calls []call
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/commits/42227a3/check-runs", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{method: r.Method, path: r.URL.RequestURI()})
		writeJSON(t, w, map[string]any{
			"total_count": 1,
			"check_runs": []map[string]any{
				{"name": "other-check", "status": "completed", "conclusion": "success"},
				{"name": "unreal-review", "status": "completed", "conclusion": "failure"},
			},
		})
	})
	mux.HandleFunc("/repos/o/r/commits/dab3e1c/check-runs", func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, call{method: r.Method, path: r.URL.RequestURI()})
		writeJSON(t, w, map[string]any{
			"total_count": 1,
			"check_runs": []map[string]any{
				{"name": "unreal-review", "status": "completed", "conclusion": "success"},
			},
		})
	})
	mux.HandleFunc("/repos/o/r/check-runs", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		calls = append(calls, call{method: r.Method, path: r.URL.RequestURI(), body: string(body)})
		writeJSON(t, w, map[string]any{"id": 1})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	client := &Client{Token: "t", BaseURL: server.URL, HTTP: server.Client()}
	ctx := context.Background()
	ok, err := client.HasSuccessfulCheck(ctx, "o", "r", "42227a3", "unreal-review")
	if err != nil || ok {
		t.Fatalf("failed conclusion must not count: ok=%v err=%v", ok, err)
	}
	ok, err = client.HasSuccessfulCheck(ctx, "o", "r", "dab3e1c", "unreal-review")
	if err != nil || !ok {
		t.Fatalf("success receipt must count: ok=%v err=%v", ok, err)
	}
	if err := client.CreateCheckRun(ctx, "o", "r", "42227a3", "unreal-review", "unreal-review", "Review posted."); err != nil {
		t.Fatalf("create: %v", err)
	}
	created := calls[len(calls)-1]
	if created.method != "POST" || created.body == "" || !strings.Contains(created.body, `"conclusion":"success"`) {
		t.Fatalf("create call: %+v", created)
	}
}

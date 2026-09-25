package forgejo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
)

var repo = config.RepoRef{Owner: "platform", Name: "foo"}

func TestClient(t *testing.T) {
	var createdLabels []string
	var issueLabels []int64
	mux := http.NewServeMux()
	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "token secret" {
				t.Errorf("%s: wrong authorization %q", r.URL.Path, r.Header.Get("Authorization"))
			}
			h(w, r)
		}
	}
	mux.HandleFunc("GET /forge/api/v1/user", auth(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"login":"github-bridge"}`)
	}))
	mux.HandleFunc("GET /forge/api/v1/repos/platform/foo/issues", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("created_by") != "github-bridge" {
			t.Errorf("created_by = %q", r.URL.Query().Get("created_by"))
		}
		if r.URL.Query().Get("since") == "" {
			t.Error("since was not passed")
		}
		fmt.Fprint(w, `[{"number":71,"title":"t","state":"open","user":{"login":"github-bridge"},"labels":[{"id":1,"name":"github"}]},
			{"number":72,"title":"pr","state":"open","user":{"login":"ska"},"pull_request":{"merged":false}}]`)
	}))
	mux.HandleFunc("GET /forge/api/v1/repos/platform/foo/issues/comments", auth(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":5,"user":{"login":"ska"},"issue_url":"https://f/platform/foo/issues/71"},
			{"id":6,"user":{"login":"ska"},"pull_request_url":"https://f/platform/foo/pulls/72","assets":[{},{}]}]`)
	}))
	mux.HandleFunc("GET /forge/api/v1/repos/platform/foo/labels", auth(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":1,"name":"GitHub"}]`)
	}))
	mux.HandleFunc("POST /forge/api/v1/repos/platform/foo/labels", auth(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		createdLabels = append(createdLabels, req.Name)
		fmt.Fprint(w, `{"id":9,"name":"`+req.Name+`"}`)
	}))
	mux.HandleFunc("POST /forge/api/v1/repos/platform/foo/issues", auth(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Labels []int64 }
		_ = json.NewDecoder(r.Body).Decode(&req)
		issueLabels = req.Labels
		fmt.Fprint(w, `{"number":73,"state":"open","user":{"login":"github-bridge"}}`)
	}))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	base, _ := url.Parse(srv.URL + "/forge")
	c := New(base, "secret", srv.Client())
	ctx := context.Background()

	if self, err := c.Self(ctx); err != nil || self != "github-bridge" {
		t.Fatalf("Self = %q, %v", self, err)
	}

	issues, err := c.ListIssuesByAuthor(ctx, repo, "github-bridge", time.Now())
	if err != nil || len(issues) != 2 {
		t.Fatalf("issues = %+v, %v", issues, err)
	}
	if issues[0].IsPullRequest || !issues[1].IsPullRequest {
		t.Errorf("pull requests not told apart: %+v", issues)
	}

	comments, err := c.ListCommentsSince(ctx, repo, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if comments[0].IssueNumber != 71 || comments[1].IssueNumber != 72 || comments[1].Attachments != 2 {
		t.Errorf("comments = %+v", comments)
	}

	// An existing label is matched regardless of case, a missing one is
	// created, and both are sent as IDs.
	if _, err := c.CreateIssue(ctx, repo, "t", "b", []string{"github", "triage"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(createdLabels, []string{"triage"}) {
		t.Errorf("created labels %v", createdLabels)
	}
	if !slices.Equal(issueLabels, []int64{1, 9}) {
		t.Errorf("issue labels %v", issueLabels)
	}
}

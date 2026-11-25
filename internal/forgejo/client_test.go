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
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/rest"
)

var repo = config.RepoRef{Owner: "platform", Name: "foo"}

func TestReviewLineComments(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/platform/foo/pulls/7/reviews/3/comments", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[
			{"id":11,"body":"new side","user":{"login":"jdoe"},"path":"a.go","commit_id":"sha1","position":12,"original_position":0,"diff_hunk":"@@"},
			{"id":12,"body":"old side","user":{"login":"jdoe"},"path":"a.go","commit_id":"","original_commit_id":"sha0","position":0,"original_position":7}]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	c := New(base, "secret", srv.Client())

	got, err := c.ListReviewLineComments(context.Background(), repo, 7, 3)
	if err != nil || len(got) != 2 {
		t.Fatalf("line comments = %+v, %v", got, err)
	}
	if got[0].Line != 12 || got[0].Side != forge.SideRight || got[0].CommitID != "sha1" || got[0].ReviewID != 3 {
		t.Errorf("a line of the changed file = %+v", got[0])
	}
	if got[1].Line != 7 || got[1].Side != forge.SideLeft || got[1].CommitID != "sha0" {
		t.Errorf("a line of the file before = %+v", got[1])
	}
}

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
			{"number":72,"title":"pr","state":"open","user":{"login":"jdoe"},"pull_request":{"merged":false}}]`)
	}))
	mux.HandleFunc("GET /forge/api/v1/repos/platform/foo/issues/comments", auth(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":5,"user":{"login":"jdoe"},"issue_url":"https://f/platform/foo/issues/71"},
			{"id":6,"user":{"login":"jdoe"},"pull_request_url":"https://f/platform/foo/pulls/72","assets":[{},{}]}]`)
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

func TestContribRepositories(t *testing.T) {
	var forkName string
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/repos/github-contrib/foo", func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	})
	mux.HandleFunc("GET /api/v1/repos/platform/foo", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"full_name":"platform/foo","fork":false,"permissions":{"admin":false,"push":false,"pull":true}}`)
	})
	mux.HandleFunc("POST /api/v1/repos/platform/foo/forks", func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Name string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		forkName = req.Name
		fmt.Fprint(w, `{"full_name":"github-contrib/foo","fork":true,"parent":{"full_name":"platform/foo"},
			"permissions":{"admin":true,"push":true,"pull":true}}`)
	})
	mux.HandleFunc("GET /api/v1/repos/github-contrib/foo/push_mirrors", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"remote_name":"remote_mirror_1"}]`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	c := New(base, "secret", srv.Client())
	ctx := context.Background()
	fork := config.RepoRef{Owner: "github-contrib", Name: "foo"}

	if _, err := c.Repository(ctx, fork); !rest.IsNotFound(err) {
		t.Errorf("a missing repository: %v", err)
	}
	canonical, err := c.Repository(ctx, repo)
	if err != nil || canonical.CanPush || canonical.IsAdmin || canonical.Fork {
		t.Errorf("canonical = %+v, %v", canonical, err)
	}
	created, err := c.Fork(ctx, repo, "foo")
	if err != nil || forkName != "foo" || !created.Fork || created.Parent != "platform/foo" || !created.CanPush {
		t.Errorf("fork = %+v (name %q), %v", created, forkName, err)
	}
	if n, err := c.PushMirrors(ctx, fork); err != nil || n != 1 {
		t.Errorf("push mirrors = %d, %v", n, err)
	}
}

// Forgejo builds its pagination links from ROOT_URL. Behind an in-cluster
// Service that is another host, and the next page must be fetched at the
// configured address rather than refused (issue #52) or sent to ROOT_URL.
func TestPaginationFollowsROOTURLLinksAtTheConfiguredAddress(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a request went to the public address: %s", r.URL)
		fmt.Fprint(w, `[]`)
	}))
	defer public.Close()
	publicURL, _ := url.Parse(public.URL + "/git")

	var pages []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "token secret" {
			t.Errorf("%s: wrong authorization", r.URL)
		}
		if r.URL.Path != "/forge/api/v1/repos/platform/foo/issues" {
			t.Errorf("path = %s", r.URL.Path)
		}
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		if page == "" {
			w.Header().Set("Link", `<`+publicURL.String()+`/api/v1/repos/platform/foo/issues?limit=50&page=2&state=all>; rel="next"`)
			fmt.Fprint(w, `[{"number":1,"title":"a","state":"open","user":{"login":"jdoe"}}]`)
			return
		}
		fmt.Fprint(w, `[{"number":2,"title":"b","state":"open","user":{"login":"jdoe"}}]`)
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL + "/forge")

	c := New(base, "secret", srv.Client(), publicURL)
	got, err := c.ListIssuesSince(context.Background(), repo, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !slices.Equal(pages, []string{"", "2"}) {
		t.Errorf("issues = %+v, pages fetched = %q", got, pages)
	}
}

func TestPaginationRefusesOtherHosts(t *testing.T) {
	var leaked bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		leaked = true
		fmt.Fprint(w, `[]`)
	}))
	defer evil.Close()
	public, _ := url.Parse("https://forgejo.example.com")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<`+evil.URL+`/api/v1/repos/platform/foo/issues?page=2>; rel="next"`)
		fmt.Fprint(w, `[]`)
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)

	c := New(base, "secret", srv.Client(), public)
	if _, err := c.ListIssuesSince(context.Background(), repo, time.Time{}); err == nil {
		t.Error("a next link to another host was followed without error")
	}
	if leaked {
		t.Fatal("the token was sent to another host")
	}
}

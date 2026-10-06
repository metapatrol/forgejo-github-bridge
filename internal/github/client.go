// Package github is the bridge's GitHub client: a GitHub App, reading and
// writing issues and comments on the public repositories.
package github

import (
	"context"
	"crypto/rsa"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"sync"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/rest"
)

// pageSize is GitHub's maximum. Fewer pages is fewer requests against the
// rate limit.
const pageSize = "100"

// etagEntries bounds the conditional-request cache per repository. The poll
// URLs are two per repository and change only when a cursor moves.
const etagEntries = 64

// Client implements forge.Client against GitHub.
type Client struct {
	app     *app
	base    *url.URL
	http    *http.Client
	observe func(*http.Response)

	mu    sync.Mutex
	repos map[string]*rest.Client
	self  string
}

var _ forge.Client = (*Client)(nil)

// New creates a client for the App with the given ID and key. observe, when
// not nil, sees every response made with an installation token; the bridge
// reads the rate limit from them. The App's own requests, made with its JWT,
// draw on a budget of their own and are not observed.
func New(base *url.URL, appID int64, key *rsa.PrivateKey, httpClient *http.Client, observe func(*http.Response)) *Client {
	return &Client{
		app:     newApp(appID, key, base, httpClient, githubHeaders()...),
		base:    base,
		http:    httpClient,
		observe: observe,
		repos:   map[string]*rest.Client{},
	}
}

func githubHeaders() []rest.Option {
	return []rest.Option{
		rest.WithHeader("Accept", "application/vnd.github+json"),
		rest.WithHeader("X-GitHub-Api-Version", "2022-11-28"),
	}
}

// api returns the client for one repository, authenticated with that
// repository's installation token.
func (c *Client) api(repo config.RepoRef) *rest.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if api, ok := c.repos[repo.String()]; ok {
		return api
	}
	authorize := func(ctx context.Context, req *http.Request) error {
		token, err := c.app.token(ctx, repo)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return nil
	}
	opts := append(githubHeaders(), rest.WithETagCache(etagEntries))
	if c.observe != nil {
		opts = append(opts, rest.WithObserver(c.observe))
	}
	api := rest.New(c.base, c.http, authorize, opts...)
	c.repos[repo.String()] = api
	return api
}

// Self returns the App's bot login, "<slug>[bot]".
func (c *Client) Self(ctx context.Context) (string, error) {
	c.mu.Lock()
	self := c.self
	c.mu.Unlock()
	if self != "" {
		return self, nil
	}
	slug, err := c.app.slug(ctx)
	if err != nil {
		return "", err
	}
	self = slug + "[bot]"
	c.mu.Lock()
	c.self = self
	c.mu.Unlock()
	return self, nil
}

func repoPath(repo config.RepoRef) string {
	return "/repos/" + rest.Seg(repo.Owner) + "/" + rest.Seg(repo.Name)
}

func sinceQuery(since time.Time) url.Values {
	q := url.Values{
		"state":     {"all"},
		"sort":      {"updated"},
		"direction": {"asc"},
		"per_page":  {pageSize},
	}
	if !since.IsZero() {
		q.Set("since", since.UTC().Format(time.RFC3339))
	}
	return q
}

// ListIssuesSince implements forge.Client.
func (c *Client) ListIssuesSince(ctx context.Context, repo config.RepoRef, since time.Time) ([]forge.Issue, error) {
	return c.listIssues(ctx, repo, sinceQuery(since))
}

// ListIssuesByAuthor implements forge.Client.
func (c *Client) ListIssuesByAuthor(ctx context.Context, repo config.RepoRef, author string, since time.Time) ([]forge.Issue, error) {
	q := sinceQuery(since)
	q.Set("creator", author)
	return c.listIssues(ctx, repo, q)
}

func (c *Client) listIssues(ctx context.Context, repo config.RepoRef, q url.Values) ([]forge.Issue, error) {
	raw, err := rest.GetAll[issue](ctx, c.api(repo), repoPath(repo)+"/issues", q)
	if err != nil {
		return nil, err
	}
	out := make([]forge.Issue, len(raw))
	for i, r := range raw {
		out[i] = r.toForge()
	}
	return out, nil
}

// ListCommentsSince implements forge.Client.
func (c *Client) ListCommentsSince(ctx context.Context, repo config.RepoRef, since time.Time) ([]forge.Comment, error) {
	q := sinceQuery(since)
	q.Del("state")
	raw, err := rest.GetAll[comment](ctx, c.api(repo), repoPath(repo)+"/issues/comments", q)
	if err != nil {
		return nil, err
	}
	return comments(raw), nil
}

// GetIssue implements forge.Client.
func (c *Client) GetIssue(ctx context.Context, repo config.RepoRef, number int64) (forge.Issue, error) {
	var r issue
	err := c.api(repo).Do(ctx, http.MethodGet, repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10), nil, nil, &r)
	return r.toForge(), err
}

// CreateIssue implements forge.Client.
func (c *Client) CreateIssue(ctx context.Context, repo config.RepoRef, title, body string, labels []string) (forge.Issue, error) {
	req := map[string]any{"title": title, "body": body}
	if len(labels) > 0 {
		req["labels"] = labels
	}
	var r issue
	err := c.api(repo).Do(ctx, http.MethodPost, repoPath(repo)+"/issues", nil, req, &r)
	return r.toForge(), err
}

// EditIssue implements forge.Client.
func (c *Client) EditIssue(ctx context.Context, repo config.RepoRef, number int64, edit forge.IssueEdit) (forge.Issue, error) {
	req := map[string]any{}
	if edit.Title != nil {
		req["title"] = *edit.Title
	}
	if edit.Body != nil {
		req["body"] = *edit.Body
	}
	if edit.Closed != nil {
		req["state"] = state(*edit.Closed)
	}
	var r issue
	err := c.api(repo).Do(ctx, http.MethodPatch, repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10), nil, req, &r)
	return r.toForge(), err
}

// ListComments implements forge.Client.
func (c *Client) ListComments(ctx context.Context, repo config.RepoRef, number int64) ([]forge.Comment, error) {
	q := url.Values{"per_page": {pageSize}}
	raw, err := rest.GetAll[comment](ctx, c.api(repo), repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10)+"/comments", q)
	if err != nil {
		return nil, err
	}
	return comments(raw), nil
}

// CreateComment implements forge.Client.
func (c *Client) CreateComment(ctx context.Context, repo config.RepoRef, number int64, body string) (forge.Comment, error) {
	var r comment
	err := c.api(repo).Do(ctx, http.MethodPost, repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10)+"/comments", nil,
		map[string]string{"body": body}, &r)
	return r.toForge(), err
}

// EditComment implements forge.Client.
func (c *Client) EditComment(ctx context.Context, repo config.RepoRef, id int64, body string) (forge.Comment, error) {
	var r comment
	err := c.api(repo).Do(ctx, http.MethodPatch, repoPath(repo)+"/issues/comments/"+strconv.FormatInt(id, 10), nil,
		map[string]string{"body": body}, &r)
	return r.toForge(), err
}

// DeleteComment implements forge.Client.
func (c *Client) DeleteComment(ctx context.Context, repo config.RepoRef, id int64) error {
	return c.api(repo).Do(ctx, http.MethodDelete, repoPath(repo)+"/issues/comments/"+strconv.FormatInt(id, 10), nil, nil, nil)
}

func state(closed bool) string {
	if closed {
		return "closed"
	}
	return "open"
}

type user struct {
	Login string `json:"login"`
}

type issue struct {
	Number      int64                   `json:"number"`
	Title       string                  `json:"title"`
	Body        string                  `json:"body"`
	State       string                  `json:"state"`
	User        user                    `json:"user"`
	Labels      []struct{ Name string } `json:"labels"`
	PullRequest *struct{}               `json:"pull_request"`
	HTMLURL     string                  `json:"html_url"`
	CreatedAt   time.Time               `json:"created_at"`
	UpdatedAt   time.Time               `json:"updated_at"`
}

func (r issue) toForge() forge.Issue {
	labels := make([]string, len(r.Labels))
	for i, l := range r.Labels {
		labels[i] = l.Name
	}
	return forge.Issue{
		Number:        r.Number,
		Title:         r.Title,
		Body:          r.Body,
		Closed:        r.State == "closed",
		Author:        r.User.Login,
		Labels:        labels,
		IsPullRequest: r.PullRequest != nil,
		URL:           r.HTMLURL,
		CreatedAt:     r.CreatedAt,
		UpdatedAt:     r.UpdatedAt,
	}
}

type comment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      user      `json:"user"`
	HTMLURL   string    `json:"html_url"`
	IssueURL  string    `json:"issue_url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r comment) toForge() forge.Comment {
	return forge.Comment{
		ID:          r.ID,
		IssueNumber: issueNumberFromURL(r.IssueURL),
		Body:        r.Body,
		Author:      r.User.Login,
		URL:         r.HTMLURL,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
	}
}

func comments(raw []comment) []forge.Comment {
	out := make([]forge.Comment, len(raw))
	for i, r := range raw {
		out[i] = r.toForge()
	}
	return out
}

// issueNumberFromURL reads the number from an API URL ending in
// /issues/<number>. Zero when it does not.
func issueNumberFromURL(u string) int64 {
	if u == "" {
		return 0
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(path.Base(parsed.Path), 10, 64)
	if err != nil || path.Base(path.Dir(parsed.Path)) != "issues" {
		return 0
	}
	return n
}

// String names the client in logs.
func (c *Client) String() string { return fmt.Sprintf("github(%s)", c.base.Host) }

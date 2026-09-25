// Package forgejo is the bridge's Forgejo client: the private side, reached
// with the github-bridge account's token.
package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/rest"
)

// pageSize is Forgejo's default maximum page size.
const pageSize = "50"

// labelColor is what a label the bridge has to create looks like. Maintainers
// own labels once they exist and may recolour them.
const labelColor = "#6e7781"

// Client implements forge.Client against Forgejo.
type Client struct {
	api *rest.Client

	mu   sync.Mutex
	self string
	// labels caches label IDs by repository and lower-cased name. Forgejo
	// takes IDs where GitHub takes names, and the IDs do not change.
	labels map[string]map[string]int64
}

var _ forge.Client = (*Client)(nil)

// New creates a client for the Forgejo instance at base, e.g.
// https://forgejo.example.com.
func New(base *url.URL, token string, httpClient *http.Client) *Client {
	api := *base
	api.Path = strings.TrimRight(base.Path, "/") + "/api/v1"
	authorize := func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "token "+token)
		return nil
	}
	return &Client{
		api:    rest.New(&api, httpClient, authorize),
		labels: map[string]map[string]int64{},
	}
}

// Self returns the login of the account the token belongs to.
func (c *Client) Self(ctx context.Context) (string, error) {
	c.mu.Lock()
	self := c.self
	c.mu.Unlock()
	if self != "" {
		return self, nil
	}
	var u user
	if err := c.api.Do(ctx, http.MethodGet, "/user", nil, nil, &u); err != nil {
		return "", fmt.Errorf("reading the Forgejo account: %w", err)
	}
	if u.Login == "" {
		return "", fmt.Errorf("forgejo returned an account without a login")
	}
	c.mu.Lock()
	c.self = u.Login
	c.mu.Unlock()
	return u.Login, nil
}

func repoPath(repo config.RepoRef) string {
	return "/repos/" + rest.Seg(repo.Owner) + "/" + rest.Seg(repo.Name)
}

func sinceQuery(since time.Time) url.Values {
	q := url.Values{"state": {"all"}, "limit": {pageSize}}
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
	q.Set("created_by", author)
	return c.listIssues(ctx, repo, q)
}

func (c *Client) listIssues(ctx context.Context, repo config.RepoRef, q url.Values) ([]forge.Issue, error) {
	raw, err := rest.GetAll[issue](ctx, c.api, repoPath(repo)+"/issues", q)
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
	raw, err := rest.GetAll[comment](ctx, c.api, repoPath(repo)+"/issues/comments", q)
	if err != nil {
		return nil, err
	}
	return comments(raw), nil
}

// GetIssue implements forge.Client.
func (c *Client) GetIssue(ctx context.Context, repo config.RepoRef, number int64) (forge.Issue, error) {
	var r issue
	err := c.api.Do(ctx, http.MethodGet, repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10), nil, nil, &r)
	return r.toForge(), err
}

// CreateIssue implements forge.Client. Labels that do not exist yet are
// created.
func (c *Client) CreateIssue(ctx context.Context, repo config.RepoRef, title, body string, labels []string) (forge.Issue, error) {
	ids := make([]int64, 0, len(labels))
	for _, name := range labels {
		id, err := c.labelID(ctx, repo, name)
		if err != nil {
			return forge.Issue{}, err
		}
		ids = append(ids, id)
	}
	req := map[string]any{"title": title, "body": body}
	if len(ids) > 0 {
		req["labels"] = ids
	}
	var r issue
	err := c.api.Do(ctx, http.MethodPost, repoPath(repo)+"/issues", nil, req, &r)
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
		if *edit.Closed {
			req["state"] = "closed"
		} else {
			req["state"] = "open"
		}
	}
	var r issue
	err := c.api.Do(ctx, http.MethodPatch, repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10), nil, req, &r)
	return r.toForge(), err
}

// ListComments implements forge.Client.
func (c *Client) ListComments(ctx context.Context, repo config.RepoRef, number int64) ([]forge.Comment, error) {
	raw, err := rest.GetAll[comment](ctx, c.api, repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10)+"/comments", nil)
	if err != nil {
		return nil, err
	}
	out := comments(raw)
	// The per-issue endpoint does not say which issue its comments are on.
	for i := range out {
		out[i].IssueNumber = number
	}
	return out, nil
}

// CreateComment implements forge.Client.
func (c *Client) CreateComment(ctx context.Context, repo config.RepoRef, number int64, body string) (forge.Comment, error) {
	var r comment
	err := c.api.Do(ctx, http.MethodPost, repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10)+"/comments", nil,
		map[string]string{"body": body}, &r)
	out := r.toForge()
	out.IssueNumber = number
	return out, err
}

// EditComment implements forge.Client.
func (c *Client) EditComment(ctx context.Context, repo config.RepoRef, id int64, body string) (forge.Comment, error) {
	var r comment
	err := c.api.Do(ctx, http.MethodPatch, repoPath(repo)+"/issues/comments/"+strconv.FormatInt(id, 10), nil,
		map[string]string{"body": body}, &r)
	return r.toForge(), err
}

// DeleteComment implements forge.Client.
func (c *Client) DeleteComment(ctx context.Context, repo config.RepoRef, id int64) error {
	return c.api.Do(ctx, http.MethodDelete, repoPath(repo)+"/issues/comments/"+strconv.FormatInt(id, 10), nil, nil, nil)
}

// labelID resolves a label name, creating the label when the repository does
// not have it.
func (c *Client) labelID(ctx context.Context, repo config.RepoRef, name string) (int64, error) {
	key := strings.ToLower(name)
	c.mu.Lock()
	if id, ok := c.labels[repo.String()][key]; ok {
		c.mu.Unlock()
		return id, nil
	}
	c.mu.Unlock()

	all, err := rest.GetAll[label](ctx, c.api, repoPath(repo)+"/labels", url.Values{"limit": {pageSize}})
	if err != nil {
		return 0, fmt.Errorf("listing the labels of %s: %w", repo, err)
	}
	byName := map[string]int64{}
	for _, l := range all {
		byName[strings.ToLower(l.Name)] = l.ID
	}
	id, ok := byName[key]
	if !ok {
		var created label
		if err := c.api.Do(ctx, http.MethodPost, repoPath(repo)+"/labels", nil,
			map[string]string{"name": name, "color": labelColor}, &created); err != nil {
			return 0, fmt.Errorf("creating label %q in %s: %w", name, repo, err)
		}
		id = created.ID
		byName[key] = id
	}

	c.mu.Lock()
	c.labels[repo.String()] = byName
	c.mu.Unlock()
	return id, nil
}

type user struct {
	Login string `json:"login"`
}

type label struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type issue struct {
	Number      int64     `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	State       string    `json:"state"`
	User        user      `json:"user"`
	Labels      []label   `json:"labels"`
	PullRequest *struct{} `json:"pull_request"`
	HTMLURL     string    `json:"html_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
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
	ID             int64      `json:"id"`
	Body           string     `json:"body"`
	User           user       `json:"user"`
	HTMLURL        string     `json:"html_url"`
	IssueURL       string     `json:"issue_url"`
	PullRequestURL string     `json:"pull_request_url"`
	Assets         []struct{} `json:"assets"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

func (r comment) toForge() forge.Comment {
	n := numberFromURL(r.IssueURL)
	if n == 0 {
		n = numberFromURL(r.PullRequestURL)
	}
	return forge.Comment{
		ID:          r.ID,
		IssueNumber: n,
		Body:        r.Body,
		Author:      r.User.Login,
		URL:         r.HTMLURL,
		CreatedAt:   r.CreatedAt,
		UpdatedAt:   r.UpdatedAt,
		Attachments: len(r.Assets),
	}
}

func comments(raw []comment) []forge.Comment {
	out := make([]forge.Comment, len(raw))
	for i, r := range raw {
		out[i] = r.toForge()
	}
	return out
}

// numberFromURL reads the number from a URL ending in /issues/<n> or
// /pulls/<n>. Zero when it does not.
func numberFromURL(u string) int64 {
	if u == "" {
		return 0
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return 0
	}
	parent := path.Base(path.Dir(parsed.Path))
	if parent != "issues" && parent != "pulls" {
		return 0
	}
	n, err := strconv.ParseInt(path.Base(parsed.Path), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

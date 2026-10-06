package github

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/rest"
)

var _ forge.GitHubPulls = (*Client)(nil)

// GetPullRequest implements forge.GitHubPulls.
func (c *Client) GetPullRequest(ctx context.Context, repo config.RepoRef, number int64) (forge.PullRequest, error) {
	var r pull
	err := c.api(repo).Do(ctx, http.MethodGet, repoPath(repo)+"/pulls/"+strconv.FormatInt(number, 10), nil, nil, &r)
	return r.toForge(), err
}

// PullRequestFiles implements forge.GitHubPulls. GitHub lists at most 3000
// files; a pull request larger than that is not one the CI check needs to be
// subtle about.
func (c *Client) PullRequestFiles(ctx context.Context, repo config.RepoRef, number int64) ([]string, error) {
	raw, err := rest.GetAll[struct {
		Filename string `json:"filename"`
	}](ctx, c.api(repo), repoPath(repo)+"/pulls/"+strconv.FormatInt(number, 10)+"/files", url.Values{"per_page": {pageSize}})
	if err != nil {
		return nil, err
	}
	out := make([]string, len(raw))
	for i, f := range raw {
		out[i] = f.Filename
	}
	return out, nil
}

// Contains implements forge.GitHubPulls. A commit GitHub does not know yet —
// the push mirror has not run — is not contained.
func (c *Client) Contains(ctx context.Context, repo config.RepoRef, branch, sha string) (bool, error) {
	cmp, err := c.compare(ctx, repo, branch, sha)
	if rest.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return cmp.Status == "behind" || cmp.Status == "identical", nil
}

// BehindBy implements forge.GitHubPulls.
func (c *Client) BehindBy(ctx context.Context, repo config.RepoRef, branch, sha string) (int, error) {
	cmp, err := c.compare(ctx, repo, branch, sha)
	return cmp.BehindBy, err
}

// ListReviewComments implements forge.GitHubPulls.
func (c *Client) ListReviewComments(ctx context.Context, repo config.RepoRef, number int64) ([]forge.LineComment, error) {
	raw, err := rest.GetAll[reviewComment](ctx, c.api(repo), repoPath(repo)+"/pulls/"+strconv.FormatInt(number, 10)+"/comments",
		url.Values{"per_page": {pageSize}})
	if err != nil {
		return nil, err
	}
	out := make([]forge.LineComment, len(raw))
	for i, r := range raw {
		out[i] = r.toForge()
	}
	return out, nil
}

// CreateReviewComment implements forge.GitHubPulls. GitHub answers 422 for a
// line its diff of that commit does not have, or a commit it does not know.
func (c *Client) CreateReviewComment(ctx context.Context, repo config.RepoRef, number int64, lc forge.LineComment) (forge.LineComment, error) {
	var r reviewComment
	err := c.api(repo).Do(ctx, http.MethodPost, repoPath(repo)+"/pulls/"+strconv.FormatInt(number, 10)+"/comments", nil,
		map[string]any{"body": lc.Body, "commit_id": lc.CommitID, "path": lc.Path, "line": lc.Line, "side": lc.Side}, &r)
	return r.toForge(), err
}

// EditReviewComment implements forge.GitHubPulls.
func (c *Client) EditReviewComment(ctx context.Context, repo config.RepoRef, id int64, body string) error {
	return c.api(repo).Do(ctx, http.MethodPatch, repoPath(repo)+"/pulls/comments/"+strconv.FormatInt(id, 10), nil,
		map[string]string{"body": body}, nil)
}

// DeleteReviewComment implements forge.GitHubPulls.
func (c *Client) DeleteReviewComment(ctx context.Context, repo config.RepoRef, id int64) error {
	return c.api(repo).Do(ctx, http.MethodDelete, repoPath(repo)+"/pulls/comments/"+strconv.FormatInt(id, 10), nil, nil, nil)
}

type reviewComment struct {
	ID        int64     `json:"id"`
	Body      string    `json:"body"`
	User      user      `json:"user"`
	HTMLURL   string    `json:"html_url"`
	Path      string    `json:"path"`
	Line      int       `json:"line"`
	Side      string    `json:"side"`
	CommitID  string    `json:"commit_id"`
	DiffHunk  string    `json:"diff_hunk"`
	ReviewID  int64     `json:"pull_request_review_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (r reviewComment) toForge() forge.LineComment {
	return forge.LineComment{
		ID: r.ID, ReviewID: r.ReviewID, Author: r.User.Login, Body: r.Body, URL: r.HTMLURL,
		Path: r.Path, Line: r.Line, Side: r.Side, CommitID: r.CommitID, DiffHunk: r.DiffHunk,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

type comparison struct {
	Status   string `json:"status"`
	AheadBy  int    `json:"ahead_by"`
	BehindBy int    `json:"behind_by"`
}

// compare compares head against base. Only the summary is wanted, so a single
// commit per page keeps the response small.
func (c *Client) compare(ctx context.Context, repo config.RepoRef, base, head string) (comparison, error) {
	var out comparison
	basehead := refSegment(base) + "..." + refSegment(head)
	err := c.api(repo).Do(ctx, http.MethodGet, repoPath(repo)+"/compare/"+basehead, url.Values{"per_page": {"1"}}, nil, &out)
	return out, err
}

// refSegment escapes a branch name for a path, keeping its slashes: GitHub
// reads release/1.0 in a compare path as the branch it is.
func refSegment(ref string) string {
	return strings.ReplaceAll(rest.Seg(ref), "%2F", "/")
}

type pull struct {
	Number         int64  `json:"number"`
	Title          string `json:"title"`
	Body           string `json:"body"`
	State          string `json:"state"`
	Merged         bool   `json:"merged"`
	MergeCommitSHA string `json:"merge_commit_sha"`
	HTMLURL        string `json:"html_url"`
	User           user   `json:"user"`
	Head           struct {
		SHA string `json:"sha"`
		Ref string `json:"ref"`
	} `json:"head"`
	Base struct {
		Ref string `json:"ref"`
	} `json:"base"`
}

func (r pull) toForge() forge.PullRequest {
	return forge.PullRequest{
		Number: r.Number, Title: r.Title, Body: r.Body, Author: r.User.Login, URL: r.HTMLURL,
		Open: r.State == "open", Merged: r.Merged, MergeCommitSHA: r.MergeCommitSHA,
		HeadSHA: r.Head.SHA, HeadRef: r.Head.Ref, BaseRef: r.Base.Ref,
	}
}

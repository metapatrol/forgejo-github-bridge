package github

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"

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

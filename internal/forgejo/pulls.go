package forgejo

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/rest"
)

var (
	_ forge.ForgejoPulls = (*Client)(nil)
	_ forge.ContribPulls = (*Client)(nil)
)

// GetPullRequest implements forge.ForgejoPulls.
func (c *Client) GetPullRequest(ctx context.Context, repo config.RepoRef, number int64) (forge.PullRequest, error) {
	var r pull
	err := c.api.Do(ctx, http.MethodGet, repoPath(repo)+"/pulls/"+strconv.FormatInt(number, 10), nil, nil, &r)
	return r.toForge(), err
}

// CreatePullRequest implements forge.ContribPulls.
func (c *Client) CreatePullRequest(ctx context.Context, repo config.RepoRef, head, base, title, body string) (forge.PullRequest, error) {
	var r pull
	err := c.api.Do(ctx, http.MethodPost, repoPath(repo)+"/pulls", nil,
		map[string]string{"head": head, "base": base, "title": title, "body": body}, &r)
	return r.toForge(), err
}

// DeleteBranch implements forge.ContribPulls. A branch that is already gone
// is not an error.
func (c *Client) DeleteBranch(ctx context.Context, repo config.RepoRef, branch string) error {
	err := c.api.Do(ctx, http.MethodDelete, repoPath(repo)+"/branches/"+rest.Seg(branch), nil, nil, nil)
	if rest.IsNotFound(err) {
		return nil
	}
	return err
}

// ListReviews implements forge.ForgejoPulls. Pending reviews are left out:
// nobody but their author can see them.
func (c *Client) ListReviews(ctx context.Context, repo config.RepoRef, number int64) ([]forge.Review, error) {
	raw, err := rest.GetAll[review](ctx, c.api, repoPath(repo)+"/pulls/"+strconv.FormatInt(number, 10)+"/reviews",
		url.Values{"limit": {pageSize}})
	if err != nil {
		return nil, err
	}
	var out []forge.Review
	for _, r := range raw {
		if r.State == "PENDING" || r.State == "REQUEST_REVIEW" || r.Dismissed {
			continue
		}
		out = append(out, forge.Review{
			ID: r.ID, Author: r.User.Login, State: r.State, Body: r.Body, URL: r.HTMLURL, SubmittedAt: r.SubmittedAt,
		})
	}
	return out, nil
}

// AddLabels implements forge.ForgejoPulls.
func (c *Client) AddLabels(ctx context.Context, repo config.RepoRef, number int64, labels []string) error {
	ids := make([]int64, 0, len(labels))
	for _, name := range labels {
		id, err := c.labelID(ctx, repo, name)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := c.api.Do(ctx, http.MethodPost, repoPath(repo)+"/issues/"+strconv.FormatInt(number, 10)+"/labels", nil,
		map[string][]int64{"labels": ids}, nil); err != nil {
		return fmt.Errorf("labelling #%d: %w", number, err)
	}
	return nil
}

// SetBase implements forge.ForgejoPulls.
func (c *Client) SetBase(ctx context.Context, repo config.RepoRef, number int64, base string) error {
	return c.api.Do(ctx, http.MethodPatch, repoPath(repo)+"/pulls/"+strconv.FormatInt(number, 10), nil,
		map[string]string{"base": base}, nil)
}

type branchInfo struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type pull struct {
	Number         int64      `json:"number"`
	Title          string     `json:"title"`
	Body           string     `json:"body"`
	State          string     `json:"state"`
	Merged         bool       `json:"merged"`
	MergeCommitSHA string     `json:"merge_commit_sha"`
	HTMLURL        string     `json:"html_url"`
	User           user       `json:"user"`
	Head           branchInfo `json:"head"`
	Base           branchInfo `json:"base"`
}

func (r pull) toForge() forge.PullRequest {
	return forge.PullRequest{
		Number: r.Number, Title: r.Title, Body: r.Body, Author: r.User.Login, URL: r.HTMLURL,
		Open: r.State == "open", Merged: r.Merged, MergeCommitSHA: r.MergeCommitSHA,
		HeadSHA: r.Head.SHA, HeadRef: r.Head.Ref, BaseRef: r.Base.Ref,
	}
}

type review struct {
	ID          int64     `json:"id"`
	User        user      `json:"user"`
	State       string    `json:"state"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	Dismissed   bool      `json:"dismissed"`
	SubmittedAt time.Time `json:"submitted_at"`
}

package forge

import (
	"context"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
)

// PullRequest is what the bridge needs of a pull request beyond its issue:
// where it comes from, where it goes, and how it ended.
type PullRequest struct {
	Number int64
	Title  string
	Body   string
	Author string
	URL    string
	Open   bool
	Merged bool
	// MergeCommitSHA is the commit the base branch points at after the
	// merge. With a rebase it is the rebased head, not a merge commit.
	MergeCommitSHA string
	HeadSHA        string
	HeadRef        string
	BaseRef        string
}

// Review is a submitted pull request review.
type Review struct {
	ID     int64
	Author string
	// State is APPROVED, REQUEST_CHANGES or COMMENT. Pending reviews are
	// never returned: they are not submitted, so nobody else can see them.
	State       string
	Body        string
	URL         string
	SubmittedAt time.Time
}

// GitHubPulls is what the bridge reads of pull requests on GitHub.
type GitHubPulls interface {
	GetPullRequest(ctx context.Context, repo config.RepoRef, number int64) (PullRequest, error)
	// PullRequestFiles lists the paths a pull request changes.
	PullRequestFiles(ctx context.Context, repo config.RepoRef, number int64) ([]string, error)
	// Contains reports whether commit sha is reachable from branch.
	Contains(ctx context.Context, repo config.RepoRef, branch, sha string) (bool, error)
	// BehindBy counts the commits on branch that sha does not have.
	BehindBy(ctx context.Context, repo config.RepoRef, branch, sha string) (int, error)
}

// ForgejoPulls is what the bridge does with pull requests in Forgejo, as the
// github-bridge account.
type ForgejoPulls interface {
	GetPullRequest(ctx context.Context, repo config.RepoRef, number int64) (PullRequest, error)
	ListReviews(ctx context.Context, repo config.RepoRef, number int64) ([]Review, error)
	// AddLabels adds labels to an issue or pull request, creating those the
	// repository does not have yet.
	AddLabels(ctx context.Context, repo config.RepoRef, number int64, labels []string) error
	// SetBase changes the branch a pull request targets.
	SetBase(ctx context.Context, repo config.RepoRef, number int64, base string) error
}

// ContribPulls is what the bridge does as the github-contrib account: open
// pull requests from the contrib repository, and remove their branches.
type ContribPulls interface {
	Self(ctx context.Context) (string, error)
	// CreatePullRequest opens a pull request on repo from head, given as
	// owner:branch, into base.
	CreatePullRequest(ctx context.Context, repo config.RepoRef, head, base, title, body string) (PullRequest, error)
	DeleteBranch(ctx context.Context, repo config.RepoRef, branch string) error
}

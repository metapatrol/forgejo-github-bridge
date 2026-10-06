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

// LineComment is a comment on a line of a pull request's diff.
type LineComment struct {
	ID int64
	// ReviewID is the review it belongs to.
	ReviewID int64
	Author   string
	Body     string
	URL      string
	// Path, Line and Side say where: Side RIGHT is a line of the file as the
	// pull request changes it, LEFT one of the file before. CommitID is the
	// head commit the comment was written on.
	Path     string
	Line     int
	Side     string
	CommitID string
	// DiffHunk is the part of the diff the comment is on.
	DiffHunk  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// The sides of a diff a line comment can be on.
const (
	SideRight = "RIGHT"
	SideLeft  = "LEFT"
)

// GitHubPulls is what the bridge does with pull requests on GitHub.
type GitHubPulls interface {
	GetPullRequest(ctx context.Context, repo config.RepoRef, number int64) (PullRequest, error)
	// PullRequestFiles lists the paths a pull request changes.
	PullRequestFiles(ctx context.Context, repo config.RepoRef, number int64) ([]string, error)
	// Contains reports whether commit sha is reachable from branch.
	Contains(ctx context.Context, repo config.RepoRef, branch, sha string) (bool, error)
	// BehindBy counts the commits on branch that sha does not have.
	BehindBy(ctx context.Context, repo config.RepoRef, branch, sha string) (int, error)
	// ListReviewComments lists the line comments of a pull request.
	ListReviewComments(ctx context.Context, repo config.RepoRef, number int64) ([]LineComment, error)
	// CreateReviewComment comments on the line c's CommitID, Path, Line and
	// Side name. ErrRejected when GitHub will not place a comment there.
	CreateReviewComment(ctx context.Context, repo config.RepoRef, number int64, c LineComment) (LineComment, error)
	EditReviewComment(ctx context.Context, repo config.RepoRef, id int64, body string) error
	DeleteReviewComment(ctx context.Context, repo config.RepoRef, id int64) error
}

// ForgejoPulls is what the bridge does with pull requests in Forgejo, as the
// github-bridge account.
type ForgejoPulls interface {
	GetPullRequest(ctx context.Context, repo config.RepoRef, number int64) (PullRequest, error)
	ListReviews(ctx context.Context, repo config.RepoRef, number int64) ([]Review, error)
	// ListReviewLineComments lists the line comments of one review.
	ListReviewLineComments(ctx context.Context, repo config.RepoRef, number, reviewID int64) ([]LineComment, error)
	// AddLabels adds labels to an issue or pull request, creating those the
	// repository does not have yet.
	AddLabels(ctx context.Context, repo config.RepoRef, number int64, labels []string) error
	// SetBase changes the branch a pull request targets.
	SetBase(ctx context.Context, repo config.RepoRef, number int64, base string) error
}

// RepoInfo is what the bridge checks about a repository, as seen by the
// account asking.
type RepoInfo struct {
	FullName string
	Fork     bool
	// Parent is the repository a fork was made from, as owner/name.
	Parent string
	// CanPush and IsAdmin are the asking account's rights on it.
	CanPush bool
	IsAdmin bool
}

// ContribPulls is what the bridge does as the github-contrib account: keep
// its forks, open pull requests from them, and remove their branches.
type ContribPulls interface {
	Self(ctx context.Context) (string, error)
	// Repository describes a repository as the contrib account sees it.
	Repository(ctx context.Context, repo config.RepoRef) (RepoInfo, error)
	// Fork forks repo into the contrib account, under name.
	Fork(ctx context.Context, repo config.RepoRef, name string) (RepoInfo, error)
	// PushMirrors counts a repository's push mirrors.
	PushMirrors(ctx context.Context, repo config.RepoRef) (int, error)
	// CreatePullRequest opens a pull request on repo from head, given as
	// owner:branch, into base.
	CreatePullRequest(ctx context.Context, repo config.RepoRef, head, base, title, body string) (PullRequest, error)
	DeleteBranch(ctx context.Context, repo config.RepoRef, branch string) error
}

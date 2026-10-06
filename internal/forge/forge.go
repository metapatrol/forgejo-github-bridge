// Package forge is what the reconciler knows about a forge: issues and
// comments, in a shape both GitHub and Forgejo can be read into.
//
// The two clients translate their APIs into these types and nothing else
// crosses. The reconciler therefore cannot depend on a field only one forge
// has, which is what keeps it symmetric: every rule in it applies in both
// directions or is written out as a case of its own.
package forge

import (
	"context"
	"errors"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
)

// ErrNotFound is returned for an issue or comment that does not exist, or no
// longer does.
var ErrNotFound = errors.New("not found")

// ErrRejected is returned when a forge refuses a well-formed request for what
// it asks, such as a review comment on a line the diff does not have.
var ErrRejected = errors.New("rejected")

// Issue is an issue or a pull request. The issue APIs of both forges return
// pull requests too; IsPullRequest says which one this is.
type Issue struct {
	Number        int64
	Title         string
	Body          string
	Closed        bool
	Author        string
	Labels        []string
	IsPullRequest bool
	URL           string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Comment is a conversation comment on an issue or a pull request.
type Comment struct {
	ID          int64
	IssueNumber int64
	Body        string
	Author      string
	URL         string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	// Attachments counts files attached to the comment outside its body.
	// Forgejo has them; GitHub embeds uploads in the body instead.
	Attachments int
}

// IssueEdit changes an issue. A nil field is left as it is.
type IssueEdit struct {
	Title  *string
	Body   *string
	Closed *bool
}

// Client is one forge, as the bridge uses it.
type Client interface {
	// Self returns the login this client acts as. The reconciler ignores
	// everything this login wrote, which is what prevents loops.
	Self(ctx context.Context) (string, error)

	// ListIssuesSince returns issues and pull requests updated at or after
	// since, in no particular order. The zero time returns all of them.
	ListIssuesSince(ctx context.Context, repo config.RepoRef, since time.Time) ([]Issue, error)
	// ListCommentsSince returns comments on any issue or pull request of the
	// repository updated at or after since.
	ListCommentsSince(ctx context.Context, repo config.RepoRef, since time.Time) ([]Comment, error)
	// ListIssuesByAuthor returns issues opened by author and updated at or
	// after since. It is how the bridge finds its own projections when the
	// database does not know about them.
	ListIssuesByAuthor(ctx context.Context, repo config.RepoRef, author string, since time.Time) ([]Issue, error)

	GetIssue(ctx context.Context, repo config.RepoRef, number int64) (Issue, error)
	CreateIssue(ctx context.Context, repo config.RepoRef, title, body string, labels []string) (Issue, error)
	EditIssue(ctx context.Context, repo config.RepoRef, number int64, edit IssueEdit) (Issue, error)

	// ListComments returns every comment on one issue, oldest first.
	ListComments(ctx context.Context, repo config.RepoRef, number int64) ([]Comment, error)
	CreateComment(ctx context.Context, repo config.RepoRef, number int64, body string) (Comment, error)
	EditComment(ctx context.Context, repo config.RepoRef, id int64, body string) (Comment, error)
	DeleteComment(ctx context.Context, repo config.RepoRef, id int64) error
}

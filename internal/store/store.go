// Package store is the bridge's state: which issue and comment on one side
// corresponds to which on the other, how far each poll has read, and which
// resources are waiting to be reconciled.
//
// The mappings are the authority on correspondence (CONCEPT.md §7). Markers in
// the projected text exist to rebuild them, never to overrule them.
package store

import (
	"context"
	"errors"
	"time"
)

// ErrConflict is returned when a mapping would claim an issue or comment that
// another mapping already has.
var ErrConflict = errors.New("mapping conflict")

// Side is one of the two forges.
type Side string

// The two sides.
const (
	GitHub  Side = "github"
	Forgejo Side = "forgejo"
)

// Stream is what a poll cursor tracks on one side of one repository.
type Stream string

// The polled streams.
const (
	Issues   Stream = "issues"
	Comments Stream = "comments"
)

// IssueMapping pairs a GitHub issue with a Forgejo issue.
type IssueMapping struct {
	ID            int64
	Repository    string
	GitHubNumber  int64
	ForgejoNumber int64
	// Origin is the side the issue was opened on. That side is authoritative
	// for title and body; the other holds the projection.
	Origin Side
	// SyncedClosed is the state both sides had after the last reconcile. A
	// side that differs from it is the side that changed.
	SyncedClosed bool
}

// CommentMapping pairs a GitHub comment with a Forgejo comment on a mapped
// issue.
type CommentMapping struct {
	ID               int64
	IssueMappingID   int64
	GitHubCommentID  int64
	ForgejoCommentID int64
	Origin           Side
}

// DirtyKey names a resource that needs reconciling: an issue on one side of a
// repository. Issues that are mapped are keyed by their GitHub number, so the
// two halves of a pair coalesce into one entry.
type DirtyKey struct {
	Repository string
	Side       Side
	Number     int64
}

// Dirty is a queued resource.
type Dirty struct {
	DirtyKey
	// MarkedAt is when it was last marked. Clearing an entry only succeeds if
	// nothing marked it again while it was being reconciled.
	MarkedAt time.Time
	Attempts int
}

// Store is everything the bridge persists.
type Store interface {
	IssueByGitHub(ctx context.Context, repository string, number int64) (IssueMapping, bool, error)
	IssueByForgejo(ctx context.Context, repository string, number int64) (IssueMapping, bool, error)
	// InsertIssue records a new pair. ErrConflict if either number is already
	// mapped.
	InsertIssue(ctx context.Context, m IssueMapping) (IssueMapping, error)
	SetIssueSyncedClosed(ctx context.Context, id int64, closed bool) error
	// DeleteIssue removes a pair and its comment mappings.
	DeleteIssue(ctx context.Context, id int64) error
	// OpenIssues lists the pairs of a repository that were open at their last
	// reconcile.
	OpenIssues(ctx context.Context, repository string) ([]IssueMapping, error)

	Comments(ctx context.Context, issueMappingID int64) ([]CommentMapping, error)
	// InsertComment records a new comment pair. ErrConflict if either ID is
	// already mapped.
	InsertComment(ctx context.Context, c CommentMapping) (CommentMapping, error)
	// UpdateComment stores new comment IDs for an existing pair, after a
	// projection was recreated.
	UpdateComment(ctx context.Context, c CommentMapping) error
	DeleteComment(ctx context.Context, id int64) error

	// Cursor returns where the poll of one stream last got to.
	Cursor(ctx context.Context, repository string, side Side, stream Stream) (time.Time, bool, error)
	SetCursor(ctx context.Context, repository string, side Side, stream Stream, at time.Time) error

	// MarkDirty queues resources. A resource already queued is marked again
	// without losing its backoff.
	MarkDirty(ctx context.Context, keys ...DirtyKey) error
	// NextDirty returns the queued resource that has been due the longest.
	NextDirty(ctx context.Context) (Dirty, bool, error)
	// ClearDirty removes an entry that was reconciled, unless it was marked
	// again in the meantime. It reports whether it removed it.
	ClearDirty(ctx context.Context, d Dirty) (bool, error)
	// DeferDirty counts a failed attempt and holds the entry back until the
	// given time.
	DeferDirty(ctx context.Context, d Dirty, until time.Time, reason string) error
	// DirtyStats returns the queue length and the time the oldest entry was
	// marked (zero when empty).
	DirtyStats(ctx context.Context) (int, time.Time, error)
}

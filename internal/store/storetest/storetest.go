// Package storetest is the contract every store.Store implements, as tests.
//
// It runs against the in-memory store in the unit tests and against
// PostgreSQL in the integration tests. The reconciler's tests use the memory
// store, so this is what makes their result mean something for production.
package storetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// Run runs the contract. newStore must return an empty store each time.
func Run(t *testing.T, newStore func(t *testing.T) store.Store) {
	for name, test := range map[string]func(*testing.T, store.Store){
		"issue mappings":            issueMappings,
		"comment mappings":          commentMappings,
		"cursors":                   cursors,
		"dirty queue":               dirtyQueue,
		"marking keeps the backoff": markingKeepsBackoff,
	} {
		t.Run(name, func(t *testing.T) { test(t, newStore(t)) })
	}
}

func issueMappings(t *testing.T, s store.Store) {
	ctx := context.Background()
	m, err := s.InsertIssue(ctx, store.IssueMapping{Repository: "a/b", GitHubNumber: 42, ForgejoNumber: 71, Origin: store.GitHub})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID == 0 {
		t.Error("no ID assigned")
	}
	for _, lookup := range []func() (store.IssueMapping, bool, error){
		func() (store.IssueMapping, bool, error) { return s.IssueByGitHub(ctx, "a/b", 42) },
		func() (store.IssueMapping, bool, error) { return s.IssueByForgejo(ctx, "a/b", 71) },
	} {
		got, ok, err := lookup()
		if err != nil || !ok || got != m {
			t.Errorf("lookup = %+v, %v, %v; want %+v", got, ok, err, m)
		}
	}
	if _, ok, _ := s.IssueByGitHub(ctx, "a/c", 42); ok {
		t.Error("a mapping leaked into another repository")
	}

	for _, dup := range []store.IssueMapping{
		{Repository: "a/b", GitHubNumber: 42, ForgejoNumber: 99, Origin: store.GitHub},
		{Repository: "a/b", GitHubNumber: 99, ForgejoNumber: 71, Origin: store.Forgejo},
	} {
		if _, err := s.InsertIssue(ctx, dup); !errors.Is(err, store.ErrConflict) {
			t.Errorf("insert %+v: want ErrConflict, have %v", dup, err)
		}
	}
	if _, err := s.InsertIssue(ctx, store.IssueMapping{Repository: "a/c", GitHubNumber: 42, ForgejoNumber: 71, Origin: store.GitHub}); err != nil {
		t.Errorf("the same numbers in another repository: %v", err)
	}

	if open, _ := s.OpenIssues(ctx, "a/b"); len(open) != 1 {
		t.Errorf("open = %+v", open)
	}
	if err := s.SetIssueSyncedClosed(ctx, m.ID, true); err != nil {
		t.Fatal(err)
	}
	if open, _ := s.OpenIssues(ctx, "a/b"); len(open) != 0 {
		t.Errorf("a closed pair is still open: %+v", open)
	}
	if got, _, _ := s.IssueByGitHub(ctx, "a/b", 42); !got.SyncedClosed {
		t.Error("synced state not stored")
	}

	if _, err := s.InsertComment(ctx, store.CommentMapping{IssueMappingID: m.ID, GitHubCommentID: 1, ForgejoCommentID: 2, Origin: store.GitHub}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteIssue(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.IssueByGitHub(ctx, "a/b", 42); ok {
		t.Error("deleted mapping still found")
	}
	if cs, _ := s.Comments(ctx, m.ID); len(cs) != 0 {
		t.Error("comment mappings survived their issue")
	}
}

func commentMappings(t *testing.T, s store.Store) {
	ctx := context.Background()
	m, err := s.InsertIssue(ctx, store.IssueMapping{Repository: "a/b", GitHubNumber: 1, ForgejoNumber: 2, Origin: store.GitHub})
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.InsertComment(ctx, store.CommentMapping{IssueMappingID: m.ID, GitHubCommentID: 10, ForgejoCommentID: 20, Origin: store.Forgejo})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertComment(ctx, store.CommentMapping{IssueMappingID: m.ID, GitHubCommentID: 10, ForgejoCommentID: 21, Origin: store.GitHub}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("a second projection of one comment: want ErrConflict, have %v", err)
	}
	if _, err := s.InsertComment(ctx, store.CommentMapping{IssueMappingID: m.ID + 1000, GitHubCommentID: 11, ForgejoCommentID: 22, Origin: store.GitHub}); !errors.Is(err, store.ErrConflict) {
		t.Errorf("a comment on a missing issue mapping: want ErrConflict, have %v", err)
	}

	c.ForgejoCommentID = 30
	if err := s.UpdateComment(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Comments(ctx, m.ID)
	if len(got) != 1 || got[0] != c {
		t.Errorf("comments = %+v, want [%+v]", got, c)
	}
	if err := s.DeleteComment(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Comments(ctx, m.ID); len(got) != 0 {
		t.Errorf("deleted comment mapping still there: %+v", got)
	}
}

func cursors(t *testing.T, s store.Store) {
	ctx := context.Background()
	if _, ok, err := s.Cursor(ctx, "a/b", store.GitHub, store.Issues); ok || err != nil {
		t.Fatalf("a cursor that was never set: %v, %v", ok, err)
	}
	at := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	for _, v := range []time.Time{at, at.Add(time.Minute)} {
		if err := s.SetCursor(ctx, "a/b", store.GitHub, store.Issues, v); err != nil {
			t.Fatal(err)
		}
		got, ok, err := s.Cursor(ctx, "a/b", store.GitHub, store.Issues)
		if err != nil || !ok || !got.Equal(v) {
			t.Errorf("cursor = %v, %v, %v; want %v", got, ok, err, v)
		}
	}
	if _, ok, _ := s.Cursor(ctx, "a/b", store.Forgejo, store.Issues); ok {
		t.Error("cursors of the two sides are not separate")
	}
}

func dirtyQueue(t *testing.T, s store.Store) {
	ctx := context.Background()
	k := store.DirtyKey{Repository: "a/b", Side: store.GitHub, Number: 42}
	if err := s.MarkDirty(ctx, k, k); err != nil {
		t.Fatal(err)
	}
	if n, oldest, _ := s.DirtyStats(ctx); n != 1 || oldest.IsZero() {
		t.Fatalf("marking twice must coalesce: n=%d oldest=%v", n, oldest)
	}

	d, ok, err := s.NextDirty(ctx)
	if err != nil || !ok || d.DirtyKey != k {
		t.Fatalf("next = %+v, %v, %v", d, ok, err)
	}

	// Marked again while being reconciled: clearing must not lose that.
	if err := s.MarkDirty(ctx, k); err != nil {
		t.Fatal(err)
	}
	if cleared, err := s.ClearDirty(ctx, d); err != nil || cleared {
		t.Fatalf("cleared a resource that was marked again: %v, %v", cleared, err)
	}
	d, _, _ = s.NextDirty(ctx)
	if cleared, err := s.ClearDirty(ctx, d); err != nil || !cleared {
		t.Fatalf("clear = %v, %v", cleared, err)
	}
	if _, ok, _ := s.NextDirty(ctx); ok {
		t.Error("the queue is not empty")
	}
	if n, oldest, _ := s.DirtyStats(ctx); n != 0 || !oldest.IsZero() {
		t.Errorf("stats of an empty queue: %d, %v", n, oldest)
	}
}

func markingKeepsBackoff(t *testing.T, s store.Store) {
	ctx := context.Background()
	k := store.DirtyKey{Repository: "a/b", Side: store.Forgejo, Number: 7}
	if err := s.MarkDirty(ctx, k); err != nil {
		t.Fatal(err)
	}
	d, _, _ := s.NextDirty(ctx)
	if err := s.DeferDirty(ctx, d, time.Now().Add(time.Hour), "forgejo is down"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDirty(ctx, k); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.NextDirty(ctx); ok {
		t.Error("marking a failing resource again cancelled its backoff")
	}
	if n, _, _ := s.DirtyStats(ctx); n != 1 {
		t.Errorf("a deferred resource left the queue: %d", n)
	}
}

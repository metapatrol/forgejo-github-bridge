// Package memory is an in-memory Store for tests.
//
// It implements the same contract as the PostgreSQL store, and the contract
// tests in storetest run against both, so a test that passes here is not
// passing because of a semantic only this implementation has.
package memory

import (
	"context"
	"sort"
	"sync"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// Store is an in-memory store.Store.
type Store struct {
	mu       sync.Mutex
	nextID   int64
	issues   map[int64]store.IssueMapping
	comments map[int64]store.CommentMapping
	pulls    map[int64]store.PullRequestState
	cursors  map[string]time.Time
	dirty    map[store.DirtyKey]*entry
	now      func() time.Time
}

type entry struct {
	markedAt  time.Time
	notBefore time.Time
	attempts  int
}

var _ store.Store = (*Store)(nil)

// New creates an empty store.
func New() *Store {
	return &Store{
		issues:   map[int64]store.IssueMapping{},
		comments: map[int64]store.CommentMapping{},
		pulls:    map[int64]store.PullRequestState{},
		cursors:  map[string]time.Time{},
		dirty:    map[store.DirtyKey]*entry{},
		now:      time.Now,
	}
}

// SetClock replaces the clock, for tests of backoff.
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = now
}

// tick returns a timestamp strictly after the previous one, as
// clock_timestamp() does for successive statements.
func (s *Store) tick(prev time.Time) time.Time {
	t := s.now()
	if !t.After(prev) {
		t = prev.Add(time.Microsecond)
	}
	return t
}

func (s *Store) id() int64 {
	s.nextID++
	return s.nextID
}

func (s *Store) IssueByGitHub(_ context.Context, repo string, n int64) (store.IssueMapping, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.issues {
		if m.Repository == repo && m.GitHubNumber == n {
			return m, true, nil
		}
	}
	return store.IssueMapping{}, false, nil
}

func (s *Store) IssueByForgejo(_ context.Context, repo string, n int64) (store.IssueMapping, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.issues {
		if m.Repository == repo && m.ForgejoNumber == n {
			return m, true, nil
		}
	}
	return store.IssueMapping{}, false, nil
}

func (s *Store) InsertIssue(_ context.Context, m store.IssueMapping) (store.IssueMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.issues {
		if o.Repository == m.Repository && (o.GitHubNumber == m.GitHubNumber || o.ForgejoNumber == m.ForgejoNumber) {
			return store.IssueMapping{}, store.ErrConflict
		}
	}
	m.ID = s.id()
	if m.Kind == "" {
		m.Kind = store.KindIssue
	}
	s.issues[m.ID] = m
	return m, nil
}

func (s *Store) SetIssueSyncedClosed(_ context.Context, id int64, closed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.issues[id]; ok {
		m.SyncedClosed = closed
		s.issues[id] = m
	}
	return nil
}

func (s *Store) SetIssueSummary(_ context.Context, id int64, title, author string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.issues[id]; ok {
		m.Title, m.Author = title, author
		s.issues[id] = m
	}
	return nil
}

func (s *Store) DeleteIssue(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.issues, id)
	delete(s.pulls, id)
	for cid, c := range s.comments {
		if c.IssueMappingID == id {
			delete(s.comments, cid)
		}
	}
	return nil
}

func (s *Store) OpenIssues(_ context.Context, repo string) ([]store.IssueMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.IssueMapping
	for _, m := range s.issues {
		if m.Repository == repo && !m.SyncedClosed {
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) PullRequest(_ context.Context, id int64) (store.PullRequestState, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.pulls[id]
	return p, ok, nil
}

func (s *Store) SavePullRequest(_ context.Context, p store.PullRequestState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.issues[p.IssueMappingID]; !ok {
		return store.ErrConflict
	}
	s.pulls[p.IssueMappingID] = p
	return nil
}

func (s *Store) Comments(_ context.Context, issueID int64) ([]store.CommentMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []store.CommentMapping
	for _, c := range s.comments {
		if c.IssueMappingID == issueID {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (s *Store) InsertComment(_ context.Context, c store.CommentMapping) (store.CommentMapping, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.issues[c.IssueMappingID]; !ok {
		return store.CommentMapping{}, store.ErrConflict
	}
	for _, o := range s.comments {
		if o.GitHubCommentID == c.GitHubCommentID || o.ForgejoCommentID == c.ForgejoCommentID {
			return store.CommentMapping{}, store.ErrConflict
		}
	}
	c.ID = s.id()
	s.comments[c.ID] = c
	return c, nil
}

func (s *Store) UpdateComment(_ context.Context, c store.CommentMapping) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, o := range s.comments {
		if o.ID != c.ID && (o.GitHubCommentID == c.GitHubCommentID || o.ForgejoCommentID == c.ForgejoCommentID) {
			return store.ErrConflict
		}
	}
	if _, ok := s.comments[c.ID]; ok {
		s.comments[c.ID] = c
	}
	return nil
}

func (s *Store) DeleteComment(_ context.Context, id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.comments, id)
	return nil
}

func cursorKey(repo string, side store.Side, stream store.Stream) string {
	return repo + "\x00" + string(side) + "\x00" + string(stream)
}

func (s *Store) Cursor(_ context.Context, repo string, side store.Side, stream store.Stream) (time.Time, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.cursors[cursorKey(repo, side, stream)]
	return t, ok, nil
}

func (s *Store) SetCursor(_ context.Context, repo string, side store.Side, stream store.Stream, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursors[cursorKey(repo, side, stream)] = at
	return nil
}

func (s *Store) MarkDirty(_ context.Context, keys ...store.DirtyKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		if e, ok := s.dirty[k]; ok {
			e.markedAt = s.tick(e.markedAt)
			continue
		}
		now := s.tick(time.Time{})
		s.dirty[k] = &entry{markedAt: now, notBefore: now}
	}
	return nil
}

func (s *Store) NextDirty(_ context.Context) (store.Dirty, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	var best store.DirtyKey
	var found *entry
	for k, e := range s.dirty {
		if e.notBefore.After(now) {
			continue
		}
		if found == nil || e.notBefore.Before(found.notBefore) ||
			(e.notBefore.Equal(found.notBefore) && less(k, best)) {
			best, found = k, e
		}
	}
	if found == nil {
		return store.Dirty{}, false, nil
	}
	return store.Dirty{DirtyKey: best, MarkedAt: found.markedAt, Attempts: found.attempts}, true, nil
}

func less(a, b store.DirtyKey) bool {
	if a.Repository != b.Repository {
		return a.Repository < b.Repository
	}
	if a.Side != b.Side {
		return a.Side < b.Side
	}
	return a.Number < b.Number
}

func (s *Store) ClearDirty(_ context.Context, d store.Dirty) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.dirty[d.DirtyKey]
	if !ok || !e.markedAt.Equal(d.MarkedAt) {
		return false, nil
	}
	delete(s.dirty, d.DirtyKey)
	return true, nil
}

func (s *Store) DeferDirty(_ context.Context, d store.Dirty, until time.Time, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.dirty[d.DirtyKey]; ok {
		e.attempts++
		e.notBefore = until
	}
	return nil
}

func (s *Store) DirtyStats(_ context.Context) (store.QueueStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := store.QueueStats{Depth: len(s.dirty)}
	for _, e := range s.dirty {
		if st.Oldest.IsZero() || e.markedAt.Before(st.Oldest) {
			st.Oldest = e.markedAt
		}
		if e.attempts > 0 {
			st.Failing++
		}
	}
	return st, nil
}

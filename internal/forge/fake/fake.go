// Package fake is an in-memory forge for tests of the reconciler.
//
// It behaves the way both real forges do in the respects the reconciler
// depends on: numbers and IDs are assigned by the forge, a comment bumps its
// issue's update time, "since" filters on update time, and a deleted item is
// ErrNotFound.
package fake

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"sync"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
)

// Forge is an in-memory forge.Client. Its methods that are not part of the
// interface act as a user of the forge would, as someone other than the
// client's own account.
//
// As returns a client for another account on the same forge: the state is
// shared, only the identity differs.
type Forge struct {
	*shared
	self string
}

type shared struct {
	mu       sync.Mutex
	first    int64
	clock    time.Time
	issues   map[string]map[int64]*forge.Issue
	comments map[int64]*stored
	nextID   int64
	pulls    map[string]map[int64]*pull
	reviews  map[string]map[int64][]forge.Review
	branches map[string]string
	// contains and behind are what GitHub knows about its branches: which
	// commits a branch holds, and how far a commit is behind it.
	contains map[string]bool
	behind   map[string]int
	// repos holds the repositories created or described through the fake;
	// any other repository exists, owned by someone else, and grants no
	// rights. rights maps "owner/name login" to what that login may do.
	repos  map[string]*forge.RepoInfo
	rights map[string][2]bool
	mirror map[string]int

	// Writes counts every change made through the forge.Client interface,
	// which is to say by the bridge.
	Writes int
	// Log records the bridge's comments and closes in order, as
	// "comment #n" and "close #n".
	Log []string
	// FailNext, when set, is returned by the next call through the interface
	// and then cleared.
	FailNext error
}

type pull struct {
	headSHA  string
	headRef  string
	base     string
	merged   bool
	mergeSHA string
	files    []string
}

type stored struct {
	repo string
	c    forge.Comment
}

var _ forge.Client = (*Forge)(nil)

// New creates a forge whose client acts as self. Issue numbers start at
// first; giving each forge a different start keeps a test from passing
// because two numbers happened to agree.
func New(self string, first int64) *Forge {
	return &Forge{self: self, shared: &shared{
		first:    first,
		clock:    time.Now().Add(-time.Hour).Truncate(time.Second),
		issues:   map[string]map[int64]*forge.Issue{},
		comments: map[int64]*stored{},
		nextID:   1000,
		pulls:    map[string]map[int64]*pull{},
		reviews:  map[string]map[int64][]forge.Review{},
		branches: map[string]string{},
		contains: map[string]bool{},
		behind:   map[string]int{},
		repos:    map[string]*forge.RepoInfo{},
		rights:   map[string][2]bool{},
		mirror:   map[string]int{},
	}}
}

// As returns a client for another account of the same forge.
func (f *Forge) As(self string) *Forge { return &Forge{shared: f.shared, self: self} }

func (f *Forge) tick() time.Time {
	f.clock = f.clock.Add(time.Second)
	return f.clock
}

func (f *Forge) fail() error {
	err := f.FailNext
	f.FailNext = nil
	return err
}

func (f *Forge) repo(r config.RepoRef) map[int64]*forge.Issue {
	m, ok := f.issues[r.String()]
	if !ok {
		m = map[int64]*forge.Issue{}
		f.issues[r.String()] = m
	}
	return m
}

func (f *Forge) newNumber(r config.RepoRef) int64 {
	// Both forges number issues and pull requests from one sequence per
	// repository, and never reuse a number.
	m := f.repo(r)
	n := f.first
	for _, is := range m {
		n = max(n, is.Number+1)
	}
	return n
}

// Advance moves the forge's clock, so that later changes fall outside a
// poll's overlap window.
func (f *Forge) Advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clock = f.clock.Add(d)
}

// --- as a user of the forge ---

// Open opens an issue as author.
func (f *Forge) Open(r config.RepoRef, author, title, body string, labels ...string) forge.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.create(r, author, title, body, labels, false)
}

// OpenPullRequest opens a pull request as author.
func (f *Forge) OpenPullRequest(r config.RepoRef, author, title string) forge.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.create(r, author, title, "", nil, true)
}

func (f *Forge) create(r config.RepoRef, author, title, body string, labels []string, pr bool) forge.Issue {
	now := f.tick()
	n := f.newNumber(r)
	is := &forge.Issue{
		Number: n, Title: title, Body: body, Author: author, Labels: slices.Clone(labels),
		IsPullRequest: pr, URL: fmt.Sprintf("https://%s/%s/issues/%d", f.self, r, n),
		CreatedAt: now, UpdatedAt: now,
	}
	f.repo(r)[n] = is
	return *is
}

// Comment comments on an issue as author.
func (f *Forge) Comment(r config.RepoRef, n int64, author, body string) forge.Comment {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, err := f.addComment(r, n, author, body)
	if err != nil {
		panic(err)
	}
	return c
}

func (f *Forge) addComment(r config.RepoRef, n int64, author, body string) (forge.Comment, error) {
	is, ok := f.repo(r)[n]
	if !ok {
		return forge.Comment{}, forge.ErrNotFound
	}
	now := f.tick()
	f.nextID++
	c := forge.Comment{
		ID: f.nextID, IssueNumber: n, Body: body, Author: author,
		URL:       fmt.Sprintf("%s#issuecomment-%d", is.URL, f.nextID),
		CreatedAt: now, UpdatedAt: now,
	}
	f.comments[c.ID] = &stored{repo: r.String(), c: c}
	is.UpdatedAt = now
	return c, nil
}

// EditBody changes an issue's body, as a user would.
func (f *Forge) EditBody(r config.RepoRef, n int64, title, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	is := f.repo(r)[n]
	is.Title, is.Body, is.UpdatedAt = title, body, f.tick()
}

// SetClosed closes or reopens an issue, as a user would.
func (f *Forge) SetClosed(r config.RepoRef, n int64, closed bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	is := f.repo(r)[n]
	is.Closed, is.UpdatedAt = closed, f.tick()
}

// SetLabels replaces an issue's labels, as a user would.
func (f *Forge) SetLabels(r config.RepoRef, n int64, labels ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	is := f.repo(r)[n]
	is.Labels, is.UpdatedAt = slices.Clone(labels), f.tick()
}

// EditCommentAs changes a comment's body, as a user would.
func (f *Forge) EditCommentAs(id int64, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s := f.comments[id]
	now := f.tick()
	s.c.Body, s.c.UpdatedAt = body, now
}

// RemoveComment deletes a comment, as a user would. Deletions bump nothing:
// neither forge reports them to a poll.
func (f *Forge) RemoveComment(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.comments, id)
}

// RemoveIssue deletes an issue and its comments.
func (f *Forge) RemoveIssue(r config.RepoRef, n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.repo(r), n)
	for id, s := range f.comments {
		if s.repo == r.String() && s.c.IssueNumber == n {
			delete(f.comments, id)
		}
	}
}

// Issue returns an issue as it is now.
func (f *Forge) Issue(r config.RepoRef, n int64) (forge.Issue, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.repo(r)[n]
	if !ok {
		return forge.Issue{}, false
	}
	return *is, true
}

// Issues returns every issue of a repository, by number.
func (f *Forge) Issues(r config.RepoRef) []forge.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []forge.Issue
	for _, is := range f.repo(r) {
		out = append(out, *is)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out
}

// CommentsOf returns the comments on an issue, oldest first.
func (f *Forge) CommentsOf(r config.RepoRef, n int64) []forge.Comment {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.commentsOf(r, n)
}

func (f *Forge) commentsOf(r config.RepoRef, n int64) []forge.Comment {
	var out []forge.Comment
	for _, s := range f.comments {
		if s.repo == r.String() && s.c.IssueNumber == n {
			out = append(out, s.c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// --- forge.Client, as the bridge ---

// Self implements forge.Client.
func (f *Forge) Self(context.Context) (string, error) { return f.self, nil }

// ListIssuesSince implements forge.Client.
func (f *Forge) ListIssuesSince(_ context.Context, r config.RepoRef, since time.Time) ([]forge.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return nil, err
	}
	var out []forge.Issue
	for _, is := range f.repo(r) {
		if !is.UpdatedAt.Before(since) {
			out = append(out, *is)
		}
	}
	return out, nil
}

// ListIssuesByAuthor implements forge.Client.
func (f *Forge) ListIssuesByAuthor(_ context.Context, r config.RepoRef, author string, since time.Time) ([]forge.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return nil, err
	}
	var out []forge.Issue
	for _, is := range f.repo(r) {
		if is.Author == author && !is.UpdatedAt.Before(since) {
			out = append(out, *is)
		}
	}
	return out, nil
}

// ListCommentsSince implements forge.Client.
func (f *Forge) ListCommentsSince(_ context.Context, r config.RepoRef, since time.Time) ([]forge.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return nil, err
	}
	var out []forge.Comment
	for _, s := range f.comments {
		if s.repo == r.String() && !s.c.UpdatedAt.Before(since) {
			out = append(out, s.c)
		}
	}
	return out, nil
}

// GetIssue implements forge.Client.
func (f *Forge) GetIssue(_ context.Context, r config.RepoRef, n int64) (forge.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.Issue{}, err
	}
	is, ok := f.repo(r)[n]
	if !ok {
		return forge.Issue{}, forge.ErrNotFound
	}
	return *is, nil
}

// CreateIssue implements forge.Client.
func (f *Forge) CreateIssue(_ context.Context, r config.RepoRef, title, body string, labels []string) (forge.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.Issue{}, err
	}
	f.Writes++
	return f.create(r, f.self, title, body, labels, false), nil
}

// EditIssue implements forge.Client.
func (f *Forge) EditIssue(_ context.Context, r config.RepoRef, n int64, edit forge.IssueEdit) (forge.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.Issue{}, err
	}
	is, ok := f.repo(r)[n]
	if !ok {
		return forge.Issue{}, forge.ErrNotFound
	}
	f.Writes++
	if edit.Title != nil {
		is.Title = *edit.Title
	}
	if edit.Body != nil {
		is.Body = *edit.Body
	}
	if edit.Closed != nil {
		is.Closed = *edit.Closed
		if is.Closed {
			f.Log = append(f.Log, fmt.Sprintf("close #%d", n))
		}
	}
	is.UpdatedAt = f.tick()
	return *is, nil
}

// ListComments implements forge.Client.
func (f *Forge) ListComments(_ context.Context, r config.RepoRef, n int64) ([]forge.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return nil, err
	}
	if _, ok := f.repo(r)[n]; !ok {
		return nil, forge.ErrNotFound
	}
	return f.commentsOf(r, n), nil
}

// CreateComment implements forge.Client.
func (f *Forge) CreateComment(_ context.Context, r config.RepoRef, n int64, body string) (forge.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.Comment{}, err
	}
	f.Writes++
	f.Log = append(f.Log, fmt.Sprintf("comment #%d", n))
	return f.addComment(r, n, f.self, body)
}

// EditComment implements forge.Client.
func (f *Forge) EditComment(_ context.Context, _ config.RepoRef, id int64, body string) (forge.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.Comment{}, err
	}
	s, ok := f.comments[id]
	if !ok {
		return forge.Comment{}, forge.ErrNotFound
	}
	f.Writes++
	s.c.Body, s.c.UpdatedAt = body, f.tick()
	return s.c, nil
}

// DeleteComment implements forge.Client.
func (f *Forge) DeleteComment(_ context.Context, _ config.RepoRef, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return err
	}
	if _, ok := f.comments[id]; !ok {
		return forge.ErrNotFound
	}
	f.Writes++
	delete(f.comments, id)
	return nil
}

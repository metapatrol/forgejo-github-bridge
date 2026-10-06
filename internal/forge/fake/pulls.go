package fake

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/gitops"
)

var (
	_ forge.GitHubPulls  = (*Forge)(nil)
	_ forge.ForgejoPulls = (*Forge)(nil)
	_ forge.ContribPulls = (*Forge)(nil)
)

func (f *Forge) pullsOf(r config.RepoRef) map[int64]*pull {
	m, ok := f.pulls[r.String()]
	if !ok {
		m = map[int64]*pull{}
		f.pulls[r.String()] = m
	}
	return m
}

// --- as a user of the forge ---

// OpenPull opens a pull request as author, from a branch at headSHA, into
// base, changing files.
func (f *Forge) OpenPull(r config.RepoRef, author, title, body, base, headSHA string, files ...string) forge.Issue {
	f.mu.Lock()
	defer f.mu.Unlock()
	is := f.create(r, author, title, body, nil, true)
	f.pullsOf(r)[is.Number] = &pull{headSHA: headSHA, headRef: "feature", base: base, files: files, shas: []string{headSHA}}
	return is
}

// PushPull moves a pull request's head, as its author pushing would.
func (f *Forge) PushPull(r config.RepoRef, n int64, sha string, files ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pullsOf(r)[n]
	p.headSHA = sha
	p.shas = append(p.shas, sha)
	p.files = append(p.files, files...)
	f.repo(r)[n].UpdatedAt = f.tick()
}

// RetargetPull changes a pull request's base, as its author would.
func (f *Forge) RetargetPull(r config.RepoRef, n int64, base string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pullsOf(r)[n].base = base
	f.repo(r)[n].UpdatedAt = f.tick()
}

// Mirror records that branch now holds sha, as a push mirror run would.
// Like GitHub, it marks an open pull request into branch whose head is sha
// as merged.
func (f *Forge) Mirror(r config.RepoRef, branch, sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contains[branch+"@"+sha] = true
	for n, p := range f.pullsOf(r) {
		is := f.repo(r)[n]
		if p.base == branch && p.headSHA == sha && !is.Closed {
			p.merged, p.mergeSHA, is.Closed, is.UpdatedAt = true, sha, true, f.tick()
		}
	}
}

// SetBehind records how far sha is behind the base branches.
func (f *Forge) SetBehind(sha string, n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.behind[sha] = n
}

// MergePull merges a pull request, as a maintainer would. mergeSHA is where
// the base ends up: the head itself for a fast-forward, a new commit for a
// rebase.
func (f *Forge) MergePull(r config.RepoRef, n int64, mergeSHA string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.pullsOf(r)[n]
	is := f.repo(r)[n]
	p.headSHA = f.branches[p.headRef]
	p.merged, p.mergeSHA, is.Closed, is.UpdatedAt = true, mergeSHA, true, f.tick()
}

// Review submits a review, as author.
func (f *Forge) Review(r config.RepoRef, n int64, author, state, body string) forge.Review {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	rv := forge.Review{ID: f.nextID, Author: author, State: state, Body: body, SubmittedAt: f.tick()}
	if f.reviews[r.String()] == nil {
		f.reviews[r.String()] = map[int64][]forge.Review{}
	}
	f.reviews[r.String()][n] = append(f.reviews[r.String()][n], rv)
	f.repo(r)[n].UpdatedAt = f.clock
	return rv
}

// LineComment adds a line comment to review reviewID, as author: in Forgejo,
// a maintainer commenting on a line of the diff.
func (f *Forge) LineComment(r config.RepoRef, n, reviewID int64, author, body, path string, line int, side, commit string) forge.LineComment {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	at := f.tick()
	lc := &forge.LineComment{ID: f.nextID, ReviewID: reviewID, Author: author, Body: body, Path: path, Line: line,
		Side: side, CommitID: commit, CreatedAt: at, UpdatedAt: at}
	f.linesOf(r)[n] = append(f.linesOf(r)[n], lc)
	f.repo(r)[n].UpdatedAt = at
	return *lc
}

// EditLineCommentAs changes a line comment's body, as its author would.
func (f *Forge) EditLineCommentAs(id int64, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if lc := f.line(id); lc != nil {
		lc.Body, lc.UpdatedAt = body, f.tick()
	}
}

// DeleteLineCommentAs removes a line comment, as somebody would.
func (f *Forge) DeleteLineCommentAs(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removeLine(id)
}

// LineComments lists the line comments of a pull request.
func (f *Forge) LineComments(r config.RepoRef, n int64) []forge.LineComment {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []forge.LineComment
	for _, lc := range f.linesOf(r)[n] {
		out = append(out, *lc)
	}
	return out
}

func (f *Forge) linesOf(r config.RepoRef) map[int64][]*forge.LineComment {
	m, ok := f.lines[r.String()]
	if !ok {
		m = map[int64][]*forge.LineComment{}
		f.lines[r.String()] = m
	}
	return m
}

func (f *Forge) line(id int64) *forge.LineComment {
	for _, byPull := range f.lines {
		for _, lcs := range byPull {
			for _, lc := range lcs {
				if lc.ID == id {
					return lc
				}
			}
		}
	}
	return nil
}

func (f *Forge) removeLine(id int64) bool {
	for _, byPull := range f.lines {
		for n, lcs := range byPull {
			for i, lc := range lcs {
				if lc.ID == id {
					byPull[n] = append(lcs[:i:i], lcs[i+1:]...)
					return true
				}
			}
		}
	}
	return false
}

// ListReviewComments implements forge.GitHubPulls.
func (f *Forge) ListReviewComments(_ context.Context, r config.RepoRef, n int64) ([]forge.LineComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return nil, err
	}
	var out []forge.LineComment
	for _, lc := range f.linesOf(r)[n] {
		out = append(out, *lc)
	}
	return out, nil
}

// CreateReviewComment implements forge.GitHubPulls. Like GitHub, it places a
// comment only on a commit the pull request had as its head.
func (f *Forge) CreateReviewComment(_ context.Context, r config.RepoRef, n int64, c forge.LineComment) (forge.LineComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.LineComment{}, err
	}
	p, ok := f.pullsOf(r)[n]
	if !ok {
		return forge.LineComment{}, forge.ErrNotFound
	}
	if !slices.Contains(p.shas, c.CommitID) || c.Line <= 0 || c.Path == "" {
		return forge.LineComment{}, fmt.Errorf("pull request review comment on %s:%d at %s: %w", c.Path, c.Line, c.CommitID, forge.ErrRejected)
	}
	f.Writes++
	f.nextID++
	at := f.tick()
	lc := &forge.LineComment{ID: f.nextID, Author: f.self, Body: c.Body, Path: c.Path, Line: c.Line, Side: c.Side,
		CommitID: c.CommitID, CreatedAt: at, UpdatedAt: at}
	f.linesOf(r)[n] = append(f.linesOf(r)[n], lc)
	f.Log = append(f.Log, fmt.Sprintf("line #%d", n))
	return *lc, nil
}

// EditReviewComment implements forge.GitHubPulls.
func (f *Forge) EditReviewComment(_ context.Context, _ config.RepoRef, id int64, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return err
	}
	lc := f.line(id)
	if lc == nil {
		return forge.ErrNotFound
	}
	f.Writes++
	lc.Body, lc.UpdatedAt = body, f.tick()
	return nil
}

// DeleteReviewComment implements forge.GitHubPulls.
func (f *Forge) DeleteReviewComment(_ context.Context, _ config.RepoRef, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return err
	}
	if !f.removeLine(id) {
		return forge.ErrNotFound
	}
	f.Writes++
	return nil
}

// PushBranch moves a branch, as someone other than the bridge pushing to it.
func (f *Forge) PushBranch(branch, sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.branches[branch] = sha
}

// Branch returns where a branch is.
func (f *Forge) Branch(branch string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	sha, ok := f.branches[branch]
	return sha, ok
}

// --- forge.GitHubPulls, forge.ForgejoPulls, forge.ContribPulls ---

// GetPullRequest implements forge.GitHubPulls and forge.ForgejoPulls.
func (f *Forge) GetPullRequest(_ context.Context, r config.RepoRef, n int64) (forge.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.PullRequest{}, err
	}
	is, ok := f.repo(r)[n]
	p, isPull := f.pullsOf(r)[n]
	if !ok || !isPull {
		return forge.PullRequest{}, forge.ErrNotFound
	}
	head := p.headSHA
	if sha, ok := f.branches[p.headRef]; ok && !p.merged {
		head = sha
	}
	return forge.PullRequest{
		Number: n, Title: is.Title, Body: is.Body, Author: is.Author, URL: is.URL,
		Open: !is.Closed, Merged: p.merged, MergeCommitSHA: p.mergeSHA,
		HeadSHA: head, HeadRef: p.headRef, BaseRef: p.base,
	}, nil
}

// PullRequestFiles implements forge.GitHubPulls.
func (f *Forge) PullRequestFiles(_ context.Context, r config.RepoRef, n int64) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.pullsOf(r)[n].files), nil
}

// Contains implements forge.GitHubPulls.
func (f *Forge) Contains(_ context.Context, _ config.RepoRef, branch, sha string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.contains[branch+"@"+sha], nil
}

// BehindBy implements forge.GitHubPulls.
func (f *Forge) BehindBy(_ context.Context, _ config.RepoRef, _, sha string) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.behind[sha], nil
}

// ListReviews implements forge.ForgejoPulls.
func (f *Forge) ListReviews(_ context.Context, r config.RepoRef, n int64) ([]forge.Review, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.reviews[r.String()][n]), nil
}

// ListReviewLineComments implements forge.ForgejoPulls.
func (f *Forge) ListReviewLineComments(_ context.Context, r config.RepoRef, n, reviewID int64) ([]forge.LineComment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return nil, err
	}
	var out []forge.LineComment
	for _, lc := range f.linesOf(r)[n] {
		if lc.ReviewID == reviewID {
			out = append(out, *lc)
		}
	}
	return out, nil
}

// AddLabels implements forge.ForgejoPulls.
func (f *Forge) AddLabels(_ context.Context, r config.RepoRef, n int64, labels []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	is, ok := f.repo(r)[n]
	if !ok {
		return forge.ErrNotFound
	}
	f.Writes++
	for _, l := range labels {
		if !slices.Contains(is.Labels, l) {
			is.Labels = append(is.Labels, l)
		}
	}
	return nil
}

// SetBase implements forge.ForgejoPulls.
func (f *Forge) SetBase(_ context.Context, r config.RepoRef, n int64, base string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Writes++
	f.pullsOf(r)[n].base = base
	return nil
}

// CreatePullRequest implements forge.ContribPulls.
func (f *Forge) CreatePullRequest(_ context.Context, r config.RepoRef, head, base, title, body string) (forge.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.PullRequest{}, err
	}
	_, branch, _ := strings.Cut(head, ":")
	sha, ok := f.branches[branch]
	if !ok {
		return forge.PullRequest{}, forge.ErrNotFound
	}
	f.Writes++
	is := f.create(r, f.self, title, body, nil, true)
	f.pullsOf(r)[is.Number] = &pull{headSHA: sha, headRef: branch, base: base}
	return forge.PullRequest{Number: is.Number, Title: title, Body: body, Author: f.self, Open: true,
		HeadSHA: sha, HeadRef: branch, BaseRef: base}, nil
}

// DeleteBranch implements forge.ContribPulls.
func (f *Forge) DeleteBranch(_ context.Context, _ config.RepoRef, branch string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.branches[branch]; ok {
		f.Writes++
		delete(f.branches, branch)
	}
	return nil
}

// Grant gives login push, and optionally admin, rights on repo.
func (f *Forge) Grant(r config.RepoRef, login string, push, admin bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rights[r.String()+" "+login] = [2]bool{push, admin}
}

// AddRepository creates a repository as its owner would, e.g. a
// repository in the contrib account's namespace that is not a fork.
func (f *Forge) AddRepository(r config.RepoRef, info forge.RepoInfo) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info.FullName = r.String()
	f.repos[r.String()] = &info
}

// DeleteRepository deletes a repository created through the fake.
func (f *Forge) DeleteRepository(r config.RepoRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.repos, r.String())
}

// AddPushMirror adds a push mirror to repo.
func (f *Forge) AddPushMirror(r config.RepoRef) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mirror[r.String()]++
}

// Forks counts the repositories created through Fork.
func (f *Forge) Forks() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.repos {
		if r.Fork {
			n++
		}
	}
	return n
}

func (f *Forge) describe(r config.RepoRef) (forge.RepoInfo, bool) {
	var info forge.RepoInfo
	if known, ok := f.repos[r.String()]; ok {
		info = *known
	} else if r.Owner == f.self {
		return forge.RepoInfo{}, false
	} else {
		info = forge.RepoInfo{FullName: r.String()}
	}
	rights := f.rights[r.String()+" "+f.self]
	info.CanPush, info.IsAdmin = rights[0] || r.Owner == f.self, rights[1] || r.Owner == f.self
	return info, true
}

// Repository implements forge.ContribPulls.
func (f *Forge) Repository(_ context.Context, r config.RepoRef) (forge.RepoInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.RepoInfo{}, err
	}
	info, ok := f.describe(r)
	if !ok {
		return forge.RepoInfo{}, forge.ErrNotFound
	}
	return info, nil
}

// Fork implements forge.ContribPulls.
func (f *Forge) Fork(_ context.Context, r config.RepoRef, name string) (forge.RepoInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail(); err != nil {
		return forge.RepoInfo{}, err
	}
	fork := config.RepoRef{Owner: f.self, Name: name}
	if _, ok := f.repos[fork.String()]; ok {
		return forge.RepoInfo{}, fmt.Errorf("repository %s already exists", fork)
	}
	f.Writes++
	f.repos[fork.String()] = &forge.RepoInfo{FullName: fork.String(), Fork: true, Parent: r.String()}
	info, _ := f.describe(fork)
	return info, nil
}

// PushMirrors implements forge.ContribPulls.
func (f *Forge) PushMirrors(_ context.Context, r config.RepoRef) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mirror[r.String()], nil
}

// Git is a fake of the bridge's Git: it moves a GitHub pull request's head
// into a branch of the Forgejo fake, with the same lease the real one takes.
type Git struct {
	GitHub  *Forge
	Forgejo *Forge
	Imports int
}

// Import implements bridge.Git.
func (g *Git) Import(_ context.Context, repo config.Repository, n int64, _, branch, expected string) (string, error) {
	g.GitHub.mu.Lock()
	sha := g.GitHub.pullsOf(repo.GitHub)[n].headSHA
	g.GitHub.mu.Unlock()

	g.Forgejo.mu.Lock()
	defer g.Forgejo.mu.Unlock()
	if g.Forgejo.branches[branch] != expected {
		return "", gitops.ErrLeaseRejected
	}
	g.Forgejo.branches[branch] = sha
	g.Imports++
	return sha, nil
}

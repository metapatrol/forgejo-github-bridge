// Package console serves the read-only console (docs/console.md): the API
// generated from api/openapi-v1.yaml, the specification itself, and the
// embedded web application.
//
// Everything it shows comes from the bridge's database through
// store.Reports. It never asks GitHub or Forgejo anything: the console costs
// no rate limit, and no path leads from it to the bridge's credentials.
package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/api"
	v1 "forgejo.metapatrol.local/platform/forgejo-github-bridge/api/v1"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/ui"
)

// Info is what the console says about the instance serving it.
type Info struct {
	Version, Commit, BuiltAt string
	// Instance is the host name.
	Instance string
	// Mode is "bridge" or "console".
	Mode string
}

// Options configure a console.
type Options struct {
	Info         Info
	Repositories []config.Repository
	// ForgejoWebURL is the Forgejo address for people's browsers.
	ForgejoWebURL *url.URL
	// GitHubWebURL defaults to https://github.com.
	GitHubWebURL *url.URL
	// UI is the web application; nil serves the API alone.
	UI  fs.FS
	Log *slog.Logger
}

// requestTimeout bounds every API request's queries.
const requestTimeout = 10 * time.Second

type server struct {
	reports store.Reports
	opts    Options
	repos   map[string]config.Repository
}

// New returns the console's handler.
func New(reports store.Reports, opts Options) http.Handler {
	if opts.GitHubWebURL == nil {
		opts.GitHubWebURL, _ = url.Parse("https://github.com")
	}
	s := &server{reports: reports, opts: opts, repos: map[string]config.Repository{}}
	for _, r := range opts.Repositories {
		s.repos[r.Key()] = r
	}

	mux := http.NewServeMux()
	v1.HandlerWithOptions(s, v1.StdHTTPServerOptions{
		BaseURL:    "/api/v1",
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeError(w, http.StatusBadRequest, err.Error())
		},
	})
	mux.HandleFunc("GET /api/v1/openapi.yaml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		_, _ = w.Write(api.OpenAPIV1)
	})
	mux.HandleFunc("GET /api/", func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "no such endpoint")
	})
	if opts.UI != nil {
		mux.Handle("GET /", ui.Handler(opts.UI))
	} else {
		mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "this binary was built without the console's web application; the API is at /api/v1", http.StatusNotFound)
		})
	}
	return secure(mux)
}

// secure sets the headers every response carries. The console is read-only
// and same-origin: no framing, no sniffing, no referrer to the forges it
// links to.
func secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; "+
			"font-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'none'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			h.Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, "the console is read-only")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v1.Error{Message: msg})
}

// fail reports an error from the database. The message stays in the log:
// it may name tables and queries, which the reader has no use for.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	s.opts.Log.Error("console request failed", "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "reading the bridge's database failed; see the bridge's log")
}

func ctxFor(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), requestTimeout)
}

// GetStatus implements v1.ServerInterface.
func (s *server) GetStatus(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFor(r)
	defer cancel()
	leader, err := s.reports.Leader(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	stats, err := s.reports.QueueSummary(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := v1.Status{
		Version: s.opts.Info.Version, Commit: s.opts.Info.Commit, BuiltAt: s.opts.Info.BuiltAt,
		Instance: s.opts.Info.Instance, Mode: v1.StatusMode(s.opts.Info.Mode),
		Queue:      queueSummary(stats),
		ForgejoUrl: s.opts.ForgejoWebURL.String(),
	}
	if leader.Present {
		l := &v1.Leader{Present: true}
		if leader.Instance != "" {
			l.Instance = &leader.Instance
		}
		if !leader.ConnectedAt.IsZero() {
			l.ConnectedAt = &leader.ConnectedAt
		}
		out.Leader = l
	}
	writeJSON(w, out)
}

func queueSummary(q store.QueueStats) v1.QueueSummary {
	out := v1.QueueSummary{Depth: q.Depth, Failing: q.Failing}
	if !q.Oldest.IsZero() {
		out.OldestMarkedAt = &q.Oldest
	}
	return out
}

// ListRepositories implements v1.ServerInterface.
func (s *server) ListRepositories(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFor(r)
	defer cancel()
	sums, err := s.reports.RepositorySummaries(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]v1.Repository, 0, len(s.opts.Repositories))
	for _, cfg := range s.opts.Repositories {
		sum := sums[cfg.Key()]
		repo := v1.Repository{
			Key: cfg.Key(), Github: cfg.GitHub.String(), Forgejo: cfg.Forgejo.String(),
			GithubUrl:      s.opts.GitHubWebURL.JoinPath(cfg.GitHub.Owner, cfg.GitHub.Name).String(),
			ForgejoUrl:     s.opts.ForgejoWebURL.JoinPath(cfg.Forgejo.Owner, cfg.Forgejo.Name).String(),
			PullRequests:   cfg.ImportsPullRequests(),
			TargetBranches: append([]string{}, cfg.TargetBranches...),
			RebaseRequests: cfg.RebaseRequests,
			ImportLabel:    cfg.ImportLabel,
			PublishLabel:   cfg.PublishLabel,
			Counts: v1.PairCounts{
				IssuesOpen: sum.Counts.IssuesOpen, IssuesClosed: sum.Counts.IssuesClosed,
				PullsOpen: sum.Counts.PullsOpen, PullsMerged: sum.Counts.PullsMerged, PullsClosed: sum.Counts.PullsClosed,
			},
			Queue:   queueSummary(sum.Queue),
			Cursors: []v1.Cursor{},
		}
		if !cfg.Contrib.Empty() {
			c := cfg.Contrib.String()
			repo.Contrib = &c
		}
		if !cfg.ImportSince.IsZero() {
			since := cfg.ImportSince
			repo.ImportSince = &since
		}
		for _, c := range sum.Cursors {
			repo.Cursors = append(repo.Cursors, v1.Cursor{
				Side: v1.Side(c.Side), Stream: v1.CursorStream(c.Stream), Cursor: c.Cursor, UpdatedAt: c.UpdatedAt,
			})
		}
		out = append(out, repo)
	}
	writeJSON(w, out)
}

// ListPairs implements v1.ServerInterface.
func (s *server) ListPairs(w http.ResponseWriter, r *http.Request, p v1.ListPairsParams) {
	f := store.PairFilter{Limit: 50}
	if p.Repository != nil {
		f.Repository = *p.Repository
	}
	if p.Kind != nil {
		if *p.Kind != v1.Issue && *p.Kind != v1.Pull {
			writeError(w, http.StatusBadRequest, "kind must be issue or pull")
			return
		}
		f.Kind = store.Kind(*p.Kind)
	}
	if p.State != nil {
		switch *p.State {
		case v1.Open, v1.Closed, v1.Merged:
		default:
			writeError(w, http.StatusBadRequest, "state must be open, closed or merged")
			return
		}
		f.State = store.PairState(*p.State)
	}
	if p.Origin != nil {
		if !validSide(*p.Origin) {
			writeError(w, http.StatusBadRequest, "origin must be github or forgejo")
			return
		}
		f.Origin = store.Side(*p.Origin)
	}
	if p.Q != nil {
		if len(*p.Q) > 200 {
			writeError(w, http.StatusBadRequest, "q is longer than 200 characters")
			return
		}
		f.Query = *p.Q
	}
	if p.Limit != nil {
		if *p.Limit < 1 || *p.Limit > 200 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		f.Limit = *p.Limit
	}
	if p.Offset != nil {
		if *p.Offset < 0 {
			writeError(w, http.StatusBadRequest, "offset must not be negative")
			return
		}
		f.Offset = *p.Offset
	}

	ctx, cancel := ctxFor(r)
	defer cancel()
	pairs, total, err := s.reports.Pairs(ctx, f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := v1.PairPage{Items: make([]v1.Pair, 0, len(pairs)), Total: total}
	for _, pr := range pairs {
		out.Items = append(out.Items, s.pair(pr))
	}
	writeJSON(w, out)
}

func validSide(s v1.Side) bool { return s == v1.Github || s == v1.Forgejo }

// GetPair implements v1.ServerInterface.
func (s *server) GetPair(w http.ResponseWriter, r *http.Request, id int64) {
	ctx, cancel := ctxFor(r)
	defer cancel()
	pr, err := s.reports.Pair(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, fmt.Sprintf("there is no pair %d", id))
		return
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	comments, err := s.reports.CommentPairs(ctx, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p := s.pair(pr)
	out := v1.PairDetail{
		Id: p.Id, Repository: p.Repository, Kind: p.Kind, Origin: p.Origin, State: p.State,
		Title: p.Title, Author: p.Author, GithubNumber: p.GithubNumber, ForgejoNumber: p.ForgejoNumber,
		GithubUrl: p.GithubUrl, ForgejoUrl: p.ForgejoUrl, Comments: p.Comments,
		CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, Pull: p.Pull, Queued: p.Queued,
		CommentPairs: make([]v1.CommentPair, 0, len(comments)),
	}
	for _, c := range comments {
		out.CommentPairs = append(out.CommentPairs, v1.CommentPair{
			Id: c.ID, Origin: v1.Side(c.Origin), CreatedAt: c.CreatedAt,
			GithubCommentId: c.GitHubCommentID, ForgejoCommentId: c.ForgejoCommentID,
			GithubUrl:  p.GithubUrl + "#issuecomment-" + strconv.FormatInt(c.GitHubCommentID, 10),
			ForgejoUrl: p.ForgejoUrl + "#issuecomment-" + strconv.FormatInt(c.ForgejoCommentID, 10),
		})
	}
	writeJSON(w, out)
}

// ListQueue implements v1.ServerInterface.
func (s *server) ListQueue(w http.ResponseWriter, r *http.Request, p v1.ListQueueParams) {
	var f store.QueueFilter
	if p.Repository != nil {
		f.Repository = *p.Repository
	}
	if p.Failing != nil {
		f.FailingOnly = *p.Failing
	}
	ctx, cancel := ctxFor(r)
	defer cancel()
	entries, err := s.reports.Queue(ctx, f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]v1.QueueEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, s.queueEntry(e))
	}
	writeJSON(w, out)
}

func (s *server) queueEntry(e store.QueueReport) v1.QueueEntry {
	out := v1.QueueEntry{
		Repository: e.Repository, Side: v1.Side(e.Side), Number: e.Number,
		MarkedAt: e.MarkedAt, NotBefore: e.NotBefore, Attempts: e.Attempts,
		Url: s.issueURL(e.Repository, e.Side, store.KindIssue, e.Number),
	}
	if e.LastError != "" {
		msg := e.LastError
		out.LastError = &msg
	}
	if e.PairID != 0 {
		id := e.PairID
		out.PairId = &id
	}
	return out
}

func (s *server) pair(p store.PairReport) v1.Pair {
	out := v1.Pair{
		Id: p.ID, Repository: p.Repository, Kind: v1.PairKind(p.Kind), Origin: v1.Side(p.Origin),
		State: v1.PairState(p.State), Title: p.Title, Author: p.Author,
		GithubNumber: p.GitHubNumber, ForgejoNumber: p.ForgejoNumber,
		GithubUrl:  s.issueURL(p.Repository, store.GitHub, p.Kind, p.GitHubNumber),
		ForgejoUrl: s.issueURL(p.Repository, store.Forgejo, p.Kind, p.ForgejoNumber),
		Comments:   p.Comments, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}
	if p.Pull != nil {
		pull := &v1.PullRequest{
			ContribBranch: p.Pull.ContribBranch, ImportedHeadSha: p.Pull.ImportedHeadSHA,
			HeadFrozen: p.Pull.HeadFrozen, TouchesCi: p.Pull.TouchesCI, RebaseRequested: p.Pull.RebaseRequested,
			OutcomeReported: p.Pull.OutcomeReported, UpdatedAt: p.Pull.UpdatedAt,
		}
		if cfg, ok := s.repos[p.Repository]; ok && !cfg.Contrib.Empty() {
			u := s.opts.ForgejoWebURL.JoinPath(cfg.Contrib.Owner, cfg.Contrib.Name, "src", "branch", p.Pull.ContribBranch).String()
			pull.ContribBranchUrl = &u
		}
		out.Pull = pull
	}
	if p.Queued != nil {
		q := s.queueEntry(*p.Queued)
		out.Queued = &q
	}
	return out
}

// issueURL links an issue or pull request on one side. A repository that is
// no longer configured is linked by its key on GitHub and not at all in
// Forgejo, whose repository the key does not name.
func (s *server) issueURL(key string, side store.Side, kind store.Kind, n int64) string {
	num := strconv.FormatInt(n, 10)
	cfg, ok := s.repos[key]
	if side == store.GitHub {
		path := "issues"
		if kind == store.KindPull {
			path = "pull"
		}
		owner, name := cfg.GitHub.Owner, cfg.GitHub.Name
		if !ok {
			ref, err := config.ParseRepoRef(key)
			if err != nil {
				return ""
			}
			owner, name = ref.Owner, ref.Name
		}
		return s.opts.GitHubWebURL.JoinPath(owner, name, path, num).String()
	}
	if !ok {
		return ""
	}
	path := "issues"
	if kind == store.KindPull {
		path = "pulls"
	}
	return s.opts.ForgejoWebURL.JoinPath(cfg.Forgejo.Owner, cfg.Forgejo.Name, path, num).String()
}

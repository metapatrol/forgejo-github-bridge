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
	"runtime"
	"strconv"
	"strings"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/api"
	v1 "forgejo.metapatrol.local/platform/forgejo-github-bridge/api/v1"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/events"
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
	// StartedAt is when the process started.
	StartedAt time.Time
	// PollInterval and FullResync are the bridge's schedule.
	PollInterval, FullResync time.Duration
	// MaintenanceInterval is the time between database maintenance rounds.
	MaintenanceInterval time.Duration
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
	UI fs.FS
	// Events feeds GET /api/v1/events; nil answers it with 503.
	Events EventSource
	Log    *slog.Logger
}

// EventSource is satisfied by *events.Hub.
type EventSource interface {
	Subscribe() (<-chan events.Event, func())
}

// keepaliveInterval keeps proxies from closing an idle event stream.
const keepaliveInterval = 25 * time.Second

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

// StreamEvents implements v1.ServerInterface: changes as Server-Sent Events.
func (s *server) StreamEvents(w http.ResponseWriter, r *http.Request) {
	if s.opts.Events == nil {
		writeError(w, http.StatusServiceUnavailable, "this console has no event stream")
		return
	}
	rc := http.NewResponseController(w)
	ch, stop := s.opts.Events.Subscribe()
	defer stop()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no") // nginx: do not buffer the stream
	w.WriteHeader(http.StatusOK)

	send := func(kind string, data []byte) error {
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", kind, data); err != nil {
			return err
		}
		return rc.Flush()
	}
	if send("ready", []byte(`{}`)) != nil {
		return
	}
	keepalive := time.NewTicker(keepaliveInterval)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok || send(ev.Kind, ev.Data) != nil {
				return
			}
		case <-keepalive.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil || rc.Flush() != nil {
				return
			}
		}
	}
}

// ListAuthors implements v1.ServerInterface.
func (s *server) ListAuthors(w http.ResponseWriter, r *http.Request, p v1.ListAuthorsParams) {
	if p.Q != nil && len(*p.Q) > 200 {
		writeError(w, http.StatusBadRequest, "q is longer than 200 characters")
		return
	}
	if p.Side != nil && !validSide(*p.Side) {
		writeError(w, http.StatusBadRequest, "side must be github or forgejo")
		return
	}
	if p.Kind != nil && *p.Kind != v1.Issue && *p.Kind != v1.Pull {
		writeError(w, http.StatusBadRequest, "kind must be issue or pull")
		return
	}
	pg, err := newPaging(p.Limit, p.Cursor, string(deref(p.Sort)), filter("side", p.Side), filter("kind", p.Kind), filter("q", p.Q))
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	ctx, cancel := ctxFor(r)
	defer cancel()
	authors, total, next, err := s.reports.Authors(ctx, store.AuthorFilter{Query: deref(p.Q),
		Side: store.Side(deref(p.Side)), Kind: store.Kind(deref(p.Kind)), Page: pg.page})
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	out := make([]v1.Author, 0, len(authors))
	for _, a := range authors {
		author := v1.Author{Login: a.Login, Side: v1.Side(a.Side), Issues: a.Issues, Pulls: a.Pulls, LastAt: a.Last}
		var profile string
		if a.Side == store.GitHub {
			profile = s.opts.GitHubWebURL.JoinPath(a.Login).String()
		} else {
			profile = s.opts.ForgejoWebURL.JoinPath(a.Login).String()
		}
		author.ProfileUrl = &profile
		out = append(out, author)
	}
	writeJSON(w, v1.AuthorPage{Items: out, Total: total, NextCursor: pg.next(next)})
}

// GetSystem implements v1.ServerInterface.
func (s *server) GetSystem(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := ctxFor(r)
	defer cancel()
	sys, err := s.reports.System(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	leader, err := s.reports.Leader(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	info := s.opts.Info
	out := v1.System{
		Service: v1.Service{
			Version: info.Version, Commit: info.Commit, BuiltAt: info.BuiltAt, GoVersion: runtime.Version(),
			Instance: info.Instance, Mode: v1.ServiceMode(info.Mode), StartedAt: info.StartedAt,
			PollIntervalSeconds: int(info.PollInterval / time.Second), FullResyncSeconds: int(info.FullResync / time.Second),
			Repositories: len(s.opts.Repositories), Leader: leaderOf(leader),
		},
		Tables:     make([]v1.SystemTable, 0, len(sys.Tables)),
		Migrations: make([]v1.Migration, 0, len(sys.Migrations)),
		Running:    append([]string{}, sys.Running...),
	}
	for _, r := range s.opts.Repositories {
		if r.ImportsPullRequests() {
			out.Service.PullRequestRepositories++
		}
	}
	d := sys.Database
	out.Database = v1.SystemDatabase{
		Version: d.Version, SizeBytes: d.SizeBytes, Connections: d.Connections, MaxConnections: d.MaxConnections,
		PoolSize: d.PoolSize, PoolInUse: d.PoolInUse, PoolMax: d.PoolMax, BlocksHit: d.BlocksHit, BlocksRead: d.BlocksRead,
		Deadlocks: d.Deadlocks, TempBytes: d.TempBytes, XidAge: d.XIDAge, FreezeMaxAge: d.FreezeMaxAge,
		StatsReset: timePtr(d.StatsReset), Settings: make([]v1.Setting, 0, len(d.Settings)),
	}
	for _, st := range d.Settings {
		out.Database.Settings = append(out.Database.Settings, v1.Setting{Name: st.Name, Value: st.Value})
	}
	for _, t := range sys.Tables {
		table := v1.SystemTable{
			Name: t.Name, TotalBytes: t.TotalBytes, HeapBytes: t.HeapBytes, IndexBytes: t.IndexBytes,
			LiveRows: t.LiveRows, DeadRows: t.DeadRows, ModifiedSinceAnalyze: t.ModifiedSinceAnalyze,
			VacuumThreshold: t.VacuumThreshold, AnalyzeThreshold: t.AnalyzeThreshold,
			LastVacuum: timePtr(t.LastVacuum), LastAutovacuum: timePtr(t.LastAutovacuum),
			LastAnalyze: timePtr(t.LastAnalyze), LastAutoanalyze: timePtr(t.LastAutoanalyze),
			Indexes: make([]v1.SystemIndex, 0, len(t.Indexes)),
		}
		for _, ix := range t.Indexes {
			table.Indexes = append(table.Indexes, v1.SystemIndex{Name: ix.Name, Method: ix.Method, Bytes: ix.Bytes, Scans: ix.Scans, Valid: ix.Valid})
		}
		out.Tables = append(out.Tables, table)
	}
	for _, m := range sys.Migrations {
		out.Migrations = append(out.Migrations, v1.Migration{Name: m.Name, AppliedAt: m.AppliedAt})
	}
	out.Maintenance = v1.Maintenance{IntervalSeconds: int(info.MaintenanceInterval / time.Second),
		Rounds: make([]v1.MaintenanceRound, 0, len(sys.Rounds))}
	var lastFinished time.Time
	for _, m := range sys.Rounds {
		out.Maintenance.Rounds = append(out.Maintenance.Rounds, v1.MaintenanceRound{StartedAt: m.StartedAt,
			FinishedAt: timePtr(m.FinishedAt), Tables: m.Tables, Rebuilt: m.Rebuilt, Dropped: m.Dropped, Failures: m.Failures})
		if m.FinishedAt.After(lastFinished) {
			lastFinished = m.FinishedAt
		}
	}
	if info.MaintenanceInterval > 0 {
		// As Maintain decides it: an interval after the last round, or soon
		// after the active instance starts when there was none.
		next := time.Now()
		if !lastFinished.IsZero() {
			next = lastFinished.Add(info.MaintenanceInterval)
		}
		out.Maintenance.NextRoundAt = &next
	}
	writeJSON(w, out)
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func leaderOf(l store.Leader) *v1.Leader {
	if !l.Present {
		return nil
	}
	out := &v1.Leader{Present: true, ConnectedAt: timePtr(l.ConnectedAt)}
	if l.Instance != "" {
		out.Instance = &l.Instance
	}
	return out
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
	out.Leader = leaderOf(leader)
	limits, err := s.reports.RateLimits(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out.GithubRateLimits = make([]v1.RateLimit, 0, len(limits))
	for _, l := range limits {
		out.GithubRateLimits = append(out.GithubRateLimits, v1.RateLimit{Resource: l.Resource, Limit: l.Limit,
			Remaining: l.Remaining, Used: l.Used, ResetAt: l.ResetAt, ObservedAt: l.ObservedAt})
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
func (s *server) ListRepositories(w http.ResponseWriter, r *http.Request, p v1.ListRepositoriesParams) {
	q, sort := deref(p.Q), string(deref(p.Sort))
	pg, err := newPaging(p.Limit, p.Cursor, sort, filter("pullRequests", p.PullRequests), filter("q", p.Q))
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	ctx, cancel := ctxFor(r)
	defer cancel()
	sums, err := s.reports.RepositorySummaries(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]v1.Repository, 0, len(s.opts.Repositories))
	needle := strings.ToLower(strings.TrimSpace(q))
	for _, cfg := range s.opts.Repositories {
		if needle != "" && !strings.Contains(strings.ToLower(cfg.GitHub.String()), needle) &&
			!strings.Contains(strings.ToLower(cfg.Forgejo.String()), needle) {
			continue
		}
		if p.PullRequests != nil && cfg.ImportsPullRequests() != *p.PullRequests {
			continue
		}
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
	items, next, err := pageInMemory(out, func(r v1.Repository) string { return r.Key }, repositoryColumns, "health", pg)
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	writeJSON(w, v1.RepositoryPage{Items: items, Total: len(out), NextCursor: pg.next(next)})
}

// repositoryColumns are what repositories sort by. health puts the ones
// with failing work first, then the ones with queued work.
var repositoryColumns = map[string]func(v1.Repository) store.SortValue{
	"health": func(r v1.Repository) store.SortValue {
		switch {
		case r.Queue.Failing > 0:
			return store.SortValueOf(0)
		case r.Queue.Depth > 0:
			return store.SortValueOf(1)
		}
		return store.SortValueOf(2)
	},
	"name":    func(r v1.Repository) store.SortValue { return store.SortValueOf(strings.ToLower(r.Github)) },
	"forgejo": func(r v1.Repository) store.SortValue { return store.SortValueOf(strings.ToLower(r.Forgejo)) },
	"issues":  func(r v1.Repository) store.SortValue { return store.SortValueOf(r.Counts.IssuesOpen) },
	"pulls":   func(r v1.Repository) store.SortValue { return store.SortValueOf(r.Counts.PullsOpen) },
	"queue":   func(r v1.Repository) store.SortValue { return store.SortValueOf(r.Queue.Depth) },
	"polled": func(r v1.Repository) store.SortValue {
		var latest time.Time
		for _, c := range r.Cursors {
			if c.UpdatedAt.After(latest) {
				latest = c.UpdatedAt
			}
		}
		if latest.IsZero() {
			return store.SortValue{}
		}
		return store.SortValueOf(latest)
	},
}

// deref is a pointer's value, or the zero value for nil.
func deref[T any](p *T) (v T) {
	if p != nil {
		v = *p
	}
	return
}

// ListPairs implements v1.ServerInterface.
func (s *server) ListPairs(w http.ResponseWriter, r *http.Request, p v1.ListPairsParams) {
	var f store.PairFilter
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
	if p.Author != nil {
		if len(*p.Author) > 100 {
			writeError(w, http.StatusBadRequest, "author is longer than 100 characters")
			return
		}
		f.Author = *p.Author
	}
	if p.Q != nil {
		if len(*p.Q) > 200 {
			writeError(w, http.StatusBadRequest, "q is longer than 200 characters")
			return
		}
		f.Query = *p.Q
	}
	pg, err := newPaging(p.Limit, p.Cursor, string(deref(p.Sort)), filter("repository", p.Repository),
		filter("kind", p.Kind), filter("state", p.State), filter("origin", p.Origin), filter("author", p.Author), filter("q", p.Q))
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	f.Page = pg.page

	ctx, cancel := ctxFor(r)
	defer cancel()
	pairs, total, next, err := s.reports.Pairs(ctx, f)
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	out := v1.PairPage{Items: make([]v1.Pair, 0, len(pairs)), Total: total, NextCursor: pg.next(next)}
	for _, pr := range pairs {
		out.Items = append(out.Items, s.pair(pr))
	}
	writeJSON(w, out)
}

func validSide(s v1.Side) bool { return s == v1.SideGithub || s == v1.SideForgejo }

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
	if p.Q != nil {
		if len(*p.Q) > 200 {
			writeError(w, http.StatusBadRequest, "q is longer than 200 characters")
			return
		}
		f.Query = *p.Q
	}
	pg, err := newPaging(p.Limit, p.Cursor, string(deref(p.Sort)),
		filter("repository", p.Repository), filter("failing", p.Failing), filter("q", p.Q))
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	f.Page = pg.page
	ctx, cancel := ctxFor(r)
	defer cancel()
	entries, total, next, err := s.reports.Queue(ctx, f)
	if err != nil {
		s.pageError(w, r, err)
		return
	}
	out := v1.QueuePage{Items: make([]v1.QueueEntry, 0, len(entries)), Total: total, NextCursor: pg.next(next)}
	for _, e := range entries {
		out.Items = append(out.Items, s.queueEntry(e))
	}
	writeJSON(w, out)
}

func (s *server) queueEntry(e store.QueueReport) v1.QueueEntry {
	out := v1.QueueEntry{
		Repository: e.Repository, Side: v1.Side(e.Side), Number: e.Number,
		MarkedAt: e.MarkedAt, NotBefore: e.NotBefore, Attempts: e.Attempts,
		Url: s.issueURL(e.Repository, e.Side, kindOr(e.Kind), e.Number),
	}
	if e.LastError != "" {
		msg := e.LastError
		out.LastError = &msg
	}
	if e.PairID != 0 {
		id, kind := e.PairID, v1.PairKind(e.Kind)
		out.PairId, out.Kind = &id, &kind
	}
	return out
}

// kindOr is a queue entry's kind, an issue when its pair is unknown: both
// forges redirect an issue link to the pull request it is.
func kindOr(k store.Kind) store.Kind {
	if k == "" {
		return store.KindIssue
	}
	return k
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
		if p.Pull.RebaseCommentID != 0 {
			u := s.issueURL(p.Repository, store.GitHub, p.Kind, p.GitHubNumber) + "#issuecomment-" + strconv.FormatInt(p.Pull.RebaseCommentID, 10)
			pull.RebaseCommentUrl = &u
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

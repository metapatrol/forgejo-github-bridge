package console

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	v1 "forgejo.metapatrol.local/platform/forgejo-github-bridge/api/v1"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/events"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// reports is a canned store.Reports that records the filters it was given.
type reports struct {
	leader       store.Leader
	stats        store.QueueStats
	sums         map[string]store.RepositorySummary
	pairs        []store.PairReport
	comments     []store.CommentReport
	queue        []store.QueueReport
	pairFilter   store.PairFilter
	queueFilter  store.QueueFilter
	system       store.System
	limits       []store.RateLimit
	authors      []store.Author
	authorFilter store.AuthorFilter
	// next is the position every list returns as where its next page starts.
	next *store.Position
	err  error
}

func (r *reports) System(context.Context) (store.System, error) { return r.system, r.err }

func (r *reports) RateLimits(context.Context) ([]store.RateLimit, error) { return r.limits, r.err }

func (r *reports) Authors(_ context.Context, f store.AuthorFilter) ([]store.Author, int, *store.Position, error) {
	r.authorFilter = f
	return r.authors, len(r.authors), r.next, r.err
}

func (r *reports) Leader(context.Context) (store.Leader, error)           { return r.leader, r.err }
func (r *reports) QueueSummary(context.Context) (store.QueueStats, error) { return r.stats, r.err }
func (r *reports) CommentPairs(context.Context, int64) ([]store.CommentReport, error) {
	return r.comments, r.err
}

func (r *reports) RepositorySummaries(context.Context) (map[string]store.RepositorySummary, error) {
	return r.sums, r.err
}

func (r *reports) Pairs(_ context.Context, f store.PairFilter) ([]store.PairReport, int, *store.Position, error) {
	r.pairFilter = f
	return r.pairs, len(r.pairs), r.next, r.err
}

func (r *reports) Pair(_ context.Context, id int64) (store.PairReport, error) {
	for _, p := range r.pairs {
		if p.ID == id {
			return p, r.err
		}
	}
	return store.PairReport{}, store.ErrNotFound
}

func (r *reports) Queue(_ context.Context, f store.QueueFilter) ([]store.QueueReport, int, *store.Position, error) {
	r.queueFilter = f
	return r.queue, len(r.queue), r.next, r.err
}

var (
	now       = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	repoCfg   = config.Repository{GitHub: config.RepoRef{Owner: "metapatrol", Name: "foo"}, Forgejo: config.RepoRef{Owner: "platform", Name: "foo"}, Contrib: config.RepoRef{Owner: "github-contrib", Name: "foo"}, TargetBranches: []string{"develop"}, ImportLabel: "github", PublishLabel: "public"}
	issuePair = store.PairReport{
		IssueMapping: store.IssueMapping{ID: 1, Repository: "metapatrol/foo", Kind: store.KindIssue, GitHubNumber: 7, ForgejoNumber: 71, Origin: store.GitHub, Title: "Crash", Author: "alice"},
		State:        store.StateOpen, Comments: 2, CreatedAt: now, UpdatedAt: now,
		Queued: &store.QueueReport{DirtyKey: store.DirtyKey{Repository: "metapatrol/foo", Side: store.GitHub, Number: 7}, Attempts: 3, LastError: "HTTP 403", PairID: 1, Kind: store.KindIssue, MarkedAt: now, NotBefore: now},
	}
	pullPair = store.PairReport{
		IssueMapping: store.IssueMapping{ID: 2, Repository: "metapatrol/foo", Kind: store.KindPull, GitHubNumber: 8, ForgejoNumber: 72, Origin: store.GitHub},
		State:        store.StateMerged, CreatedAt: now, UpdatedAt: now,
		Pull: &store.PullReport{PullRequestState: store.PullRequestState{ContribBranch: "pr/8", ImportedHeadSHA: "abc", State: store.PullMerged, TouchesCI: true, RebaseCommentID: 55}, UpdatedAt: now},
	}
)

func newConsole(t *testing.T, r *reports) http.Handler {
	t.Helper()
	web, _ := url.Parse("https://forgejo.example.com")
	return New(r, Options{
		Info:          Info{Version: "1.2.3", Commit: "abc", BuiltAt: "now", Instance: "pod-1", Mode: "bridge"},
		Repositories:  []config.Repository{repoCfg},
		ForgejoWebURL: web,
		UI:            fstest.MapFS{"index.html": {Data: []byte("<app>")}, "main-ABCDEFGH.js": {Data: []byte("js")}},
		Log:           slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
}

func get(t *testing.T, h http.Handler, path string, into any) *http.Response {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	res := rec.Result()
	if into != nil && res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(into); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
	}
	return res
}

func TestStatus(t *testing.T) {
	h := newConsole(t, &reports{
		leader: store.Leader{Present: true, Instance: "pod-0", ConnectedAt: now},
		stats:  store.QueueStats{Depth: 3, Failing: 1, Oldest: now},
		limits: []store.RateLimit{{Resource: "core", Limit: 5000, Remaining: 4870, Used: 130, ResetAt: now, ObservedAt: now}},
	})
	var s v1.Status
	get(t, h, "/api/v1/status", &s)
	if s.Version != "1.2.3" || s.Instance != "pod-1" || s.Mode != v1.StatusModeBridge || s.ForgejoUrl != "https://forgejo.example.com" {
		t.Errorf("status = %+v", s)
	}
	if len(s.GithubRateLimits) != 1 || s.GithubRateLimits[0].Remaining != 4870 {
		t.Errorf("rate limits = %+v", s.GithubRateLimits)
	}
	if s.Leader == nil || *s.Leader.Instance != "pod-0" || s.Queue.Failing != 1 || s.Queue.OldestMarkedAt == nil {
		t.Errorf("leader %+v, queue %+v", s.Leader, s.Queue)
	}

	h = newConsole(t, &reports{})
	s = v1.Status{}
	get(t, h, "/api/v1/status", &s)
	if s.Leader != nil {
		t.Errorf("a leader nobody holds: %+v", s.Leader)
	}
}

func TestRepositoriesListEveryConfiguredOne(t *testing.T) {
	h := newConsole(t, &reports{sums: map[string]store.RepositorySummary{
		"metapatrol/foo": {Counts: store.PairCounts{IssuesOpen: 4}, Cursors: []store.CursorReport{{Side: store.GitHub, Stream: store.Issues, Cursor: now, UpdatedAt: now}}},
		"gone/repo":      {Counts: store.PairCounts{IssuesOpen: 9}},
	}})
	var page v1.RepositoryPage
	get(t, h, "/api/v1/repositories", &page)
	repos := page.Items
	if len(repos) != 1 || page.Total != 1 {
		t.Fatalf("repos = %+v; only configured repositories are listed", repos)
	}
	r := repos[0]
	if r.Key != "metapatrol/foo" || r.GithubUrl != "https://github.com/metapatrol/foo" || r.ForgejoUrl != "https://forgejo.example.com/platform/foo" {
		t.Errorf("repo = %+v", r)
	}
	if !r.PullRequests || *r.Contrib != "github-contrib/foo" || r.Counts.IssuesOpen != 4 || len(r.Cursors) != 1 {
		t.Errorf("repo = %+v", r)
	}
}

func TestPairsAreLinkedOnBothSides(t *testing.T) {
	r := &reports{pairs: []store.PairReport{issuePair, pullPair}}
	h := newConsole(t, r)
	var page v1.PairPage
	get(t, h, "/api/v1/pairs?repository=metapatrol/foo&kind=pull&state=merged&origin=github&author=alice&q=%238&limit=10&sort=-updated", &page)
	want := store.PairFilter{Repository: "metapatrol/foo", Kind: store.KindPull, State: store.StateMerged, Origin: store.GitHub,
		Author: "alice", Query: "#8", Page: store.Page{Sort: "-updated", Limit: 10}}
	if r.pairFilter != want {
		t.Errorf("filter = %+v, want %+v", r.pairFilter, want)
	}
	if page.Total != 2 || len(page.Items) != 2 {
		t.Fatalf("page = %+v", page)
	}
	issue, pull := page.Items[0], page.Items[1]
	if issue.GithubUrl != "https://github.com/metapatrol/foo/issues/7" || issue.ForgejoUrl != "https://forgejo.example.com/platform/foo/issues/71" {
		t.Errorf("issue links = %s, %s", issue.GithubUrl, issue.ForgejoUrl)
	}
	if issue.Queued == nil || *issue.Queued.LastError != "HTTP 403" || issue.Queued.Url != "https://github.com/metapatrol/foo/issues/7" || *issue.Queued.Kind != v1.Issue {
		t.Errorf("queued = %+v", issue.Queued)
	}
	if pull.GithubUrl != "https://github.com/metapatrol/foo/pull/8" || pull.ForgejoUrl != "https://forgejo.example.com/platform/foo/pulls/72" {
		t.Errorf("pull links = %s, %s", pull.GithubUrl, pull.ForgejoUrl)
	}
	if pull.Pull == nil || *pull.Pull.RebaseCommentUrl != "https://github.com/metapatrol/foo/pull/8#issuecomment-55" {
		t.Errorf("rebase comment = %v", pull.Pull)
	}
	if pull.Pull == nil || !pull.Pull.TouchesCi || *pull.Pull.ContribBranchUrl != "https://forgejo.example.com/github-contrib/foo/src/branch/pr/8" {
		t.Errorf("pull = %+v", pull.Pull)
	}

	get(t, h, "/api/v1/pairs", nil)
	if r.pairFilter.Limit != 0 || r.pairFilter.After != nil {
		t.Errorf("without limit every pair comes in one answer: %+v", r.pairFilter.Page)
	}
}

func TestBadFiltersAreRefused(t *testing.T) {
	h := newConsole(t, &reports{})
	for _, q := range []string{"kind=bug", "state=stale", "origin=gitlab", "limit=0", "limit=1001", "limit=x", "cursor=%25%25", "cursor=e30", "q=" + strings.Repeat("a", 201)} {
		res := get(t, h, "/api/v1/pairs?"+q, nil)
		if res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", q, res.StatusCode)
		}
	}
}

func TestOnePairWithItsComments(t *testing.T) {
	h := newConsole(t, &reports{
		pairs:    []store.PairReport{issuePair},
		comments: []store.CommentReport{{CommentMapping: store.CommentMapping{ID: 5, GitHubCommentID: 100, ForgejoCommentID: 200, Origin: store.Forgejo}, CreatedAt: now}},
	})
	var p v1.PairDetail
	get(t, h, "/api/v1/pairs/1", &p)
	if p.Title != "Crash" || len(p.CommentPairs) != 1 {
		t.Fatalf("pair = %+v", p)
	}
	c := p.CommentPairs[0]
	if c.GithubUrl != "https://github.com/metapatrol/foo/issues/7#issuecomment-100" || c.ForgejoUrl != "https://forgejo.example.com/platform/foo/issues/71#issuecomment-200" {
		t.Errorf("comment links = %s, %s", c.GithubUrl, c.ForgejoUrl)
	}
	if res := get(t, h, "/api/v1/pairs/9", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("a missing pair: %d", res.StatusCode)
	}
	if res := get(t, h, "/api/v1/pairs/abc", nil); res.StatusCode != http.StatusBadRequest {
		t.Errorf("a malformed id: %d", res.StatusCode)
	}
}

func TestQueue(t *testing.T) {
	r := &reports{queue: []store.QueueReport{{DirtyKey: store.DirtyKey{Repository: "metapatrol/foo", Side: store.Forgejo, Number: 80}, MarkedAt: now, NotBefore: now}}}
	h := newConsole(t, r)
	var page v1.QueuePage
	get(t, h, "/api/v1/queue?repository=metapatrol/foo&failing=true&q=403&sort=-attempts", &page)
	q := page.Items
	if r.queueFilter != (store.QueueFilter{Repository: "metapatrol/foo", FailingOnly: true, Query: "403", Page: store.Page{Sort: "-attempts"}}) {
		t.Errorf("filter = %+v", r.queueFilter)
	}
	if len(q) != 1 || q[0].Url != "https://forgejo.example.com/platform/foo/issues/80" || q[0].PairId != nil || q[0].LastError != nil {
		t.Errorf("queue = %+v", q)
	}
}

func TestDatabaseErrorsStayInTheLog(t *testing.T) {
	h := newConsole(t, &reports{err: errors.New(`relation "dirty" does not exist`)})
	res := get(t, h, "/api/v1/queue", nil)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusInternalServerError || strings.Contains(string(body), "relation") {
		t.Errorf("%d %s", res.StatusCode, body)
	}
}

func TestTheConsoleIsReadOnly(t *testing.T) {
	h := newConsole(t, &reports{})
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/pairs", strings.NewReader("{}")))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d", method, rec.Code)
		}
	}
}

func TestTheApplicationAndTheSpecification(t *testing.T) {
	h := newConsole(t, &reports{})
	for path, want := range map[string]string{
		"/":                    "<app>",
		"/pairs/12":            "<app>",
		"/main-ABCDEFGH.js":    "js",
		"/api/v1/openapi.yaml": "openapi: \"3.0.3\"",
	} {
		res := get(t, h, path, nil)
		body, _ := io.ReadAll(res.Body)
		if res.StatusCode != http.StatusOK || !strings.HasPrefix(string(body), want) {
			t.Errorf("%s: %d %.40q", path, res.StatusCode, body)
		}
		if res.Header.Get("X-Frame-Options") != "DENY" || res.Header.Get("Content-Security-Policy") == "" {
			t.Errorf("%s: security headers missing", path)
		}
	}
	if res := get(t, h, "/main-ABCDEFGH.js", nil); !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Errorf("a fingerprinted asset is not cached: %q", res.Header.Get("Cache-Control"))
	}
	if res := get(t, h, "/", nil); res.Header.Get("Cache-Control") != "no-cache" {
		t.Errorf("index.html is cached: %q", res.Header.Get("Cache-Control"))
	}
	if res := get(t, h, "/api/v1/nothing", nil); res.StatusCode != http.StatusNotFound || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		t.Errorf("an unknown API path: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
}

func TestSystem(t *testing.T) {
	h := newConsole(t, &reports{
		leader: store.Leader{Present: true, Instance: "pod-0", ConnectedAt: now},
		system: store.System{
			Database: store.SystemDatabase{Version: "18.0", SizeBytes: 8 << 20, XIDAge: 1200, FreezeMaxAge: 200_000_000,
				Settings: []store.Setting{{Name: "autovacuum", Value: "on"}}},
			Tables: []store.SystemTable{{Name: "dirty", DeadRows: 40, VacuumThreshold: 50, LastAutovacuum: now,
				Indexes: []store.SystemIndex{{Name: "dirty_due", Method: "btree", Valid: true}}}},
			Migrations: []store.Migration{{Name: "0001_initial.sql", AppliedAt: now}},
		},
	})
	var s v1.System
	get(t, h, "/api/v1/system", &s)
	if s.Service.Version != "1.2.3" || s.Service.Repositories != 1 || s.Service.PullRequestRepositories != 1 ||
		s.Service.GoVersion == "" || s.Service.Leader == nil {
		t.Errorf("service = %+v", s.Service)
	}
	if s.Database.Version != "18.0" || s.Database.XidAge != 1200 || len(s.Database.Settings) != 1 || s.Database.StatsReset != nil {
		t.Errorf("database = %+v", s.Database)
	}
	if len(s.Tables) != 1 || s.Tables[0].LastAutovacuum == nil || s.Tables[0].LastVacuum != nil || len(s.Tables[0].Indexes) != 1 {
		t.Errorf("tables = %+v", s.Tables)
	}
	if len(s.Migrations) != 1 || s.Running == nil {
		t.Errorf("migrations %+v, running %v", s.Migrations, s.Running)
	}
}

// source is an EventSource that hands out one channel.
type source struct{ ch chan events.Event }

func (s *source) Subscribe() (<-chan events.Event, func()) { return s.ch, func() {} }

func TestEventsAreStreamed(t *testing.T) {
	src := &source{ch: make(chan events.Event, 1)}
	web, _ := url.Parse("https://forgejo.example.com")
	srv := httptest.NewServer(New(&reports{}, Options{ForgejoWebURL: web, Events: src,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}))
	defer srv.Close()

	res, err := srv.Client().Get(srv.URL + "/api/v1/events")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("content type %q", res.Header.Get("Content-Type"))
	}
	src.ch <- events.Event{Kind: "queue", Data: []byte(`{"kind":"queue","number":7}`)}
	close(src.ch)
	body, _ := io.ReadAll(res.Body)
	want := "event: ready\ndata: {}\n\nevent: queue\ndata: {\"kind\":\"queue\",\"number\":7}\n\n"
	if string(body) != want {
		t.Errorf("stream:\n%s\nwant:\n%s", body, want)
	}
}

func TestNoEventsWithoutASource(t *testing.T) {
	h := newConsole(t, &reports{})
	if res := get(t, h, "/api/v1/events", nil); res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status %d", res.StatusCode)
	}
}

func TestAuthorsLinkToTheirProfiles(t *testing.T) {
	r := &reports{authors: []store.Author{
		{Login: "alice", Side: store.GitHub, Issues: 3, Pulls: 1, Last: now},
		{Login: "jdoe", Side: store.Forgejo, Issues: 1, Last: now},
	}}
	h := newConsole(t, r)
	var page v1.AuthorPage
	get(t, h, "/api/v1/authors?q=al&sort=login", &page)
	a := page.Items
	if r.authorFilter != (store.AuthorFilter{Query: "al", Page: store.Page{Sort: "login"}}) {
		t.Errorf("filter = %+v", r.authorFilter)
	}
	if len(a) != 2 || *a[0].ProfileUrl != "https://github.com/alice" || *a[1].ProfileUrl != "https://forgejo.example.com/jdoe" || a[0].Issues != 3 {
		t.Errorf("authors = %+v", a)
	}
}

func TestACursorReadsOnWhereThePageEnded(t *testing.T) {
	r := &reports{pairs: []store.PairReport{issuePair}, next: &store.Position{
		Value: store.SortValueOf(now), Tie: []store.SortValue{store.SortValueOf(int64(1))}}}
	h := newConsole(t, r)
	var first v1.PairPage
	get(t, h, "/api/v1/pairs?kind=issue&q=crash&limit=1", &first)
	if first.NextCursor == nil {
		t.Fatal("no cursor although the store has more")
	}

	var second v1.PairPage
	res := get(t, h, "/api/v1/pairs?kind=issue&q=crash&limit=1&cursor="+*first.NextCursor, &second)
	if res.StatusCode != http.StatusOK || r.pairFilter.After == nil || r.pairFilter.After.Tie[0].Param() != int64(1) ||
		!r.pairFilter.After.Value.Time.Equal(now) {
		t.Errorf("status %d, after %+v", res.StatusCode, r.pairFilter.After)
	}

	// The same cursor with other filters, q or sort is refused: it names a
	// row of another list.
	for _, other := range []string{"kind=pull&q=crash", "kind=issue&q=other", "kind=issue&q=crash&sort=title"} {
		if res := get(t, h, "/api/v1/pairs?"+other+"&limit=1&cursor="+*first.NextCursor, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d", other, res.StatusCode)
		}
	}
	r.next = nil
	var last v1.PairPage
	get(t, h, "/api/v1/pairs?kind=issue&q=crash&limit=1", &last)
	if last.NextCursor != nil {
		t.Errorf("a cursor on the last page: %s", *last.NextCursor)
	}
}

func TestAnUnknownSortIsRefused(t *testing.T) {
	h := newConsole(t, &reports{err: fmt.Errorf("%w: cannot sort by \"x\"", store.ErrInvalid)})
	if res := get(t, h, "/api/v1/pairs?sort=x", nil); res.StatusCode != http.StatusBadRequest {
		t.Errorf("status %d", res.StatusCode)
	}
}

func TestRepositoriesAreSearchedSortedAndPaged(t *testing.T) {
	ref := func(s string) config.RepoRef { r, _ := config.ParseRepoRef(s); return r }
	repos := []config.Repository{
		{GitHub: ref("metapatrol/alpha"), Forgejo: ref("platform/alpha")},
		{GitHub: ref("metapatrol/beta"), Forgejo: ref("platform/beta")},
		{GitHub: ref("metapatrol/gamma"), Forgejo: ref("ai/gamma")},
	}
	web, _ := url.Parse("https://forgejo.example.com")
	h := New(&reports{sums: map[string]store.RepositorySummary{
		"metapatrol/gamma": {Queue: store.QueueStats{Depth: 2, Failing: 1}},
		"metapatrol/beta":  {Queue: store.QueueStats{Depth: 1}},
	}}, Options{Repositories: repos, ForgejoWebURL: web, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})

	keys := func(p v1.RepositoryPage) (out []string) {
		for _, r := range p.Items {
			out = append(out, r.Key)
		}
		return
	}
	var page v1.RepositoryPage
	get(t, h, "/api/v1/repositories", &page)
	if got := strings.Join(keys(page), ","); got != "metapatrol/gamma,metapatrol/beta,metapatrol/alpha" {
		t.Errorf("health order = %s", got)
	}
	get(t, h, "/api/v1/repositories?q=PLATFORM&sort=-name", &page)
	if got := strings.Join(keys(page), ","); got != "metapatrol/beta,metapatrol/alpha" || page.Total != 2 {
		t.Errorf("q and -name = %s of %d", got, page.Total)
	}

	pr := []config.Repository{repos[0], repos[1]}
	pr[0].TargetBranches = []string{"develop"}
	h2 := New(&reports{}, Options{Repositories: pr, ForgejoWebURL: web, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	get(t, h2, "/api/v1/repositories?pullRequests=true", &page)
	if got := strings.Join(keys(page), ","); got != "metapatrol/alpha" || page.Total != 1 {
		t.Errorf("pullRequests=true = %s of %d", got, page.Total)
	}

	var seen []string
	cursor := ""
	for i := 0; i < 5; i++ {
		var p v1.RepositoryPage
		get(t, h, "/api/v1/repositories?sort=name&limit=2"+cursor, &p)
		seen = append(seen, keys(p)...)
		if p.NextCursor == nil {
			break
		}
		cursor = "&cursor=" + *p.NextCursor
	}
	if got := strings.Join(seen, ","); got != "metapatrol/alpha,metapatrol/beta,metapatrol/gamma" {
		t.Errorf("paged = %s", got)
	}
}

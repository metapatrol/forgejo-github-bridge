package integration

import (
	"context"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/events"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/postgres"
)

// seedReports writes two repositories' worth of pairs, comments, cursors and
// queue entries.
func seedReports(t *testing.T, st *postgres.Store) (issue, pull store.IssueMapping) {
	t.Helper()
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	var err error
	issue, err = st.InsertIssue(ctx, store.IssueMapping{Repository: "a/b", GitHubNumber: 1, ForgejoNumber: 71, Origin: store.GitHub})
	must(err)
	must(st.SetIssueSummary(ctx, issue.ID, "Crash on 100% load_test", "alice"))
	closed, err := st.InsertIssue(ctx, store.IssueMapping{Repository: "a/b", GitHubNumber: 2, ForgejoNumber: 72, Origin: store.Forgejo, SyncedClosed: true})
	must(err)
	must(st.SetIssueSummary(ctx, closed.ID, "Roadmap", "jdoe"))
	pull, err = st.InsertIssue(ctx, store.IssueMapping{Repository: "a/b", Kind: store.KindPull, GitHubNumber: 3, ForgejoNumber: 73, Origin: store.GitHub})
	must(err)
	must(st.SetIssueSummary(ctx, pull.ID, "Fix the parser", "bob"))
	must(st.SavePullRequest(ctx, store.PullRequestState{IssueMappingID: pull.ID, ContribBranch: "pr/3",
		ImportedHeadSHA: "abc123", State: store.PullMerged, TouchesCI: true, RebaseCommentID: 9, OutcomeReported: true}))
	_, err = st.InsertIssue(ctx, store.IssueMapping{Repository: "c/d", GitHubNumber: 1, ForgejoNumber: 5, Origin: store.GitHub})
	must(err)

	_, err = st.InsertComment(ctx, store.CommentMapping{IssueMappingID: issue.ID, GitHubCommentID: 100, ForgejoCommentID: 200, Origin: store.GitHub})
	must(err)
	_, err = st.InsertComment(ctx, store.CommentMapping{IssueMappingID: issue.ID, GitHubCommentID: 101, ForgejoCommentID: 201, Origin: store.Forgejo})
	must(err)

	must(st.SetCursor(ctx, "a/b", store.GitHub, store.Issues, time.Now().Add(-time.Minute)))
	must(st.SetCursor(ctx, "a/b", store.Forgejo, store.Comments, time.Now().Add(-2*time.Minute)))

	must(st.MarkDirty(ctx,
		store.DirtyKey{Repository: "a/b", Side: store.GitHub, Number: 1},
		store.DirtyKey{Repository: "a/b", Side: store.Forgejo, Number: 99},
		store.DirtyKey{Repository: "c/d", Side: store.GitHub, Number: 1}))
	d, ok, err := st.NextDirty(ctx)
	must(err)
	if !ok {
		t.Fatal("nothing queued")
	}
	for d.Repository != "a/b" || d.Number != 1 {
		// Defer the others out of the way until the pair's entry comes up.
		must(st.DeferDirty(ctx, d, time.Now().Add(time.Hour), ""))
		must(st.Pool().QueryRow(ctx, `UPDATE dirty SET attempts = 0 WHERE repository = $1 AND side = $2 AND number = $3
			RETURNING repository`, d.Repository, string(d.Side), d.Number).Scan(new(string)))
		d, _, err = st.NextDirty(ctx)
		must(err)
	}
	must(st.DeferDirty(ctx, d, time.Now().Add(time.Minute), "HTTP 403 from Forgejo"))
	return issue, pull
}

func TestReportsSummarise(t *testing.T) {
	st := open(t)
	seedReports(t, st)
	ctx := context.Background()

	sums, err := st.RepositorySummaries(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ab := sums["a/b"]
	want := store.PairCounts{IssuesOpen: 1, IssuesClosed: 1, PullsMerged: 1}
	if ab.Counts != want {
		t.Errorf("counts = %+v, want %+v", ab.Counts, want)
	}
	if ab.Queue.Depth != 2 || ab.Queue.Failing != 1 {
		t.Errorf("queue = %+v", ab.Queue)
	}
	if len(ab.Cursors) != 2 || ab.Cursors[0].Side != store.Forgejo {
		t.Errorf("cursors = %+v", ab.Cursors)
	}
	if cd := sums["c/d"]; cd.Counts.IssuesOpen != 1 || cd.Queue.Depth != 1 {
		t.Errorf("c/d = %+v", cd)
	}

	stats, err := st.QueueSummary(ctx)
	if err != nil || stats.Depth != 3 || stats.Failing != 1 {
		t.Errorf("stats = %+v, %v", stats, err)
	}
}

func TestReportsListPairs(t *testing.T) {
	st := open(t)
	issue, pull := seedReports(t, st)
	ctx := context.Background()

	all, total, _, err := st.Pairs(ctx, store.PairFilter{})
	if err != nil || total != 4 || len(all) != 4 {
		t.Fatalf("all = %d of %d, %v", len(all), total, err)
	}
	if all[0].Repository != "c/d" {
		t.Errorf("not newest first: %+v", all[0])
	}

	first, total, next, _ := st.Pairs(ctx, store.PairFilter{Repository: "a/b", Page: store.Page{Limit: 2}})
	if total != 3 || len(first) != 2 || next == nil {
		t.Fatalf("first page = %d of %d, next %v", len(first), total, next)
	}
	page, _, next, _ := st.Pairs(ctx, store.PairFilter{Repository: "a/b", Page: store.Page{Limit: 2, After: next}})
	if len(page) != 1 || page[0].ID != issue.ID || next != nil {
		t.Errorf("second page = %+v, next %v", page, next)
	}

	for _, tc := range []struct {
		name string
		f    store.PairFilter
		want []int64
	}{
		{"merged pulls", store.PairFilter{Repository: "a/b", State: store.StateMerged}, []int64{pull.ID}},
		{"pulls", store.PairFilter{Repository: "a/b", Kind: store.KindPull}, []int64{pull.ID}},
		{"a literal percent sign", store.PairFilter{Query: "100%"}, []int64{issue.ID}},
		{"an underscore is not a wildcard", store.PairFilter{Query: "load_t"}, []int64{issue.ID}},
		{"case-insensitive", store.PairFilter{Query: "PARSER"}, []int64{pull.ID}},
		{"a Forgejo number", store.PairFilter{Repository: "a/b", Query: "#71"}, []int64{issue.ID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _, _, err := st.Pairs(ctx, tc.f)
			if err != nil {
				t.Fatal(err)
			}
			var ids []int64
			for _, p := range got {
				ids = append(ids, p.ID)
			}
			if len(ids) != len(tc.want) || (len(ids) > 0 && ids[0] != tc.want[0]) {
				t.Errorf("ids = %v, want %v", ids, tc.want)
			}
		})
	}

	got, err := st.Pair(ctx, pull.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.StateMerged || got.Pull == nil || !got.Pull.TouchesCI || got.Pull.RebaseCommentID != 9 || got.Title != "Fix the parser" {
		t.Errorf("pull = %+v %+v", got, got.Pull)
	}
	got, _ = st.Pair(ctx, issue.ID)
	if got.Comments != 2 || got.Queued == nil || got.Queued.Attempts != 1 || got.Queued.LastError != "HTTP 403 from Forgejo" {
		t.Errorf("issue = %+v queued %+v", got, got.Queued)
	}
	if _, err := st.Pair(ctx, 999999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("a missing pair: %v", err)
	}
	comments, err := st.CommentPairs(ctx, issue.ID)
	if err != nil || len(comments) != 2 || comments[1].Origin != store.Forgejo {
		t.Errorf("comments = %+v, %v", comments, err)
	}
}

func TestReportsListTheQueueFailingFirst(t *testing.T) {
	st := open(t)
	issue, _ := seedReports(t, st)
	ctx := context.Background()

	q, _, _, err := st.Queue(ctx, store.QueueFilter{})
	if err != nil || len(q) != 3 {
		t.Fatalf("queue = %+v, %v", q, err)
	}
	if q[0].Attempts != 1 || q[0].PairID != issue.ID || q[0].Kind != store.KindIssue {
		t.Errorf("first = %+v", q[0])
	}
	for _, e := range q[1:] {
		if e.Repository == "a/b" && e.PairID != 0 {
			t.Errorf("an unpaired entry names a pair: %+v", e)
		}
	}
	q, _, _, _ = st.Queue(ctx, store.QueueFilter{Repository: "c/d"})
	if len(q) != 1 || q[0].PairID == 0 {
		t.Errorf("c/d = %+v", q)
	}
	q, _, _, _ = st.Queue(ctx, store.QueueFilter{FailingOnly: true})
	if len(q) != 1 {
		t.Errorf("failing = %+v", q)
	}
}

func TestReportsNameTheLeader(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if l, err := st.Leader(ctx); err != nil || l.Present {
		t.Fatalf("leader before anyone leads = %+v, %v", l, err)
	}
	leadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, err := postgres.AcquireLeader(leadCtx, st.Pool(), quiet); err != nil {
		t.Fatal(err)
	}
	l, err := st.Leader(ctx)
	host, _ := os.Hostname()
	if err != nil || !l.Present || l.Instance != host || l.ConnectedAt.IsZero() {
		t.Errorf("leader = %+v, %v; want %s", l, err, host)
	}
}

func TestTheConsoleOnlyReads(t *testing.T) {
	open(t)
	ctx := context.Background()
	ro, err := postgres.OpenReadOnly(ctx, os.Getenv("BRIDGE_TEST_DATABASE_URL"), 2, quiet)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, _, _, err := ro.Pairs(ctx, store.PairFilter{}); err != nil {
		t.Fatal(err)
	}
	err = ro.MarkDirty(ctx, store.DirtyKey{Repository: "a/b", Side: store.GitHub, Number: 1})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("a write through the console's session: %v", err)
	}
}

func TestChangesAreAnnounced(t *testing.T) {
	st := open(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// The console's own session, read-only, is the one that listens.
	ro, err := postgres.OpenReadOnly(ctx, os.Getenv("BRIDGE_TEST_DATABASE_URL"), 2, quiet)
	if err != nil {
		t.Fatal(err)
	}
	hub := events.NewHub(ro.Pool(), ro.Leader, quiet)
	// The hub's connection goes back only when it stops listening, and the
	// pool waits for it: stop the hub first.
	defer func() {
		cancel()
		hub.Close()
		ro.Close()
	}()
	ch, stop := hub.Subscribe()
	defer stop()
	go hub.Run(ctx)

	next := func() events.Event {
		t.Helper()
		select {
		case ev := <-ch:
			return ev
		case <-time.After(5 * time.Second):
			t.Fatal("no event")
			return events.Event{}
		}
	}
	// LISTEN takes effect asynchronously; retry the first change until the
	// hub hears one.
	var first events.Event
	for i := 0; ; i++ {
		if err := st.SetCursor(ctx, "a/b", store.GitHub, store.Issues, time.Now()); err != nil {
			t.Fatal(err)
		}
		select {
		case first = <-ch:
		case <-time.After(200 * time.Millisecond):
			if i < 25 {
				continue
			}
			t.Fatal("the hub never heard a change")
		}
		break
	}
	if first.Kind != "cursor" || !strings.Contains(string(first.Data), `"stream" : "issues"`) && !strings.Contains(string(first.Data), `"stream":"issues"`) {
		t.Errorf("cursor event = %s %s", first.Kind, first.Data)
	}
	for len(ch) > 0 {
		<-ch
	}

	m, err := st.InsertIssue(ctx, store.IssueMapping{Repository: "a/b", Kind: store.KindPull, GitHubNumber: 3, ForgejoNumber: 73, Origin: store.GitHub})
	if err != nil {
		t.Fatal(err)
	}
	if ev := next(); ev.Kind != "pair" || !strings.Contains(string(ev.Data), `"pairKind" : "pull"`) {
		t.Errorf("pair event = %s %s", ev.Kind, ev.Data)
	}
	if err := st.MarkDirty(ctx, store.DirtyKey{Repository: "a/b", Side: store.GitHub, Number: 3}); err != nil {
		t.Fatal(err)
	}
	if ev := next(); ev.Kind != "queue" {
		t.Errorf("queue event = %s %s", ev.Kind, ev.Data)
	}
	if err := st.SavePullRequest(ctx, store.PullRequestState{IssueMappingID: m.ID, ContribBranch: "pr/3", ImportedHeadSHA: "a", State: store.PullOpen}); err != nil {
		t.Fatal(err)
	}
	if ev := next(); ev.Kind != "pair" || !strings.Contains(string(ev.Data), strconv.FormatInt(m.ID, 10)) {
		t.Errorf("pull state event = %s %s", ev.Kind, ev.Data)
	}
}

func TestSystemDescribesTheDatabase(t *testing.T) {
	st := open(t)
	seedReports(t, st)
	sys, err := st.System(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if sys.Database.Version == "" || sys.Database.SizeBytes == 0 || sys.Database.PoolMax != 4 || len(sys.Database.Settings) == 0 {
		t.Errorf("database = %+v", sys.Database)
	}
	tables := map[string]store.SystemTable{}
	for _, tb := range sys.Tables {
		tables[tb.Name] = tb
	}
	if d, ok := tables["dirty"]; !ok || len(d.Indexes) == 0 || d.VacuumThreshold <= 0 {
		t.Errorf("dirty = %+v", d)
	}
	if len(sys.Migrations) < 5 || sys.Migrations[0].Name != "0001_initial.sql" {
		t.Errorf("migrations = %+v", sys.Migrations)
	}
}

func TestRateLimitsAreKept(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if _, err := st.Pool().Exec(ctx, `TRUNCATE github_rate_limit`); err != nil {
		t.Fatal(err)
	}
	reset := time.Now().Add(30 * time.Minute).Truncate(time.Second)
	for _, remaining := range []int{4990, 4870} {
		if err := st.SaveRateLimits(ctx, []store.RateLimit{{Resource: "core", Limit: 5000, Remaining: remaining,
			Used: 5000 - remaining, ResetAt: reset, ObservedAt: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.RateLimits(ctx)
	if err != nil || len(got) != 1 || got[0].Remaining != 4870 || !got[0].ResetAt.Equal(reset) {
		t.Errorf("rate limits = %+v, %v", got, err)
	}
}

func TestAuthorsAndTheirPairs(t *testing.T) {
	st := open(t)
	seedReports(t, st)
	ctx := context.Background()
	authors, _, _, err := st.Authors(ctx, store.AuthorFilter{})
	if err != nil {
		t.Fatal(err)
	}
	byLogin := map[string]store.Author{}
	for _, a := range authors {
		byLogin[a.Login] = a
	}
	if a := byLogin["bob"]; a.Pulls != 1 || a.Side != store.GitHub {
		t.Errorf("bob = %+v", a)
	}
	if a := byLogin["jdoe"]; a.Issues != 1 || a.Side != store.Forgejo {
		t.Errorf("jdoe = %+v", a)
	}
	pairs, total, _, err := st.Pairs(ctx, store.PairFilter{Author: "alice"})
	if err != nil || total != 1 || pairs[0].Author != "alice" {
		t.Errorf("alice's pairs = %+v of %d, %v", pairs, total, err)
	}
}

func TestAMaintenanceRoundVacuumsEveryTable(t *testing.T) {
	st := open(t)
	ctx := context.Background()
	if _, err := st.Pool().Exec(ctx, `TRUNCATE maintenance_round`); err != nil {
		t.Fatal(err)
	}
	if err := postgres.RunMaintenance(ctx, st.Pool(), quiet); err != nil {
		t.Fatal(err)
	}
	sys, err := st.System(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sys.Rounds) != 1 {
		t.Fatalf("rounds = %+v", sys.Rounds)
	}
	r := sys.Rounds[0]
	if r.FinishedAt.IsZero() || len(r.Failures) != 0 || !slices.Contains(r.Tables, "dirty") || !slices.Contains(r.Tables, "maintenance_round") {
		t.Errorf("round = %+v", r)
	}
	for _, tb := range sys.Tables {
		if tb.Name == "issue_mapping" && tb.LastVacuum.IsZero() {
			t.Errorf("issue_mapping was not vacuumed: %+v", tb)
		}
	}

	// A round already running elsewhere: this one does nothing.
	conn, err := st.Pool().Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(0x464742524d41494e)); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock($1)`, int64(0x464742524d41494e)) }()
	if err := postgres.RunMaintenance(ctx, st.Pool(), quiet); err != nil {
		t.Fatal(err)
	}
	if sys, _ := st.System(ctx); len(sys.Rounds) != 1 {
		t.Errorf("a second round ran beside the first: %d rounds", len(sys.Rounds))
	}
}

// walk reads every page of a list, a page of two at a time, and returns
// the keys in the order it saw them.
func walk[T any](t *testing.T, list func(store.Page) ([]T, int, *store.Position, error), sort string, key func(T) string) []string {
	t.Helper()
	var seen []string
	var after *store.Position
	for i := 0; i < 50; i++ {
		page, _, next, err := list(store.Page{Sort: sort, Limit: 2, After: after})
		if err != nil {
			t.Fatalf("sort %s: %v", sort, err)
		}
		for _, it := range page {
			seen = append(seen, key(it))
		}
		if next == nil {
			return seen
		}
		after = next
	}
	t.Fatalf("sort %s: the pages do not end", sort)
	return nil
}

func TestEverySortPagesThroughEveryRowOnce(t *testing.T) {
	st := open(t)
	seedReports(t, st)
	ctx := context.Background()
	pairs := func(p store.Page) ([]store.PairReport, int, *store.Position, error) {
		return st.Pairs(ctx, store.PairFilter{Page: p})
	}
	queue := func(p store.Page) ([]store.QueueReport, int, *store.Position, error) {
		return st.Queue(ctx, store.QueueFilter{Page: p})
	}
	authors := func(p store.Page) ([]store.Author, int, *store.Position, error) {
		return st.Authors(ctx, store.AuthorFilter{Page: p})
	}
	check := func(sort string, paged []string, whole []string) {
		t.Helper()
		if strings.Join(paged, ",") != strings.Join(whole, ",") {
			t.Errorf("sort %s: paged %v, whole %v", sort, paged, whole)
		}
	}
	for _, sort := range []string{"created", "-created", "updated", "title", "-title", "repository", "author", "-author",
		"github", "-forgejo", "state", "-comments"} {
		whole, _, _, err := st.Pairs(ctx, store.PairFilter{Page: store.Page{Sort: sort}})
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, p := range whole {
			keys = append(keys, strconv.FormatInt(p.ID, 10))
		}
		check(sort, walk(t, pairs, sort, func(p store.PairReport) string { return strconv.FormatInt(p.ID, 10) }), keys)
	}
	for _, sort := range []string{"due", "-due", "next", "marked", "-attempts", "repository", "number", "error", "-error"} {
		whole, _, _, err := st.Queue(ctx, store.QueueFilter{Page: store.Page{Sort: sort}})
		if err != nil {
			t.Fatal(err)
		}
		key := func(q store.QueueReport) string {
			return q.Repository + string(q.Side) + strconv.FormatInt(q.Number, 10)
		}
		var keys []string
		for _, q := range whole {
			keys = append(keys, key(q))
		}
		check(sort, walk(t, queue, sort, key), keys)
	}
	for _, sort := range []string{"-pairs", "pairs", "login", "-last", "issues"} {
		whole, _, _, err := st.Authors(ctx, store.AuthorFilter{Page: store.Page{Sort: sort}})
		if err != nil {
			t.Fatal(err)
		}
		key := func(a store.Author) string { return a.Login + string(a.Side) }
		var keys []string
		for _, a := range whole {
			keys = append(keys, key(a))
		}
		check(sort, walk(t, authors, sort, key), keys)
	}

	// Rows without a value come last, whichever the direction.
	q, _, _, _ := st.Queue(ctx, store.QueueFilter{Page: store.Page{Sort: "-error"}})
	if len(q) < 2 || q[0].LastError == "" || q[len(q)-1].LastError != "" {
		t.Errorf("-error = %+v", q)
	}
	// The default puts failing entries first.
	q, _, _, _ = st.Queue(ctx, store.QueueFilter{})
	if q[0].Attempts == 0 {
		t.Errorf("due: a failing entry is not first: %+v", q)
	}
	if _, _, _, err := st.Pairs(ctx, store.PairFilter{Page: store.Page{Sort: "bogus"}}); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("an unknown sort: %v", err)
	}
}

func TestQueueAndAuthorsAreSearched(t *testing.T) {
	st := open(t)
	seedReports(t, st)
	ctx := context.Background()
	q, total, _, err := st.Queue(ctx, store.QueueFilter{Query: "http 403"})
	if err != nil || total != 1 || len(q) != 1 {
		t.Errorf("queue by error = %+v of %d, %v", q, total, err)
	}
	q, total, _, _ = st.Queue(ctx, store.QueueFilter{Query: "c/"})
	if total != 1 || q[0].Repository != "c/d" {
		t.Errorf("queue by repository = %+v", q)
	}
	a, total, _, err := st.Authors(ctx, store.AuthorFilter{Query: "AL"})
	if err != nil || total != 1 || a[0].Login != "alice" {
		t.Errorf("authors = %+v of %d, %v", a, total, err)
	}
	a, total, _, _ = st.Authors(ctx, store.AuthorFilter{Kind: store.KindPull})
	if total != 1 || a[0].Login != "bob" || a[0].Issues != 0 || a[0].Pulls != 1 {
		t.Errorf("pull request authors = %+v of %d", a, total)
	}
	a, total, _, _ = st.Authors(ctx, store.AuthorFilter{Side: store.Forgejo})
	if total != 1 || a[0].Login != "jdoe" {
		t.Errorf("Forgejo authors = %+v of %d", a, total)
	}
}

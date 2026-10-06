package integration

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

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

	all, total, err := st.Pairs(ctx, store.PairFilter{})
	if err != nil || total != 4 || len(all) != 4 {
		t.Fatalf("all = %d of %d, %v", len(all), total, err)
	}
	if all[0].Repository != "c/d" {
		t.Errorf("not newest first: %+v", all[0])
	}

	page, total, _ := st.Pairs(ctx, store.PairFilter{Repository: "a/b", Limit: 2, Offset: 2})
	if total != 3 || len(page) != 1 || page[0].ID != issue.ID {
		t.Errorf("page = %+v of %d", page, total)
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
			got, _, err := st.Pairs(ctx, tc.f)
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

	q, err := st.Queue(ctx, store.QueueFilter{})
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
	q, _ = st.Queue(ctx, store.QueueFilter{Repository: "c/d"})
	if len(q) != 1 || q[0].PairID == 0 {
		t.Errorf("c/d = %+v", q)
	}
	q, _ = st.Queue(ctx, store.QueueFilter{FailingOnly: true})
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
	if _, _, err := ro.Pairs(ctx, store.PairFilter{}); err != nil {
		t.Fatal(err)
	}
	err = ro.MarkDirty(ctx, store.DirtyKey{Repository: "a/b", Side: store.GitHub, Number: 1})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Errorf("a write through the console's session: %v", err)
	}
}

// Package integration runs the store and the reconciler against a real
// PostgreSQL. `make integration-test` starts one; without
// BRIDGE_TEST_DATABASE_URL the tests are skipped, so a plain `go test ./...`
// stays runnable anywhere.
package integration

import (
	"context"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/bridge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge/fake"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/postgres"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/storetest"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// open connects and empties every table, so each test starts from nothing.
func open(t *testing.T) *postgres.Store {
	t.Helper()
	dsn := os.Getenv("BRIDGE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("BRIDGE_TEST_DATABASE_URL is not set; run `make integration-test`")
	}
	ctx := context.Background()
	st, err := postgres.Open(ctx, dsn, 4, quiet)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(st.Close)
	if _, err := st.Pool().Exec(ctx, `TRUNCATE issue_mapping, comment_mapping, pull_request_state, poll_cursor, dirty, github_rate_limit`); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestStoreContract(t *testing.T) {
	storetest.Run(t, func(t *testing.T) store.Store { return open(t) })
}

func TestMigrationsAreIdempotent(t *testing.T) {
	open(t)
	// A second process starting against a migrated database, as the second
	// replica of a rollout does.
	open(t)
}

func TestOnlyOneInstanceLeads(t *testing.T) {
	first, second := open(t), open(t)
	ctx, cancel := context.WithCancel(context.Background())
	lease, err := postgres.AcquireLeader(ctx, first.Pool(), quiet)
	if err != nil {
		t.Fatal(err)
	}

	waitCtx, cancelWait := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelWait()
	if _, err := postgres.AcquireLeader(waitCtx, second.Pool(), quiet); err == nil {
		t.Fatal("a second instance became leader while the first held the lock")
	}

	// The first instance stops; its session, and with it the lock, ends.
	cancel()
	<-lease.Lost()
	takeover, cancelTakeover := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelTakeover()
	if _, err := postgres.AcquireLeader(takeover, second.Pool(), quiet); err != nil {
		t.Fatalf("the second instance did not take over: %v", err)
	}
}

// TestBridgeOnPostgres runs a conversation through the reconciler with the
// real store underneath: what the memory store's contract tests cannot show
// is that the reconciler's sequence of calls works against real SQL.
func TestBridgeOnPostgres(t *testing.T) {
	st := open(t)
	gh, fj := fake.New("bridge-app[bot]", 41), fake.New("github-bridge", 71)
	ghRepo := config.RepoRef{Owner: "metapatrol", Name: "foo"}
	fjRepo := config.RepoRef{Owner: "platform", Name: "foo"}
	base, _ := url.Parse("https://forgejo.internal")
	b := bridge.New(st, gh, fj, []config.Repository{{
		GitHub: ghRepo, Forgejo: fjRepo,
		ImportLabel: "github", PublishLabel: "public", InternalPrefix: "/internal",
	}}, bridge.Options{PollInterval: time.Minute, FullResync: time.Hour, ForgejoURLs: []*url.URL{base}}, quiet)
	ctx := context.Background()
	if err := b.Init(ctx); err != nil {
		t.Fatal(err)
	}
	sync := func() {
		b.PollAll(ctx)
		for i := 0; ; i++ {
			processed, err := b.Step(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !processed {
				return
			}
			if i > 100 {
				t.Fatal("the queue does not drain")
			}
		}
	}

	src := gh.Open(ghRepo, "alice", "Crash", "It crashes.")
	gh.Comment(ghRepo, src.Number, "alice", "on every start")
	sync()
	proj := fj.Issues(fjRepo)
	if len(proj) != 1 {
		t.Fatalf("want one imported issue, have %d", len(proj))
	}
	fj.Comment(fjRepo, proj[0].Number, "jdoe", "fixed on develop")
	fj.SetClosed(fjRepo, proj[0].Number, true)
	sync()
	sync()

	got, _ := gh.Issue(ghRepo, src.Number)
	if !got.Closed {
		t.Error("closing in Forgejo did not reach GitHub")
	}
	comments := gh.CommentsOf(ghRepo, src.Number)
	if len(comments) != 2 || !strings.Contains(comments[1].Body, "fixed on develop") {
		t.Errorf("comments on GitHub = %+v", comments)
	}
	if n := len(fj.CommentsOf(fjRepo, proj[0].Number)); n != 2 {
		t.Errorf("want two comments in Forgejo, have %d", n)
	}
	if q, _ := st.DirtyStats(ctx); q.Depth != 0 {
		t.Errorf("the queue is not empty: %+v", q)
	}
}

// TestPullRequestOnPostgres takes a pull request from import to a rebased
// merge with the real store underneath.
func TestPullRequestOnPostgres(t *testing.T) {
	st := open(t)
	gh, fj := fake.New("bridge-app[bot]", 41), fake.New("github-bridge", 71)
	ghRepo := config.RepoRef{Owner: "metapatrol", Name: "foo"}
	fjRepo := config.RepoRef{Owner: "platform", Name: "foo"}
	base, _ := url.Parse("https://forgejo.internal")
	b := bridge.New(st, gh, fj, []config.Repository{{
		GitHub: ghRepo, Forgejo: fjRepo, Contrib: config.RepoRef{Owner: "github-contrib", Name: "foo"},
		TargetBranches: []string{"develop"}, CIPaths: config.DefaultCIPaths,
		ImportLabel: "github", PublishLabel: "public", InternalPrefix: "/internal",
	}}, bridge.Options{PollInterval: time.Minute, FullResync: time.Hour, ForgejoURLs: []*url.URL{base}}, quiet).
		WithPullRequests(bridge.Pulls{GitHub: gh, Forgejo: fj, Contrib: fj.As("github-contrib"), Git: &fake.Git{GitHub: gh, Forgejo: fj}})
	ctx := context.Background()
	if err := b.Init(ctx); err != nil {
		t.Fatal(err)
	}
	sync := func() {
		b.PollAll(ctx)
		for i := 0; i < 100; i++ {
			processed, err := b.Step(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if !processed {
				return
			}
		}
		t.Fatal("the queue does not drain")
	}

	src := gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1", ".forgejo/workflows/ci.yml")
	sync()
	gh.PushPull(ghRepo, src.Number, "sha2")
	sync()
	var pr forge.Issue
	for _, is := range fj.Issues(fjRepo) {
		if is.IsPullRequest {
			pr = is
		}
	}
	if pr.Number == 0 || pr.Author != "github-contrib" {
		t.Fatalf("no imported pull request: %+v", fj.Issues(fjRepo))
	}
	if sha, _ := fj.Branch("pr/" + strconv.FormatInt(src.Number, 10)); sha != "sha2" {
		t.Fatalf("contrib branch at %q", sha)
	}

	// A review with a line comment GitHub takes and one it does not.
	rv := fj.Review(fjRepo, pr.Number, "jdoe", "COMMENT", "")
	fj.LineComment(fjRepo, pr.Number, rv.ID, "jdoe", "On the line.", "ci.yml", 3, forge.SideRight, "sha2")
	fj.LineComment(fjRepo, pr.Number, rv.ID, "jdoe", "Off the record.", "x.go", 9, forge.SideRight, "unpublished")
	sync()
	sync()
	m, _, _ := st.IssueByGitHub(ctx, ghRepo.String(), src.Number)
	cms, err := st.Comments(ctx, m.ID)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[store.CommentKind]int{}
	for _, cm := range cms {
		kinds[cm.KindOf()]++
	}
	if kinds[store.CommentKindLine] != 1 || kinds[store.CommentKindLineAsComment] != 1 {
		t.Errorf("comment pairs by kind = %v", kinds)
	}
	if n := len(gh.LineComments(ghRepo, src.Number)); n != 1 {
		t.Errorf("want one review comment on GitHub, have %d", n)
	}

	fj.MergePull(fjRepo, pr.Number, "rebased")
	gh.Mirror(ghRepo, "develop", "rebased")
	sync()
	got, _ := gh.GetPullRequest(ctx, ghRepo, src.Number)
	if got.Open {
		t.Error("the GitHub pull request is still open after the merge was reported")
	}
	var reported int
	for _, c := range gh.CommentsOf(ghRepo, src.Number) {
		if strings.Contains(c.Body, "as rebased") && strings.Contains(c.Body, "were rebased") {
			reported++
		}
	}
	if reported != 1 {
		t.Errorf("the merge was reported %d times", reported)
	}
}

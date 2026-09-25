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
	"strings"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/bridge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
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
	if _, err := st.Pool().Exec(ctx, `TRUNCATE issue_mapping, comment_mapping, poll_cursor, dirty`); err != nil {
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
	fj.Comment(fjRepo, proj[0].Number, "ska", "fixed on develop")
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
	if n, _, _ := st.DirtyStats(ctx); n != 0 {
		t.Errorf("the queue is not empty: %d", n)
	}
}

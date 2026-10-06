package bridge

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge/fake"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/memory"
)

const (
	appBot     = "bridge-app[bot]"
	bridgeUser = "github-bridge"
)

var (
	ghRepo = config.RepoRef{Owner: "metapatrol", Name: "foo"}
	fjRepo = config.RepoRef{Owner: "platform", Name: "foo"}
)

type harness struct {
	t      *testing.T
	gh, fj *fake.Forge
	st     store.Store
	b      *Bridge
	repo   config.Repository
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		t:  t,
		gh: fake.New(appBot, 41),
		fj: fake.New(bridgeUser, 71),
		st: memory.New(),
		repo: config.Repository{
			GitHub:         ghRepo,
			Forgejo:        fjRepo,
			ImportLabel:    config.DefaultImportLabel,
			PublishLabel:   config.DefaultPublishLabel,
			InternalPrefix: config.DefaultInternalPrefix,
		},
	}
	h.restart()
	return h
}

// restart builds a new bridge over the same forges and store, as a new
// process would.
func (h *harness) restart() {
	h.t.Helper()
	base, _ := url.Parse("https://forgejo.internal")
	h.b = New(h.st, h.gh, h.fj, []config.Repository{h.repo}, Options{
		PollInterval: time.Minute,
		FullResync:   time.Hour,
		ForgejoURLs:  []*url.URL{base},
		UserMapping:  map[string]string{"jdoe": "janedoe"},
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := h.b.Init(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

// sync polls both sides and drains the queue, the way one tick of the running
// bridge would. Any reconcile error fails the test.
func (h *harness) sync() {
	h.t.Helper()
	ctx := context.Background()
	h.b.PollAll(ctx)
	h.drain()
}

func (h *harness) drain() {
	h.t.Helper()
	ctx := context.Background()
	for i := 0; ; i++ {
		if i > 500 {
			h.t.Fatal("the queue does not drain")
		}
		d, ok, err := h.st.NextDirty(ctx)
		if err != nil {
			h.t.Fatal(err)
		}
		if !ok {
			return
		}
		if err := h.b.reconcile(ctx, d.DirtyKey); err != nil {
			h.t.Fatalf("reconcile %+v: %v", d.DirtyKey, err)
		}
		if _, err := h.st.ClearDirty(ctx, d); err != nil {
			h.t.Fatal(err)
		}
	}
}

// settle syncs until nothing changes, and fails if that takes more than a few
// rounds: a bridge that keeps writing is a loop.
func (h *harness) settle() {
	h.t.Helper()
	for i := 0; i < 5; i++ {
		before := h.gh.Writes + h.fj.Writes
		h.sync()
		if h.gh.Writes+h.fj.Writes == before {
			return
		}
	}
	h.t.Fatal("the bridge keeps writing: a loop")
}

func (h *harness) only(f *fake.Forge, r config.RepoRef) forge.Issue {
	h.t.Helper()
	all := f.Issues(r)
	if len(all) != 1 {
		h.t.Fatalf("want exactly one issue in %s, have %d", r, len(all))
	}
	return all[0]
}

func TestImportsAGitHubIssueWithAttributionAndLabel(t *testing.T) {
	h := newHarness(t)
	src := h.gh.Open(ghRepo, "alice", "Crash on start", "It crashes.")

	h.settle()

	got := h.only(h.fj, fjRepo)
	if got.Title != "Crash on start" {
		t.Errorf("title = %q", got.Title)
	}
	for _, want := range []string{"It crashes.", "[alice](https://github.com/alice)", src.URL} {
		if !strings.Contains(got.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, got.Body)
		}
	}
	if mk, ok := marker.Parse(got.Body); !ok || mk.Origin != marker.OriginGitHub || mk.ID != src.Number {
		t.Errorf("marker = %+v, %v", mk, ok)
	}
	if !hasLabel(got.Labels, "github") {
		t.Errorf("labels = %v, want the import label", got.Labels)
	}
	if got.Number == src.Number {
		t.Fatal("the fakes number alike; the test proves nothing about mapping")
	}
}

func TestForgejoIssuesArePrivateUnlessLabelled(t *testing.T) {
	h := newHarness(t)
	h.fj.Open(fjRepo, "jdoe", "Internal refactoring", "Secret plans.")
	h.settle()
	if n := len(h.gh.Issues(ghRepo)); n != 0 {
		t.Fatalf("an unlabelled Forgejo issue reached GitHub (%d issues)", n)
	}

	pub := h.fj.Open(fjRepo, "jdoe", "Roadmap", "What comes next.", "public")
	h.settle()
	got := h.only(h.gh, ghRepo)
	if got.Title != "Roadmap" || !strings.Contains(got.Body, "What comes next.") {
		t.Errorf("projection = %+v", got)
	}
	if !strings.Contains(got.Body, "[janedoe](https://github.com/janedoe)") {
		t.Errorf("a mapped maintainer is not named:\n%s", got.Body)
	}
	if mk, _ := marker.Parse(got.Body); mk.Origin != marker.OriginForgejo || mk.ID != pub.Number {
		t.Errorf("marker = %+v", mk)
	}
}

func TestCommentsFlowBothWaysWithoutLooping(t *testing.T) {
	h := newHarness(t)
	src := h.gh.Open(ghRepo, "alice", "Bug", "Broken.")
	h.settle()
	proj := h.only(h.fj, fjRepo)

	h.gh.Comment(ghRepo, src.Number, "alice", "Any news?")
	h.fj.Comment(fjRepo, proj.Number, "jdoe", "Looking into it, @alice.")
	h.settle()

	ghc := h.gh.CommentsOf(ghRepo, src.Number)
	fjc := h.fj.CommentsOf(fjRepo, proj.Number)
	if len(ghc) != 2 || len(fjc) != 2 {
		t.Fatalf("want two comments on each side, have github=%d forgejo=%d", len(ghc), len(fjc))
	}
	reply := findBy(ghc, appBot)
	if !strings.Contains(reply.Body, "Looking into it, @alice.") {
		t.Errorf("a mention of a participant must still notify them:\n%s", reply.Body)
	}
	if !strings.Contains(reply.Body, "[janedoe](https://github.com/janedoe) wrote:") {
		t.Errorf("attribution:\n%s", reply.Body)
	}
	if imported := findBy(fjc, bridgeUser); !strings.Contains(imported.Body, "Any news?") {
		t.Errorf("imported comment:\n%s", imported.Body)
	}
}

func TestInternalCommentsStayInForgejo(t *testing.T) {
	h := newHarness(t)
	src := h.gh.Open(ghRepo, "alice", "Bug", "Broken.")
	h.settle()
	proj := h.only(h.fj, fjRepo)

	c := h.fj.Comment(fjRepo, proj.Number, "jdoe", "/internal this is the customer from last week")
	h.settle()
	if n := len(h.gh.CommentsOf(ghRepo, src.Number)); n != 0 {
		t.Fatalf("an internal comment reached GitHub")
	}

	// Made public by editing, and internal again: the projection follows.
	h.fj.EditCommentAs(c.ID, "Fixed in the next release.")
	h.settle()
	if n := len(h.gh.CommentsOf(ghRepo, src.Number)); n != 1 {
		t.Fatalf("want the comment on GitHub once it is public, have %d", n)
	}
	h.fj.EditCommentAs(c.ID, "/internal never mind")
	h.settle()
	if n := len(h.gh.CommentsOf(ghRepo, src.Number)); n != 0 {
		t.Fatalf("a comment turned internal must be withdrawn from GitHub")
	}
}

func TestEditsAndDeletionsFollowTheSource(t *testing.T) {
	h := newHarness(t)
	src := h.gh.Open(ghRepo, "alice", "Bug", "Broken.")
	c := h.gh.Comment(ghRepo, src.Number, "alice", "first version")
	h.settle()
	proj := h.only(h.fj, fjRepo)

	h.gh.EditBody(ghRepo, src.Number, "Bug in parser", "Broken in the parser.")
	h.gh.EditCommentAs(c.ID, "second version")
	h.settle()
	got, _ := h.fj.Issue(fjRepo, proj.Number)
	if got.Title != "Bug in parser" || !strings.Contains(got.Body, "Broken in the parser.") {
		t.Errorf("issue edit not carried: %+v", got)
	}
	fjc := h.fj.CommentsOf(fjRepo, proj.Number)
	if len(fjc) != 1 || !strings.Contains(fjc[0].Body, "second version") {
		t.Fatalf("comment edit not carried: %+v", fjc)
	}

	// A deletion is invisible to incremental polls; the full resync finds it.
	// The clocks move past the polls' overlap first, or the issue would be
	// re-read anyway for having changed a moment ago.
	h.gh.Advance(time.Hour)
	h.fj.Advance(time.Hour)
	h.gh.RemoveComment(c.ID)
	h.settle()
	if len(h.fj.CommentsOf(fjRepo, proj.Number)) != 1 {
		t.Fatal("a deletion was noticed without a resync; the test's premise is wrong")
	}
	if err := h.b.Resync(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.drain()
	if n := len(h.fj.CommentsOf(fjRepo, proj.Number)); n != 0 {
		t.Fatalf("the projection of a deleted comment survived the resync (%d left)", n)
	}
}

func TestClosingAndReopeningWorksFromEitherSide(t *testing.T) {
	h := newHarness(t)
	src := h.gh.Open(ghRepo, "alice", "Bug", "Broken.")
	h.settle()
	proj := h.only(h.fj, fjRepo)

	h.fj.SetClosed(fjRepo, proj.Number, true)
	h.settle()
	if got, _ := h.gh.Issue(ghRepo, src.Number); !got.Closed {
		t.Fatal("closing in Forgejo did not close on GitHub")
	}

	h.gh.SetClosed(ghRepo, src.Number, false)
	h.settle()
	if got, _ := h.fj.Issue(fjRepo, proj.Number); got.Closed {
		t.Fatal("reopening on GitHub did not reopen in Forgejo")
	}
}

func TestRemovingThePublishLabelFreezesThePair(t *testing.T) {
	h := newHarness(t)
	src := h.fj.Open(fjRepo, "jdoe", "Roadmap", "v1", "public")
	h.settle()
	pub := h.only(h.gh, ghRepo)

	h.fj.SetLabels(fjRepo, src.Number)
	h.fj.EditBody(fjRepo, src.Number, "Roadmap", "v2 with internal details")
	h.fj.Comment(fjRepo, src.Number, "jdoe", "internal follow-up")
	h.gh.Comment(ghRepo, pub.Number, "alice", "will this land?")
	h.settle()

	got, _ := h.gh.Issue(ghRepo, pub.Number)
	if strings.Contains(got.Body, "internal details") {
		t.Error("an issue whose label was removed kept publishing its body")
	}
	if n := len(h.gh.CommentsOf(ghRepo, pub.Number)); n != 1 {
		t.Errorf("want only alice's comment on GitHub, have %d", n)
	}
	if n := len(h.fj.CommentsOf(fjRepo, src.Number)); n != 1 {
		t.Errorf("a frozen pair still imported comments: %d", n)
	}
}

func TestReferencesAreTranslated(t *testing.T) {
	h := newHarness(t)
	first := h.gh.Open(ghRepo, "alice", "First", "one")
	h.settle()
	firstFJ := h.only(h.fj, fjRepo)

	second := h.gh.Open(ghRepo, "bob", "Second", "Same as #"+itoa(first.Number)+", see also #999.")
	h.settle()

	var secondFJ forge.Issue
	for _, is := range h.fj.Issues(fjRepo) {
		if is.Number != firstFJ.Number {
			secondFJ = is
		}
	}
	if !strings.Contains(secondFJ.Body, "Same as #"+itoa(firstFJ.Number)) {
		t.Errorf("a mapped reference was not rewritten:\n%s", secondFJ.Body)
	}
	if !strings.Contains(secondFJ.Body, "[github#999](https://github.com/metapatrol/foo/issues/999)") {
		t.Errorf("an unmapped reference must point at GitHub:\n%s", secondFJ.Body)
	}

	private := h.fj.Open(fjRepo, "jdoe", "Private", "nothing public")
	h.fj.Comment(fjRepo, secondFJ.Number, "jdoe",
		"Duplicate of #"+itoa(firstFJ.Number)+", related to #"+itoa(private.Number)+
			" and https://forgejo.internal/platform/foo/issues/"+itoa(private.Number))
	h.settle()
	reply := findBy(h.gh.CommentsOf(ghRepo, second.Number), appBot)
	if !strings.Contains(reply.Body, "Duplicate of #"+itoa(first.Number)) {
		t.Errorf("a mapped reference was not rewritten:\n%s", reply.Body)
	}
	if strings.Contains(reply.Body, "#"+itoa(private.Number)) || strings.Contains(reply.Body, "forgejo.internal") {
		t.Errorf("a private issue leaked:\n%s", reply.Body)
	}
}

func TestPullRequestsAndClosedIssuesAreNotImported(t *testing.T) {
	h := newHarness(t)
	h.gh.OpenPullRequest(ghRepo, "alice", "Fix typo")
	closed := h.gh.Open(ghRepo, "alice", "Old", "done")
	h.gh.SetClosed(ghRepo, closed.Number, true)
	h.settle()
	if n := len(h.fj.Issues(fjRepo)); n != 0 {
		t.Fatalf("want nothing imported, have %d", n)
	}
}

func TestImportSinceBoundsTheFirstImport(t *testing.T) {
	h := newHarness(t)
	old := h.gh.Open(ghRepo, "alice", "Old", "from before")
	h.repo.ImportSince = old.CreatedAt.Add(time.Millisecond)
	h.restart()
	h.gh.Open(ghRepo, "bob", "New", "from after")
	h.settle()
	if got := h.only(h.fj, fjRepo); got.Title != "New" {
		t.Fatalf("imported %q", got.Title)
	}
}

func TestALostDatabaseDoesNotDuplicateAnything(t *testing.T) {
	h := newHarness(t)
	gi := h.gh.Open(ghRepo, "alice", "Bug", "Broken.")
	h.gh.Comment(ghRepo, gi.Number, "alice", "details")
	roadmap := h.fj.Open(fjRepo, "jdoe", "Roadmap", "v1", "public")
	h.fj.Comment(fjRepo, roadmap.Number, "jdoe", "reply")
	h.settle()
	ghBefore, fjBefore := len(h.gh.Issues(ghRepo)), len(h.fj.Issues(fjRepo))

	h.st = memory.New()
	h.restart()
	h.gh.Comment(ghRepo, gi.Number, "alice", "after the restore")
	h.settle()

	if len(h.gh.Issues(ghRepo)) != ghBefore || len(h.fj.Issues(fjRepo)) != fjBefore {
		t.Fatalf("issues were duplicated after losing the database")
	}
	imported := projectionOf(t, h.fj.Issues(fjRepo), marker.OriginGitHub, gi.Number)
	published := projectionOf(t, h.gh.Issues(ghRepo), marker.OriginForgejo, roadmap.Number)
	for _, check := range []struct {
		name     string
		comments []forge.Comment
		want     int
	}{
		{"imported issue", h.fj.CommentsOf(fjRepo, imported.Number), 2},
		{"published issue", h.gh.CommentsOf(ghRepo, published.Number), 1},
	} {
		if len(check.comments) != check.want {
			t.Errorf("%s: want %d comments, have %d", check.name, check.want, len(check.comments))
		}
	}
}

func projectionOf(t *testing.T, issues []forge.Issue, origin marker.Origin, n int64) forge.Issue {
	t.Helper()
	for _, is := range issues {
		if mk, ok := marker.Parse(is.Body); ok && mk.Origin == origin && mk.ID == n {
			return is
		}
	}
	t.Fatalf("no projection of %s #%d", origin, n)
	return forge.Issue{}
}

func TestACrashBetweenCreatingAndRecordingDoesNotDuplicate(t *testing.T) {
	h := newHarness(t)
	src := h.gh.Open(ghRepo, "alice", "Bug", "Broken.")
	h.settle()

	// The projection exists and the database forgets it — the state a crash
	// between the two writes leaves behind — while the cursors survive, so
	// no recovery scan runs.
	m, _, _ := h.st.IssueByGitHub(context.Background(), ghRepo.String(), src.Number)
	if err := h.st.DeleteIssue(context.Background(), m.ID); err != nil {
		t.Fatal(err)
	}
	h.gh.Comment(ghRepo, src.Number, "alice", "ping")
	h.settle()

	if n := len(h.fj.Issues(fjRepo)); n != 1 {
		t.Fatalf("want one projection, have %d", n)
	}
}

func TestAFailedReconcileIsRetried(t *testing.T) {
	h := newHarness(t)
	h.gh.Open(ghRepo, "alice", "Bug", "Broken.")
	h.b.PollAll(context.Background())

	h.fj.FailNext = errors.New("forgejo is down")
	processed, err := h.b.Step(context.Background())
	if !processed || err != nil {
		t.Fatalf("step: processed=%v err=%v", processed, err)
	}
	if q, _ := h.st.DirtyStats(context.Background()); q.Depth != 1 || q.Failing != 1 {
		t.Fatalf("a failed resource must stay queued and count as failing: %+v", q)
	}
	if len(h.fj.Issues(fjRepo)) != 0 {
		t.Fatal("the failure was not injected")
	}

	// Held back by the backoff, then picked up.
	if processed, _ := h.b.Step(context.Background()); processed {
		t.Fatal("a failed resource was retried before its backoff")
	}
	h.st.(*memory.Store).SetClock(func() time.Time { return time.Now().Add(time.Hour) })
	if processed, err := h.b.Step(context.Background()); !processed || err != nil {
		t.Fatalf("retry: processed=%v err=%v", processed, err)
	}
	if n := len(h.fj.Issues(fjRepo)); n != 1 {
		t.Fatalf("the retry did not import, have %d", n)
	}
}

func TestOwnItemsAreNeverSources(t *testing.T) {
	h := newHarness(t)
	// Someone types a marker into their own issue, claiming it is the
	// projection of Forgejo #71.
	h.gh.Open(ghRepo, "mallory", "Totally legit", "<!-- forgejo-github-bridge origin=forgejo kind=issue id=71 -->")
	h.fj.Open(fjRepo, "jdoe", "Secret", "private")
	h.settle()

	all := h.fj.Issues(fjRepo)
	if len(all) != 2 {
		t.Fatalf("want the private issue and mallory's import, have %d", len(all))
	}
	m, ok, _ := h.st.IssueByForgejo(context.Background(), ghRepo.String(), 71)
	if ok {
		t.Fatalf("a forged marker created a mapping to the private issue: %+v", m)
	}
	for _, is := range all {
		if mk, _ := marker.Parse(is.Body); is.Author == bridgeUser && mk.Origin != marker.OriginGitHub {
			t.Errorf("the forged marker survived into the projection:\n%s", is.Body)
		}
	}
}

func findBy(cs []forge.Comment, author string) forge.Comment {
	for _, c := range cs {
		if c.Author == author {
			return c
		}
	}
	return forge.Comment{}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func TestPairsKeepTheirPublicTitle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	src := h.gh.Open(ghRepo, "alice", "Crash on start", "")
	h.settle()
	m, _, _ := h.st.IssueByGitHub(ctx, ghRepo.String(), src.Number)
	if m.Title != "Crash on start" || m.Author != "alice" {
		t.Errorf("imported pair = %q by %q", m.Title, m.Author)
	}

	pub := h.fj.Open(fjRepo, "jdoe", "Roadmap", "v1", "public")
	h.settle()
	m, _, _ = h.st.IssueByForgejo(ctx, ghRepo.String(), pub.Number)
	if m.Title != "Roadmap" || m.Author != "jdoe" {
		t.Errorf("published pair = %q by %q", m.Title, m.Author)
	}

	// Unpublished, then renamed: the new title never reached GitHub and must
	// not reach the console either.
	h.fj.SetLabels(fjRepo, pub.Number)
	h.fj.EditBody(fjRepo, pub.Number, "Roadmap: the secret acquisition", "v2")
	h.settle()
	m, _, _ = h.st.IssueByForgejo(ctx, ghRepo.String(), pub.Number)
	if m.Title != "Roadmap" {
		t.Errorf("a frozen pair took the title GitHub never saw: %q", m.Title)
	}
}

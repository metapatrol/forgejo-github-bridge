package bridge

import (
	"context"
	"strings"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge/fake"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store/memory"
)

const contribUser = "github-contrib"

var contribRepo = config.RepoRef{Owner: contribUser, Name: "foo"}

type pullHarness struct {
	*harness
	git *fake.Git
}

func newPullHarness(t *testing.T, tweak func(*config.Repository)) *pullHarness {
	t.Helper()
	h := newHarness(t)
	h.repo.Contrib = contribRepo
	h.repo.TargetBranches = []string{"develop"}
	h.repo.CIPaths = config.DefaultCIPaths
	if tweak != nil {
		tweak(&h.repo)
	}
	p := &pullHarness{harness: h, git: &fake.Git{GitHub: h.gh, Forgejo: h.fj}}
	p.restart()
	return p
}

func (p *pullHarness) restart() {
	p.harness.restart()
	p.b.WithPullRequests(Pulls{GitHub: p.gh, Forgejo: p.fj, Contrib: p.fj.As(contribUser), Git: p.git})
	if err := p.b.Init(context.Background()); err != nil {
		p.t.Fatal(err)
	}
}

// stepAll polls and works the queue the way the running bridge does,
// deferring what fails instead of failing the test. Used where waiting is
// part of the behaviour under test.
func (p *pullHarness) stepAll() {
	p.t.Helper()
	ctx := context.Background()
	p.b.PollAll(ctx)
	for i := 0; ; i++ {
		if i > 200 {
			p.t.Fatal("the queue does not drain")
		}
		processed, err := p.b.Step(ctx)
		if err != nil {
			p.t.Fatal(err)
		}
		if !processed {
			return
		}
	}
}

// later lets deferred entries come due.
func (p *pullHarness) later() {
	p.st.(*memory.Store).SetClock(func() time.Time { return time.Now().Add(24 * time.Hour) })
}

func (p *pullHarness) forgejoPull() forge.Issue {
	p.t.Helper()
	var out []forge.Issue
	for _, is := range p.fj.Issues(fjRepo) {
		if is.IsPullRequest {
			out = append(out, is)
		}
	}
	if len(out) != 1 {
		p.t.Fatalf("want exactly one pull request in Forgejo, have %d", len(out))
	}
	return out[0]
}

func (p *pullHarness) botComments(n int64) []forge.Comment {
	var out []forge.Comment
	for _, c := range p.gh.CommentsOf(ghRepo, n) {
		if c.Author == appBot {
			out = append(out, c)
		}
	}
	return out
}

func TestImportsAPullRequestFromTheContribAccount(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix the parser", "Fixes #9.", "develop", "sha1", "parser.go")
	p.settle()

	got := p.forgejoPull()
	if got.Author != contribUser {
		t.Errorf("opened by %q; imported pull requests must come from the untrusted contrib account", got.Author)
	}
	if !hasLabel(got.Labels, "github") || hasLabel(got.Labels, touchesCILabel) {
		t.Errorf("labels = %v", got.Labels)
	}
	if mk, _ := marker.Parse(got.Body); mk.Kind != marker.KindPull || mk.ID != src.Number {
		t.Errorf("marker = %+v", mk)
	}
	if !strings.Contains(got.Body, "Opened by [alice](https://github.com/alice) on GitHub") {
		t.Errorf("body:\n%s", got.Body)
	}
	if sha, _ := p.fj.Branch(branchFor(src.Number)); sha != "sha1" {
		t.Errorf("contrib branch at %q", sha)
	}
	pr, _ := p.fj.GetPullRequest(context.Background(), fjRepo, got.Number)
	if pr.BaseRef != "develop" || pr.HeadSHA != "sha1" {
		t.Errorf("pull request = %+v", pr)
	}
}

func TestAPullRequestAgainstAnotherBranchIsExplainedOnce(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "main", "sha1")
	p.settle()
	p.settle()
	if n := len(p.botComments(src.Number)); n != 1 {
		t.Fatalf("want one explanation, have %d comments", n)
	}
	for _, is := range p.fj.Issues(fjRepo) {
		if is.IsPullRequest {
			t.Fatal("a pull request against main was imported")
		}
	}

	p.gh.RetargetPull(ghRepo, src.Number, "develop")
	p.settle()
	p.forgejoPull()
}

func TestNewCommitsFollowUnlessSomebodyElsePushed(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
	p.settle()
	branch := branchFor(src.Number)

	p.gh.PushPull(ghRepo, src.Number, "sha2")
	p.settle()
	if sha, _ := p.fj.Branch(branch); sha != "sha2" {
		t.Fatalf("contrib branch at %q, want the new head", sha)
	}

	// A maintainer pushes a fixup to the contrib branch.
	p.fj.PushBranch(branch, "maintainer-fixup")
	p.gh.PushPull(ghRepo, src.Number, "sha3")
	p.settle()
	p.gh.PushPull(ghRepo, src.Number, "sha4")
	p.settle()
	if sha, _ := p.fj.Branch(branch); sha != "maintainer-fixup" {
		t.Fatalf("the maintainer's push was overwritten with %q", sha)
	}
	var notes int
	for _, c := range p.fj.CommentsOf(fjRepo, p.forgejoPull().Number) {
		if c.Author == bridgeUser && strings.Contains(c.Body, "changed by someone other than the bridge") {
			notes++
		}
	}
	if notes != 1 {
		t.Errorf("want one note about the frozen branch, have %d", notes)
	}
}

func TestCIChangesAreLabelledAndWarnedAboutOnce(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "mallory", "Speed up CI", "", "develop", "sha1", "README.md", ".forgejo/workflows/ci.yml")
	p.settle()
	p.gh.PushPull(ghRepo, src.Number, "sha2", ".github/workflows/x.yml")
	p.settle()

	got := p.forgejoPull()
	if !hasLabel(got.Labels, touchesCILabel) {
		t.Errorf("labels = %v", got.Labels)
	}
	var warnings int
	for _, c := range p.fj.CommentsOf(fjRepo, got.Number) {
		if strings.Contains(c.Body, "changes CI configuration") {
			warnings++
			if !strings.Contains(c.Body, ".forgejo/workflows/ci.yml") || strings.Contains(c.Body, "README.md") {
				t.Errorf("warning names the wrong files:\n%s", c.Body)
			}
		}
	}
	if warnings != 1 {
		t.Errorf("want one warning, have %d", warnings)
	}
}

func TestAFastForwardMergeIsMergedOnGitHub(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
	p.settle()
	fj := p.forgejoPull()

	p.fj.MergePull(fjRepo, fj.Number, "sha1")
	p.stepAll()
	if len(p.botComments(src.Number)) != 0 {
		t.Fatal("the merge was reported before the push mirror brought it to GitHub")
	}
	if _, ok := p.fj.Branch(branchFor(src.Number)); ok {
		t.Error("the contrib branch outlived the merge")
	}

	p.gh.Mirror(ghRepo, "develop", "sha1")
	p.later()
	p.stepAll()
	got, _ := p.gh.GetPullRequest(context.Background(), ghRepo, src.Number)
	if !got.Merged {
		t.Fatal("GitHub did not mark the pull request merged; the fake's premise is wrong")
	}
	notes := p.botComments(src.Number)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "Merged into `develop` as sha1") {
		t.Fatalf("notes = %+v", notes)
	}
	p.stepAll()
	if n := len(p.botComments(src.Number)); n != 1 {
		t.Errorf("the merge was reported %d times", n)
	}
}

func TestARebasedMergeIsReportedAndClosed(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
	p.settle()
	p.fj.MergePull(fjRepo, p.forgejoPull().Number, "rebased1")
	p.gh.Mirror(ghRepo, "develop", "rebased1")
	p.stepAll()

	got, _ := p.gh.GetPullRequest(context.Background(), ghRepo, src.Number)
	if got.Open || got.Merged {
		t.Fatalf("GitHub pull request = %+v; want closed, not merged", got)
	}
	notes := p.botComments(src.Number)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "Integrated into `develop` as rebased1") {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestClosingEitherSideClosesTheOther(t *testing.T) {
	t.Run("maintainers close it", func(t *testing.T) {
		p := newPullHarness(t, nil)
		src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
		p.settle()
		p.fj.SetClosed(fjRepo, p.forgejoPull().Number, true)
		p.settle()
		got, _ := p.gh.GetPullRequest(context.Background(), ghRepo, src.Number)
		if got.Open {
			t.Error("the GitHub pull request stayed open")
		}
		if notes := p.botComments(src.Number); len(notes) != 1 || !strings.Contains(notes[0].Body, "closed this pull request without merging") {
			t.Errorf("notes = %+v", notes)
		}
		if _, ok := p.fj.Branch(branchFor(src.Number)); ok {
			t.Error("the contrib branch outlived the close")
		}
	})
	t.Run("the contributor withdraws it, and reopens it", func(t *testing.T) {
		p := newPullHarness(t, nil)
		src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
		p.settle()
		fj := p.forgejoPull()
		p.gh.SetClosed(ghRepo, src.Number, true)
		p.settle()
		if got, _ := p.fj.Issue(fjRepo, fj.Number); !got.Closed {
			t.Fatal("the Forgejo pull request stayed open")
		}
		if _, ok := p.fj.Branch(branchFor(src.Number)); ok {
			t.Error("the contrib branch outlived the close")
		}

		p.gh.PushPull(ghRepo, src.Number, "sha2")
		p.gh.SetClosed(ghRepo, src.Number, false)
		p.settle()
		if got, _ := p.fj.Issue(fjRepo, fj.Number); got.Closed {
			t.Fatal("reopening on GitHub did not reopen in Forgejo")
		}
		if sha, _ := p.fj.Branch(branchFor(src.Number)); sha != "sha2" {
			t.Errorf("contrib branch at %q after reopening", sha)
		}
	})
}

func TestRebaseRequestsAreOneCommentKeptUpToDate(t *testing.T) {
	p := newPullHarness(t, func(r *config.Repository) { r.RebaseRequests = true })
	p.gh.SetBehind("sha1", 3)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
	p.settle()
	p.settle()
	notes := p.botComments(src.Number)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "moved on by 3 commit(s)") {
		t.Fatalf("notes = %+v", notes)
	}

	p.gh.PushPull(ghRepo, src.Number, "sha2")
	p.settle()
	notes = p.botComments(src.Number)
	if len(notes) != 1 || !strings.Contains(notes[0].Body, "up to date") {
		t.Fatalf("notes after the rebase = %+v", notes)
	}
	for _, c := range p.fj.CommentsOf(fjRepo, p.forgejoPull().Number) {
		if strings.Contains(c.Body, "rebase") {
			t.Errorf("a rebase request crossed into Forgejo:\n%s", c.Body)
		}
	}
}

func TestNoRebaseRequestsUnlessConfigured(t *testing.T) {
	p := newPullHarness(t, nil)
	p.gh.SetBehind("sha1", 3)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
	p.settle()
	if n := len(p.botComments(src.Number)); n != 0 {
		t.Fatalf("asked for a rebase without being configured to")
	}
}

func TestConversationAndReviewsCross(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
	p.settle()
	fj := p.forgejoPull()

	p.gh.Comment(ghRepo, src.Number, "alice", "Ready for review.")
	p.fj.Comment(fjRepo, fj.Number, "ska", "Thanks @alice, one question.")
	p.fj.Review(fjRepo, fj.Number, "ska", "REQUEST_CHANGES", "Please add a test.")
	p.fj.Review(fjRepo, fj.Number, "ska", "COMMENT", "")
	p.fj.Review(fjRepo, fj.Number, "ska", "APPROVED", "")
	p.settle()

	bodies := ""
	for _, c := range p.botComments(src.Number) {
		bodies += c.Body + "\n---\n"
	}
	for _, want := range []string{"Thanks @alice, one question.", "requested changes", "Please add a test.", "approved these changes"} {
		if !strings.Contains(bodies, want) {
			t.Errorf("GitHub lacks %q:\n%s", want, bodies)
		}
	}
	if n := len(p.botComments(src.Number)); n != 3 {
		t.Errorf("want the comment and two reviews on GitHub, have %d", n)
	}
	var imported bool
	for _, c := range p.fj.CommentsOf(fjRepo, fj.Number) {
		imported = imported || strings.Contains(c.Body, "Ready for review.")
	}
	if !imported {
		t.Error("the contributor's comment did not reach Forgejo")
	}
}

func TestMaintainersOwnPullRequestsStayPrivate(t *testing.T) {
	p := newPullHarness(t, nil)
	p.fj.OpenPull(fjRepo, "ska", "Internal refactoring", "", "develop", "x", "a.go")
	p.fj.SetLabels(fjRepo, p.fj.Issues(fjRepo)[0].Number, "public")
	p.settle()
	if n := len(p.gh.Issues(ghRepo)); n != 0 {
		t.Fatalf("a maintainer's pull request reached GitHub")
	}
}

func TestPullRequestsSurviveALostDatabase(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
	p.settle()

	p.st = memory.New()
	p.restart()
	p.gh.PushPull(ghRepo, src.Number, "sha2")
	p.settle()

	p.forgejoPull()
	if sha, _ := p.fj.Branch(branchFor(src.Number)); sha != "sha2" {
		t.Errorf("contrib branch at %q after recovery", sha)
	}
}

func TestAnInterruptedImportIsFinished(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1")
	// The branch was pushed, and the process died before opening the pull
	// request.
	p.fj.PushBranch(branchFor(src.Number), "sha1")
	p.settle()
	p.forgejoPull()
}

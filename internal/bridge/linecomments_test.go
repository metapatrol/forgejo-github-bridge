package bridge

import (
	"context"
	"strings"
	"testing"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
)

// botLines are the line comments the bridge wrote on the GitHub pull request.
func (p *pullHarness) botLines(n int64) []forge.LineComment {
	var out []forge.LineComment
	for _, lc := range p.gh.LineComments(ghRepo, n) {
		if lc.Author == appBot {
			out = append(out, lc)
		}
	}
	return out
}

// pairID is the mapping of GitHub pull request n.
func (p *pullHarness) pairID(n int64) int64 {
	p.t.Helper()
	m, ok, err := p.st.IssueByGitHub(context.Background(), ghRepo.String(), n)
	if err != nil || !ok {
		p.t.Fatalf("no pair for GitHub #%d: %v", n, err)
	}
	return m.ID
}

// resync reconciles every open pair, as the periodic full resync does: what
// no poll sees, such as a deleted review comment, it does.
func (p *pullHarness) resync() {
	p.t.Helper()
	if err := p.b.Resync(context.Background()); err != nil {
		p.t.Fatal(err)
	}
	p.drain()
}

func TestALineCommentLandsOnItsLine(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix the parser", "", "develop", "sha1", "parser.go")
	p.settle()
	fj := p.forgejoPull()

	rv := p.fj.Review(fjRepo, fj.Number, "jdoe", "REQUEST_CHANGES", "A few things.")
	p.fj.LineComment(fjRepo, fj.Number, rv.ID, "jdoe", "This loops forever on `\\n`, @alice.", "parser.go", 12, forge.SideRight, "sha1")
	p.fj.LineComment(fjRepo, fj.Number, rv.ID, "jdoe", "Why was this removed?", "parser.go", 7, forge.SideLeft, "sha1")
	p.settle()

	lines := p.botLines(src.Number)
	if len(lines) != 2 {
		t.Fatalf("want two line comments on GitHub, have %+v", lines)
	}
	first, second := lines[0], lines[1]
	if first.Path != "parser.go" || first.Line != 12 || first.Side != forge.SideRight || first.CommitID != "sha1" {
		t.Errorf("first line comment at %s:%d %s %s", first.Path, first.Line, first.Side, first.CommitID)
	}
	if second.Line != 7 || second.Side != forge.SideLeft {
		t.Errorf("a comment on a removed line went to %d %s", second.Line, second.Side)
	}
	if !strings.Contains(first.Body, "This loops forever on `\\n`, @alice.") || !strings.Contains(first.Body, "wrote:") {
		t.Errorf("body:\n%s", first.Body)
	}
	if mk, ok := marker.Parse(first.Body); !ok || mk.Kind != marker.KindLine || mk.Origin != marker.OriginForgejo {
		t.Errorf("marker = %+v", mk)
	}
	// The review's verdict and summary still reach the conversation, once.
	var summaries int
	for _, c := range p.botComments(src.Number) {
		if strings.Contains(c.Body, "requested changes") {
			summaries++
		}
		if strings.Contains(c.Body, "loops forever") {
			t.Errorf("a line comment also went into the conversation:\n%s", c.Body)
		}
	}
	if summaries != 1 {
		t.Errorf("want the review summary once, have %d", summaries)
	}

	// Nothing is written twice.
	before := p.gh.Writes
	p.resync()
	if p.gh.Writes != before {
		t.Errorf("a resync wrote %d times without a change", p.gh.Writes-before)
	}
}

func TestALineCommentFollowsItsSource(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1", "parser.go")
	p.settle()
	fj := p.forgejoPull()
	rv := p.fj.Review(fjRepo, fj.Number, "jdoe", "COMMENT", "")
	lc := p.fj.LineComment(fjRepo, fj.Number, rv.ID, "jdoe", "First thought.", "parser.go", 3, forge.SideRight, "sha1")
	p.settle()

	p.fj.EditLineCommentAs(lc.ID, "Second thought.")
	p.resync()
	if lines := p.botLines(src.Number); len(lines) != 1 || !strings.Contains(lines[0].Body, "Second thought.") {
		t.Fatalf("after an edit: %+v", lines)
	}

	// Deleted on GitHub by somebody: written again.
	p.gh.DeleteLineCommentAs(p.botLines(src.Number)[0].ID)
	p.resync()
	if lines := p.botLines(src.Number); len(lines) != 1 {
		t.Fatalf("a deleted projection was not written again: %+v", lines)
	}

	// Turned internal: withdrawn.
	p.fj.EditLineCommentAs(lc.ID, "/internal let us not say this")
	p.resync()
	if lines := p.botLines(src.Number); len(lines) != 0 {
		t.Fatalf("an internal line comment is still on GitHub: %+v", lines)
	}
	p.fj.EditLineCommentAs(lc.ID, "Third thought.")
	p.resync()

	// Deleted in Forgejo: gone from GitHub.
	p.fj.DeleteLineCommentAs(lc.ID)
	p.resync()
	if lines := p.botLines(src.Number); len(lines) != 0 {
		t.Errorf("a deleted line comment is still on GitHub: %+v", lines)
	}
	if cs, _ := p.st.Comments(context.Background(), p.pairID(src.Number)); len(cs) != 0 {
		t.Errorf("mappings left: %+v", cs)
	}
}

func TestALineGitHubWillNotTakeGoesIntoTheConversation(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1", "parser.go")
	p.settle()
	fj := p.forgejoPull()
	rv := p.fj.Review(fjRepo, fj.Number, "jdoe", "COMMENT", "")
	// A commit GitHub never had: a maintainer pushed it to the contrib
	// branch. Its code is not public, and the projection quotes none of it.
	lc := p.fj.LineComment(fjRepo, fj.Number, rv.ID, "jdoe", "Careful here.", "secret/internal.go", 40, forge.SideRight, "maintainer-sha")
	p.settle()

	if lines := p.botLines(src.Number); len(lines) != 0 {
		t.Fatalf("placed on a commit GitHub does not have: %+v", lines)
	}
	var found []forge.Comment
	for _, c := range p.botComments(src.Number) {
		if strings.Contains(c.Body, "Careful here.") {
			found = append(found, c)
		}
	}
	if len(found) != 1 || !strings.Contains(found[0].Body, "line 40 of `secret/internal.go`") {
		t.Fatalf("conversation fallback = %+v", found)
	}
	cs, _ := p.st.Comments(context.Background(), p.pairID(src.Number))
	if len(cs) != 1 || cs[0].Kind != store.CommentKindLineAsComment || cs[0].ForgejoCommentID != lc.ID {
		t.Errorf("mapping = %+v", cs)
	}

	// Edited: the fallback follows, and stays one comment.
	p.fj.EditLineCommentAs(lc.ID, "Careful here, really.")
	p.resync()
	p.resync()
	var after []forge.Comment
	for _, c := range p.botComments(src.Number) {
		if strings.Contains(c.Body, "Careful here") {
			after = append(after, c)
		}
	}
	if len(after) != 1 || !strings.Contains(after[0].Body, "really") {
		t.Errorf("after an edit: %+v", after)
	}
}

func TestAnUnrecordedLineProjectionIsAdopted(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1", "parser.go")
	p.settle()
	fj := p.forgejoPull()
	rv := p.fj.Review(fjRepo, fj.Number, "jdoe", "COMMENT", "")
	p.fj.LineComment(fjRepo, fj.Number, rv.ID, "jdoe", "Hm.", "parser.go", 1, forge.SideRight, "sha1")
	p.settle()

	// The process died after writing the projection, before recording it.
	ctx := context.Background()
	cs, _ := p.st.Comments(ctx, p.pairID(src.Number))
	for _, cm := range cs {
		if cm.KindOf() == store.CommentKindLine {
			if err := p.st.DeleteComment(ctx, cm.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	p.resync()
	if lines := p.botLines(src.Number); len(lines) != 1 {
		t.Errorf("want the one projection adopted, have %d", len(lines))
	}
}

func TestAContributorsLineCommentStaysOnGitHub(t *testing.T) {
	p := newPullHarness(t, nil)
	src := p.gh.OpenPull(ghRepo, "alice", "Fix", "", "develop", "sha1", "parser.go")
	p.settle()
	fj := p.forgejoPull()
	// A review comment by somebody on GitHub. Only the Forgejo → GitHub
	// direction is carried; this must neither cross nor be taken for a
	// projection.
	p.gh.LineComment(ghRepo, src.Number, 0, "alice", "Note to self.", "parser.go", 2, forge.SideRight, "sha1")
	p.fj.Review(fjRepo, fj.Number, "jdoe", "APPROVED", "")
	p.settle()
	p.resync()
	if got := p.gh.LineComments(ghRepo, src.Number); len(got) != 1 || got[0].Author != "alice" {
		t.Errorf("GitHub's line comments = %+v", got)
	}
	for _, c := range p.fj.CommentsOf(fjRepo, fj.Number) {
		if strings.Contains(c.Body, "Note to self.") {
			t.Errorf("a contributor's line comment crossed: %s", c.Body)
		}
	}
}

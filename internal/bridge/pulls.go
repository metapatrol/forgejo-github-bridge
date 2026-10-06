package bridge

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/gitops"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/telemetry"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/translate"
)

// touchesCILabel marks an imported pull request that changes CI
// configuration (docs/design.md §12).
const touchesCILabel = "touches-ci"

// errWaitingForMirror is returned while a merge in Forgejo has not reached
// GitHub yet. The pull request stays queued and is looked at again with
// backoff; it is not a failure.
var errWaitingForMirror = errors.New("waiting for the push mirror to bring the merge to GitHub")

// Git moves pull request heads into the contrib repository.
type Git interface {
	// Import pushes GitHub pull request number's head to branch in the contrib
	// repository if the branch is at expected ("" for absent), and returns
	// the pushed commit. gitops.ErrLeaseRejected if it is not.
	Import(ctx context.Context, repo config.Repository, number int64, base, branch, expected string) (string, error)
}

// Pulls is what the bridge needs for pull requests.
type Pulls struct {
	GitHub  forge.GitHubPulls
	Forgejo forge.ForgejoPulls
	Contrib forge.ContribPulls
	Git     Git
}

// WithPullRequests enables importing pull requests for the repositories
// that name target branches.
func (b *Bridge) WithPullRequests(p Pulls) *Bridge {
	b.pulls = &p
	return b
}

func branchFor(n int64) string { return "pr/" + strconv.FormatInt(n, 10) }

// importPull imports a GitHub pull request that has no Forgejo counterpart
// yet (docs/design.md §11).
func (b *Bridge) importPull(ctx context.Context, r *repo, n int64) error {
	if b.pulls == nil || !r.cfg.ImportsPullRequests() {
		return nil
	}
	pr, err := b.pulls.GitHub.GetPullRequest(ctx, r.cfg.GitHub, n)
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !pr.Open {
		return nil
	}
	if !slices.Contains(r.cfg.TargetBranches, pr.BaseRef) {
		return b.noticeOnce(ctx, r, n, r.msg.Render("unsupportedBase", b.data(r, pr)))
	}

	m, found, err := b.findProjectionAs(ctx, r, store.Forgejo, b.contribSelf, marker.KindPull, n)
	if err != nil || found {
		if found {
			return b.pair(ctx, r, m)
		}
		return err
	}

	branch := branchFor(n)
	sha, err := b.importHead(ctx, r, n, pr.BaseRef, branch, "")
	if errors.Is(err, gitops.ErrLeaseRejected) {
		// The branch exists. If it holds exactly this head, it is the
		// bridge's own from an attempt that died before opening the pull
		// request; anything else belongs to somebody, and is left alone.
		// The contrib fork was checked a moment ago.
		sha, err = b.pulls.Git.Import(ctx, r.cfg, n, pr.BaseRef, branch, pr.HeadSHA)
		if errors.Is(err, gitops.ErrLeaseRejected) {
			return fmt.Errorf("branch %s already exists in %s and was not made by the bridge; "+
				"GitHub pull request #%d cannot be imported until it is removed: %w", branch, r.cfg.Contrib, n, err)
		}
	}
	if err != nil {
		return fmt.Errorf("importing the head of GitHub pull request #%d: %w", n, err)
	}

	refs, err := b.refsToForgejo(ctx, r, pr.Body)
	if err != nil {
		return err
	}
	title, body := r.tr.PullRequestToForgejo(pr, refs)
	created, err := b.pulls.Contrib.CreatePullRequest(ctx, r.cfg.Forgejo, r.cfg.Contrib.Owner+":"+branch, pr.BaseRef, title, body)
	if err != nil {
		return fmt.Errorf("opening the Forgejo pull request for GitHub #%d: %w", n, err)
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "pull", "create")
	b.log.Info("imported a GitHub pull request", "repository", r.cfg.Key(), "github", n, "forgejo", created.Number, "head", sha)

	m, err = b.store.InsertIssue(ctx, store.IssueMapping{
		Repository: r.cfg.Key(), Kind: store.KindPull, GitHubNumber: n, ForgejoNumber: created.Number, Origin: store.GitHub,
	})
	if err != nil {
		return err
	}
	st := store.PullRequestState{IssueMappingID: m.ID, ContribBranch: branch, ImportedHeadSHA: sha, State: store.PullOpen}
	if err := b.store.SavePullRequest(ctx, st); err != nil {
		return err
	}
	// The label is set by the bridge's account: the contrib account that
	// opened the pull request has no write access to label it.
	if err := b.pulls.Forgejo.AddLabels(ctx, r.cfg.Forgejo, created.Number, []string{r.cfg.ImportLabel}); err != nil {
		return err
	}
	return b.pair(ctx, r, m)
}

// pairPull reconciles a pull request pair.
func (b *Bridge) pairPull(ctx context.Context, r *repo, m store.IssueMapping) error {
	if b.pulls == nil || !r.cfg.ImportsPullRequests() {
		return nil
	}
	gh, ghErr := b.pulls.GitHub.GetPullRequest(ctx, r.cfg.GitHub, m.GitHubNumber)
	fj, fjErr := b.pulls.Forgejo.GetPullRequest(ctx, r.cfg.Forgejo, m.ForgejoNumber)
	if isNotFound(ghErr) || isNotFound(fjErr) {
		b.log.Warn("one side of a pull request pair no longer exists", "repository", r.cfg.Key(),
			"github", m.GitHubNumber, "forgejo", m.ForgejoNumber)
		return b.store.DeleteIssue(ctx, m.ID)
	}
	if err := errors.Join(ghErr, fjErr); err != nil {
		return err
	}
	// Imported pull requests always come from GitHub.
	if err := b.summarize(ctx, m, gh.Title, gh.Author); err != nil {
		return err
	}

	st, ok, err := b.store.PullRequest(ctx, m.ID)
	if err != nil {
		return err
	}
	if !ok {
		// Recovered from markers: the contrib branch holds whatever the pull
		// request shows.
		st = store.PullRequestState{IssueMappingID: m.ID, ContribBranch: branchFor(m.GitHubNumber),
			ImportedHeadSHA: fj.HeadSHA, State: store.PullOpen}
		if !fj.Open {
			st.State, st.OutcomeReported = store.PullClosed, true
			if fj.Merged {
				st.State = store.PullMerged
			}
		}
	}

	ghComments, err := b.github.ListComments(ctx, r.cfg.GitHub, m.GitHubNumber)
	if err != nil {
		return err
	}
	fjComments, err := b.forgej.ListComments(ctx, r.cfg.Forgejo, m.ForgejoNumber)
	if err != nil {
		return err
	}
	reviews, err := b.pulls.Forgejo.ListReviews(ctx, r.cfg.Forgejo, m.ForgejoNumber)
	if err != nil {
		return err
	}
	participants := b.participantsOf(gh.Author, ghComments)

	if err := b.syncPullContent(ctx, r, m, gh, fj); err != nil {
		return err
	}
	// The conversation first: a pull request closed with a comment is read
	// on GitHub with the comment before the close, the way it was written.
	if err := b.syncComments(ctx, r, m, ghComments, fjComments, reviews, participants); err != nil {
		return err
	}
	if err := b.syncLineComments(ctx, r, m, reviews, ghComments, participants); err != nil {
		return err
	}

	lifecycleErr := b.lifecycle(ctx, r, m, &st, gh, fj, ghComments)
	if err := b.store.SavePullRequest(ctx, st); err != nil {
		return err
	}
	if err := b.store.SetIssueSyncedClosed(ctx, m.ID, st.State != store.PullOpen); err != nil {
		return err
	}
	return lifecycleErr
}

// data is what message templates get for a pull request.
func (b *Bridge) data(r *repo, pr forge.PullRequest) translate.MessageData {
	return translate.MessageData{
		Base: pr.BaseRef, Targets: translate.TargetList(r.cfg.TargetBranches),
		Contributor: pr.Author, Branch: branchFor(pr.Number),
	}
}

// syncPullContent writes the GitHub pull request's title and description to
// the Forgejo one. The GitHub side is the source: it is the contributor's
// description of their change.
func (b *Bridge) syncPullContent(ctx context.Context, r *repo, m store.IssueMapping, gh, fj forge.PullRequest) error {
	refs, err := b.refsToForgejo(ctx, r, gh.Body)
	if err != nil {
		return err
	}
	title, body := r.tr.PullRequestToForgejo(gh, refs)
	if fj.Title == title && translate.Equal(fj.Body, body) {
		return nil
	}
	if _, err := b.forgej.EditIssue(ctx, r.cfg.Forgejo, m.ForgejoNumber, forge.IssueEdit{Title: &title, Body: &body}); err != nil {
		return err
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "pull", "edit")
	return nil
}

// lifecycle moves a pull request pair through open, merged and closed.
func (b *Bridge) lifecycle(ctx context.Context, r *repo, m store.IssueMapping, st *store.PullRequestState, gh, fj forge.PullRequest, ghComments []forge.Comment) error {
	switch st.State {
	case store.PullOpen:
		switch {
		case fj.Merged:
			st.State = store.PullMerged
			b.log.Info("an imported pull request was merged", "repository", r.cfg.Key(),
				"forgejo", m.ForgejoNumber, "commit", fj.MergeCommitSHA)
			if err := b.deleteBranch(ctx, r, st); err != nil {
				return err
			}
			return b.reportMerge(ctx, r, m, st, fj)
		case gh.Merged:
			// The contributor's commits reached the base by some other way,
			// and GitHub says so itself. Forgejo detects the same, or a
			// maintainer closes it; there is nothing to report.
			st.State, st.OutcomeReported = store.PullMerged, true
			return b.deleteBranch(ctx, r, st)
		case !fj.Open:
			st.State = store.PullClosed
			if err := b.deleteBranch(ctx, r, st); err != nil {
				return err
			}
			if gh.Open {
				if err := b.notice(ctx, r, m.GitHubNumber, r.msg.Render("closedUpstream", b.data(r, gh))); err != nil {
					return err
				}
				if err := b.closeGitHub(ctx, r, m); err != nil {
					return err
				}
			}
			st.OutcomeReported = true
			return nil
		case !gh.Open:
			// Withdrawn by the contributor.
			st.State, st.OutcomeReported = store.PullClosed, true
			closed := true
			if _, err := b.forgej.EditIssue(ctx, r.cfg.Forgejo, m.ForgejoNumber, forge.IssueEdit{Closed: &closed}); err != nil {
				return err
			}
			telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "pull", "close")
			return b.deleteBranch(ctx, r, st)
		}
		if err := b.syncBase(ctx, r, m, gh, fj); err != nil {
			return err
		}
		if err := b.syncHead(ctx, r, m, st, gh); err != nil {
			return err
		}
		return b.rebaseRequest(ctx, r, m, st, gh, ghComments)

	case store.PullMerged:
		if !st.OutcomeReported {
			return b.reportMerge(ctx, r, m, st, fj)
		}
	case store.PullClosed:
		if gh.Open && !gh.Merged {
			// Reopened on GitHub: imported again, into the branch the close
			// removed.
			sha, err := b.importHead(ctx, r, m.GitHubNumber, gh.BaseRef, st.ContribBranch, "")
			if err != nil {
				return err
			}
			open := false
			if _, err := b.forgej.EditIssue(ctx, r.cfg.Forgejo, m.ForgejoNumber, forge.IssueEdit{Closed: &open}); err != nil {
				return err
			}
			telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "pull", "reopen")
			*st = store.PullRequestState{IssueMappingID: st.IssueMappingID, ContribBranch: st.ContribBranch,
				ImportedHeadSHA: sha, State: store.PullOpen, TouchesCI: st.TouchesCI, RebaseCommentID: st.RebaseCommentID}
		}
	}
	return nil
}

// reportMerge tells the GitHub pull request how it ended, once the push
// mirror has brought the merge there.
func (b *Bridge) reportMerge(ctx context.Context, r *repo, m store.IssueMapping, st *store.PullRequestState, fj forge.PullRequest) error {
	if st.OutcomeReported {
		return nil
	}
	sha := fj.MergeCommitSHA
	arrived, err := b.pulls.GitHub.Contains(ctx, r.cfg.GitHub, fj.BaseRef, sha)
	if err != nil {
		return err
	}
	if !arrived {
		return errWaitingForMirror
	}
	gh, err := b.pulls.GitHub.GetPullRequest(ctx, r.cfg.GitHub, m.GitHubNumber)
	if err != nil {
		return err
	}
	d := b.data(r, gh)
	d.Base, d.Commit = fj.BaseRef, sha
	if gh.Merged {
		if err := b.notice(ctx, r, m.GitHubNumber, r.msg.Render("merged", d)); err != nil {
			return err
		}
	} else {
		if err := b.notice(ctx, r, m.GitHubNumber, r.msg.Render("integrated", d)); err != nil {
			return err
		}
		if gh.Open {
			if err := b.closeGitHub(ctx, r, m); err != nil {
				return err
			}
		}
	}
	st.OutcomeReported = true
	return nil
}

// syncBase follows a base branch the contributor changed on GitHub, if the
// new one takes contributions.
func (b *Bridge) syncBase(ctx context.Context, r *repo, m store.IssueMapping, gh, fj forge.PullRequest) error {
	if gh.BaseRef == fj.BaseRef {
		return nil
	}
	if !slices.Contains(r.cfg.TargetBranches, gh.BaseRef) {
		return b.noticeOnce(ctx, r, m.GitHubNumber, r.msg.Render("unsupportedBase", b.data(r, gh)))
	}
	if err := b.pulls.Forgejo.SetBase(ctx, r.cfg.Forgejo, m.ForgejoNumber, gh.BaseRef); err != nil {
		return err
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "pull", "rebase-target")
	return nil
}

// syncHead imports new commits the contributor pushed.
func (b *Bridge) syncHead(ctx context.Context, r *repo, m store.IssueMapping, st *store.PullRequestState, gh forge.PullRequest) error {
	if !st.TouchesCI {
		if err := b.checkCI(ctx, r, m, st); err != nil {
			return err
		}
	}
	if st.HeadFrozen || gh.HeadSHA == st.ImportedHeadSHA {
		return nil
	}
	sha, err := b.importHead(ctx, r, m.GitHubNumber, gh.BaseRef, st.ContribBranch, st.ImportedHeadSHA)
	if errors.Is(err, gitops.ErrLeaseRejected) {
		st.HeadFrozen = true
		b.log.Warn("the contrib branch was moved by someone else; no longer importing into it",
			"repository", r.cfg.Key(), "branch", st.ContribBranch)
		_, err := b.forgej.CreateComment(ctx, r.cfg.Forgejo, m.ForgejoNumber,
			translate.Notice(marker.OriginGitHub, m.GitHubNumber, r.msg.Render("headFrozen", translate.MessageData{Branch: st.ContribBranch})))
		return err
	}
	if err != nil {
		return err
	}
	b.log.Info("imported new commits", "repository", r.cfg.Key(), "github", m.GitHubNumber, "head", sha)
	telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "pull", "push")
	st.ImportedHeadSHA = sha
	if st.TouchesCI {
		return nil
	}
	return b.checkCI(ctx, r, m, st)
}

// checkCI labels and warns about a pull request that changes CI
// configuration. Once is enough: the label stays, and so does the warning.
func (b *Bridge) checkCI(ctx context.Context, r *repo, m store.IssueMapping, st *store.PullRequestState) error {
	files, err := b.pulls.GitHub.PullRequestFiles(ctx, r.cfg.GitHub, m.GitHubNumber)
	if err != nil {
		return err
	}
	var hits []string
	for _, f := range files {
		for _, p := range r.cfg.CIPaths {
			if strings.HasPrefix(f, p) {
				hits = append(hits, f)
				break
			}
		}
	}
	if len(hits) == 0 {
		return nil
	}
	if err := b.pulls.Forgejo.AddLabels(ctx, r.cfg.Forgejo, m.ForgejoNumber, []string{touchesCILabel}); err != nil {
		return err
	}
	if _, err := b.forgej.CreateComment(ctx, r.cfg.Forgejo, m.ForgejoNumber,
		translate.Notice(marker.OriginGitHub, m.GitHubNumber, r.msg.Render("touchesCI", translate.MessageData{Paths: translate.PathList(hits)}))); err != nil {
		return err
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "pull", "touches-ci")
	st.TouchesCI = true
	return nil
}

// rebaseRequest keeps the one comment about rebasing in step with whether
// the pull request is behind its base.
//
// Each time the pull request falls behind, the contributor is told once: a
// new comment, which notifies them. While it stays behind and the base only
// moves further, that comment is edited, which notifies nobody. Once it is
// up to date, the comment says so. There is never more than one.
func (b *Bridge) rebaseRequest(ctx context.Context, r *repo, m store.IssueMapping, st *store.PullRequestState, gh forge.PullRequest, ghComments []forge.Comment) error {
	if !r.cfg.RebaseRequests || st.HeadFrozen {
		return nil
	}
	behind, err := b.pulls.GitHub.BehindBy(ctx, r.cfg.GitHub, gh.BaseRef, gh.HeadSHA)
	if err != nil {
		return err
	}
	var existing *forge.Comment
	for i := range ghComments {
		if st.RebaseCommentID != 0 && ghComments[i].ID == st.RebaseCommentID {
			existing = &ghComments[i]
		}
	}
	d := b.data(r, gh)
	d.Behind = behind

	switch {
	case behind > 0 && existing != nil && st.RebaseRequested:
		// Still behind: keep the request current, quietly.
		return b.editNotice(ctx, r, m, existing, r.msg.Render("rebaseRequest", d))

	case behind > 0:
		// Newly behind, or the request went missing: a new comment, so the
		// contributor hears about it. The old one, saying it was up to date,
		// would only contradict it.
		if existing != nil {
			if err := b.github.DeleteComment(ctx, r.cfg.GitHub, existing.ID); err != nil && !isNotFound(err) {
				return err
			}
		}
		created, err := b.github.CreateComment(ctx, r.cfg.GitHub, m.GitHubNumber,
			translate.Notice(marker.OriginGitHub, m.GitHubNumber, r.msg.Render("rebaseRequest", d)))
		if err != nil {
			return err
		}
		telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "notice", "create")
		st.RebaseCommentID, st.RebaseRequested = created.ID, true
		return nil

	case existing != nil:
		st.RebaseRequested = false
		return b.editNotice(ctx, r, m, existing, r.msg.Render("upToDate", d))

	default:
		// Up to date, and the comment is gone or there never was one.
		st.RebaseCommentID, st.RebaseRequested = 0, false
		return nil
	}
}

// editNotice brings one of the bridge's own comments to the given text.
func (b *Bridge) editNotice(ctx context.Context, r *repo, m store.IssueMapping, c *forge.Comment, text string) error {
	body := translate.Notice(marker.OriginGitHub, m.GitHubNumber, text)
	if translate.Equal(c.Body, body) {
		return nil
	}
	if _, err := b.github.EditComment(ctx, r.cfg.GitHub, c.ID, body); err != nil {
		return err
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "notice", "edit")
	return nil
}

func (b *Bridge) deleteBranch(ctx context.Context, r *repo, st *store.PullRequestState) error {
	if st.HeadFrozen {
		// Somebody else's work may be on it.
		return nil
	}
	return b.pulls.Contrib.DeleteBranch(ctx, r.cfg.Contrib, st.ContribBranch)
}

func (b *Bridge) closeGitHub(ctx context.Context, r *repo, m store.IssueMapping) error {
	closed := true
	if _, err := b.github.EditIssue(ctx, r.cfg.GitHub, m.GitHubNumber, forge.IssueEdit{Closed: &closed}); err != nil {
		return err
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "pull", "close")
	return nil
}

// notice comments on GitHub issue or pull request n as the bridge.
func (b *Bridge) notice(ctx context.Context, r *repo, n int64, text string) error {
	if _, err := b.github.CreateComment(ctx, r.cfg.GitHub, n, translate.Notice(marker.OriginForgejo, n, text)); err != nil {
		return err
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "notice", "create")
	return nil
}

// noticeOnce comments unless the bridge already said the same thing there.
func (b *Bridge) noticeOnce(ctx context.Context, r *repo, n int64, text string) error {
	body := translate.Notice(marker.OriginForgejo, n, text)
	comments, err := b.github.ListComments(ctx, r.cfg.GitHub, n)
	if err != nil {
		return err
	}
	for _, c := range comments {
		if c.Author == b.githubSelf && translate.Equal(c.Body, body) {
			return nil
		}
	}
	return b.notice(ctx, r, n, text)
}

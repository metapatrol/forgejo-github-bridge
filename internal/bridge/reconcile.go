package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/telemetry"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/translate"
)

// reconcile makes both sides of one resource agree.
func (b *Bridge) reconcile(ctx context.Context, k store.DirtyKey) error {
	r, ok := b.repos[k.Repository]
	if !ok {
		// Queued before the repository was removed from the configuration.
		// Nothing to do, and nothing that could be done.
		return nil
	}
	if k.Side == store.GitHub {
		m, ok, err := b.store.IssueByGitHub(ctx, k.Repository, k.Number)
		if err != nil {
			return err
		}
		if ok {
			return b.pair(ctx, r, m)
		}
		return b.importIssue(ctx, r, k.Number)
	}
	m, ok, err := b.store.IssueByForgejo(ctx, k.Repository, k.Number)
	if err != nil {
		return err
	}
	if ok {
		return b.pair(ctx, r, m)
	}
	return b.publishIssue(ctx, r, k.Number)
}

// importIssue creates the Forgejo projection of a GitHub issue that has none.
func (b *Bridge) importIssue(ctx context.Context, r *repo, n int64) error {
	is, err := b.github.GetIssue(ctx, r.cfg.GitHub, n)
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case is.IsPullRequest:
		return b.importPull(ctx, r, n)
	case is.Author == b.githubSelf:
		return b.adopt(ctx, r, store.GitHub, is)
	case is.Closed:
		// A projection of something already finished helps nobody, and
		// would reopen nothing. Closed issues are never imported.
		return nil
	case !r.cfg.ImportSince.IsZero() && is.CreatedAt.Before(r.cfg.ImportSince):
		return nil
	}

	m, found, err := b.findProjection(ctx, r, store.Forgejo, n)
	if err != nil || found {
		if found {
			return b.pair(ctx, r, m)
		}
		return err
	}

	refs, err := b.refsToForgejo(ctx, r, is.Body)
	if err != nil {
		return err
	}
	title, body := r.tr.IssueToForgejo(is, refs)
	created, err := b.forgej.CreateIssue(ctx, r.cfg.Forgejo, title, body, []string{r.cfg.ImportLabel})
	if err != nil {
		return fmt.Errorf("creating the Forgejo issue for GitHub #%d: %w", n, err)
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "issue", "create")
	b.log.Info("imported a GitHub issue", "repository", r.cfg.Key(), "github", n, "forgejo", created.Number)

	m, err = b.store.InsertIssue(ctx, store.IssueMapping{
		Repository: r.cfg.Key(), GitHubNumber: n, ForgejoNumber: created.Number, Origin: store.GitHub,
	})
	if err != nil {
		return err
	}
	return b.pair(ctx, r, m)
}

// publishIssue creates the GitHub projection of a Forgejo issue that has
// none, if it is meant to be public.
func (b *Bridge) publishIssue(ctx context.Context, r *repo, n int64) error {
	is, err := b.forgej.GetIssue(ctx, r.cfg.Forgejo, n)
	if isNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	switch {
	case is.IsPullRequest && is.Author == b.contribSelf && b.contribSelf != "":
		return b.adopt(ctx, r, store.Forgejo, is)
	case is.IsPullRequest:
		// Maintainers' own pull requests stay in Forgejo.
		return nil
	case is.Author == b.forgejoSelf:
		return b.adopt(ctx, r, store.Forgejo, is)
	case is.Closed:
		return nil
	case !hasLabel(is.Labels, r.cfg.PublishLabel):
		// Private by default (docs/design.md §10). This is the rule the whole
		// issue flow hangs on: nothing leaves Forgejo without the label.
		return nil
	}

	m, found, err := b.findProjection(ctx, r, store.GitHub, n)
	if err != nil || found {
		if found {
			return b.pair(ctx, r, m)
		}
		return err
	}

	refs, err := b.refsToGitHub(ctx, r, is.Body)
	if err != nil {
		return err
	}
	title, body := r.tr.IssueToGitHub(is, refs, nil)
	created, err := b.github.CreateIssue(ctx, r.cfg.GitHub, title, body, nil)
	if err != nil {
		return fmt.Errorf("creating the GitHub issue for Forgejo #%d: %w", n, err)
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "issue", "create")
	b.log.Info("published a Forgejo issue", "repository", r.cfg.Key(), "forgejo", n, "github", created.Number)

	m, err = b.store.InsertIssue(ctx, store.IssueMapping{
		Repository: r.cfg.Key(), GitHubNumber: created.Number, ForgejoNumber: n, Origin: store.Forgejo,
	})
	if err != nil {
		return err
	}
	return b.pair(ctx, r, m)
}

// adopt records a projection the bridge made but the database does not know
// about, and then reconciles it.
func (b *Bridge) adopt(ctx context.Context, r *repo, side store.Side, projection forge.Issue) error {
	want := marker.OriginForgejo
	if side == store.Forgejo {
		want = marker.OriginGitHub
	}
	mk, ok := marker.Parse(projection.Body)
	wantKind := marker.KindIssue
	if projection.IsPullRequest {
		wantKind = marker.KindPull
	}
	if !ok || mk.Origin != want || mk.Kind != wantKind {
		return nil
	}
	m, err := b.store.InsertIssue(ctx, recoveredMapping(r.cfg.Key(), side, projection, mk))
	if isConflict(err) {
		return nil
	}
	if err != nil {
		return err
	}
	b.log.Warn("adopted a projection the database did not know", "repository", r.cfg.Key(),
		"side", side, "number", projection.Number)
	return b.pair(ctx, r, m)
}

// findProjection looks on one side for a recent issue the bridge opened as
// the projection of source number n, and records it when there is one. It is
// the guard against duplicating an issue when the process died between
// creating a projection and recording it.
func (b *Bridge) findProjection(ctx context.Context, r *repo, side store.Side, n int64) (store.IssueMapping, bool, error) {
	self := b.githubSelf
	if side == store.Forgejo {
		self = b.forgejoSelf
	}
	return b.findProjectionAs(ctx, r, side, self, marker.KindIssue, n)
}

// findProjectionAs is findProjection for projections opened by a given
// account, of a given kind.
func (b *Bridge) findProjectionAs(ctx context.Context, r *repo, side store.Side, self string, kind marker.Kind, n int64) (store.IssueMapping, bool, error) {
	client, ref, origin := b.github, r.cfg.GitHub, marker.OriginForgejo
	if side == store.Forgejo {
		client, ref, origin = b.forgej, r.cfg.Forgejo, marker.OriginGitHub
	}
	recent, err := client.ListIssuesByAuthor(ctx, ref, self, b.now().Add(-projectionLookback))
	if err != nil {
		return store.IssueMapping{}, false, fmt.Errorf("looking for an existing projection: %w", err)
	}
	for _, is := range recent {
		if is.Author != self {
			continue
		}
		mk, ok := marker.Parse(is.Body)
		if !ok || mk.Origin != origin || mk.Kind != kind || mk.ID != n {
			continue
		}
		m, err := b.store.InsertIssue(ctx, recoveredMapping(r.cfg.Key(), side, is, mk))
		if err != nil {
			return store.IssueMapping{}, false, err
		}
		b.log.Warn("found an unrecorded projection instead of creating a second one",
			"repository", r.cfg.Key(), "side", side, "number", is.Number)
		return m, true, nil
	}
	return store.IssueMapping{}, false, nil
}

// pair reconciles a mapped issue: content, state and comments.
func (b *Bridge) pair(ctx context.Context, r *repo, m store.IssueMapping) error {
	if m.Kind == store.KindPull {
		return b.pairPull(ctx, r, m)
	}
	gh, ghErr := b.github.GetIssue(ctx, r.cfg.GitHub, m.GitHubNumber)
	fj, fjErr := b.forgej.GetIssue(ctx, r.cfg.Forgejo, m.ForgejoNumber)
	if isNotFound(ghErr) || isNotFound(fjErr) {
		return b.vanished(ctx, r, m, isNotFound(ghErr), gh, fj)
	}
	if err := errors.Join(ghErr, fjErr); err != nil {
		return err
	}

	if m.Origin == store.Forgejo && !hasLabel(fj.Labels, r.cfg.PublishLabel) {
		// The label was removed after publishing. The GitHub issue stays, and
		// stops receiving anything (docs/design.md §10) — in both directions,
		// since replies to it would carry the conversation on in public.
		b.log.Debug("publish label removed; pair frozen", "repository", r.cfg.Key(), "forgejo", m.ForgejoNumber)
		return nil
	}

	ghComments, err := b.github.ListComments(ctx, r.cfg.GitHub, m.GitHubNumber)
	if err != nil {
		return err
	}
	fjComments, err := b.forgej.ListComments(ctx, r.cfg.Forgejo, m.ForgejoNumber)
	if err != nil {
		return err
	}
	participants := b.participantsOf(gh.Author, ghComments)

	if err := b.syncContent(ctx, r, m, gh, fj, participants); err != nil {
		return err
	}
	// Comments before state: an issue closed with a comment is read on the
	// other side with the comment first, the way it was written.
	if err := b.syncComments(ctx, r, m, ghComments, fjComments, nil, participants); err != nil {
		return err
	}
	return b.syncState(ctx, r, m, gh, fj)
}

// participantsOf are the GitHub accounts taking part in an issue. A Forgejo
// comment that mentions one of them is meant for them, and may notify them.
func (b *Bridge) participantsOf(author string, comments []forge.Comment) map[string]bool {
	p := map[string]bool{}
	if author != b.githubSelf {
		p[strings.ToLower(author)] = true
	}
	for _, c := range comments {
		if c.Author != b.githubSelf {
			p[strings.ToLower(c.Author)] = true
		}
	}
	return p
}

// syncContent writes the source's title and body to the projection.
func (b *Bridge) syncContent(ctx context.Context, r *repo, m store.IssueMapping, gh, fj forge.Issue, participants map[string]bool) error {
	if m.Origin == store.GitHub {
		refs, err := b.refsToForgejo(ctx, r, gh.Body)
		if err != nil {
			return err
		}
		title, body := r.tr.IssueToForgejo(gh, refs)
		if fj.Title == title && translate.Equal(fj.Body, body) {
			return nil
		}
		if _, err := b.forgej.EditIssue(ctx, r.cfg.Forgejo, m.ForgejoNumber, forge.IssueEdit{Title: &title, Body: &body}); err != nil {
			return err
		}
		telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "issue", "edit")
		return nil
	}

	refs, err := b.refsToGitHub(ctx, r, fj.Body)
	if err != nil {
		return err
	}
	title, body := r.tr.IssueToGitHub(fj, refs, participants)
	if gh.Title == title && translate.Equal(gh.Body, body) {
		return nil
	}
	if _, err := b.github.EditIssue(ctx, r.cfg.GitHub, m.GitHubNumber, forge.IssueEdit{Title: &title, Body: &body}); err != nil {
		return err
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "issue", "edit")
	return nil
}

// syncState carries a close or reopen across. Either side may do either: the
// side whose state differs from the last synced one is the side that acted.
func (b *Bridge) syncState(ctx context.Context, r *repo, m store.IssueMapping, gh, fj forge.Issue) error {
	var final bool
	switch {
	case gh.Closed == fj.Closed:
		final = gh.Closed
	case gh.Closed != m.SyncedClosed:
		if _, err := b.forgej.EditIssue(ctx, r.cfg.Forgejo, m.ForgejoNumber, forge.IssueEdit{Closed: &gh.Closed}); err != nil {
			return err
		}
		telemetry.RecordChange(r.cfg.Key(), string(store.Forgejo), "issue", state(gh.Closed))
		final = gh.Closed
	default:
		if _, err := b.github.EditIssue(ctx, r.cfg.GitHub, m.GitHubNumber, forge.IssueEdit{Closed: &fj.Closed}); err != nil {
			return err
		}
		telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "issue", state(fj.Closed))
		final = fj.Closed
	}
	if final == m.SyncedClosed {
		return nil
	}
	return b.store.SetIssueSyncedClosed(ctx, m.ID, final)
}

func state(closed bool) string {
	if closed {
		return "close"
	}
	return "reopen"
}

// vanished handles a pair one of whose issues no longer exists. When the
// source is gone, the projection is closed; when the projection is gone, the
// mapping is dropped and the source is treated as new the next time it
// changes.
func (b *Bridge) vanished(ctx context.Context, r *repo, m store.IssueMapping, githubGone bool, gh, fj forge.Issue) error {
	sourceGone := githubGone == (m.Origin == store.GitHub)
	b.log.Warn("one side of a pair no longer exists", "repository", r.cfg.Key(),
		"github", m.GitHubNumber, "forgejo", m.ForgejoNumber, "github_gone", githubGone, "source_gone", sourceGone)

	if sourceGone {
		closed := true
		if githubGone && !fj.Closed && fj.Number != 0 {
			if _, err := b.forgej.EditIssue(ctx, r.cfg.Forgejo, m.ForgejoNumber, forge.IssueEdit{Closed: &closed}); err != nil && !isNotFound(err) {
				return err
			}
		}
		if !githubGone && !gh.Closed && gh.Number != 0 {
			if _, err := b.github.EditIssue(ctx, r.cfg.GitHub, m.GitHubNumber, forge.IssueEdit{Closed: &closed}); err != nil && !isNotFound(err) {
				return err
			}
		}
	}
	return b.store.DeleteIssue(ctx, m.ID)
}

// refsToForgejo maps the GitHub issue numbers a GitHub body refers to.
func (b *Bridge) refsToForgejo(ctx context.Context, r *repo, body string) (map[int64]int64, error) {
	out := map[int64]int64{}
	for _, n := range r.tr.GitHubRefs(body) {
		m, ok, err := b.store.IssueByGitHub(ctx, r.cfg.Key(), n)
		if err != nil {
			return nil, err
		}
		if ok {
			out[n] = m.ForgejoNumber
		}
	}
	return out, nil
}

// refsToGitHub maps the Forgejo issue numbers a Forgejo body refers to. Only
// mapped issues have a GitHub number; every other reference stays internal.
func (b *Bridge) refsToGitHub(ctx context.Context, r *repo, body string) (map[int64]int64, error) {
	out := map[int64]int64{}
	for _, n := range r.tr.ForgejoRefs(body) {
		m, ok, err := b.store.IssueByForgejo(ctx, r.cfg.Key(), n)
		if err != nil {
			return nil, err
		}
		if ok {
			out[n] = m.GitHubNumber
		}
	}
	return out, nil
}

func isConflict(err error) bool { return errors.Is(err, store.ErrConflict) }

// repoRef returns the repository of one side.
func repoRef(r *repo, side store.Side) config.RepoRef {
	if side == store.GitHub {
		return r.cfg.GitHub
	}
	return r.cfg.Forgejo
}

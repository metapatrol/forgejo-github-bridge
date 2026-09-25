package bridge

import (
	"context"
	"fmt"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/telemetry"
)

// poll reads what changed on one side of one repository since the last poll
// and marks it dirty. It changes nothing on either forge.
func (b *Bridge) poll(ctx context.Context, r *repo, side store.Side) error {
	key := r.cfg.Key()
	client, self, ref := b.github, b.githubSelf, r.cfg.GitHub
	if side == store.Forgejo {
		client, self, ref = b.forgej, b.forgejoSelf, r.cfg.Forgejo
	}

	issueCursor, seen, err := b.store.Cursor(ctx, key, side, store.Issues)
	if err != nil {
		return err
	}
	if !seen {
		// The first poll of this side. If the database was lost, the
		// projections the bridge made are still out there, and importing
		// their sources again would duplicate every one of them.
		if err := b.recover(ctx, r, side); err != nil {
			return fmt.Errorf("recovering mappings: %w", err)
		}
	}
	commentCursor, _, err := b.store.Cursor(ctx, key, side, store.Comments)
	if err != nil {
		return err
	}

	since := func(cursor time.Time) time.Time {
		if cursor.IsZero() {
			if side == store.GitHub {
				return r.cfg.ImportSince
			}
			return time.Time{}
		}
		return cursor.Add(-pollOverlap)
	}

	issues, err := client.ListIssuesSince(ctx, ref, since(issueCursor))
	if err != nil {
		return fmt.Errorf("listing issues: %w", err)
	}
	comments, err := client.ListCommentsSince(ctx, ref, since(commentCursor))
	if err != nil {
		return fmt.Errorf("listing comments: %w", err)
	}

	issueSeen := b.seen.window(key, side, store.Issues)
	commentSeen := b.seen.window(key, side, store.Comments)

	numbers := map[int64]bool{}
	var seenIssues, seenComments []seenItem
	issueMax := issueCursor
	for _, is := range issues {
		issueMax = later(issueMax, is.UpdatedAt)
		if issueSeen.fresh(is.Number, is.UpdatedAt) {
			numbers[is.Number] = true
			seenIssues = append(seenIssues, seenItem{is.Number, is.UpdatedAt})
		}
	}
	commentMax := commentCursor
	for _, c := range comments {
		commentMax = later(commentMax, c.UpdatedAt)
		// The bridge's own comments are projections. Their issue changes
		// only because the bridge changed it, and reconciling it again for
		// that would find nothing to do.
		if c.Author == self || c.IssueNumber == 0 {
			continue
		}
		if commentSeen.fresh(c.ID, c.UpdatedAt) {
			numbers[c.IssueNumber] = true
			seenComments = append(seenComments, seenItem{c.ID, c.UpdatedAt})
		}
	}

	keys := make([]store.DirtyKey, 0, len(numbers))
	for n := range numbers {
		k, err := b.dirtyKey(ctx, key, side, n)
		if err != nil {
			return err
		}
		keys = append(keys, k)
	}
	if err := b.store.MarkDirty(ctx, keys...); err != nil {
		return err
	}
	telemetry.RecordMarked(key, "poll", len(keys))
	// Remembered only once queued: an item remembered and then not queued
	// because the database failed would be skipped by the next poll too.
	issueSeen.record(seenIssues)
	commentSeen.record(seenComments)

	// The cursors move only after everything they covered is queued. A crash
	// in between reads the same window again, which is harmless.
	if issueMax.After(issueCursor) {
		if err := b.store.SetCursor(ctx, key, side, store.Issues, issueMax); err != nil {
			return err
		}
	} else if !seen {
		// An empty repository still counts as polled, or it would be
		// recovered on every poll. The cursor stays at the import bound.
		if err := b.store.SetCursor(ctx, key, side, store.Issues, since(time.Time{})); err != nil {
			return err
		}
	}
	if commentMax.After(commentCursor) {
		if err := b.store.SetCursor(ctx, key, side, store.Comments, commentMax); err != nil {
			return err
		}
	}
	issueSeen.prune(issueMax.Add(-pollOverlap))
	commentSeen.prune(commentMax.Add(-pollOverlap))
	if len(keys) > 0 {
		b.log.Debug("poll marked resources", "repository", key, "side", side, "count", len(keys))
	}
	return nil
}

// dirtyKey keys a changed issue. A mapped Forgejo issue is keyed by its
// GitHub number, so that a change on each side of one pair is one entry.
func (b *Bridge) dirtyKey(ctx context.Context, key string, side store.Side, n int64) (store.DirtyKey, error) {
	if side == store.Forgejo {
		m, ok, err := b.store.IssueByForgejo(ctx, key, n)
		if err != nil {
			return store.DirtyKey{}, err
		}
		if ok {
			return store.DirtyKey{Repository: key, Side: store.GitHub, Number: m.GitHubNumber}, nil
		}
	}
	return store.DirtyKey{Repository: key, Side: side, Number: n}, nil
}

// recover rebuilds issue mappings from the projections the bridge wrote on
// one side. It reads only issues opened by the bridge's own account, so a
// marker someone typed into their own issue is never taken for provenance.
func (b *Bridge) recover(ctx context.Context, r *repo, side store.Side) error {
	key := r.cfg.Key()
	client, self, ref, want := b.github, b.githubSelf, r.cfg.GitHub, marker.OriginForgejo
	if side == store.Forgejo {
		client, self, ref, want = b.forgej, b.forgejoSelf, r.cfg.Forgejo, marker.OriginGitHub
	}
	own, err := client.ListIssuesByAuthor(ctx, ref, self, time.Time{})
	if err != nil {
		return err
	}
	recovered := 0
	for _, is := range own {
		if is.Author != self || is.IsPullRequest {
			continue
		}
		mk, ok := marker.Parse(is.Body)
		if !ok || mk.Origin != want || mk.Kind != marker.KindIssue {
			continue
		}
		if _, err := b.store.InsertIssue(ctx, recoveredMapping(key, side, is, mk)); err != nil {
			if isConflict(err) {
				continue
			}
			return err
		}
		recovered++
	}
	if recovered > 0 {
		b.log.Warn("rebuilt issue mappings from the bridge's own projections",
			"repository", key, "side", side, "count", recovered)
	}
	return nil
}

func recoveredMapping(key string, side store.Side, projection forge.Issue, mk marker.Marker) store.IssueMapping {
	m := store.IssueMapping{Repository: key, SyncedClosed: projection.Closed}
	if side == store.GitHub {
		m.GitHubNumber, m.ForgejoNumber, m.Origin = projection.Number, mk.ID, store.Forgejo
	} else {
		m.GitHubNumber, m.ForgejoNumber, m.Origin = mk.ID, projection.Number, store.GitHub
	}
	return m
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

// seenSet remembers what the polls already queued within their overlap
// window.
//
// The overlap makes every poll read the last two minutes again, and a cursor
// only moves when something newer turns up. Without this, the most recently
// changed issue of a quiet repository would be reconciled on every poll for
// as long as nothing else happened. It is memory only: after a restart the
// window is read once more, which is exactly the overlap's purpose.
type seenSet struct {
	windows map[string]*window
}

type window struct {
	updated map[int64]time.Time
}

func (s *seenSet) window(repo string, side store.Side, stream store.Stream) *window {
	if s.windows == nil {
		s.windows = map[string]*window{}
	}
	key := repo + "\x00" + string(side) + "\x00" + string(stream)
	w, ok := s.windows[key]
	if !ok {
		w = &window{updated: map[int64]time.Time{}}
		s.windows[key] = w
	}
	return w
}

type seenItem struct {
	id      int64
	updated time.Time
}

// fresh reports whether an item has changed since it was last queued.
func (w *window) fresh(id int64, updated time.Time) bool {
	prev, ok := w.updated[id]
	return !ok || updated.After(prev)
}

// record remembers items as queued.
func (w *window) record(items []seenItem) {
	for _, it := range items {
		w.updated[it.id] = it.updated
	}
}

// prune forgets items that the overlap no longer reaches.
func (w *window) prune(before time.Time) {
	for id, t := range w.updated {
		if t.Before(before) {
			delete(w.updated, id)
		}
	}
}

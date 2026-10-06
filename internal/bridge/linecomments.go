package bridge

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/telemetry"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/translate"
)

// syncLineComments carries the line comments of Forgejo reviews to the GitHub
// pull request (docs/design.md §11): each becomes a GitHub review comment on
// the same line of the same commit. The bridge pushes the GitHub head
// unchanged into the contrib branch, so a commit a maintainer commented on
// is one GitHub has — unless someone else pushed to the branch, or the line
// is one GitHub's diff of that commit does not show. GitHub then refuses the
// comment, and it becomes a conversation comment naming the file and the
// line instead.
//
// Only that direction: a contributor's line comment on GitHub stays there.
// The rules are the conversation comments': every line comment that is not
// the bridge's own or marked internal is a source with one projection, which
// follows its edits and goes with it.
func (b *Bridge) syncLineComments(ctx context.Context, r *repo, m store.IssueMapping, reviews []forge.Review,
	ghComments []forge.Comment, participants map[string]bool) error {
	var sources []forge.LineComment
	byID := map[int64]forge.LineComment{}
	for _, rv := range reviews {
		lcs, err := b.pulls.Forgejo.ListReviewLineComments(ctx, r.cfg.Forgejo, m.ForgejoNumber, rv.ID)
		if err != nil {
			return fmt.Errorf("reading the line comments of Forgejo review %d: %w", rv.ID, err)
		}
		for _, lc := range lcs {
			sources = append(sources, lc)
			byID[lc.ID] = lc
		}
	}
	ghLines, err := b.pulls.GitHub.ListReviewComments(ctx, r.cfg.GitHub, m.GitHubNumber)
	if err != nil {
		return err
	}
	onLine := map[int64]forge.LineComment{}
	for _, lc := range ghLines {
		onLine[lc.ID] = lc
	}
	inConversation := map[int64]forge.Comment{}
	for _, c := range ghComments {
		inConversation[c.ID] = c
	}

	all, err := b.store.Comments(ctx, m.ID)
	if err != nil {
		return err
	}
	var mappings []store.CommentMapping
	mapped := map[int64]bool{}
	taken := map[store.CommentKind]map[int64]bool{store.CommentKindLine: {}, store.CommentKindLineAsComment: {}}
	for _, cm := range all {
		if k := cm.KindOf(); k == store.CommentKindLine || k == store.CommentKindLineAsComment {
			mappings = append(mappings, cm)
			mapped[cm.ForgejoCommentID] = true
			taken[k][cm.GitHubCommentID] = true
		}
	}

	// Projections written but not recorded, as for conversation comments
	// (syncComments): adopted before anything is written again.
	adopt := func(kind store.CommentKind, id int64, author, body string) error {
		if author != b.githubSelf || taken[kind][id] {
			return nil
		}
		mk, ok := marker.Parse(body)
		if !ok || mk.Origin != marker.OriginForgejo || mk.Kind != marker.KindLine {
			return nil
		}
		if _, exists := byID[mk.ID]; !exists || mapped[mk.ID] {
			return nil
		}
		cm, err := b.store.InsertComment(ctx, store.CommentMapping{IssueMappingID: m.ID, GitHubCommentID: id,
			ForgejoCommentID: mk.ID, Origin: store.Forgejo, Kind: kind})
		if err != nil {
			return err
		}
		mappings = append(mappings, cm)
		mapped[mk.ID], taken[kind][id] = true, true
		return nil
	}
	for _, lc := range ghLines {
		if err := adopt(store.CommentKindLine, lc.ID, lc.Author, lc.Body); err != nil {
			return err
		}
	}
	for _, c := range ghComments {
		if err := adopt(store.CommentKindLineAsComment, c.ID, c.Author, c.Body); err != nil {
			return err
		}
	}

	for _, cm := range mappings {
		source, exists := byID[cm.ForgejoCommentID]
		var projBody string
		var projExists bool
		if cm.KindOf() == store.CommentKindLine {
			p, ok := onLine[cm.GitHubCommentID]
			projBody, projExists = p.Body, ok
		} else {
			p, ok := inConversation[cm.GitHubCommentID]
			projBody, projExists = p.Body, ok
		}

		if !exists || !b.isLineSource(r, source) {
			if projExists {
				if err := b.deleteLineProjection(ctx, r, cm); err != nil && !isNotFound(err) {
					return err
				}
				telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "line comment", "delete")
			}
			if err := b.store.DeleteComment(ctx, cm.ID); err != nil {
				return err
			}
			continue
		}

		if !projExists {
			// Somebody deleted it; write it again, on its line if GitHub
			// takes it there now.
			id, kind, err := b.projectLineComment(ctx, r, m, source, participants)
			if err != nil {
				return err
			}
			cm.GitHubCommentID, cm.Kind = id, kind
			if err := b.store.UpdateComment(ctx, cm); err != nil {
				return err
			}
			continue
		}
		body, err := b.renderLineComment(ctx, r, source, cm.KindOf(), participants)
		if err != nil {
			return err
		}
		if translate.Equal(projBody, body) {
			continue
		}
		if cm.KindOf() == store.CommentKindLine {
			err = b.pulls.GitHub.EditReviewComment(ctx, r.cfg.GitHub, cm.GitHubCommentID, body)
		} else {
			_, err = b.github.EditComment(ctx, r.cfg.GitHub, cm.GitHubCommentID, body)
		}
		if err != nil {
			return err
		}
		telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "line comment", "edit")
	}

	// New line comments, in the order the reviews and their comments came.
	for _, lc := range sources {
		if mapped[lc.ID] || !b.isLineSource(r, lc) {
			continue
		}
		id, kind, err := b.projectLineComment(ctx, r, m, lc, participants)
		if err != nil {
			return err
		}
		if _, err := b.store.InsertComment(ctx, store.CommentMapping{IssueMappingID: m.ID, GitHubCommentID: id,
			ForgejoCommentID: lc.ID, Origin: store.Forgejo, Kind: kind}); err != nil {
			return err
		}
		mapped[lc.ID] = true
	}
	return nil
}

// projectLineComment writes a line comment on GitHub: on its line, or, when
// GitHub will not place it there, in the conversation.
func (b *Bridge) projectLineComment(ctx context.Context, r *repo, m store.IssueMapping, lc forge.LineComment,
	participants map[string]bool) (int64, store.CommentKind, error) {
	body, err := b.renderLineComment(ctx, r, lc, store.CommentKindLine, participants)
	if err != nil {
		return 0, "", err
	}
	created, err := b.pulls.GitHub.CreateReviewComment(ctx, r.cfg.GitHub, m.GitHubNumber, forge.LineComment{
		Body: body, CommitID: lc.CommitID, Path: lc.Path, Line: lc.Line, Side: lc.Side,
	})
	if err == nil {
		telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "line comment", "create")
		return created.ID, store.CommentKindLine, nil
	}
	if !errors.Is(err, forge.ErrRejected) {
		return 0, "", fmt.Errorf("projecting Forgejo line comment %d: %w", lc.ID, err)
	}
	b.log.Info("GitHub would not place a line comment on its line; posting it in the conversation",
		"repository", r.cfg.Key(), "forgejo", m.ForgejoNumber, "path", lc.Path, "line", lc.Line, "commit", lc.CommitID)
	body, err = b.renderLineComment(ctx, r, lc, store.CommentKindLineAsComment, participants)
	if err != nil {
		return 0, "", err
	}
	c, err := b.github.CreateComment(ctx, r.cfg.GitHub, m.GitHubNumber, body)
	if err != nil {
		return 0, "", fmt.Errorf("projecting Forgejo line comment %d into the conversation: %w", lc.ID, err)
	}
	telemetry.RecordChange(r.cfg.Key(), string(store.GitHub), "line comment", "create")
	return c.ID, store.CommentKindLineAsComment, nil
}

func (b *Bridge) renderLineComment(ctx context.Context, r *repo, lc forge.LineComment, kind store.CommentKind,
	participants map[string]bool) (string, error) {
	refs, err := b.refsToGitHub(ctx, r, lc.Body)
	if err != nil {
		return "", err
	}
	if kind == store.CommentKindLineAsComment {
		return r.tr.LineCommentAsCommentToGitHub(lc, refs, participants), nil
	}
	return r.tr.LineCommentToGitHub(lc, refs, participants), nil
}

func (b *Bridge) deleteLineProjection(ctx context.Context, r *repo, cm store.CommentMapping) error {
	if cm.KindOf() == store.CommentKindLine {
		return b.pulls.GitHub.DeleteReviewComment(ctx, r.cfg.GitHub, cm.GitHubCommentID)
	}
	return b.github.DeleteComment(ctx, r.cfg.GitHub, cm.GitHubCommentID)
}

// isLineSource reports whether a Forgejo line comment is to be projected: not
// the bridge's own, and not marked internal.
func (b *Bridge) isLineSource(r *repo, lc forge.LineComment) bool {
	if b.own(store.Forgejo, lc.Author) {
		return false
	}
	return r.cfg.InternalPrefix == "" || !strings.HasPrefix(strings.TrimSpace(lc.Body), r.cfg.InternalPrefix)
}

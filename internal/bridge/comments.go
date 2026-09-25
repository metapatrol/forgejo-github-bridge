package bridge

import (
	"context"
	"fmt"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/store"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/telemetry"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/translate"
)

// side bundles what the comment sync needs about one side of a pair.
type side struct {
	name     store.Side
	client   forge.Client
	self     string
	number   int64
	comments map[int64]forge.Comment
	order    []forge.Comment
}

// syncComments makes the comments of a pair agree.
//
// Every comment that is not the bridge's own is a source, unless it is a
// Forgejo comment marked internal. Every source has exactly one projection
// on the other side. A projection whose source was deleted, or turned
// internal, is deleted with it; one somebody deleted is written again.
//
// Reviews of a pull request in Forgejo are sources too, projected to GitHub
// as comments. They take part under their negated ID: Forgejo numbers
// reviews and comments separately, and the two must not collide in the
// mapping.
func (b *Bridge) syncComments(ctx context.Context, r *repo, m store.IssueMapping, ghComments, fjComments []forge.Comment, reviews []forge.Review, participants map[string]bool) error {
	b.reviews = map[int64]forge.Review{}
	for _, rv := range reviews {
		b.reviews[-rv.ID] = rv
		fjComments = append(fjComments, forge.Comment{
			ID: -rv.ID, IssueNumber: m.ForgejoNumber, Body: rv.Body, Author: rv.Author, URL: rv.URL,
			CreatedAt: rv.SubmittedAt, UpdatedAt: rv.SubmittedAt,
		})
	}
	gh := newSide(store.GitHub, b.github, b.githubSelf, m.GitHubNumber, ghComments)
	fj := newSide(store.Forgejo, b.forgej, b.forgejoSelf, m.ForgejoNumber, fjComments)

	mappings, err := b.store.Comments(ctx, m.ID)
	if err != nil {
		return err
	}
	mapped := map[store.Side]map[int64]bool{store.GitHub: {}, store.Forgejo: {}}
	for _, cm := range mappings {
		mapped[store.GitHub][cm.GitHubCommentID] = true
		mapped[store.Forgejo][cm.ForgejoCommentID] = true
	}

	// Projections the bridge wrote but did not record: the process died
	// between writing one and recording it. Adopting them first is what keeps
	// the loop below from writing a second.
	for _, pair := range [][2]*side{{gh, fj}, {fj, gh}} {
		proj, src := pair[0], pair[1]
		for _, c := range proj.order {
			if c.Author != proj.self || mapped[proj.name][c.ID] {
				continue
			}
			mk, ok := marker.Parse(c.Body)
			if !ok || store.Side(mk.Origin) != src.name {
				continue
			}
			srcID := mk.ID
			switch mk.Kind {
			case marker.KindComment:
			case marker.KindReview:
				srcID = -mk.ID
			default:
				continue
			}
			if _, exists := src.comments[srcID]; !exists || mapped[src.name][srcID] {
				continue
			}
			cm := store.CommentMapping{IssueMappingID: m.ID, Origin: src.name}
			cm.GitHubCommentID, cm.ForgejoCommentID = ids(src.name, srcID, c.ID)
			cm, err := b.store.InsertComment(ctx, cm)
			if err != nil {
				return err
			}
			mappings = append(mappings, cm)
			mapped[proj.name][c.ID], mapped[src.name][srcID] = true, true
		}
	}

	for _, cm := range mappings {
		src, proj := gh, fj
		srcID, projID := cm.GitHubCommentID, cm.ForgejoCommentID
		if cm.Origin == store.Forgejo {
			src, proj = fj, gh
			srcID, projID = cm.ForgejoCommentID, cm.GitHubCommentID
		}
		source, sourceExists := src.comments[srcID]
		projection, projExists := proj.comments[projID]

		if !sourceExists || !b.isSource(r, src, source) {
			if projExists {
				if err := proj.client.DeleteComment(ctx, repoRef(r, proj.name), projID); err != nil && !isNotFound(err) {
					return err
				}
				telemetry.RecordChange(r.cfg.Key(), string(proj.name), "comment", "delete")
			}
			if err := b.store.DeleteComment(ctx, cm.ID); err != nil {
				return err
			}
			continue
		}

		body, err := b.renderComment(ctx, r, src.name, source, participants)
		if err != nil {
			return err
		}
		if !projExists {
			created, err := proj.client.CreateComment(ctx, repoRef(r, proj.name), proj.number, body)
			if err != nil {
				return err
			}
			telemetry.RecordChange(r.cfg.Key(), string(proj.name), "comment", "create")
			cm.GitHubCommentID, cm.ForgejoCommentID = ids(src.name, srcID, created.ID)
			if err := b.store.UpdateComment(ctx, cm); err != nil {
				return err
			}
			continue
		}
		if !translate.Equal(projection.Body, body) {
			if _, err := proj.client.EditComment(ctx, repoRef(r, proj.name), projID, body); err != nil {
				return err
			}
			telemetry.RecordChange(r.cfg.Key(), string(proj.name), "comment", "edit")
		}
	}

	// New sources, oldest first on each side, so a conversation reads in the
	// order it was written on the side it was written on.
	for _, pair := range [][2]*side{{gh, fj}, {fj, gh}} {
		src, proj := pair[0], pair[1]
		for _, c := range src.order {
			if mapped[src.name][c.ID] || !b.isSource(r, src, c) {
				continue
			}
			body, err := b.renderComment(ctx, r, src.name, c, participants)
			if err != nil {
				return err
			}
			created, err := proj.client.CreateComment(ctx, repoRef(r, proj.name), proj.number, body)
			if err != nil {
				return fmt.Errorf("projecting %s comment %d: %w", src.name, c.ID, err)
			}
			telemetry.RecordChange(r.cfg.Key(), string(proj.name), "comment", "create")
			cm := store.CommentMapping{IssueMappingID: m.ID, Origin: src.name}
			cm.GitHubCommentID, cm.ForgejoCommentID = ids(src.name, c.ID, created.ID)
			if _, err := b.store.InsertComment(ctx, cm); err != nil {
				return err
			}
			mapped[src.name][c.ID] = true
		}
	}
	return nil
}

// isSource reports whether a comment is to be projected: not the bridge's
// own, and on the Forgejo side not marked internal. A review that neither
// decides anything nor says anything has nothing to project.
func (b *Bridge) isSource(r *repo, s *side, c forge.Comment) bool {
	if b.own(s.name, c.Author) {
		return false
	}
	if rv, ok := b.reviews[c.ID]; ok && c.ID < 0 && rv.State == "COMMENT" && strings.TrimSpace(rv.Body) == "" {
		return false
	}
	if s.name == store.Forgejo && r.cfg.InternalPrefix != "" {
		if strings.HasPrefix(strings.TrimSpace(c.Body), r.cfg.InternalPrefix) {
			return false
		}
	}
	return true
}

func (b *Bridge) renderComment(ctx context.Context, r *repo, origin store.Side, c forge.Comment, participants map[string]bool) (string, error) {
	if origin == store.GitHub {
		refs, err := b.refsToForgejo(ctx, r, c.Body)
		if err != nil {
			return "", err
		}
		return r.tr.CommentToForgejo(c, refs), nil
	}
	refs, err := b.refsToGitHub(ctx, r, c.Body)
	if err != nil {
		return "", err
	}
	if rv, ok := b.reviews[c.ID]; ok && c.ID < 0 {
		return r.tr.ReviewToGitHub(rv, refs, participants), nil
	}
	return r.tr.CommentToGitHub(c, refs, participants), nil
}

func newSide(name store.Side, client forge.Client, self string, number int64, comments []forge.Comment) *side {
	s := &side{name: name, client: client, self: self, number: number, comments: map[int64]forge.Comment{}, order: comments}
	for _, c := range comments {
		s.comments[c.ID] = c
	}
	return s
}

// ids orders a source and a projection ID as GitHub, Forgejo.
func ids(origin store.Side, sourceID, projectionID int64) (github, forgejo int64) {
	if origin == store.GitHub {
		return sourceID, projectionID
	}
	return projectionID, sourceID
}

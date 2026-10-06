package translate

import (
	"fmt"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
)

// PullRequestToForgejo renders a GitHub pull request as the description of
// its Forgejo pull request.
func (t *Translator) PullRequestToForgejo(pr forge.PullRequest, refs map[int64]int64) (title, body string) {
	head := fmt.Sprintf("> Opened by %s on GitHub: %s", t.githubUser(pr.Author), pr.URL)
	text := t.textToForgejo(pr.Body, refs)
	return pr.Title, marker.Append(head+"\n\n"+text, marker.Marker{Origin: marker.OriginGitHub, Kind: marker.KindPull, ID: pr.Number})
}

// LineCommentToGitHub renders a Forgejo line comment as a GitHub review
// comment on the same line.
func (t *Translator) LineCommentToGitHub(c forge.LineComment, refs map[int64]int64, participants map[string]bool) string {
	head := fmt.Sprintf("> %s wrote:", upperFirst(t.maintainer(c.Author)))
	text := t.textToGitHub(c.Body, refs, participants, 0)
	return marker.Append(head+"\n\n"+text, marker.Marker{Origin: marker.OriginForgejo, Kind: marker.KindLine, ID: c.ID})
}

// LineCommentAsCommentToGitHub renders a Forgejo line comment as a GitHub
// conversation comment, for a line GitHub would not place it on: a commit
// GitHub does not have, or a line its diff does not show. It names the file
// and the line but quotes no code: a commit GitHub does not have may hold
// code that is not public.
func (t *Translator) LineCommentAsCommentToGitHub(c forge.LineComment, refs map[int64]int64, participants map[string]bool) string {
	where := fmt.Sprintf("line %d of `%s`", c.Line, c.Path)
	if c.Side == forge.SideLeft {
		where = fmt.Sprintf("line %d of `%s` as it was before the change", c.Line, c.Path)
	}
	head := fmt.Sprintf("> %s wrote, on %s:", upperFirst(t.maintainer(c.Author)), where)
	text := t.textToGitHub(c.Body, refs, participants, 0)
	return marker.Append(head+"\n\n"+text, marker.Marker{Origin: marker.OriginForgejo, Kind: marker.KindLine, ID: c.ID})
}

// ReviewToGitHub renders a Forgejo review as a GitHub comment: its verdict
// and summary. Its line comments are projected on their own
// (LineCommentToGitHub).
func (t *Translator) ReviewToGitHub(r forge.Review, refs map[int64]int64, participants map[string]bool) string {
	var verdict string
	switch r.State {
	case "APPROVED":
		verdict = "approved these changes"
	case "REQUEST_CHANGES":
		verdict = "requested changes"
	default:
		verdict = "reviewed"
	}
	head := fmt.Sprintf("> %s %s.", upperFirst(t.maintainer(r.Author)), verdict)
	text := ""
	if strings.TrimSpace(r.Body) != "" {
		text = "\n\n" + t.textToGitHub(r.Body, refs, participants, 0)
	}
	return marker.Append(head+text, marker.Marker{Origin: marker.OriginForgejo, Kind: marker.KindReview, ID: r.ID})
}

// Notice renders something the bridge says itself, about GitHub issue or
// pull request number.
func Notice(origin marker.Origin, number int64, text string) string {
	return marker.Append(text, marker.Marker{Origin: origin, Kind: marker.KindNotice, ID: number})
}

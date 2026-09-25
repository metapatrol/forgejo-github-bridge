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

// ReviewToGitHub renders a Forgejo review as a GitHub comment. Line comments
// are not carried (phase 3); the review's verdict and summary are.
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

// The bridge's own messages. Kept together so the voice stays one voice.

// UnsupportedBase is said on a GitHub pull request whose base branch does not
// take contributions.
func UnsupportedBase(base string, targets []string) string {
	quoted := make([]string, len(targets))
	for i, b := range targets {
		quoted[i] = "`" + b + "`"
	}
	return fmt.Sprintf("Thanks for the contribution! Pull requests for this project are taken against %s, "+
		"and this one targets `%s`. Please change the base branch; it is picked up automatically once you do.",
		strings.Join(quoted, " or "), base)
}

// RebaseRequest asks for a rebase onto base.
func RebaseRequest(base string, behind int) string {
	return fmt.Sprintf("`%s` has moved on by %d commit(s) since this branch was last updated. "+
		"Please rebase onto `%s` so that it can be merged as it is.", base, behind, base)
}

// UpToDate replaces a rebase request once it is answered.
func UpToDate(base string) string {
	return fmt.Sprintf("Thanks, this is up to date with `%s`.", base)
}

// Merged is said when GitHub recognised the merge by itself.
func Merged(base, sha string) string {
	return fmt.Sprintf("Merged into `%s` as %s. Thank you!", base, sha)
}

// Integrated is said when the commits were rebased on merge, so GitHub could
// not recognise them, and the pull request is closed by the bridge.
func Integrated(base, sha string) string {
	return fmt.Sprintf("Integrated into `%s` as %s. The commits were rebased onto `%s` when merging, "+
		"so GitHub does not show this pull request as merged; your authorship is kept on every commit. Thank you!",
		base, sha, base)
}

// ClosedUpstream is said when maintainers closed the pull request without
// merging it.
func ClosedUpstream() string {
	return "The maintainers closed this pull request without merging it."
}

// HeadFrozen is said in Forgejo when the contrib branch was moved by somebody
// other than the bridge.
func HeadFrozen(branch string) string {
	return fmt.Sprintf("The branch `%s` was changed by someone other than the bridge. "+
		"New commits from GitHub are no longer imported into this pull request, so that nothing pushed here is overwritten.", branch)
}

// TouchesCI is said in Forgejo when a pull request changes CI configuration.
func TouchesCI(paths []string) string {
	return "**This pull request changes CI configuration:**\n\n- `" + strings.Join(paths, "`\n- `") + "`\n\n" +
		"Workflows run from the pull request's own files. Read these changes before approving a workflow run, " +
		"and never approve it with \"Approve always\": every imported pull request comes from the same account."
}

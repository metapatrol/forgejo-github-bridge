// Package translate rewrites text crossing the bridge so that it means the
// same thing on the other side and leaks nothing on the way out.
//
// The two forges share a syntax and not a namespace. #42 on GitHub and #42 in
// Forgejo are different issues; @alice on GitHub and @alice in Forgejo are
// different people; a link to the private Forgejo is a dead link on GitHub
// that also tells the world what the private host is called. Everything here
// is CONCEPT.md §13.
package translate

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/marker"
)

// Placeholders for what cannot cross. Set as code spans, so that a reader
// sees at a glance that something was taken out rather than written so.
const (
	internalReference = "`internal reference`"
	internalLink      = "`internal link`"
	missingAttachment = "`attachment not available publicly`"
	noDescription     = "_No description provided._"
)

var (
	// issueRef is #N or owner/repo#N, where a reference may start: at the
	// start of a line, after whitespace or an opening bracket. Both forges
	// link only there, so text such as a URL fragment (page#3) is not one.
	issueRef = regexp.MustCompile(`(^|[\s(\[])(?:([A-Za-z0-9][A-Za-z0-9._-]*/[A-Za-z0-9._-]+))?#([0-9]+)\b`)
	// mention is @login, not preceded by anything that makes it part of a
	// word, an e-mail address or a path. A trailing slash marks a team
	// (@org/team), which is left alone.
	mention = regexp.MustCompile(`(^|[^A-Za-z0-9_@/.` + "`" + `])@([A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?)(/?)`)
)

// Translator translates for one bridged repository.
type Translator struct {
	repo       config.Repository
	githubBase string
	// users maps lower-cased Forgejo logins to GitHub logins, and back.
	toGitHub  map[string]string
	toForgejo map[string]string

	forgejoLink  *regexp.Regexp
	forgejoURL   *regexp.Regexp
	forgejoIssue *regexp.Regexp
}

// New creates a translator. forgejoBases are the addresses Forgejo is known
// by — the one the bridge talks to and the one people's browsers use — and a
// link starting with any of them is a link into the private instance.
func New(repo config.Repository, forgejoBases []*url.URL, users map[string]string) *Translator {
	var alternatives []string
	for _, b := range forgejoBases {
		alternatives = append(alternatives, regexp.QuoteMeta(strings.TrimRight(b.String(), "/")))
	}
	quoted := "(?:" + strings.Join(alternatives, "|") + ")"
	// Relative attachment links are what Forgejo writes into a body for an
	// upload, and they are as private as absolute ones.
	target := `(?:` + quoted + `[^)\s]*|/attachments/[^)\s]*|/[^/\s)]+/[^/\s)]+/attachments/[^)\s]*)`

	t := &Translator{
		repo:         repo,
		githubBase:   "https://github.com/" + repo.GitHub.String(),
		toGitHub:     map[string]string{},
		toForgejo:    map[string]string{},
		forgejoLink:  regexp.MustCompile(`(!?)\[([^\]]*)\]\((` + target + `)(?:\s+"[^"]*")?\)`),
		forgejoURL:   regexp.MustCompile(quoted + `(?:/[^\s)>\]]*)?`),
		forgejoIssue: regexp.MustCompile(`^` + quoted + `/` + regexp.QuoteMeta(repo.Forgejo.Owner) + `/` + regexp.QuoteMeta(repo.Forgejo.Name) + `/(?:issues|pulls)/([0-9]+)(#[^\s]*)?$`),
	}
	for fj, gh := range users {
		t.toGitHub[strings.ToLower(fj)] = gh
		t.toForgejo[strings.ToLower(gh)] = fj
	}
	return t
}

// ForgejoRefs returns the Forgejo issue numbers a Forgejo body refers to, by
// #N or by URL. The reconciler looks them up and passes the mapping back.
func (t *Translator) ForgejoRefs(body string) []int64 {
	set := map[int64]bool{}
	mapProse(body, func(text string) string {
		for _, m := range issueRef.FindAllStringSubmatch(text, -1) {
			if m[2] == "" || strings.EqualFold(m[2], t.repo.Forgejo.String()) {
				addNumber(set, m[3])
			}
		}
		for _, u := range t.forgejoURL.FindAllString(text, -1) {
			if m := t.forgejoIssue.FindStringSubmatch(u); m != nil {
				addNumber(set, m[1])
			}
		}
		return text
	})
	return sorted(set)
}

// GitHubRefs returns the GitHub issue numbers a GitHub body refers to by #N.
func (t *Translator) GitHubRefs(body string) []int64 {
	set := map[int64]bool{}
	mapProse(body, func(text string) string {
		for _, m := range issueRef.FindAllStringSubmatch(text, -1) {
			if m[2] == "" || strings.EqualFold(m[2], t.repo.GitHub.String()) {
				addNumber(set, m[3])
			}
		}
		return text
	})
	return sorted(set)
}

// IssueToForgejo renders a GitHub issue as its Forgejo projection. refs maps
// GitHub issue numbers to Forgejo ones.
func (t *Translator) IssueToForgejo(is forge.Issue, refs map[int64]int64) (title, body string) {
	head := fmt.Sprintf("> Reported by %s via GitHub: %s", t.githubUser(is.Author), is.URL)
	text := t.textToForgejo(is.Body, refs)
	return is.Title, marker.Append(head+"\n\n"+text, marker.Marker{Origin: marker.OriginGitHub, Kind: marker.KindIssue, ID: is.Number})
}

// CommentToForgejo renders a GitHub comment as its Forgejo projection.
func (t *Translator) CommentToForgejo(c forge.Comment, refs map[int64]int64) string {
	head := fmt.Sprintf("> %s commented on GitHub: %s", t.githubUser(c.Author), c.URL)
	text := t.textToForgejo(c.Body, refs)
	return marker.Append(head+"\n\n"+text, marker.Marker{Origin: marker.OriginGitHub, Kind: marker.KindComment, ID: c.ID})
}

// IssueToGitHub renders a Forgejo issue as its GitHub projection. refs maps
// Forgejo issue numbers to GitHub ones; participants are the lower-cased
// GitHub logins taking part in the issue, whom a mention may still reach.
func (t *Translator) IssueToGitHub(is forge.Issue, refs map[int64]int64, participants map[string]bool) (title, body string) {
	text := t.textToGitHub(is.Body, refs, participants, 0)
	foot := fmt.Sprintf("> Opened by %s.", t.maintainer(is.Author))
	return is.Title, marker.Append(text+"\n\n"+foot, marker.Marker{Origin: marker.OriginForgejo, Kind: marker.KindIssue, ID: is.Number})
}

// CommentToGitHub renders a Forgejo comment as its GitHub projection.
func (t *Translator) CommentToGitHub(c forge.Comment, refs map[int64]int64, participants map[string]bool) string {
	head := fmt.Sprintf("> %s wrote:", upperFirst(t.maintainer(c.Author)))
	text := t.textToGitHub(c.Body, refs, participants, c.Attachments)
	return marker.Append(head+"\n\n"+text, marker.Marker{Origin: marker.OriginForgejo, Kind: marker.KindComment, ID: c.ID})
}

func (t *Translator) textToForgejo(body string, refs map[int64]int64) string {
	body = strings.TrimSpace(marker.Strip(normalize(body)))
	if body == "" {
		return noDescription
	}
	return mapProse(body, func(text string) string {
		text = replace(issueRef, text, func(m []string) string {
			lead, repo, num := m[1], m[2], m[3]
			n, _ := strconv.ParseInt(num, 10, 64)
			if repo == "" || strings.EqualFold(repo, t.repo.GitHub.String()) {
				if fj, ok := refs[n]; ok {
					return lead + "#" + strconv.FormatInt(fj, 10)
				}
				// Written so that Forgejo does not read it as a reference of
				// its own: github#42 is not a pattern it links.
				return fmt.Sprintf("%s[github#%s](%s/issues/%s)", lead, num, t.githubBase, num)
			}
			// A reference to another GitHub repository. Forgejo would read
			// owner/repo#N as one of its own repositories, which it is not.
			return fmt.Sprintf("%s[github:%s#%s](https://github.com/%s/issues/%s)", lead, repo, num, repo, num)
		})
		return replace(mention, text, func(m []string) string {
			lead, login, slash := m[1], m[2], m[3]
			if slash != "" {
				return m[0]
			}
			if fj, ok := t.toForgejo[strings.ToLower(login)]; ok {
				return lead + "@" + fj
			}
			// A link, not a mention: a Forgejo account that happens to share
			// the name is somebody else.
			return lead + fmt.Sprintf("[%s](https://github.com/%s)", login, login)
		})
	})
}

func (t *Translator) textToGitHub(body string, refs map[int64]int64, participants map[string]bool, attachments int) string {
	body = strings.TrimSpace(marker.Strip(normalize(body)))
	if body == "" && attachments == 0 {
		return noDescription
	}
	out := mapProse(body, func(text string) string {
		text = replace(t.forgejoLink, text, func(m []string) string {
			image, label, target := m[1] == "!", m[2], m[3]
			if image || isAttachment(target) {
				return missingAttachment
			}
			if gh, ok := t.issueURLToGitHub(target, refs); ok {
				return fmt.Sprintf("[%s](%s)", label, gh)
			}
			return label
		})
		text = replace(t.forgejoURL, text, func(m []string) string {
			if gh, ok := t.issueURLToGitHub(m[0], refs); ok {
				return gh
			}
			return internalLink
		})
		text = replace(issueRef, text, func(m []string) string {
			lead, repo, num := m[1], m[2], m[3]
			n, _ := strconv.ParseInt(num, 10, 64)
			if repo == "" || strings.EqualFold(repo, t.repo.Forgejo.String()) {
				if gh, ok := refs[n]; ok {
					return lead + "#" + strconv.FormatInt(gh, 10)
				}
			}
			return lead + internalReference
		})
		return replace(mention, text, func(m []string) string {
			lead, login, slash := m[1], m[2], m[3]
			if slash != "" {
				return m[0]
			}
			if gh, ok := t.toGitHub[strings.ToLower(login)]; ok {
				return lead + "@" + gh
			}
			if participants[strings.ToLower(login)] {
				return lead + "@" + login
			}
			// Nobody on GitHub this could be meant for. Kept as a name so
			// the sentence still reads, without notifying a stranger.
			return lead + login
		})
	})
	if attachments > 0 {
		out = strings.TrimSpace(out + "\n\n" + fmt.Sprintf("`%d attachment(s) not available publicly`", attachments))
	}
	return out
}

func (t *Translator) issueURLToGitHub(target string, refs map[int64]int64) (string, bool) {
	m := t.forgejoIssue.FindStringSubmatch(target)
	if m == nil {
		return "", false
	}
	n, _ := strconv.ParseInt(m[1], 10, 64)
	gh, ok := refs[n]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s/issues/%d", t.githubBase, gh), true
}

// githubUser names a GitHub account in a Forgejo projection: a link to the
// profile, never a Forgejo mention.
func (t *Translator) githubUser(login string) string {
	return fmt.Sprintf("[%s](https://github.com/%s)", login, login)
}

// maintainer names a Forgejo account in a GitHub projection. Only a mapped
// account is named; an unmapped login is an internal detail.
func (t *Translator) maintainer(login string) string {
	if gh, ok := t.toGitHub[strings.ToLower(login)]; ok {
		return fmt.Sprintf("[%s](https://github.com/%s)", gh, gh)
	}
	return "a maintainer"
}

func isAttachment(target string) bool {
	return strings.Contains(target, "/attachments/")
}

// normalize makes bodies comparable: GitHub returns what a browser sent,
// which is often CRLF.
func normalize(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// Equal reports whether a projection's current body is the rendered one.
func Equal(current, rendered string) bool {
	return strings.TrimSpace(normalize(current)) == strings.TrimSpace(normalize(rendered))
}

// replace is ReplaceAllStringFunc with the submatches.
func replace(re *regexp.Regexp, s string, f func([]string) string) string {
	matches := re.FindAllStringSubmatchIndex(s, -1)
	if matches == nil {
		return s
	}
	var b strings.Builder
	last := 0
	for _, idx := range matches {
		groups := make([]string, len(idx)/2)
		for g := range groups {
			if idx[2*g] >= 0 {
				groups[g] = s[idx[2*g]:idx[2*g+1]]
			}
		}
		b.WriteString(s[last:idx[0]])
		b.WriteString(f(groups))
		last = idx[1]
	}
	b.WriteString(s[last:])
	return b.String()
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func addNumber(set map[int64]bool, s string) {
	if n, err := strconv.ParseInt(s, 10, 64); err == nil && n > 0 {
		set[n] = true
	}
}

func sorted(set map[int64]bool) []int64 {
	out := make([]int64, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

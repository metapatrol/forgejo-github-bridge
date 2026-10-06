// Package marker writes and reads the hidden line every projection carries.
//
// A marker says where a projected issue or comment came from:
//
//	<!-- forgejo-github-bridge origin=github kind=comment id=927362 -->
//
// It is not trusted. Anyone can type it into an issue on GitHub, so it never
// decides anything on its own: the bridge only reads markers on items its own
// accounts wrote, and only to rebuild a mapping the database lost. The
// database is the authority on what corresponds to what.
package marker

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Origin is the side an item was originally written on.
type Origin string

// The two sides.
const (
	OriginGitHub  Origin = "github"
	OriginForgejo Origin = "forgejo"
)

// Kind is what the marker is attached to.
type Kind string

// The kinds of projection.
const (
	KindIssue   Kind = "issue"
	KindPull    Kind = "pull"
	KindComment Kind = "comment"
	KindReview  Kind = "review"
	// KindLine marks the projection of a comment on a line of a pull
	// request's diff; ID is the line comment's.
	KindLine Kind = "line"
	// KindNotice marks something the bridge says itself, such as a request
	// to rebase. It projects nothing; ID is the issue or pull request it is
	// about.
	KindNotice Kind = "notice"
)

// Marker identifies the source of a projection. ID is the source's number for
// an issue and its database ID for a comment.
type Marker struct {
	Origin Origin
	Kind   Kind
	ID     int64
}

// String renders the marker as the HTML comment that ends a projection.
func (m Marker) String() string {
	return fmt.Sprintf("<!-- forgejo-github-bridge origin=%s kind=%s id=%d -->", m.Origin, m.Kind, m.ID)
}

var pattern = regexp.MustCompile(`<!-- forgejo-github-bridge origin=(github|forgejo) kind=(issue|pull|comment|review|line|notice) id=([0-9]+) -->`)

// Append adds the marker to a body, separated by a blank line.
func Append(body string, m Marker) string {
	return strings.TrimRight(body, "\n") + "\n\n" + m.String()
}

// Parse finds the last marker in a body. The last one, because the bridge
// appends its marker after everything else, and a marker quoted earlier in
// the body is content rather than provenance.
func Parse(body string) (Marker, bool) {
	all := pattern.FindAllStringSubmatch(body, -1)
	if len(all) == 0 {
		return Marker{}, false
	}
	m := all[len(all)-1]
	id, err := strconv.ParseInt(m[3], 10, 64)
	if err != nil {
		return Marker{}, false
	}
	return Marker{Origin: Origin(m[1]), Kind: Kind(m[2]), ID: id}, true
}

// Strip removes every marker from a body. Text crossing the bridge has any
// marker it carries removed before it is projected, so a contributor who pastes
// one cannot make a projection claim a second, forged origin.
func Strip(body string) string {
	return pattern.ReplaceAllString(body, "")
}

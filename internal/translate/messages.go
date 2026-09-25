package translate

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// MessageData is what a message template can use.
type MessageData struct {
	// Base is the branch the pull request targets.
	Base string
	// Targets lists the branches that take contributions, formatted for
	// prose: `develop` or `main`.
	Targets string
	// Behind is how many commits the base has that the pull request lacks.
	Behind int
	// Commit is the commit the pull request was merged as; ShortCommit its
	// first seven characters.
	Commit      string
	ShortCommit string
	// Branch is the contrib branch the pull request's head lives on.
	Branch string
	// Paths are the CI files a pull request changes, one per line as a
	// Markdown list.
	Paths string
	// Contributor is the GitHub login of the pull request's author.
	Contributor string
}

// DefaultMessages are what the bridge says when nothing else is configured.
// They are written for a person who took the time to contribute.
var DefaultMessages = map[string]string{
	"unsupportedBase": "Thanks for the contribution! This project takes pull requests against {{.Targets}}, " +
		"and this one targets `{{.Base}}`. Could you switch the base branch? It is picked up automatically once you do.",
	"rebaseRequest": "Thanks for your patience! `{{.Base}}` has moved on by {{.Behind}} commit(s) since this branch was last updated. " +
		"When you have a moment, could you rebase onto `{{.Base}}`? That way your commits can be merged exactly as they are, " +
		"and GitHub shows this pull request as merged.",
	"upToDate": "Thanks, this is up to date with `{{.Base}}` now.",
	"merged":   "Merged into `{{.Base}}` as {{.Commit}}. Thank you for contributing!",
	"integrated": "Merged into `{{.Base}}` as {{.Commit}}. Your commits were rebased onto `{{.Base}}` while merging, " +
		"so GitHub does not show this pull request as merged; your authorship is kept on every commit. Thank you for contributing!",
	"closedUpstream": "The maintainers have closed this pull request without merging it. Thank you for taking the time to contribute.",
	"headFrozen": "The branch `{{.Branch}}` was changed by someone other than the bridge. " +
		"New commits from GitHub are no longer imported into this pull request, so that nothing pushed here is overwritten.",
	"touchesCI": "**This pull request changes CI configuration:**\n\n{{.Paths}}\n\n" +
		"Workflows run from the pull request's own files. Read these changes before approving a workflow run, " +
		"and never approve it with \"Approve always\": every imported pull request comes from the same account.",
}

// sample is what templates are tried against when they are loaded, so that
// a template that fails does so at startup rather than on a contributor's
// pull request.
var sample = MessageData{
	Base: "develop", Targets: "`develop`", Behind: 2, Commit: "0123456789abcdef0123456789abcdef01234567",
	ShortCommit: "0123456", Branch: "pr/1", Paths: "- `.forgejo/workflows/ci.yml`", Contributor: "octocat",
}

// Messages renders the bridge's own messages.
type Messages struct {
	templates map[string]*template.Template
}

// NewMessages builds the messages from the defaults and overrides. An
// override for a message that does not exist, or a template that does not
// render, is an error.
func NewMessages(overrides ...map[string]string) (*Messages, error) {
	m := &Messages{templates: map[string]*template.Template{}}
	texts := map[string]string{}
	for k, v := range DefaultMessages {
		texts[k] = v
	}
	for _, o := range overrides {
		for k, v := range o {
			if _, ok := DefaultMessages[k]; !ok {
				return nil, fmt.Errorf("unknown message %q; known are %s", k, strings.Join(messageNames(), ", "))
			}
			texts[k] = v
		}
	}
	for k, v := range texts {
		t, err := template.New(k).Option("missingkey=error").Parse(v)
		if err != nil {
			return nil, fmt.Errorf("message %s: %w", k, err)
		}
		if err := t.Execute(&bytes.Buffer{}, sample); err != nil {
			return nil, fmt.Errorf("message %s: %w", k, err)
		}
		m.templates[k] = t
	}
	return m, nil
}

// MustDefaultMessages returns the default messages.
func MustDefaultMessages() *Messages {
	m, err := NewMessages()
	if err != nil {
		panic(err)
	}
	return m
}

func messageNames() []string {
	names := make([]string, 0, len(DefaultMessages))
	for k := range DefaultMessages {
		names = append(names, k)
	}
	return sortedStrings(names)
}

// Render renders one message. The templates were tried at load time, so a
// failure here is a bug; it falls back to the default text rather than
// saying nothing.
func (m *Messages) Render(name string, d MessageData) string {
	if len(d.Commit) >= 7 && d.ShortCommit == "" {
		d.ShortCommit = d.Commit[:7]
	}
	var b bytes.Buffer
	if err := m.templates[name].Execute(&b, d); err != nil {
		return DefaultMessages[name]
	}
	return strings.TrimSpace(b.String())
}

// TargetList formats branch names for prose: `a`, `b` or `c`.
func TargetList(targets []string) string {
	quoted := make([]string, len(targets))
	for i, b := range targets {
		quoted[i] = "`" + b + "`"
	}
	if len(quoted) <= 1 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " or " + quoted[len(quoted)-1]
}

// PathList formats paths as a Markdown list.
func PathList(paths []string) string {
	return "- `" + strings.Join(paths, "`\n- `") + "`"
}

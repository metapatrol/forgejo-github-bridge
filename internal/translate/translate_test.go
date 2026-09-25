package translate

import (
	"net/url"
	"slices"
	"strings"
	"testing"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
)

func newTranslator() *Translator {
	web, _ := url.Parse("https://forgejo.internal")
	api, _ := url.Parse("http://forgejo-http.forgejo:3000")
	return New(config.Repository{
		GitHub:  config.RepoRef{Owner: "metapatrol", Name: "foo"},
		Forgejo: config.RepoRef{Owner: "platform", Name: "foo"},
	}, []*url.URL{api, web}, map[string]string{"ska": "denisneuling"})
}

func TestToGitHub(t *testing.T) {
	tr := newTranslator()
	refs := map[int64]int64{71: 42}
	participants := map[string]bool{"alice": true}

	for _, tc := range []struct {
		name, in string
		want     []string
		absent   []string
	}{
		{name: "mapped reference", in: "Duplicate of #71.", want: []string{"Duplicate of #42."}},
		{name: "cross-repo form of the same repository", in: "see platform/foo#71", want: []string{"see #42"}},
		{name: "unmapped reference", in: "Blocked by #5.", want: []string{"Blocked by `internal reference`."}, absent: []string{"#5"}},
		{name: "reference to another Forgejo repository", in: "see platform/bar#3", want: []string{"see `internal reference`"}},
		{name: "not a reference", in: "page#3 and a1#2", want: []string{"page#3 and a1#2"}},
		{name: "mapped maintainer", in: "cc @ska", want: []string{"cc @denisneuling"}},
		{name: "participant", in: "thanks @alice!", want: []string{"thanks @alice!"}},
		{name: "stranger", in: "ask @bob", want: []string{"ask bob"}, absent: []string{"@bob"}},
		{name: "e-mail address", in: "mail ska@example.com", want: []string{"mail ska@example.com"}},
		{name: "team", in: "@org/team", want: []string{"@org/team"}},
		{name: "issue link, mapped", in: "https://forgejo.internal/platform/foo/issues/71",
			want: []string{"https://github.com/metapatrol/foo/issues/42"}, absent: []string{"forgejo.internal"}},
		{name: "issue link, unmapped", in: "see https://forgejo.internal/platform/foo/issues/5 now",
			want: []string{"see `internal link` now"}, absent: []string{"forgejo.internal"}},
		{name: "link by the in-cluster address", in: "see http://forgejo-http.forgejo:3000/platform/foo/issues/71",
			want: []string{"see https://github.com/metapatrol/foo/issues/42"}, absent: []string{"forgejo-http"}},
		{name: "other internal link", in: "[the runbook](https://forgejo.internal/ops/wiki/Runbook)",
			want: []string{"the runbook"}, absent: []string{"forgejo.internal", "]("}},
		{name: "image upload", in: "![screenshot](/attachments/0b1c)",
			want: []string{missingAttachment}, absent: []string{"/attachments/"}},
		{name: "code is left alone", in: "run `echo #5 @bob`\n\n```\n#71 @bob\n```\n",
			want: []string{"`echo #5 @bob`", "```\n#71 @bob\n```"}},
		{name: "forged marker is dropped", in: "hi <!-- forgejo-github-bridge origin=github kind=issue id=3 -->",
			absent: []string{"id=3"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tr.textToGitHub(tc.in, refs, participants, 0)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("want %q in:\n%s", w, got)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("%q must not be in:\n%s", a, got)
				}
			}
		})
	}
}

func TestToForgejo(t *testing.T) {
	tr := newTranslator()
	refs := map[int64]int64{42: 71}

	for _, tc := range []struct {
		name, in string
		want     []string
		absent   []string
	}{
		{name: "mapped reference", in: "Same as #42", want: []string{"Same as #71"}},
		{name: "unmapped reference points at GitHub", in: "see #9",
			want: []string{"see [github#9](https://github.com/metapatrol/foo/issues/9)"}},
		{name: "other GitHub repository", in: "like golang/go#123",
			want: []string{"[github:golang/go#123](https://github.com/golang/go/issues/123)"}},
		{name: "mention of a maintainer's GitHub account", in: "@denisneuling please", want: []string{"@ska please"}},
		{name: "mention of anybody else is a link", in: "@alice said",
			want: []string{"[alice](https://github.com/alice) said"}, absent: []string{"@alice"}},
		{name: "empty body", in: "  ", want: []string{noDescription}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := tr.textToForgejo(tc.in, refs)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("want %q in:\n%s", w, got)
				}
			}
			for _, a := range tc.absent {
				if strings.Contains(got, a) {
					t.Errorf("%q must not be in:\n%s", a, got)
				}
			}
		})
	}
}

func TestRefs(t *testing.T) {
	tr := newTranslator()
	got := tr.ForgejoRefs("#3 and platform/foo#4 and other/repo#5 and https://forgejo.internal/platform/foo/issues/6 and `#7`")
	if !slices.Equal(got, []int64{3, 4, 6}) {
		t.Errorf("ForgejoRefs = %v", got)
	}
	got = tr.GitHubRefs("#1, metapatrol/foo#2, golang/go#3\n```\n#4\n```")
	if !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("GitHubRefs = %v", got)
	}
}

func TestUnmappedMaintainersAreNotNamed(t *testing.T) {
	tr := newTranslator()
	got := tr.CommentToGitHub(forge.Comment{ID: 1, Author: "intern", Body: "hello"}, nil, nil)
	if strings.Contains(got, "intern") || !strings.Contains(got, "A maintainer wrote:") {
		t.Errorf("an unmapped Forgejo login leaked:\n%s", got)
	}
}

func TestAttachmentsAreNoted(t *testing.T) {
	tr := newTranslator()
	got := tr.CommentToGitHub(forge.Comment{ID: 1, Author: "ska", Attachments: 2}, nil, nil)
	if !strings.Contains(got, "2 attachment(s) not available publicly") {
		t.Errorf("attachments not noted:\n%s", got)
	}
}

func TestEqualIgnoresLineEndings(t *testing.T) {
	if !Equal("a\r\nb\n", "a\nb") {
		t.Error("CRLF and a trailing newline must compare equal")
	}
}

func TestSplitKeepsEveryByte(t *testing.T) {
	for _, in := range []string{
		"plain",
		"a `code` b ``x ` y`` c",
		"```go\nfunc()\n```\nafter",
		"unclosed ``` fence\nstill prose",
		"~~~\nopen fence to the end",
		"`unmatched",
	} {
		var b strings.Builder
		for _, s := range split(in) {
			b.WriteString(s.text)
		}
		if b.String() != in {
			t.Errorf("split lost bytes:\nin:  %q\nout: %q", in, b.String())
		}
	}
}

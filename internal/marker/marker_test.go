package marker

import (
	"strings"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	m := Marker{Origin: OriginGitHub, Kind: KindComment, ID: 927362}
	body := Append("hello\n\n", m)
	got, ok := Parse(body)
	if !ok || got != m {
		t.Fatalf("Parse(Append(m)) = %+v, %v", got, ok)
	}
	if !strings.HasPrefix(body, "hello\n\n<!--") {
		t.Errorf("body = %q", body)
	}
}

func TestTheLastMarkerWins(t *testing.T) {
	quoted := Marker{Origin: OriginForgejo, Kind: KindIssue, ID: 1}.String()
	own := Marker{Origin: OriginGitHub, Kind: KindIssue, ID: 2}
	got, _ := Parse(Append("someone pasted "+quoted, own))
	if got != own {
		t.Errorf("Parse = %+v, want the appended marker", got)
	}
}

func TestStrip(t *testing.T) {
	body := "a " + Marker{Origin: OriginForgejo, Kind: KindIssue, ID: 1}.String() + " b"
	if got := Strip(body); strings.Contains(got, "forgejo-github-bridge") {
		t.Errorf("Strip left %q", got)
	}
	if _, ok := Parse("<!-- forgejo-github-bridge origin=elsewhere kind=issue id=1 -->"); ok {
		t.Error("an unknown origin parsed")
	}
}

package github

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/forge"
)

var repo = config.RepoRef{Owner: "metapatrol", Name: "foo"}

type server struct {
	t        *testing.T
	key      *rsa.PrivateKey
	mu       sync.Mutex
	issued   int
	requests []string
	notMod   int
	*httptest.Server
}

func newServer(t *testing.T) (*server, *Client) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	s := &server{t: t, key: key}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /app", s.asApp(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"slug":"bridge-app"}`)
	}))
	mux.HandleFunc("GET /repos/metapatrol/foo/installation", s.asApp(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"id":77}`)
	}))
	mux.HandleFunc("POST /app/installations/77/access_tokens", s.asApp(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Repositories []string }
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Repositories) != 1 || req.Repositories[0] != "foo" {
			t.Errorf("the token must be restricted to the repository, asked for %v", req.Repositories)
		}
		s.mu.Lock()
		s.issued++
		s.mu.Unlock()
		fmt.Fprintf(w, `{"token":"inst-token","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339))
	}))
	mux.HandleFunc("GET /repos/metapatrol/foo/issues", s.asInstallation(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprint(w, `[{"number":3,"title":"pr","state":"open","user":{"login":"bob"},"pull_request":{"url":"x"}}]`)
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			s.mu.Lock()
			s.notMod++
			s.mu.Unlock()
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/metapatrol/foo/issues?page=2>; rel="next", <%s/x>; rel="last"`, s.URL, s.URL))
		fmt.Fprint(w, `[{"number":2,"title":"bug","body":"b","state":"closed","user":{"login":"alice"},"labels":[{"name":"x"}],"html_url":"https://github.com/metapatrol/foo/issues/2","updated_at":"2026-09-01T10:00:00Z"}]`)
	}))
	mux.HandleFunc("GET /repos/metapatrol/foo/issues/comments", s.asInstallation(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[{"id":9,"body":"c","user":{"login":"alice"},"issue_url":"https://api.github.com/repos/metapatrol/foo/issues/2"}]`)
	}))
	mux.HandleFunc("GET /repos/metapatrol/foo/issues/404", s.asInstallation(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	}))
	mux.HandleFunc("PATCH /repos/metapatrol/foo/issues/2", s.asInstallation(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req) != 1 || req["state"] != "closed" {
			t.Errorf("an edit must send only what changes, sent %v", req)
		}
		fmt.Fprint(w, `{"number":2,"state":"closed","user":{"login":"alice"}}`)
	}))
	s.Server = httptest.NewServer(mux)
	t.Cleanup(s.Close)

	base, _ := url.Parse(s.URL)
	return s, New(base, 42, key, s.Client(), nil)
}

// asApp checks the App JWT: RS256 over the header and claims, issued by the
// App ID, valid now.
func (s *server) asApp(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		parts := strings.Split(token, ".")
		if !ok || len(parts) != 3 {
			s.t.Errorf("%s: no App JWT", r.URL.Path)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
		digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
		if err := rsa.VerifyPKCS1v15(&s.key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
			s.t.Errorf("%s: bad JWT signature: %v", r.URL.Path, err)
		}
		raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
		var claims struct {
			Iat, Exp int64
			Iss      string
		}
		_ = json.Unmarshal(raw, &claims)
		now := time.Now().Unix()
		if claims.Iss != "42" || claims.Iat > now || claims.Exp <= now || claims.Exp-claims.Iat > 600 {
			s.t.Errorf("%s: bad claims %+v", r.URL.Path, claims)
		}
		h(w, r)
	}
}

func (s *server) asInstallation(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.record(r)
		if r.Header.Get("Authorization") != "Bearer inst-token" {
			s.t.Errorf("%s: not authenticated with the installation token", r.URL.Path)
		}
		if r.Header.Get("X-GitHub-Api-Version") == "" {
			s.t.Errorf("%s: no API version", r.URL.Path)
		}
		h(w, r)
	}
}

func (s *server) record(r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
}

func TestSelfIsTheBotLogin(t *testing.T) {
	_, c := newServer(t)
	self, err := c.Self(context.Background())
	if err != nil || self != "bridge-app[bot]" {
		t.Fatalf("Self = %q, %v", self, err)
	}
}

func TestListingPaginatesAndMaps(t *testing.T) {
	s, c := newServer(t)
	ctx := context.Background()
	issues, err := c.ListIssuesSince(ctx, repo, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("want both pages, have %d issues", len(issues))
	}
	got := issues[0]
	if got.Number != 2 || !got.Closed || got.Author != "alice" || got.Labels[0] != "x" || got.IsPullRequest {
		t.Errorf("issue = %+v", got)
	}
	if !issues[1].IsPullRequest {
		t.Error("a pull request was not recognised")
	}

	// The token is reused, and the unchanged first page is answered by its
	// ETag.
	again, err := c.ListIssuesSince(ctx, repo, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 2 {
		t.Errorf("a cached first page must still lead to the second, have %d issues", len(again))
	}
	if s.issued != 1 {
		t.Errorf("issued %d installation tokens, want one reused", s.issued)
	}
	if s.notMod != 1 {
		t.Errorf("want the second listing answered from the ETag, %d 304s", s.notMod)
	}

	comments, err := c.ListCommentsSince(ctx, repo, time.Time{})
	if err != nil || len(comments) != 1 || comments[0].IssueNumber != 2 {
		t.Fatalf("comments = %+v, %v", comments, err)
	}
}

func TestNotFound(t *testing.T) {
	_, c := newServer(t)
	_, err := c.GetIssue(context.Background(), repo, 404)
	if !errors.Is(err, forge.ErrNotFound) {
		t.Fatalf("want ErrNotFound, have %v", err)
	}
}

func TestEditSendsOnlyTheChange(t *testing.T) {
	_, c := newServer(t)
	closed := true
	if _, err := c.EditIssue(context.Background(), repo, 2, forge.IssueEdit{Closed: &closed}); err != nil {
		t.Fatal(err)
	}
}

func TestPaginationNeverLeavesTheHost(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	var leaked bool
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = true
		fmt.Fprint(w, `[]`)
	}))
	defer evil.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/metapatrol/foo/installation":
			fmt.Fprint(w, `{"id":1}`)
		case "/app/installations/1/access_tokens":
			fmt.Fprintf(w, `{"token":"t","expires_at":%q}`, time.Now().Add(time.Hour).Format(time.RFC3339))
		default:
			w.Header().Set("Link", `<`+evil.URL+`/steal>; rel="next"`)
			fmt.Fprint(w, `[]`)
		}
	}))
	defer srv.Close()
	base, _ := url.Parse(srv.URL)
	c := New(base, 1, key, srv.Client(), nil)
	if _, err := c.ListIssuesSince(context.Background(), repo, time.Time{}); err == nil {
		t.Error("a next link to another host was followed without error")
	}
	if leaked {
		t.Fatal("the installation token was sent to another host")
	}
}

func TestParsePrivateKeyAcceptsBothEncodings(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pkcs8 := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	for name, raw := range map[string][]byte{"pkcs1": pkcs1, "pkcs8": pkcs8} {
		if _, err := ParsePrivateKey(raw); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParsePrivateKey([]byte("nope")); err == nil {
		t.Error("garbage parsed as a key")
	}
}

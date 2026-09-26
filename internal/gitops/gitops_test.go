package gitops

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
)

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestImport runs the real git against two local repositories standing in
// for GitHub and the contrib fork: a pull request ref on one side, a branch
// pushed to the other with a lease.
func TestImport(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("no git")
	}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	githubDir := filepath.Join(root, "github", "metapatrol")
	forgejoDir := filepath.Join(root, "forgejo", "github-contrib")
	for _, d := range []string{work, githubDir, forgejoDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git(t, githubDir, "init", "--bare", "-q", "foo.git")
	git(t, forgejoDir, "init", "--bare", "-q", "foo.git")

	git(t, work, "init", "-q", "-b", "develop")
	git(t, work, "commit", "-q", "--allow-empty", "-m", "base")
	git(t, work, "push", "-q", filepath.Join(forgejoDir, "foo.git"), "develop")
	git(t, work, "commit", "-q", "--allow-empty", "-m", "contribution")
	first := git(t, work, "rev-parse", "HEAD")
	git(t, work, "push", "-q", filepath.Join(githubDir, "foo.git"), "HEAD:refs/pull/7/head")

	forgejo, _ := url.Parse("file://" + filepath.Join(root, "forgejo"))
	g := New(filepath.Join(root, "cache"), "file://"+filepath.Join(root, "github"), forgejo, "github-contrib", "token", "")
	repo := config.Repository{
		GitHub:  config.RepoRef{Owner: "metapatrol", Name: "foo"},
		Contrib: config.RepoRef{Owner: "github-contrib", Name: "foo"},
	}
	ctx := context.Background()

	sha, err := g.Import(ctx, repo, 7, "develop", "pr/7", "")
	if err != nil {
		t.Fatal(err)
	}
	if sha != first || git(t, forgejoDir, "--git-dir=foo.git", "rev-parse", "pr/7") != first {
		t.Fatalf("imported %s, want %s", sha, first)
	}

	// The contributor force-pushes; the bridge follows from where it left.
	git(t, work, "commit", "-q", "--amend", "--allow-empty", "-m", "contribution, amended")
	second := git(t, work, "rev-parse", "HEAD")
	git(t, work, "push", "-q", "-f", filepath.Join(githubDir, "foo.git"), "HEAD:refs/pull/7/head")
	if sha, err = g.Import(ctx, repo, 7, "develop", "pr/7", first); err != nil || sha != second {
		t.Fatalf("update: %s, %v", sha, err)
	}

	// Somebody else moved the branch: the lease refuses, nothing is lost.
	git(t, work, "push", "-q", "-f", filepath.Join(forgejoDir, "foo.git"), first+":refs/heads/pr/7")
	if _, err := g.Import(ctx, repo, 7, "develop", "pr/7", second); !errors.Is(err, ErrLeaseRejected) {
		t.Fatalf("want ErrLeaseRejected, have %v", err)
	}
	if got := git(t, forgejoDir, "--git-dir=foo.git", "rev-parse", "pr/7"); got != first {
		t.Fatalf("the other push was overwritten: branch at %s", got)
	}

	// A new branch must not exist yet.
	if _, err := g.Import(ctx, repo, 7, "develop", "pr/7", ""); !errors.Is(err, ErrLeaseRejected) {
		t.Fatalf("pushing over an existing branch as new: %v", err)
	}
}

func TestOnlyBridgeBranches(t *testing.T) {
	g := New(t.TempDir(), "file:///nowhere", &url.URL{Scheme: "file", Path: "/nowhere"}, "u", "t", "")
	for _, branch := range []string{"develop", "main", "pr/1/../main", "refs/heads/pr/1", "pr/x"} {
		if _, err := g.Import(context.Background(), config.Repository{}, 1, "develop", branch, ""); err == nil ||
			!strings.Contains(err.Error(), "not a bridge branch") {
			t.Errorf("%q: %v", branch, err)
		}
	}
}

// TestPrivateRepositories serves the GitHub side over real smart HTTP, with
// git http-backend behind a check of the Authorization header, the way a
// private repository refuses an anonymous fetch.
func TestPrivateRepositories(t *testing.T) {
	backend := filepath.Join(strings.TrimSpace(git(t, ".", "--exec-path")), "git-http-backend")
	if _, err := os.Stat(backend); err != nil {
		t.Skip("no git-http-backend")
	}
	root := t.TempDir()
	githubRoot := filepath.Join(root, "github")
	forgejoDir := filepath.Join(root, "forgejo", "github-contrib")
	work := filepath.Join(root, "work")
	for _, d := range []string{filepath.Join(githubRoot, "metapatrol"), forgejoDir, work} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	git(t, filepath.Join(githubRoot, "metapatrol"), "init", "--bare", "-q", "foo.git")
	git(t, forgejoDir, "init", "--bare", "-q", "foo.git")
	git(t, work, "init", "-q", "-b", "develop")
	git(t, work, "commit", "-q", "--allow-empty", "-m", "private contribution")
	head := git(t, work, "rev-parse", "HEAD")
	git(t, work, "push", "-q", filepath.Join(githubRoot, "metapatrol", "foo.git"), "HEAD:refs/pull/3/head")

	const token = "installation-token"
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("x-access-token:"+token))
	var private bool
	var anonymous, authorised int
	var mu sync.Mutex
	cgiHandler := &cgi.Handler{Path: backend, Env: []string{"GIT_PROJECT_ROOT=" + githubRoot, "GIT_HTTP_EXPORT_ALL=1"}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Header.Get("Authorization") {
		case "":
			anonymous++
		case want:
			authorised++
		default:
			http.Error(w, "bad credentials", http.StatusForbidden)
			return
		}
		if private && r.Header.Get("Authorization") != want {
			w.Header().Set("WWW-Authenticate", `Basic realm="GitHub"`)
			http.Error(w, "repository not found", http.StatusUnauthorized)
			return
		}
		cgiHandler.ServeHTTP(w, r)
	}))
	defer srv.Close()

	forgejo, _ := url.Parse("file://" + filepath.Join(root, "forgejo"))
	repo := config.Repository{
		GitHub:  config.RepoRef{Owner: "metapatrol", Name: "foo"},
		Contrib: config.RepoRef{Owner: "github-contrib", Name: "foo"},
	}
	asked := 0
	tokens := func(_ context.Context, r config.RepoRef) (string, error) {
		asked++
		if r != repo.GitHub {
			t.Errorf("token asked for %s", r)
		}
		return token, nil
	}
	ctx := context.Background()

	t.Run("public: no token is asked for", func(t *testing.T) {
		g := New(filepath.Join(root, "cache-public"), srv.URL, forgejo, "c", "t", "").WithGitHubToken(tokens)
		if _, err := g.Import(ctx, repo, 3, "develop", "pr/3", ""); err != nil {
			t.Fatal(err)
		}
		if asked != 0 || authorised != 0 {
			t.Errorf("a public fetch used the token (asked %d, authorised requests %d)", asked, authorised)
		}
		git(t, forgejoDir, "--git-dir=foo.git", "branch", "-D", "pr/3")
	})

	private = true
	t.Run("private: retried with the installation token", func(t *testing.T) {
		g := New(filepath.Join(root, "cache-private"), srv.URL, forgejo, "c", "t", "").WithGitHubToken(tokens)
		sha, err := g.Import(ctx, repo, 3, "develop", "pr/3", "")
		if err != nil {
			t.Fatal(err)
		}
		if sha != head {
			t.Errorf("imported %s, want %s", sha, head)
		}
		if asked != 1 || authorised == 0 {
			t.Errorf("want one token and authorised requests, have asked %d, authorised %d", asked, authorised)
		}
	})

	t.Run("private without a token: says what is missing", func(t *testing.T) {
		g := New(filepath.Join(root, "cache-none"), srv.URL, forgejo, "c", "t", "")
		_, err := g.Import(ctx, repo, 3, "develop", "pr/3", head)
		if err == nil || !strings.Contains(err.Error(), "fetching GitHub pull request #3") {
			t.Fatalf("want a fetch error, have %v", err)
		}
	})

	t.Run("private with a token that does not reach it: names the permission", func(t *testing.T) {
		g := New(filepath.Join(root, "cache-wrong"), srv.URL, forgejo, "c", "t", "").
			WithGitHubToken(func(context.Context, config.RepoRef) (string, error) { return "not-it", nil })
		_, err := g.Import(ctx, repo, 3, "develop", "pr/3", head)
		if err == nil || !strings.Contains(err.Error(), "Contents: read") {
			t.Fatalf("want an error naming the permission, have %v", err)
		}
		if strings.Contains(err.Error(), "not-it") {
			t.Fatal("the token appears in the error")
		}
	})
}

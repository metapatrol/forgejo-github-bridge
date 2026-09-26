// Package gitops moves a GitHub pull request's head into the contrib
// repository with the git CLI.
//
// It does exactly two things, and neither takes a URL, a ref or a branch from
// anywhere but its configuration and a pull request number:
//
//	fetch  refs/pull/<n>/head  from the configured GitHub repository
//	push   refs/heads/pr/<n>   to the configured contrib repository
//
// That is docs/design.md §15's "bounded Git operations", and the reason the
// bridge never becomes a Git proxy for whatever GitHub tells it.
package gitops

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
)

// ErrLeaseRejected is returned when the contrib branch is not where the
// bridge last left it: somebody else pushed to it.
var ErrLeaseRejected = errors.New("the branch moved since the bridge last pushed it")

// branchPattern is the only shape of branch this package pushes.
var branchPattern = regexp.MustCompile(`^pr/[0-9]+$`)

// GitHubToken returns an installation token for a GitHub repository. It is
// only asked for when an anonymous fetch fails, which is what a private
// repository does.
type GitHubToken func(ctx context.Context, repo config.RepoRef) (string, error)

// Git runs the git CLI against a cache of bare repositories.
type Git struct {
	// cacheDir holds one bare repository per bridged pair. Kept between
	// imports so that only new objects are fetched and pushed.
	cacheDir   string
	githubURL  string
	forgejoURL *url.URL
	// auth is the Authorization header for the contrib account.
	auth   string
	caFile string
	// githubToken, when set, lets a fetch that failed anonymously be retried
	// with the GitHub App's installation token.
	githubToken GitHubToken
}

// remote is how git authenticates to one side: an Authorization header, and
// a CA to trust. The zero value is anonymous.
type remote struct {
	header string
	caFile string
}

// WithGitHubToken lets pull requests of private GitHub repositories be
// fetched with the App's installation token. The App then needs the
// Contents: read permission.
func (g *Git) WithGitHubToken(f GitHubToken) *Git {
	g.githubToken = f
	return g
}

func basic(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

// New creates a Git for the given contrib account. githubURL is
// https://github.com except in tests.
func New(cacheDir, githubURL string, forgejoURL *url.URL, login, token, caFile string) *Git {
	return &Git{
		cacheDir:   cacheDir,
		githubURL:  strings.TrimRight(githubURL, "/"),
		forgejoURL: forgejoURL,
		auth:       basic(login, token),
		caFile:     caFile,
	}
}

// Import fetches the pull request's head from GitHub and pushes it to branch
// in the contrib repository, but only if the branch is at expected ("" means
// it must not exist). base is fetched from the contrib repository first, so
// that only the pull request's own commits travel. It returns the pushed
// commit.
func (g *Git) Import(ctx context.Context, repo config.Repository, number int64, base, branch, expected string) (string, error) {
	if !branchPattern.MatchString(branch) {
		return "", fmt.Errorf("refusing to push to %q: not a bridge branch", branch)
	}
	dir, err := g.repository(ctx, repo)
	if err != nil {
		return "", err
	}
	contrib := g.forgejoURL.JoinPath(repo.Contrib.Owner, repo.Contrib.Name+".git").String()
	forgejo := &remote{header: g.auth, caFile: g.caFile}
	local := "refs/pull/" + strconv.FormatInt(number, 10)

	// The base, so the push negotiates against history the contrib
	// repository already has. Failing is not fatal: the push then sends more.
	_, _ = g.run(ctx, dir, forgejo, "fetch", "--no-tags", "--quiet", contrib, "+refs/heads/"+base+":refs/contrib/"+base)

	if err := g.fetchPull(ctx, dir, repo.GitHub, number, local); err != nil {
		return "", err
	}
	sha, err := g.run(ctx, dir, nil, "rev-parse", "--verify", local+"^{commit}")
	if err != nil {
		return "", err
	}
	sha = strings.TrimSpace(sha)

	out, err := g.run(ctx, dir, forgejo, "push", "--porcelain",
		"--force-with-lease=refs/heads/"+branch+":"+expected,
		contrib, local+":refs/heads/"+branch)
	if err != nil {
		if strings.Contains(out, "stale info") || strings.Contains(out, "[rejected]") {
			return "", ErrLeaseRejected
		}
		return "", fmt.Errorf("pushing %s: %w", branch, err)
	}
	return sha, nil
}

// fetchPull fetches a pull request's head from GitHub: anonymously first,
// which is all a public repository needs, and with the App's installation
// token if that fails, which is what a private repository needs. The token
// is asked for only then, and goes to github.com only.
func (g *Git) fetchPull(ctx context.Context, dir string, repo config.RepoRef, number int64, local string) error {
	url := g.githubURL + "/" + repo.Owner + "/" + repo.Name + ".git"
	refspec := "+refs/pull/" + strconv.FormatInt(number, 10) + "/head:" + local
	_, anonErr := g.run(ctx, dir, nil, "fetch", "--no-tags", "--quiet", url, refspec)
	if anonErr == nil {
		return nil
	}
	if g.githubToken == nil {
		return fmt.Errorf("fetching GitHub pull request #%d: %w", number, anonErr)
	}
	token, err := g.githubToken(ctx, repo)
	if err != nil {
		return fmt.Errorf("fetching GitHub pull request #%d: anonymously: %w; no token to retry with: %v", number, anonErr, err)
	}
	if _, err := g.run(ctx, dir, &remote{header: basic("x-access-token", token)}, "fetch", "--no-tags", "--quiet", url, refspec); err != nil {
		return fmt.Errorf("fetching GitHub pull request #%d, anonymously and with the App's token "+
			"(a private repository needs the App's Contents: read permission): %w", number, err)
	}
	return nil
}

// repository returns the bare cache repository for a pair, creating it.
func (g *Git) repository(ctx context.Context, repo config.Repository) (string, error) {
	dir := filepath.Join(g.cacheDir, repo.GitHub.Owner+"__"+repo.GitHub.Name+".git")
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, dir, nil, "init", "--bare", "--quiet"); err != nil {
		return "", err
	}
	return dir, nil
}

// run runs git in dir, authenticated towards one remote when r is set. The
// credentials go through git's environment, never its arguments, so they do
// not show up in a process listing.
func (g *Git) run(ctx context.Context, dir string, r *remote, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // the arguments are built from configuration
	cmd.Dir = dir
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		// No system or user configuration: nothing on the host decides what
		// this git does.
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
	if r != nil {
		env = append(env, "GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: "+r.header,
			"GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1=")
		if r.caFile != "" {
			env = append(env, "GIT_SSL_CAINFO="+r.caFile)
		}
	}
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(out.String()))
	}
	return out.String(), nil
}

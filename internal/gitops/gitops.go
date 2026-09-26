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

// Git runs the git CLI against a cache of bare repositories.
type Git struct {
	// cacheDir holds one bare repository per bridged pair. Kept between
	// imports so that only new objects are fetched and pushed.
	cacheDir   string
	githubURL  string
	forgejoURL *url.URL
	// auth is the Authorization header for the contrib account. Passed to
	// git through its environment, never its arguments, so it does not show
	// up in a process listing.
	auth   string
	caFile string
}

// New creates a Git for the given contrib account. githubURL is
// https://github.com except in tests.
func New(cacheDir, githubURL string, forgejoURL *url.URL, login, token, caFile string) *Git {
	return &Git{
		cacheDir:   cacheDir,
		githubURL:  strings.TrimRight(githubURL, "/"),
		forgejoURL: forgejoURL,
		auth:       "Basic " + base64.StdEncoding.EncodeToString([]byte(login+":"+token)),
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
	github := g.githubURL + "/" + repo.GitHub.Owner + "/" + repo.GitHub.Name + ".git"
	local := "refs/pull/" + strconv.FormatInt(number, 10)

	// The base, so the push negotiates against history the contrib
	// repository already has. Failing is not fatal: the push then sends more.
	_, _ = g.run(ctx, dir, true, "fetch", "--no-tags", "--quiet", contrib, "+refs/heads/"+base+":refs/contrib/"+base)

	if _, err := g.run(ctx, dir, false, "fetch", "--no-tags", "--quiet", github,
		"+refs/pull/"+strconv.FormatInt(number, 10)+"/head:"+local); err != nil {
		return "", fmt.Errorf("fetching GitHub pull request #%d: %w", number, err)
	}
	sha, err := g.run(ctx, dir, false, "rev-parse", "--verify", local+"^{commit}")
	if err != nil {
		return "", err
	}
	sha = strings.TrimSpace(sha)

	out, err := g.run(ctx, dir, true, "push", "--porcelain",
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

// repository returns the bare cache repository for a pair, creating it.
func (g *Git) repository(ctx context.Context, repo config.Repository) (string, error) {
	dir := filepath.Join(g.cacheDir, repo.GitHub.Owner+"__"+repo.GitHub.Name+".git")
	if _, err := os.Stat(filepath.Join(dir, "HEAD")); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, dir, false, "init", "--bare", "--quiet"); err != nil {
		return "", err
	}
	return dir, nil
}

// run runs git in dir. withAuth adds the contrib account's credentials and
// CA, which only requests to Forgejo get: GitHub is fetched anonymously.
func (g *Git) run(ctx context.Context, dir string, withAuth bool, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // the arguments are built from configuration
	cmd.Dir = dir
	env := append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		// No system or user configuration: nothing on the host decides what
		// this git does.
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
	if withAuth {
		env = append(env, "GIT_CONFIG_COUNT=2",
			"GIT_CONFIG_KEY_0=http.extraHeader", "GIT_CONFIG_VALUE_0=Authorization: "+g.auth,
			"GIT_CONFIG_KEY_1=credential.helper", "GIT_CONFIG_VALUE_1=")
		if g.caFile != "" {
			env = append(env, "GIT_SSL_CAINFO="+g.caFile)
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

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
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/config"
	"forgejo.metapatrol.local/platform/forgejo-github-bridge/internal/rest"
)

// tokenRefreshMargin is how long before its expiry an installation token is
// replaced. GitHub issues them for an hour; a request that starts with a
// minute left must not fail halfway through a paginated list.
const tokenRefreshMargin = 5 * time.Minute

// app authenticates as the GitHub App and hands out installation tokens.
//
// Tokens are requested per repository and restricted to it. An installation
// may cover more repositories than the bridge is configured for, and a token
// that can only touch the repository it was asked for is the one that cannot
// be misused against the others.
type app struct {
	id  int64
	key *rsa.PrivateKey
	api *rest.Client
	now func() time.Time

	mu     sync.Mutex
	tokens map[string]installationToken
}

type installationToken struct {
	token   string
	expires time.Time
}

// LoadPrivateKey reads the App's PEM-encoded private key. GitHub hands out
// PKCS #1; PKCS #8 is accepted too, since that is what a key converted by any
// other tool ends up as.
func LoadPrivateKey(path string) (*rsa.PrivateKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading the GitHub App private key: %w", err)
	}
	return ParsePrivateKey(raw)
}

// ParsePrivateKey parses a PEM-encoded RSA private key.
func ParsePrivateKey(raw []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("the GitHub App private key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing the GitHub App private key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("the GitHub App private key is not an RSA key")
	}
	return key, nil
}

func newApp(id int64, key *rsa.PrivateKey, base *url.URL, httpClient *http.Client, opts ...rest.Option) *app {
	a := &app{id: id, key: key, now: time.Now, tokens: map[string]installationToken{}}
	a.api = rest.New(base, httpClient, a.authorizeAsApp, opts...)
	return a
}

// jwt signs the short-lived token that authenticates the App itself. Issued a
// minute in the past, because GitHub rejects a token from the future and the
// two clocks are not the same clock.
func (a *app) jwt() (string, error) {
	now := a.now()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{
		"iat": now.Add(-time.Minute).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": strconv.FormatInt(a.id, 10),
	})
	enc := base64.RawURLEncoding
	signing := enc.EncodeToString(header) + "." + enc.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, a.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", fmt.Errorf("signing the GitHub App token: %w", err)
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

func (a *app) authorizeAsApp(_ context.Context, req *http.Request) error {
	token, err := a.jwt()
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return nil
}

// slug returns the App's slug, from which its bot login is derived. Nothing
// about the App's name is configured: it is whatever GitHub says it is.
func (a *app) slug(ctx context.Context) (string, error) {
	var out struct {
		Slug string `json:"slug"`
	}
	if err := a.api.Do(ctx, http.MethodGet, "/app", nil, nil, &out); err != nil {
		return "", fmt.Errorf("reading the GitHub App: %w", err)
	}
	if out.Slug == "" {
		return "", errors.New("GitHub returned an App without a slug")
	}
	return out.Slug, nil
}

// token returns an installation token restricted to one repository.
func (a *app) token(ctx context.Context, repo config.RepoRef) (string, error) {
	key := repo.String()
	a.mu.Lock()
	cached, ok := a.tokens[key]
	a.mu.Unlock()
	if ok && a.now().Add(tokenRefreshMargin).Before(cached.expires) {
		return cached.token, nil
	}

	var inst struct {
		ID int64 `json:"id"`
	}
	path := "/repos/" + rest.Seg(repo.Owner) + "/" + rest.Seg(repo.Name) + "/installation"
	if err := a.api.Do(ctx, http.MethodGet, path, nil, nil, &inst); err != nil {
		if rest.IsNotFound(err) {
			return "", fmt.Errorf("the GitHub App is not installed on %s", repo)
		}
		return "", fmt.Errorf("finding the GitHub App installation for %s: %w", repo, err)
	}

	var issued struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	req := map[string]any{"repositories": []string{repo.Name}}
	path = "/app/installations/" + strconv.FormatInt(inst.ID, 10) + "/access_tokens"
	if err := a.api.Do(ctx, http.MethodPost, path, nil, req, &issued); err != nil {
		return "", fmt.Errorf("issuing an installation token for %s: %w", repo, err)
	}
	if issued.Token == "" {
		return "", fmt.Errorf("GitHub issued an empty installation token for %s", repo)
	}

	a.mu.Lock()
	a.tokens[key] = installationToken{token: issued.Token, expires: issued.ExpiresAt}
	a.mu.Unlock()
	return issued.Token, nil
}

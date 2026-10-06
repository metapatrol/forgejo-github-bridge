// Package config loads the bridge's configuration.
//
// It comes from two places, split by shape. Scalars and credentials come from
// environment variables: in Kubernetes the credentials arrive through a Secret
// anyway, and a DSN or a token has no business in a file that is also a
// ConfigMap. The repository list and the user mapping come from a YAML file,
// because they are structured and because they are the part an operator edits:
// which public repository belongs to which private one is a reviewed decision,
// not an environment variable.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// EnvPrefix is prepended to every variable this package reads.
const EnvPrefix = "BRIDGE_"

// Defaults for the per-repository labels and markers. They are what docs/design.md
// describes, and every one can be overridden per repository.
const (
	DefaultImportLabel    = "github"
	DefaultPublishLabel   = "public"
	DefaultInternalPrefix = "/internal"
)

// RepoRef names a repository on one forge as owner and name.
type RepoRef struct {
	Owner string
	Name  string
}

// String renders the reference as owner/name.
func (r RepoRef) String() string { return r.Owner + "/" + r.Name }

// Empty reports whether the reference is unset.
func (r RepoRef) Empty() bool { return r.Owner == "" && r.Name == "" }

// repoRefPattern is deliberately narrower than what either forge accepts. The
// references end up in URL paths and in Git remotes, and a character neither
// forge would allow in an owner or a name is one that has no reason to be
// there.
// branchPattern bounds target branch names. They end up in Git refspecs and
// compare paths.
var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

var repoRefPattern = regexp.MustCompile(`^([A-Za-z0-9][A-Za-z0-9._-]*)/([A-Za-z0-9._-]+)$`)

// ParseRepoRef parses owner/name.
func ParseRepoRef(s string) (RepoRef, error) {
	m := repoRefPattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil || m[2] == "." || m[2] == ".." {
		return RepoRef{}, fmt.Errorf("%q is not an owner/name repository reference", s)
	}
	return RepoRef{Owner: m[1], Name: m[2]}, nil
}

// Repository is one bridged pair: a public GitHub repository and the private
// Forgejo repository that is its source of truth.
type Repository struct {
	GitHub  RepoRef
	Forgejo RepoRef
	// Contrib is the Forgejo fork contributions are imported into. Empty
	// means the contrib account's fork of the canonical repository, under
	// its name, which the bridge creates when it is missing.
	// unset means pull requests are not imported.
	Contrib RepoRef
	// TargetBranches are the base branches an imported pull request may target.
	TargetBranches []string
	// RebaseRequests asks contributors on GitHub to rebase a pull request
	// whose base has moved on. It mirrors a branch protection that blocks
	// outdated merges, which the bridge's account is not allowed to read.
	RebaseRequests bool
	// CIPaths are path prefixes whose change in a pull request gets it the
	// touches-ci label and a warning (docs/design.md §12).
	CIPaths []string

	// ImportLabel marks Forgejo issues that were imported from GitHub, so a
	// maintainer can tell them apart in the issue list.
	ImportLabel string
	// PublishLabel is what makes a Forgejo-native issue public. Opt-in: an
	// issue without it never leaves Forgejo.
	PublishLabel string
	// InternalPrefix marks a Forgejo comment that stays in Forgejo although it
	// was written on a bridged issue.
	InternalPrefix string
	// ImportSince bounds the first import. Zero imports every open issue.
	ImportSince time.Time
	// Messages overrides what the bridge says for this repository, by
	// message name. See translate.DefaultMessages.
	Messages map[string]string
}

// ImportsPullRequests reports whether pull requests are imported for this
// pair: they are once target branches are named.
func (r Repository) ImportsPullRequests() bool { return len(r.TargetBranches) > 0 }

// DefaultCIPaths are where Forgejo reads workflows from. .github counts: a
// repository without .forgejo/workflows has its .github/workflows run.
var DefaultCIPaths = []string{".forgejo/", ".github/"}

// Key identifies the pair in the database. The GitHub side names it because
// that is the side a repository mapping cannot be reassigned on without also
// being a different public project.
func (r Repository) Key() string { return r.GitHub.String() }

// Config is the whole configuration of the bridge.
type Config struct {
	// Mode is ModeBridge, or ModeConsole for a process that serves the
	// console alone (docs/console.md): it needs no credential but the
	// database's, may use a role that can only read, and does no work.
	Mode string
	// ListenAddr serves /healthz, /readyz and /metrics. Cluster-internal; the
	// bridge has no public endpoint at all.
	ListenAddr string
	// ConsoleAddr serves the read-only console; empty turns it off. It is a
	// listener of its own, so that who may reach it can be decided apart
	// from probes and metrics.
	ConsoleAddr string
	LogFormat   string
	LogLevel    string

	DatabaseURL      string
	DatabaseMaxConns int32

	GitHubAPIURL         *url.URL
	GitHubAppID          int64
	GitHubPrivateKeyFile string

	ForgejoURL *url.URL
	// ForgejoWebURL is the address people's browsers use for Forgejo, when it
	// is not ForgejoURL — typically ForgejoURL is the in-cluster Service. Links
	// starting with either never reach GitHub.
	ForgejoWebURL *url.URL
	// ForgejoToken belongs to the github-bridge account, which writes issues and
	// comments.
	ForgejoToken string
	// ForgejoContribToken belongs to the github-contrib account, which opens
	// imported pull requests. Phase 2.
	ForgejoContribToken string
	// ForgejoCAFile is an additional trust anchor for Forgejo, for an instance
	// that uses a private CA.
	ForgejoCAFile string

	// PollInterval is how often each repository is polled on each side.
	PollInterval time.Duration
	// GitCacheDir holds the bare repositories pull request heads pass
	// through on their way to the contrib repository.
	GitCacheDir string

	// FullResync is how often every open bridged issue is reconciled whether or
	// not a poll saw it change. It is what notices deletions, which incremental
	// polls cannot.
	FullResync time.Duration

	// MaintenanceInterval is the time between two database maintenance
	// rounds; zero turns them off.
	MaintenanceInterval time.Duration

	Repositories []Repository
	// UserMapping maps Forgejo logins to GitHub logins. It is used for mentions
	// and attribution only; it grants nothing.
	UserMapping map[string]string
	// Messages overrides what the bridge says, for every repository. A
	// repository's own Messages win over these.
	Messages map[string]string
}

// The modes a process runs in.
const (
	ModeBridge  = "bridge"
	ModeConsole = "console"
)

// file is the shape of the YAML configuration file.
type file struct {
	Repositories []struct {
		GitHub         string            `yaml:"github"`
		Forgejo        string            `yaml:"forgejo"`
		Contrib        string            `yaml:"contrib"`
		TargetBranches []string          `yaml:"targetBranches"`
		RebaseRequests bool              `yaml:"rebaseRequests"`
		CIPaths        []string          `yaml:"ciPaths"`
		Messages       map[string]string `yaml:"messages"`
		ImportLabel    string            `yaml:"importLabel"`
		PublishLabel   string            `yaml:"publishLabel"`
		InternalPrefix *string           `yaml:"internalPrefix"`
		ImportSince    string            `yaml:"importSince"`
	} `yaml:"repositories"`
	UserMapping map[string]string `yaml:"userMapping"`
	Messages    map[string]string `yaml:"messages"`
}

// Load reads the environment and the configuration file it names.
func Load() (*Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (*Config, error) {
	env := func(name, def string) string {
		if v := strings.TrimSpace(getenv(EnvPrefix + name)); v != "" {
			return v
		}
		return def
	}

	var errs []error
	// secret reads a credential from NAME, or from the file NAME_FILE names —
	// the convention Docker secrets and systemd credentials follow, so that a
	// token need not sit in an environment every process of the host can read.
	secret := func(name string) string {
		if v := env(name, ""); v != "" {
			return v
		}
		path := env(name+"_FILE", "")
		if path == "" {
			return ""
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s%s_FILE: %w", EnvPrefix, name, err))
			return ""
		}
		return strings.TrimSpace(string(raw))
	}

	cfg := &Config{
		Mode:                 env("MODE", ModeBridge),
		ListenAddr:           env("LISTEN_ADDR", ":8080"),
		LogFormat:            env("LOG_FORMAT", "json"),
		LogLevel:             env("LOG_LEVEL", "info"),
		DatabaseURL:          secret("DATABASE_URL"),
		GitHubPrivateKeyFile: env("GITHUB_PRIVATE_KEY_FILE", ""),
		ForgejoToken:         secret("FORGEJO_TOKEN"),
		ForgejoContribToken:  secret("FORGEJO_CONTRIB_TOKEN"),
		ForgejoCAFile:        env("FORGEJO_CA_FILE", ""),
	}

	required := func(name, value string) {
		if value == "" {
			errs = append(errs, fmt.Errorf("%s%s is required", EnvPrefix, name))
		}
	}
	bridge := true
	switch cfg.Mode {
	case ModeBridge:
		cfg.ConsoleAddr = env("CONSOLE_ADDR", "")
	case ModeConsole:
		bridge = false
		cfg.ConsoleAddr = env("CONSOLE_ADDR", ":8090")
	default:
		errs = append(errs, fmt.Errorf("%sMODE must be %s or %s, not %q", EnvPrefix, ModeBridge, ModeConsole, cfg.Mode))
	}
	if cfg.ConsoleAddr != "" && cfg.ConsoleAddr == cfg.ListenAddr {
		errs = append(errs, fmt.Errorf("%sCONSOLE_ADDR must differ from %sLISTEN_ADDR", EnvPrefix, EnvPrefix))
	}

	required("DATABASE_URL", cfg.DatabaseURL)
	// A console reads the database and nothing else; it is given no
	// credential it would not use.
	if bridge {
		required("GITHUB_PRIVATE_KEY_FILE", cfg.GitHubPrivateKeyFile)
		required("FORGEJO_TOKEN", cfg.ForgejoToken)
	}

	switch cfg.LogFormat {
	case "json", "text":
	default:
		errs = append(errs, fmt.Errorf("%sLOG_FORMAT must be json or text, not %q", EnvPrefix, cfg.LogFormat))
	}
	switch cfg.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		errs = append(errs, fmt.Errorf("%sLOG_LEVEL must be debug, info, warn or error, not %q", EnvPrefix, cfg.LogLevel))
	}

	if n, err := strconv.ParseInt(env("DATABASE_MAX_CONNS", "5"), 10, 32); err != nil || n < 2 {
		// Two, not one: the leader lock holds a connection for as long as the
		// process is active, and the work needs another.
		errs = append(errs, fmt.Errorf("%sDATABASE_MAX_CONNS must be an integer of at least 2", EnvPrefix))
	} else {
		cfg.DatabaseMaxConns = int32(n)
	}

	if raw := env("GITHUB_APP_ID", ""); raw == "" {
		if bridge {
			errs = append(errs, fmt.Errorf("%sGITHUB_APP_ID is required", EnvPrefix))
		}
	} else if id, err := strconv.ParseInt(raw, 10, 64); err != nil || id <= 0 {
		errs = append(errs, fmt.Errorf("%sGITHUB_APP_ID must be a positive integer, not %q", EnvPrefix, raw))
	} else {
		cfg.GitHubAppID = id
	}

	var err error
	if cfg.GitHubAPIURL, err = parseBaseURL(env("GITHUB_API_URL", "https://api.github.com")); err != nil {
		errs = append(errs, fmt.Errorf("%sGITHUB_API_URL: %w", EnvPrefix, err))
	}
	if raw := env("FORGEJO_URL", ""); raw == "" {
		errs = append(errs, fmt.Errorf("%sFORGEJO_URL is required", EnvPrefix))
	} else if cfg.ForgejoURL, err = parseBaseURL(raw); err != nil {
		errs = append(errs, fmt.Errorf("%sFORGEJO_URL: %w", EnvPrefix, err))
	}

	if raw := env("FORGEJO_WEB_URL", ""); raw != "" {
		if cfg.ForgejoWebURL, err = parseBaseURL(raw); err != nil {
			errs = append(errs, fmt.Errorf("%sFORGEJO_WEB_URL: %w", EnvPrefix, err))
		}
	}

	cfg.PollInterval = duration(env, "POLL_INTERVAL", "60s", 10*time.Second, &errs)
	cfg.FullResync = duration(env, "FULL_RESYNC", "6h", time.Minute, &errs)
	// Zero turns maintenance off; anything else is at least an hour, since a
	// round vacuums every table.
	if raw := env("DB_MAINTENANCE_INTERVAL", "24h"); raw == "0" {
		cfg.MaintenanceInterval = 0
	} else {
		cfg.MaintenanceInterval = duration(env, "DB_MAINTENANCE_INTERVAL", "24h", time.Hour, &errs)
	}

	if path := env("CONFIG", ""); path == "" {
		errs = append(errs, fmt.Errorf("%sCONFIG must name the repository configuration file", EnvPrefix))
	} else if err := cfg.loadFile(path); err != nil {
		errs = append(errs, err)
	}

	cfg.GitCacheDir = env("GIT_CACHE_DIR", "/var/cache/forgejo-github-bridge")

	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	// Pull requests are opened by a second account (docs/design.md §8), and
	// without its token they cannot be imported at all.
	for _, r := range cfg.Repositories {
		if bridge && r.ImportsPullRequests() && cfg.ForgejoContribToken == "" {
			return nil, fmt.Errorf("%sFORGEJO_CONTRIB_TOKEN is required: %s imports pull requests", EnvPrefix, r.GitHub)
		}
	}
	return cfg, nil
}

// ForgejoBrowserURL is the Forgejo address for people's browsers.
func (c *Config) ForgejoBrowserURL() *url.URL {
	if c.ForgejoWebURL != nil {
		return c.ForgejoWebURL
	}
	return c.ForgejoURL
}

// ForgejoURLs returns every address Forgejo is known by.
func (c *Config) ForgejoURLs() []*url.URL {
	urls := []*url.URL{c.ForgejoURL}
	if c.ForgejoWebURL != nil {
		urls = append(urls, c.ForgejoWebURL)
	}
	return urls
}

func (c *Config) loadFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	var f file
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	// A misspelt key is refused rather than ignored. "publishlabel" silently
	// falling back to the default is how an issue ends up public under a label
	// nobody meant.
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return c.apply(f)
}

func (c *Config) apply(f file) error {
	var errs []error
	if len(f.Repositories) == 0 {
		errs = append(errs, errors.New("the configuration names no repositories"))
	}

	seenGitHub := map[string]bool{}
	seenForgejo := map[string]bool{}
	seenContrib := map[string]bool{}
	for i, r := range f.Repositories {
		where := fmt.Sprintf("repositories[%d]", i)
		repo := Repository{
			TargetBranches: r.TargetBranches,
			RebaseRequests: r.RebaseRequests,
			CIPaths:        r.CIPaths,
			Messages:       r.Messages,
			ImportLabel:    orDefault(r.ImportLabel, DefaultImportLabel),
			PublishLabel:   orDefault(r.PublishLabel, DefaultPublishLabel),
			InternalPrefix: DefaultInternalPrefix,
		}
		if r.InternalPrefix != nil {
			// An explicit empty string is honoured: it means no comment on a
			// bridged issue stays internal.
			repo.InternalPrefix = strings.TrimSpace(*r.InternalPrefix)
		}

		var err error
		if repo.GitHub, err = ParseRepoRef(r.GitHub); err != nil {
			errs = append(errs, fmt.Errorf("%s.github: %w", where, err))
		}
		if repo.Forgejo, err = ParseRepoRef(r.Forgejo); err != nil {
			errs = append(errs, fmt.Errorf("%s.forgejo: %w", where, err))
		}
		if r.Contrib != "" {
			if repo.Contrib, err = ParseRepoRef(r.Contrib); err != nil {
				errs = append(errs, fmt.Errorf("%s.contrib: %w", where, err))
			} else if strings.EqualFold(repo.Contrib.String(), repo.Forgejo.String()) {
				// The whole point of the contrib repository is that it is not the
				// canonical one: see docs/design.md §11 and §12.
				errs = append(errs, fmt.Errorf("%s.contrib must not be the canonical repository", where))
			}
		}
		if len(repo.CIPaths) == 0 {
			repo.CIPaths = DefaultCIPaths
		}
		if !repo.Contrib.Empty() && len(repo.TargetBranches) == 0 {
			// A contrib repository without target branches imports nothing,
			// which is not what anyone setting it meant. Which branches
			// outsiders may propose changes to is a decision, not a default.
			errs = append(errs, fmt.Errorf("%s.targetBranches must name the branches pull requests may target; contrib alone enables nothing", where))
		}
		for _, b := range repo.TargetBranches {
			if !branchPattern.MatchString(b) {
				errs = append(errs, fmt.Errorf("%s.targetBranches: %q is not a branch name the bridge accepts", where, b))
			}
		}
		if r.ImportSince != "" {
			if repo.ImportSince, err = time.Parse(time.RFC3339, r.ImportSince); err != nil {
				errs = append(errs, fmt.Errorf("%s.importSince must be an RFC 3339 time: %w", where, err))
			}
		}

		// Each side may appear once. Two pairs sharing a repository would
		// reconcile the same issues against two different counterparts.
		gh, fj := strings.ToLower(repo.GitHub.String()), strings.ToLower(repo.Forgejo.String())
		if seenGitHub[gh] {
			errs = append(errs, fmt.Errorf("%s: GitHub repository %s is configured twice", where, repo.GitHub))
		}
		if seenForgejo[fj] {
			errs = append(errs, fmt.Errorf("%s: Forgejo repository %s is configured twice", where, repo.Forgejo))
		}
		if seenContrib[fj] {
			errs = append(errs, fmt.Errorf("%s: Forgejo repository %s is another pair's contrib repository", where, repo.Forgejo))
		}
		seenGitHub[gh], seenForgejo[fj] = true, true
		if !repo.Contrib.Empty() {
			// Branches in a contrib repository are named after GitHub pull
			// request numbers, which are only unique per repository. Two pairs
			// sharing one would both claim pr/1.
			c := strings.ToLower(repo.Contrib.String())
			if seenContrib[c] || seenForgejo[c] {
				errs = append(errs, fmt.Errorf("%s: contrib repository %s is used by another pair", where, repo.Contrib))
			}
			seenContrib[c] = true
		}

		c.Repositories = append(c.Repositories, repo)
	}

	c.Messages = f.Messages
	c.UserMapping = map[string]string{}
	for fj, gh := range f.UserMapping {
		if fj == "" || gh == "" {
			errs = append(errs, errors.New("userMapping entries need both a Forgejo and a GitHub login"))
			continue
		}
		c.UserMapping[fj] = gh
	}
	return errors.Join(errs...)
}

func orDefault(v, def string) string {
	if v = strings.TrimSpace(v); v != "" {
		return v
	}
	return def
}

func duration(env func(string, string) string, name, def string, minimum time.Duration, errs *[]error) time.Duration {
	raw := env(name, def)
	d, err := time.ParseDuration(raw)
	if err != nil || d < minimum {
		*errs = append(*errs, fmt.Errorf("%s%s must be a duration of at least %s, not %q", EnvPrefix, name, minimum, raw))
	}
	return d
}

func parseBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("%q is not an http(s) URL", raw)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("%q has no host", raw)
	}
	return u, nil
}

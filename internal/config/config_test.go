package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func env(t *testing.T, file string, overrides map[string]string) func(string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(file), 0o600); err != nil {
		t.Fatal(err)
	}
	vars := map[string]string{
		"BRIDGE_CONFIG":                  path,
		"BRIDGE_DATABASE_URL":            "postgres://x",
		"BRIDGE_GITHUB_APP_ID":           "123456",
		"BRIDGE_GITHUB_PRIVATE_KEY_FILE": "/k.pem",
		"BRIDGE_FORGEJO_URL":             "https://forgejo.internal/",
		"BRIDGE_FORGEJO_TOKEN":           "t",
	}
	for k, v := range overrides {
		vars[k] = v
	}
	return func(k string) string { return vars[k] }
}

const minimal = `
repositories:
  - github: metapatrol/foo
    forgejo: platform/foo
`

func TestDefaults(t *testing.T) {
	cfg, err := load(env(t, minimal, nil))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Repositories[0]
	if r.GitHub.String() != "metapatrol/foo" || r.Forgejo.String() != "platform/foo" {
		t.Errorf("repository = %+v", r)
	}
	if r.PublishLabel != "public" || r.ImportLabel != "github" || r.InternalPrefix != "/internal" {
		t.Errorf("defaults = %+v", r)
	}
	if cfg.PollInterval != time.Minute || cfg.FullResync != 6*time.Hour {
		t.Errorf("intervals = %s, %s", cfg.PollInterval, cfg.FullResync)
	}
	if cfg.ForgejoURL.String() != "https://forgejo.internal" {
		t.Errorf("a trailing slash must not survive into URLs built from the base: %s", cfg.ForgejoURL)
	}
}

func TestRefusals(t *testing.T) {
	for _, tc := range []struct {
		name, file string
		env        map[string]string
		want       string
	}{
		{name: "misspelt key", file: minimal + "    publishlabel: x\n", want: "publishlabel"},
		{name: "no repositories", file: "repositories: []\n", want: "no repositories"},
		{name: "bad reference", file: "repositories:\n  - github: foo\n    forgejo: a/b\n", want: "owner/name"},
		{name: "path traversal", file: "repositories:\n  - github: a/..\n    forgejo: a/b\n", want: "owner/name"},
		{name: "duplicate", file: minimal + "  - github: metapatrol/FOO\n    forgejo: platform/bar\n", want: "configured twice"},
		{name: "contrib is canonical", file: minimal + "    contrib: platform/foo\n", want: "contrib"},
		{name: "contrib without target branches", file: minimal + "    contrib: contrib/foo\n",
			env: map[string]string{"BRIDGE_FORGEJO_CONTRIB_TOKEN": "c"}, want: "targetBranches"},
		{name: "pull requests without contrib token", file: minimal + "    targetBranches: [develop]\n", want: "FORGEJO_CONTRIB_TOKEN"},
		{name: "odd target branch", file: minimal + "    contrib: contrib/foo\n    targetBranches: [\"-x\"]\n",
			env: map[string]string{"BRIDGE_FORGEJO_CONTRIB_TOKEN": "c"}, want: "not a branch name"},
		{name: "shared contrib repository", file: minimal + "    contrib: c/foo\n    targetBranches: [develop]\n" +
			"  - github: metapatrol/bar\n    forgejo: platform/bar\n    contrib: c/foo\n    targetBranches: [develop]\n",
			env: map[string]string{"BRIDGE_FORGEJO_CONTRIB_TOKEN": "c"}, want: "used by another pair"},
		{name: "contrib is another pair's canonical", file: minimal + "    contrib: platform/bar\n    targetBranches: [develop]\n" +
			"  - github: metapatrol/bar\n    forgejo: platform/bar\n",
			env: map[string]string{"BRIDGE_FORGEJO_CONTRIB_TOKEN": "c"}, want: "another pair's contrib"},
		{name: "bad import bound", file: minimal + "    importSince: yesterday\n", want: "importSince"},
		{name: "missing token", file: minimal, env: map[string]string{"BRIDGE_FORGEJO_TOKEN": ""}, want: "FORGEJO_TOKEN"},
		{name: "bad app id", file: minimal, env: map[string]string{"BRIDGE_GITHUB_APP_ID": "1.2e+07"}, want: "GITHUB_APP_ID"},
		{name: "poll too often", file: minimal, env: map[string]string{"BRIDGE_POLL_INTERVAL": "1s"}, want: "POLL_INTERVAL"},
		{name: "one connection", file: minimal, env: map[string]string{"BRIDGE_DATABASE_MAX_CONNS": "1"}, want: "DATABASE_MAX_CONNS"},
		{name: "plain host", file: minimal, env: map[string]string{"BRIDGE_FORGEJO_URL": "forgejo.internal"}, want: "FORGEJO_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(env(t, tc.file, tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want an error mentioning %q, have %v", tc.want, err)
			}
		})
	}
}

func TestExplicitlyEmptyInternalPrefix(t *testing.T) {
	cfg, err := load(env(t, minimal+"    internalPrefix: \"\"\n", nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repositories[0].InternalPrefix != "" {
		t.Errorf("an explicit empty prefix was replaced by %q", cfg.Repositories[0].InternalPrefix)
	}
}

func TestPullRequestSettings(t *testing.T) {
	cfg, err := load(env(t, minimal+"    contrib: github-contrib/foo\n    targetBranches: [develop]\n    rebaseRequests: true\n",
		map[string]string{"BRIDGE_FORGEJO_CONTRIB_TOKEN": "c"}))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Repositories[0]
	if !r.ImportsPullRequests() || !r.RebaseRequests || r.TargetBranches[0] != "develop" {
		t.Errorf("repository = %+v", r)
	}
	if len(r.CIPaths) != 2 || r.CIPaths[0] != ".forgejo/" {
		t.Errorf("CI paths default = %v", r.CIPaths)
	}
}

func TestMessages(t *testing.T) {
	cfg, err := load(env(t, minimal+"    messages:\n      merged: repo\nmessages:\n  merged: global\n  upToDate: yes\n", nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Messages["merged"] != "global" || cfg.Messages["upToDate"] != "yes" {
		t.Errorf("global messages = %v", cfg.Messages)
	}
	if cfg.Repositories[0].Messages["merged"] != "repo" {
		t.Errorf("repository messages = %v", cfg.Repositories[0].Messages)
	}
}

func TestSecretsFromFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cfg, err := load(env(t, minimal, map[string]string{
		"BRIDGE_DATABASE_URL":       "",
		"BRIDGE_DATABASE_URL_FILE":  write("dsn", "postgres://from-file\n"),
		"BRIDGE_FORGEJO_TOKEN":      "",
		"BRIDGE_FORGEJO_TOKEN_FILE": write("token", "  t0ken\n"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DatabaseURL != "postgres://from-file" || cfg.ForgejoToken != "t0ken" {
		t.Errorf("read %q and %q; want the files' contents without surrounding whitespace", cfg.DatabaseURL, cfg.ForgejoToken)
	}

	// The variable itself wins over its file.
	cfg, err = load(env(t, minimal, map[string]string{"BRIDGE_FORGEJO_TOKEN_FILE": write("other", "ignored")}))
	if err != nil || cfg.ForgejoToken != "t" {
		t.Errorf("token = %q, %v", cfg.ForgejoToken, err)
	}

	// A file that is named and missing is an error, not an empty secret.
	_, err = load(env(t, minimal, map[string]string{"BRIDGE_FORGEJO_TOKEN": "", "BRIDGE_FORGEJO_TOKEN_FILE": filepath.Join(dir, "missing")}))
	if err == nil || !strings.Contains(err.Error(), "FORGEJO_TOKEN_FILE") {
		t.Errorf("want an error naming FORGEJO_TOKEN_FILE, have %v", err)
	}
}

func TestPullRequestsWithoutAnExplicitContrib(t *testing.T) {
	cfg, err := load(env(t, minimal+"    targetBranches: [develop]\n", map[string]string{"BRIDGE_FORGEJO_CONTRIB_TOKEN": "c"}))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Repositories[0]
	if !r.ImportsPullRequests() || !r.Contrib.Empty() {
		t.Errorf("want pull requests on and the contrib repository left to the bridge: %+v", r)
	}
}

func TestTheConsoleIsOffUnlessAsked(t *testing.T) {
	cfg, err := load(env(t, minimal, nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Mode != ModeBridge || cfg.ConsoleAddr != "" {
		t.Errorf("mode %q, console %q", cfg.Mode, cfg.ConsoleAddr)
	}
	cfg, err = load(env(t, minimal, map[string]string{"BRIDGE_CONSOLE_ADDR": ":8090"}))
	if err != nil || cfg.ConsoleAddr != ":8090" {
		t.Errorf("console %q, %v", cfg.ConsoleAddr, err)
	}
}

func TestAConsoleNeedsOnlyTheDatabase(t *testing.T) {
	cfg, err := load(env(t, minimal+"    targetBranches: [develop]\n", map[string]string{
		"BRIDGE_MODE":                    "console",
		"BRIDGE_GITHUB_APP_ID":           "",
		"BRIDGE_GITHUB_PRIVATE_KEY_FILE": "",
		"BRIDGE_FORGEJO_TOKEN":           "",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConsoleAddr != ":8090" || cfg.ForgejoBrowserURL().String() != "https://forgejo.internal" {
		t.Errorf("console %q, Forgejo %s", cfg.ConsoleAddr, cfg.ForgejoBrowserURL())
	}
}

func TestModeRefusals(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
		want string
	}{
		{"an unknown mode", map[string]string{"BRIDGE_MODE": "both"}, "BRIDGE_MODE"},
		{"the console on the probe listener", map[string]string{"BRIDGE_CONSOLE_ADDR": ":8080"}, "BRIDGE_CONSOLE_ADDR"},
		{"a console without a database", map[string]string{"BRIDGE_MODE": "console", "BRIDGE_DATABASE_URL": ""}, "BRIDGE_DATABASE_URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := load(env(t, minimal, tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

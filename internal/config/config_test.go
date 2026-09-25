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

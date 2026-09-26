# Configuration

The bridge reads two things: environment variables for scalars and
credentials, and a YAML file for the repositories, the user mapping and the
messages. The Helm chart renders both from its values; the tables below name
the value that sets each.

Everything is read once, at startup. The chart rolls the pod when the
repository file changes.

**Contents**

- [Environment](#environment)
- [The repository file](#the-repository-file)
- [Messages](#messages)
- [Chart-only values](#chart-only-values)

## Environment

| Variable | Default | Chart value | |
|---|---|---|---|
| `BRIDGE_CONFIG` | — | (rendered) | path to the repository file. **Required** |
| `BRIDGE_DATABASE_URL` | — | `database.existingSecret`, `.existingSecretKey` (`uri`) | PostgreSQL DSN. **Required** |
| `BRIDGE_DATABASE_MAX_CONNS` | `5` | `database.maxConnections` | at least 2: the leader lock holds one |
| `BRIDGE_GITHUB_APP_ID` | — | `github.appID` | **Required** |
| `BRIDGE_GITHUB_PRIVATE_KEY_FILE` | — | `github.existingSecret`, `.existingSecretKey` (`private-key.pem`) | PEM, PKCS #1 or #8. **Required** |
| `BRIDGE_GITHUB_API_URL` | `https://api.github.com` | `github.apiURL` | only tests change it |
| `BRIDGE_FORGEJO_URL` | — | `forgejo.url` | the address the bridge talks to Forgejo on. **Required** |
| `BRIDGE_FORGEJO_WEB_URL` | — | `forgejo.webURL` | the address browsers use, when it differs; links starting with either never reach GitHub |
| `BRIDGE_FORGEJO_TOKEN` | — | `forgejo.existingSecret`, `.existingSecretKey` (`token`) | the `github-bridge` account's token. **Required** |
| `BRIDGE_FORGEJO_CONTRIB_TOKEN` | — | `forgejo.contribExistingSecret`, `.contribExistingSecretKey` (`token`) | the `github-contrib` account's token. Required as soon as a repository sets `contrib` |
| `BRIDGE_FORGEJO_CA_FILE` | — | `forgejo.caSecret`, `.caSecretKey` (`ca.crt`) | an extra CA to trust for Forgejo, over HTTPS and for git |
| `BRIDGE_GIT_CACHE_DIR` | `/var/cache/forgejo-github-bridge` | (an `emptyDir` of `gitCacheSize`) | bare repositories pull request heads pass through |
| `BRIDGE_POLL_INTERVAL` | `60s` | `poll.interval` | at least 10 s |
| `BRIDGE_FULL_RESYNC` | `6h` | `poll.fullResync` | at least 1 min |
| `BRIDGE_LISTEN_ADDR` | `:8080` | `port` | `/healthz`, `/readyz`, `/metrics` |
| `BRIDGE_LOG_FORMAT` | `json` | `logging.format` | or `text` |
| `BRIDGE_LOG_LEVEL` | `info` | `logging.level` | `debug`, `info`, `warn`, `error` |

`BRIDGE_DATABASE_URL`, `BRIDGE_FORGEJO_TOKEN` and
`BRIDGE_FORGEJO_CONTRIB_TOKEN` can instead be read from a file: set
`BRIDGE_DATABASE_URL_FILE` and so on to its path, as Docker secrets and systemd
credentials provide them. The variable itself wins if both are set; a named
file that does not exist stops the bridge.

`HOME` is set to `/tmp` by the chart and the image; git needs a home, and the
root filesystem is read-only.

## The repository file

```yaml
repositories:
  - github: example-org/foo              # required
    forgejo: platform/foo               # required
    importLabel: github                 # default
    publishLabel: public                # default
    internalPrefix: /internal           # default; "" makes every comment public
    importSince: "2026-09-01T00:00:00Z" # optional
    targetBranches: [develop]           # enables pull requests
    contrib: github-contrib/foo         # default <contrib login>/<name>
    rebaseRequests: false               # default
    ciPaths: [.forgejo/, .github/]      # default
    messages: {}                        # per-repository overrides

userMapping:
  jdoe: janedoe                   # Forgejo login: GitHub login

messages: {}                            # overrides for every repository
```

In the chart, `repositories`, `userMapping` and `messages` are values of the
same names.

### Repository fields

| Field | |
|---|---|
| `github` | the public repository, `owner/name` |
| `forgejo` | the canonical repository, `owner/name` |
| `importLabel` | put on every issue and pull request imported from GitHub, so maintainers can tell them apart. Created in Forgejo if missing |
| `publishLabel` | makes a Forgejo issue public. Opt-in: without it, nothing leaves Forgejo. Removing it freezes the pair |
| `internalPrefix` | a Forgejo comment starting with it stays in Forgejo, even on a bridged issue. Editing the prefix in or out withdraws or publishes the comment |
| `importSince` | GitHub issues opened before this RFC 3339 time are never imported. Unset imports every open one |
| `targetBranches` | the base branches a pull request may target. Naming any turns on importing pull requests. A pull request against another branch gets one explanation and waits |
| `contrib` | the Forgejo fork imported pull request heads are pushed to. Defaults to the contrib account's fork under the canonical repository's name, e.g. `github-contrib/foo`, which the bridge creates at startup if it is missing. Set it only for a fork under another name — two canonical repositories of the same name in different organisations need that. Its own per pair; never the canonical repository |
| `rebaseRequests` | ask contributors to rebase a pull request that is behind its base. Meant to go with a branch protection that blocks outdated merges, which the bridge cannot read itself |
| `ciPaths` | path prefixes whose change gets a pull request the `touches-ci` label and a warning. `.github/` is included by default because Forgejo runs `.github/workflows` when `.forgejo/workflows` does not exist |
| `messages` | see below |

The file is refused, with every problem listed, when: a key is unknown or
misspelt; a repository reference is not `owner/name`; a repository appears
twice on either side; `contrib` is the canonical repository, another pair's
canonical repository, or another pair's contrib; `contrib` is set without
`targetBranches`; `targetBranches` is set without the contrib token; a target
branch has an odd name.

With pull requests on, the bridge also refuses to start when `github-contrib`
can write to a canonical repository, when the contrib repository is not a
fork of it or has a push mirror, or when two pairs would default to the same
fork. It checks the write access again before every import.

### userMapping

Forgejo login to GitHub login, for maintainers. It decides only how people
are named when text crosses:

- A maintainer's comment on GitHub is attributed to their GitHub account;
  an unmapped maintainer appears as "a maintainer". A Forgejo login never
  appears on GitHub.
- `@jdoe` in Forgejo becomes `@janedoe` on GitHub, and `@janedoe` on GitHub
  becomes `@jdoe` in Forgejo.

It grants nothing.

## Messages

What the bridge says in its own voice is a set of Go templates with defaults
written for someone who took the time to contribute. Override any of them
under `messages`, globally or per repository; a repository's own win.

| Name | Where | When |
|---|---|---|
| `unsupportedBase` | GitHub | a pull request targets a branch not in `targetBranches` |
| `rebaseRequest` | GitHub | a pull request is behind its base, with `rebaseRequests` |
| `upToDate` | GitHub | the same comment, once rebased |
| `merged` | GitHub | after a fast-forward merge; GitHub shows it merged |
| `integrated` | GitHub | after a rebase merge; the bridge closes the pull request |
| `closedUpstream` | GitHub | maintainers closed the pull request without merging |
| `headFrozen` | Forgejo | someone other than the bridge pushed to the contrib branch |
| `touchesCI` | Forgejo | the pull request changes CI configuration |

Fields available to every template:

| Field | |
|---|---|
| `{{.Base}}` | the pull request's base branch |
| `{{.Targets}}` | the target branches, as prose: `` `develop` or `main` `` |
| `{{.Behind}}` | commits the base has that the pull request lacks |
| `{{.Commit}}`, `{{.ShortCommit}}` | the commit it was merged as |
| `{{.Branch}}` | the contrib branch |
| `{{.Paths}}` | the CI files a pull request changes, as a Markdown list |
| `{{.Contributor}}` | the pull request author's GitHub login |

A field a message has no value for renders empty, or `0` for `{{.Behind}}`. The defaults are in
[internal/translate/messages.go](../internal/translate/messages.go).

```yaml
messages:
  rebaseRequest: >-
    Hi @{{.Contributor}}, `{{.Base}}` has moved on by {{.Behind}} commit(s).
    Would you mind rebasing onto it? Thank you!
```

Templates are tried at startup. An unknown message name, a syntax error or an
unknown field stops the bridge, rather than failing on a contributor's pull
request.

Keep in mind how the rebase request behaves: a **new** comment each time the
pull request falls behind (GitHub notifies about new comments only), edits
while it stays behind, and never more than one. `@{{.Contributor}}` in it adds
nothing on the first comment — the author is notified anyway — but does no
harm.

## Chart-only values

| Value | Default | |
|---|---|---|
| `replicaCount` | `1` | a second replica only waits on the leader lock and shortens failover |
| `image.repository`, `.tag`, `.pullPolicy` | zot, chart's `appVersion` | a chart read from Git by Flux uses the commit's image |
| `gitCacheSize` | `1Gi` | size limit of the git cache |
| `resources` | 10m/32Mi requested, 500m/128Mi limit | |
| `serviceMonitor.enabled` | `false` | needs the Prometheus operator's CRDs |
| `networkPolicy.enabled` | `false` | ingress to the metrics port from `networkPolicy.metricsNamespaceSelector` only |

The complete list, generated from the chart, is in the
[chart README](../charts/forgejo-github-bridge/README.md).

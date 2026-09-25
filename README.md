# forgejo-github-bridge

Keeps issues and comments in step between a private Forgejo and the public
GitHub repositories it push-mirrors to.

Forgejo stays the place where development happens and stays unreachable from
the internet. GitHub is where outside contributors find the project, and they
use it the way they use any other GitHub project. The bridge runs next to
Forgejo, polls both sides, and carries issues and conversations across in
both directions. It has no public endpoint.

[CONCEPT.md](CONCEPT.md) is the design: what crosses the bridge and what never
does, and why. The [documentation](docs/README.md) covers the rest:
[architecture](docs/architecture.md), [installation](docs/installation.md),
[configuration](docs/configuration.md), [operations](docs/operations.md),
[working with it as a maintainer](docs/maintainers.md),
[security](docs/security.md) and [development](docs/development.md).

## Status

Phases 1 and 2 of [CONCEPT.md §19](CONCEPT.md#19-phases) are implemented and
have passed end-to-end tests against GitHub and Forgejo 16, including a stress
run with concurrent issues and pull requests. An optional read-only API and
console are proposed as phase 4 ([§23](CONCEPT.md#23-inspection-api-and-console-proposal)).

Issues:

- GitHub issues are imported into Forgejo, labelled `github`.
- Forgejo issues are published to GitHub only when labelled `public`.
- Comments flow both ways on bridged issues. A Forgejo comment starting with
  `/internal` stays in Forgejo.
- Closing and reopening works from either side.
- Edits follow the side the item was written on. Deleted comments are
  withdrawn at the next full resync.
- `#N` references, `@mentions` and links into Forgejo are translated
  ([§13](CONCEPT.md#13-content-translation)).

Pull requests, for repositories that configure a contrib fork:

- A GitHub pull request against one of the `targetBranches` is pushed to
  `pr/<n>` in the contrib fork and opened in Forgejo by the `github-contrib`
  account, which has no write access, so its CI waits for approval.
- New commits follow. A branch somebody else pushed to is never overwritten;
  the bridge stops importing into it and says so.
- Changes under `.forgejo/` or `.github/` get the label `touches-ci` and a
  warning.
- Conversation comments cross both ways; Forgejo reviews appear on GitHub as
  comments.
- A merge in Forgejo is reported on GitHub once the push mirror has brought
  it there: GitHub marks a fast-forwarded pull request merged by itself; a
  rebased one is closed with a comment naming the commit.
- Closing, withdrawing and reopening work from either side.
- With `rebaseRequests`, a pull request behind its base gets one comment
  asking for a rebase, kept up to date.

## How it works

```text
poll GitHub ─┐                              ┌─> GitHub API
             ├─> dirty queue ─> reconcile ──┤
poll Forgejo ┘   (PostgreSQL)               └─> Forgejo API
```

Polls only mark issues as changed. The reconciler then reads the current state
of both sides and makes them agree, so running it twice does what running it
once did, and every failure is handled the same way: the issue stays queued
and is retried with backoff.

The mappings between GitHub and Forgejo items live in PostgreSQL. Every
projection also carries a hidden marker, from which the mappings can be
rebuilt if the database is lost. Markers are only ever read from items the
bridge's own accounts wrote.

Only one instance works at a time. It holds a PostgreSQL advisory lock; a
second replica waits on it and takes over when the first one's session ends.

## Setup

Once per installation:

1. **A database.** A CloudNativePG `Cluster`, or a database in an existing
   one. The chart reads the connection string from the app Secret CNPG writes.
   [samples/cnpg-cluster.yaml](samples/cnpg-cluster.yaml) is a minimal one.
2. **A GitHub App.** Any name; the bridge reads it from GitHub.
   - Permissions: *Metadata* read, *Issues* read and write, *Pull requests*
     read and write.
   - Webhook: off. Untick *Active*; the bridge polls.
   - Put the App's private key into a Secret.
3. **A Forgejo account** for the bridge, conventionally `github-bridge`, with
   write access to each bridged repository and a token with the
   `write:issue`, `read:repository` and `read:user` scopes. Put the token into
   a Secret.
4. **For pull requests, a second Forgejo account**, conventionally
   `github-contrib`, with read access only to the canonical repositories and
   a token with `write:repository` and `read:user`. It owns the contrib forks
   and opens the imported pull requests. Never approve its workflow runs with
   *Approve always* ([CONCEPT.md §12](CONCEPT.md#12-ci-isolation)).

Per maintainer who merges in Forgejo: set the account's primary e-mail to
the address of their GitHub account (or its `…@users.noreply.github.com`
address). Merges record the merging account as committer, and the push mirror
publishes it; an internal address would end up public and unlinked
([CONCEPT.md §17](CONCEPT.md#17-deployment)).

Per bridged repository:

1. Install the GitHub App on the GitHub repository, and only there.
2. Make sure Forgejo's push mirror to it has *sync on commit* enabled.
3. For pull requests: fork the canonical repository as `github-contrib`, with
   no push mirror, no secrets and no variables, and verify with a test pull
   request that workflow runs from it wait for approval.
4. Add the pair to `repositories` in the chart values.

[samples/values-example.yaml](samples/values-example.yaml) puts it together.

```bash
helm upgrade --install forgejo-github-bridge \
  oci://ghcr.io/metapatrol/charts/forgejo-github-bridge --version 0.1.0 \
  --namespace forgejo-github-bridge --create-namespace \
  -f samples/values-example.yaml
```

Images (`linux/amd64`) are at
`ghcr.io/metapatrol/forgejo-github-bridge`, charts at
`oci://ghcr.io/metapatrol/charts/`, and binaries on the
[releases page](https://github.com/metapatrol/forgejo-github-bridge/releases).
The step-by-step version, with the checks to run before the first real
contribution, is [docs/installation.md](docs/installation.md).

## Configuration

The chart renders all of this; the table is for running the binary directly.

| variable | default | |
|---|---|---|
| `BRIDGE_CONFIG` | — | the repository file, below. Required. |
| `BRIDGE_DATABASE_URL` | — | PostgreSQL DSN. Required. |
| `BRIDGE_DATABASE_MAX_CONNS` | `5` | at least 2: the leader lock holds one |
| `BRIDGE_GITHUB_APP_ID` | — | Required. |
| `BRIDGE_GITHUB_PRIVATE_KEY_FILE` | — | PEM, PKCS #1 or #8. Required. |
| `BRIDGE_GITHUB_API_URL` | `https://api.github.com` | |
| `BRIDGE_FORGEJO_URL` | — | the address the bridge talks to Forgejo on. Required. |
| `BRIDGE_FORGEJO_WEB_URL` | — | the address browsers use, when it differs. Links starting with either never reach GitHub. |
| `BRIDGE_FORGEJO_TOKEN` | — | the `github-bridge` account's token. Required. |
| `BRIDGE_FORGEJO_CONTRIB_TOKEN` | — | the `github-contrib` account's token; required when a repository sets `contrib` |
| `BRIDGE_FORGEJO_CA_FILE` | — | an extra CA to trust for Forgejo |
| `BRIDGE_GIT_CACHE_DIR` | `/var/cache/forgejo-github-bridge` | bare repositories pull request heads pass through |
| `BRIDGE_POLL_INTERVAL` | `60s` | |
| `BRIDGE_FULL_RESYNC` | `6h` | how often every open pair is reconciled regardless of polls |
| `BRIDGE_LISTEN_ADDR` | `:8080` | `/healthz`, `/readyz`, `/metrics` |
| `BRIDGE_LOG_FORMAT` | `json` | or `text` |
| `BRIDGE_LOG_LEVEL` | `info` | |

The repository file:

```yaml
repositories:
  - github: example-org/foo          # the public repository
    forgejo: platform/foo           # its source of truth
    publishLabel: public            # default
    importLabel: github             # default
    internalPrefix: /internal       # default; "" makes every comment public
    importSince: "2026-09-01T00:00:00Z"  # optional: never import older GitHub issues
    contrib: github-contrib/foo     # pull requests: the fork heads are pushed to
    targetBranches: [develop]       # required with contrib
    rebaseRequests: false           # ask contributors to rebase outdated PRs
    ciPaths: [.forgejo/, .github/]  # default
userMapping:
  jdoe: janedoe                     # Forgejo login: GitHub login
```

### Messages

What the bridge says in its own voice is configurable, globally under
`messages` or per repository. Each is a Go template; only the ones to change
need to be given.

| name | said | default |
|---|---|---|
| `unsupportedBase` | on GitHub, to a pull request against a branch not in `targetBranches` | Thanks for the contribution! This project takes pull requests against {{.Targets}}, and this one targets `{{.Base}}`. Could you switch the base branch? … |
| `rebaseRequest` | on GitHub, when a pull request falls behind (with `rebaseRequests`) | Thanks for your patience! `{{.Base}}` has moved on by {{.Behind}} commit(s) … could you rebase onto `{{.Base}}`? … |
| `upToDate` | the same comment, once rebased | Thanks, this is up to date with `{{.Base}}` now. |
| `merged` | on GitHub, after a fast-forward merge | Merged into `{{.Base}}` as {{.Commit}}. Thank you for contributing! |
| `integrated` | on GitHub, after a rebase merge | Merged into `{{.Base}}` as {{.Commit}}. Your commits were rebased … |
| `closedUpstream` | on GitHub, when maintainers close without merging | The maintainers have closed this pull request without merging it. Thank you … |
| `headFrozen` | in Forgejo, when someone else pushed to the contrib branch | The branch `{{.Branch}}` was changed by someone other than the bridge … |
| `touchesCI` | in Forgejo, when a pull request changes CI configuration | **This pull request changes CI configuration:** {{.Paths}} … |

Fields: `{{.Base}}`, `{{.Targets}}`, `{{.Behind}}`, `{{.Commit}}`,
`{{.ShortCommit}}`, `{{.Branch}}`, `{{.Paths}}`, `{{.Contributor}}`. The full
defaults are in `internal/translate/messages.go`.

```yaml
messages:
  rebaseRequest: >-
    Hi @{{.Contributor}}, `{{.Base}}` has moved on by {{.Behind}} commit(s).
    Would you mind rebasing onto it? Thank you!
```

`userMapping` is for mentions and attribution only. A maintainer who is not
mapped appears on GitHub as "a maintainer"; their Forgejo login never does.

## Metrics

| metric | |
|---|---|
| `forgejo_github_bridge_last_successful_poll_timestamp_seconds` | per repository and side; alert when it stops moving |
| `forgejo_github_bridge_poll_errors_total` | |
| `forgejo_github_bridge_dirty_queue_depth` | |
| `forgejo_github_bridge_dirty_oldest_age_seconds` | alert when it grows: something keeps failing |
| `forgejo_github_bridge_dirty_failing` | resources whose last reconcile failed; alert when above zero for, say, 30 minutes — the log says why |
| `forgejo_github_bridge_reconciles_total` | by outcome |
| `forgejo_github_bridge_changes_total` | writes, by side, kind and action |
| `forgejo_github_bridge_github_rate_limit_remaining` | |
| `forgejo_github_bridge_leader` | 1 on the active instance |

The [dashboard chart](charts/forgejo-github-bridge-grafana-dashboard/README.md)
ships a Grafana dashboard for all of them.

## Development

```bash
make test               # unit tests, no dependencies
make integration-test   # against a throwaway PostgreSQL in Docker
make lint               # golangci-lint, the version CI uses
make helm-verify        # renders the chart and checks what it must refuse
```

CI is described in [.forgejo/README.md](.forgejo/README.md).

## Layout

    cmd/forgejo-github-bridge   the binary
    internal/bridge             polls, queue worker and reconciler
    internal/translate          references, mentions, links, attribution
    internal/github             GitHub App client
    internal/forgejo            Forgejo client
    internal/rest               the JSON client both share
    internal/gitops             moves pull request heads into the contrib fork
    internal/store              state: memory for tests, postgres for real
    internal/marker             the hidden provenance line
    charts/                     the Helm chart
    tests/integration           against real PostgreSQL

## Contributing

This project is developed in a private Forgejo and published here — through
itself. Issues and pull requests opened on GitHub are carried into Forgejo by
the bridge, and the maintainers' answers come back the same way. See
[CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache License 2.0, see [LICENSE](LICENSE).

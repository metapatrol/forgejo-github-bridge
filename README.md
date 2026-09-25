# forgejo-github-bridge

Keeps issues and comments in step between a private Forgejo and the public
GitHub repositories it push-mirrors to.

Forgejo stays the place where development happens and stays unreachable from
the internet. GitHub is where outside contributors find the project, and they
use it the way they use any other GitHub project. The bridge runs next to
Forgejo, polls both sides, and carries issues and conversations across in
both directions. It has no public endpoint.

[CONCEPT.md](CONCEPT.md) is the design: what crosses the bridge and what never
does, and why.

## Status

Phase 1 of [CONCEPT.md §19](CONCEPT.md#19-phases) is implemented:

- GitHub issues are imported into Forgejo, labelled `github`.
- Forgejo issues are published to GitHub only when labelled `public`.
- Comments flow both ways on bridged issues. A Forgejo comment starting with
  `/internal` stays in Forgejo.
- Closing and reopening works from either side.
- Edits follow the side the item was written on. Deleted comments are
  withdrawn at the next full resync.
- `#N` references, `@mentions` and links into Forgejo are translated
  ([§13](CONCEPT.md#13-content-translation)).

Pull requests (phase 2) are not imported yet; they are skipped.

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

Per bridged repository:

1. Install the GitHub App on the GitHub repository, and only there.
2. Make sure Forgejo's push mirror to it has *sync on commit* enabled.
3. Add the pair to `repositories` in the chart values.

[samples/values-k3s-dev.yaml](samples/values-k3s-dev.yaml) puts it together.

```bash
helm upgrade --install forgejo-github-bridge charts/forgejo-github-bridge \
  --kube-context k3s-dev --namespace forgejo-github-bridge --create-namespace \
  -f samples/values-k3s-dev.yaml
```

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
| `BRIDGE_FORGEJO_CA_FILE` | — | an extra CA to trust for Forgejo |
| `BRIDGE_POLL_INTERVAL` | `60s` | |
| `BRIDGE_FULL_RESYNC` | `6h` | how often every open pair is reconciled regardless of polls |
| `BRIDGE_LISTEN_ADDR` | `:8080` | `/healthz`, `/readyz`, `/metrics` |
| `BRIDGE_LOG_FORMAT` | `json` | or `text` |
| `BRIDGE_LOG_LEVEL` | `info` | |

The repository file:

```yaml
repositories:
  - github: metapatrol/foo          # the public repository
    forgejo: platform/foo           # its source of truth
    publishLabel: public            # default
    importLabel: github             # default
    internalPrefix: /internal       # default; "" makes every comment public
    importSince: "2026-09-01T00:00:00Z"  # optional: never import older GitHub issues
userMapping:
  ska: denisneuling                 # Forgejo login: GitHub login
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
| `forgejo_github_bridge_reconciles_total` | by outcome |
| `forgejo_github_bridge_changes_total` | writes, by side, kind and action |
| `forgejo_github_bridge_github_rate_limit_remaining` | |
| `forgejo_github_bridge_leader` | 1 on the active instance |

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
    internal/store              state: memory for tests, postgres for real
    internal/marker             the hidden provenance line
    charts/                     the Helm chart
    tests/integration           against real PostgreSQL

## License

Apache License 2.0, see [LICENSE](LICENSE).

# Operations

**Contents**

- [Health](#health)
- [Metrics](#metrics)
- [Logs](#logs)
- [When something is stuck](#when-something-is-stuck)
- [Upgrades](#upgrades)
- [Rotating credentials](#rotating-credentials)
- [Losing the database](#losing-the-database)
- [Removing a repository](#removing-a-repository)
- [Pausing](#pausing)

## Health

| Endpoint | |
|---|---|
| `GET /healthz` | the process is up |
| `GET /readyz` | the database answers. A replica waiting for the leader lock is ready too: readiness is about whether it could take over |
| `GET /metrics` | Prometheus |

All three on `BRIDGE_LISTEN_ADDR` (`:8080`), cluster-internal. The bridge has
no public endpoint.

## Metrics

| Metric | Labels | |
|---|---|---|
| `forgejo_github_bridge_leader` | | 1 on the instance doing the work |
| `forgejo_github_bridge_last_successful_poll_timestamp_seconds` | repository, side | when a poll last succeeded |
| `forgejo_github_bridge_poll_errors_total` | repository, side | |
| `forgejo_github_bridge_poll_duration_seconds` | repository, side | histogram |
| `forgejo_github_bridge_resources_marked_total` | repository, source (`poll`, `resync`) | |
| `forgejo_github_bridge_dirty_queue_depth` | | queued resources |
| `forgejo_github_bridge_dirty_failing` | | queued resources whose last reconcile failed |
| `forgejo_github_bridge_dirty_oldest_age_seconds` | | how long the oldest entry has waited |
| `forgejo_github_bridge_reconciles_total` | repository, outcome (`ok`, `error`) | |
| `forgejo_github_bridge_changes_total` | repository, side, kind, action | every write the bridge makes |
| `forgejo_github_bridge_github_rate_limit_remaining` | | as of the last GitHub response |
| `forgejo_github_bridge_build_info` | version, commit, built_at | |

The queue gauges are refreshed every 15 seconds.

A Grafana dashboard covering all of them ships as its own chart,
[forgejo-github-bridge-grafana-dashboard](../charts/forgejo-github-bridge-grafana-dashboard/README.md),
for the namespace the Grafana sidecar watches.

### Alerts worth having

```yaml
groups:
  - name: forgejo-github-bridge
    rules:
      - alert: ForgejoGitHubBridgeNotWorking
        expr: max(forgejo_github_bridge_leader) < 1 or absent(forgejo_github_bridge_leader)
        for: 10m
      - alert: ForgejoGitHubBridgePollsFailing
        expr: time() - forgejo_github_bridge_last_successful_poll_timestamp_seconds > 900
        for: 5m
      - alert: ForgejoGitHubBridgeStuck
        expr: forgejo_github_bridge_dirty_failing > 0
        for: 30m
      - alert: ForgejoGitHubBridgeRateLimitLow
        expr: forgejo_github_bridge_github_rate_limit_remaining < 500
        for: 10m
```

A failing entry that waits for the push mirror after a merge counts as
failing too, briefly; `for: 30m` keeps that from alerting.

## Logs

Structured, one event per line (`json` by default). The ones that matter:

| Message | Level | Meaning |
|---|---|---|
| `holding the leader lock; this instance does the work` | info | this replica is active |
| `another instance is active; waiting for the leader lock` | info | normal for a second replica, and for a few seconds during a rollout |
| `imported a GitHub issue` / `published a Forgejo issue` / `imported a GitHub pull request` | info | a new pair |
| `imported new commits` | info | a contributor pushed |
| `created the contrib fork` | info | first start with pull requests on for a repository |
| `not importing a pull request` | error | `github-contrib` can write to the canonical repository. Take the write access away; the import follows |
| `an imported pull request was merged` | info | |
| `merged; waiting for the push mirror` | info | retried until GitHub has the merge |
| `poll failed` | error | a forge was unreachable or refused; retried next interval |
| `reconcile failed; retrying later` | warn | carries `error`, `attempt`, `retry_in` |
| `the contrib branch was moved by someone else; no longer importing into it` | warn | expected after a maintainer pushed to `pr/<n>` |
| `rebuilt issue mappings from the bridge's own projections` / `adopted a projection the database did not know` / `found an unrecorded projection instead of creating a second one` | warn | recovery; expected after losing the database, otherwise worth a look |
| `the connection holding the leader lock failed; stopping work` | error | the process exits and restarts |

## When something is stuck

`forgejo_github_bridge_dirty_failing` above zero, or a user reports that
something did not cross.

1. Find the error. The log line `reconcile failed; retrying later` carries it,
   and the queue keeps the last one:

   ```sql
   SELECT repository, side, number, attempts, not_before, last_error
   FROM dirty WHERE attempts > 0;
   ```

   Run it with `psql` against the bridge's database, with the same connection
   string the bridge uses.

2. Common causes:

   | `last_error` says | Cause | Fix |
   |---|---|---|
   | `the GitHub App is not installed on …` | App not installed on that repository | install it (installation, part 5) |
   | `HTTP 401` / `HTTP 403` from Forgejo | token revoked, or the account lacks access | new token, or grant access |
   | `HTTP 403` on labels or closing | `github-bridge` has read, not write | grant write |
   | `branch pr/<n> already exists in … and was not made by the bridge` | someone created that branch in the contrib fork | delete the branch there; the import follows |
   | `the contrib account has write access to the canonical repository` | `github-contrib` was given write, admin or a team with write | take it away; imports are held until then |
   | `waiting for the push mirror` for long | the push mirror is failing or has no sync-on-commit | check the mirror in the repository's settings |
   | `fetching GitHub pull request #<n>, anonymously and with the App's token` | the repository is private and the App lacks *Contents: read* | grant it in the App's permissions, and accept the change on the installation |
   | `fetching GitHub pull request #<n>` | GitHub unreachable | wait; it is retried |

3. After fixing the cause, nothing needs to be done: the entry is retried at
   the latest after 30 minutes. To retry now, make it due:

   ```sql
   UPDATE dirty SET not_before = now() WHERE attempts > 0;
   ```

A restart does not requeue anything. To have an item reconciled that is not
queued at all, touch it on either side (a comment, an edit), or wait for the
full resync.

## Upgrades

`helm upgrade` with the new chart. The new pod starts, applies migrations
(under a lock, idempotent), waits for the leader lock, and takes over when the
old pod stops — typically within five seconds. Nothing is lost: the queue is
in the database.

A migration is never edited once released; a changed one stops the start with
`migration … was applied with a different content`.

## Rotating credentials

| Credential | How |
|---|---|
| GitHub App key | generate a new key on the App's page, replace the Secret, restart the bridge, delete the old key on the App's page |
| `github-bridge` / `github-contrib` token | create a new token, replace the Secret, restart the bridge, delete the old token |
| database password | change it where the database is run, update the bridge's connection string, restart the bridge |

The bridge reads credentials at startup, so every rotation needs a restart:

| | |
|---|---|
| Helm | `kubectl -n forgejo-github-bridge rollout restart deploy/forgejo-github-bridge` |
| Docker Compose | `docker compose restart bridge` |
| systemd | `systemctl restart forgejo-github-bridge` |

## Losing the database

Restore the database from a backup if there is one. If there is not, start
the bridge against an empty database. It rebuilds what it can from the
markers in its own projections:

- On the first poll of each side, it reads every issue and pull request its
  own accounts opened and records the pairs their markers name.
- Comments are adopted by their markers when their issue is next reconciled.
- Pull request state is taken from Forgejo: the contrib branch as it is, open
  or merged or closed as Forgejo says.

What is lost: poll cursors (the first poll reads everything again, which is
harmless), the queue (the first poll and the full resync fill it again), and
which rebase comment is the current one — an outdated pull request may get a
second request, the first one staying as it was. Pull requests that were
merged or closed already are taken as reported and are not reported again.

## Removing a repository

Remove it from `repositories` and upgrade. The bridge stops touching it;
queued entries for it are dropped as they come up. Its mappings stay in the
database and are harmless. Uninstall the App from the GitHub repository and
remove the accounts' access in Forgejo as needed.

## Pausing

Stop the bridge. Nothing is lost; when it starts again it polls from its
cursors and catches up.

| | |
|---|---|
| Helm | `kubectl -n forgejo-github-bridge scale deploy/forgejo-github-bridge --replicas=0` |
| Docker Compose | `docker compose stop bridge` |
| systemd | `systemctl stop forgejo-github-bridge` |

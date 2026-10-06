# Installation

From nothing to a bridged repository.

The bridge is one process with three needs: a PostgreSQL database, outbound
HTTPS to GitHub and to your Forgejo, and a few secrets. Where it runs is up to
you. This guide covers two ways — a Helm chart on Kubernetes, and a container
with Docker Compose — and the binary runs just as well under systemd.

The examples use Forgejo at `forgejo.example.com`, the GitHub organisation
`example-org`, and one repository pair:

| | |
|---|---|
| canonical, in Forgejo | `platform/foo` |
| public, on GitHub | `example-org/foo` |
| contrib fork, in Forgejo | `github-contrib/foo` (only for pull requests; the bridge creates it) |

Parts 1 to 3 are done once per installation, part 4 once per maintainer,
part 5 once per repository.

**Contents**

- [1. A PostgreSQL database](#1-a-postgresql-database)
- [2. The GitHub App](#2-the-github-app)
- [3. The Forgejo accounts](#3-the-forgejo-accounts)
- [4. Per maintainer](#4-per-maintainer)
- [5. Per repository](#5-per-repository)
- [6. Deploy](#6-deploy)
  - [With Helm on Kubernetes](#with-helm-on-kubernetes)
  - [With Docker Compose](#with-docker-compose)
  - [Anywhere else](#anywhere-else)
- [7. Verify before the first real contribution](#7-verify-before-the-first-real-contribution)

## 1. A PostgreSQL database

Any PostgreSQL from version 14 on will do: a managed service, a database in an
existing server, an operator on Kubernetes, or the container in the Compose
file below. The bridge needs one database and a user that owns it; it creates
and migrates its own schema at startup.

```sql
CREATE USER bridge WITH PASSWORD '…';
CREATE DATABASE bridge OWNER bridge;
```

The bridge takes a connection string:

```text
postgres://bridge:…@db.example.com:5432/bridge?sslmode=require
```

The state is small — mappings, cursors, a queue — and back it up the way you
back up your other databases. The bridge can rebuild most of it without a
backup ([operations](operations.md#losing-the-database)), but not all of it.

On Kubernetes with [CloudNativePG](https://cloudnative-pg.io/),
[samples/cloudnative-pg.yaml](../samples/cloudnative-pg.yaml) is a minimal
cluster; CloudNativePG writes the connection string into a Secret under the
key `uri`, which is what the Helm chart reads by default.

## 2. The GitHub App

Create it in the organisation:
`https://github.com/organizations/<your-org>/settings/apps/new`

| Field | Value |
|---|---|
| GitHub App name | anything, unique on GitHub. Comments appear as `<slug>[bot]`; the bridge reads the name itself, so renaming later needs no change |
| Homepage URL | anything, e.g. the organisation's page |
| Callback URL, OAuth during installation | empty, off |
| Webhook → Active | **off**: the bridge polls |
| Repository permissions | Issues: **Read and write**; Pull requests: **Read and write**. Metadata: read-only is set automatically |
| … for a private repository | also Contents: **Read-only**, so the bridge can fetch pull requests with the App's token. A public repository does not need it |
| Everything else | No access |
| Where can it be installed | Only on this account |

Then, on the App's page:

1. Note the **App ID**.
2. *Private keys → Generate a private key*. Keep the downloaded `.pem` out of
   chats and tickets; it goes straight into your deployment's secrets (part 6).

The App does not need an OAuth client secret; if GitHub generated one, delete
it.

## 3. The Forgejo accounts

### github-bridge

The account that writes issues and comments.

1. Create a user, e.g. `github-bridge`.
2. Give it **write** access to each canonical repository that will be bridged
   (collaborator, or a team with write on issues and pull requests).
3. Create a token for it with the scopes `write:issue`, `read:repository` and
   `read:user`.

A token can be created without logging in as the account:

```bash
curl -u '<login-or-email>:<password>' -H 'Content-Type: application/json' \
  -X POST https://forgejo.example.com/api/v1/users/github-bridge/tokens \
  -d '{"name":"forgejo-github-bridge","scopes":["write:issue","read:repository","read:user"]}'
```

### github-contrib

Only needed for pull requests. The account imported pull requests are opened
by.

1. Create a user, e.g. `github-contrib`.
2. Give it **read access only** to the canonical repositories — no write, not
   in any team with write on Actions, never an owner or admin. This is what
   makes Forgejo hold its pull requests' workflow runs for approval
   ([security](security.md#ci)).
3. Create a token with `write:repository` and `read:user`.

The bridge refuses to start if both tokens belong to the same account, and
if `github-contrib` can write to a canonical repository.

## 4. Per maintainer

Every maintainer who merges in Forgejo sets their account's **primary
e-mail** to an address that may be public and belongs to their GitHub
account: the address verified on GitHub, or their
`…@users.noreply.github.com` address.

A merge through Forgejo's web interface records the merging account as the
committer of every commit, and the push mirror publishes it. With an internal
address, that address ends up public on GitHub and the committer is linked to
nobody.

## 5. Per repository

### On GitHub

1. Create the repository, e.g. `example-org/foo`. The bridge fetches pull
   requests anonymously from a public repository, and with the App's token
   from a private one — which is when the App needs *Contents: read*.
2. Set its default branch to the branch contributions go to, e.g. `develop`
   (it exists once the mirror has pushed it).
3. Install the App on it: App page → *Install App* → the organisation →
   *Only select repositories* → `foo`.

### In Forgejo

1. Add a push mirror on `platform/foo` to `ssh://git@github.com/example-org/foo.git`
   with **sync on commit** enabled, so merges reach GitHub within seconds.
2. Give `github-bridge` write access (part 3).
3. Keep the merge style *rebase, then fast-forward*. To have GitHub show every
   imported pull request as merged, also block merging outdated pull requests
   in the base branch's protection, and set `rebaseRequests: true` below.

For pull requests, additionally:

4. Give `github-contrib` read access to `platform/foo`.

The contrib fork, `github-contrib/foo`, is the bridge's: it creates it at
startup, and refuses to import into one that is not a fork of `platform/foo`
or has a push mirror. Add no Actions secrets or variables to it.

### In the bridge's configuration

```yaml
repositories:
  - github: example-org/foo
    forgejo: platform/foo
    # for pull requests:
    targetBranches: [develop]
    rebaseRequests: true
userMapping:
  jdoe: janedoe    # Forgejo login: GitHub login
```

With Helm, this goes into the values; everywhere else, into the
configuration file (`BRIDGE_CONFIG`). Every field is described in
[configuration](configuration.md#the-repository-file).

## 6. Deploy

Whichever way, the bridge needs the same things:

| | |
|---|---|
| database | the connection string from part 1 |
| GitHub | the App ID and its private key, as a file |
| Forgejo | its URL, and the two tokens from part 3 |
| repositories | the configuration from part 5 |

Only one instance does the work at a time; a second one waits and takes over
if the first stops. One is enough.

### With Helm on Kubernetes

Store the credentials as Secrets:

```bash
kubectl create namespace forgejo-github-bridge
kubectl -n forgejo-github-bridge create secret generic forgejo-github-bridge-db \
  --from-literal=uri='postgres://bridge:…@db.example.com:5432/bridge?sslmode=require'
kubectl -n forgejo-github-bridge create secret generic forgejo-github-bridge-github \
  --from-file=private-key.pem=./<app>.private-key.pem
kubectl -n forgejo-github-bridge create secret generic forgejo-github-bridge-forgejo \
  --from-literal=token=<github-bridge token>
kubectl -n forgejo-github-bridge create secret generic forgejo-github-bridge-contrib \
  --from-literal=token=<github-contrib token>
```

A values file, starting from
[samples/values-example.yaml](../samples/values-example.yaml):

```yaml
database:
  existingSecret: forgejo-github-bridge-db
github:
  appID: 123456
  existingSecret: forgejo-github-bridge-github
forgejo:
  url: https://forgejo.example.com
  existingSecret: forgejo-github-bridge-forgejo
  contribExistingSecret: forgejo-github-bridge-contrib
repositories:
  - github: example-org/foo
    forgejo: platform/foo
    targetBranches: [develop]
    rebaseRequests: true
userMapping:
  jdoe: janedoe
```

If the bridge reaches Forgejo on an in-cluster address, set `forgejo.url` to
that and `forgejo.webURL` to the address people's browsers use: links starting
with either are recognised as internal and kept off GitHub.

```bash
helm upgrade --install forgejo-github-bridge \
  oci://ghcr.io/metapatrol/charts/forgejo-github-bridge \
  -n forgejo-github-bridge -f values.yaml
```

Every value is listed in the [chart's README](../charts/forgejo-github-bridge/README.md).

To also get the read-only [console](console.md), add `console.enabled: true`
and open it with a port-forward or behind your authenticating proxy.

### With Docker Compose

[samples/compose/](../samples/compose/) has a complete setup: the bridge and a
PostgreSQL container, with the configuration and the secrets as files next to
it.

```bash
cp -r samples/compose forgejo-github-bridge && cd forgejo-github-bridge
cp .env.example .env              # fill in the App ID, Forgejo URL and database password
cp ~/Downloads/<app>.private-key.pem secrets/github-app.pem
echo -n '<github-bridge token>'  > secrets/forgejo-token
echo -n '<github-contrib token>' > secrets/forgejo-contrib-token
$EDITOR config.yaml               # the repositories from part 5
docker compose up -d
docker compose logs -f bridge
```

To use a database you already have, remove the `postgres` service and point
`BRIDGE_DATABASE_URL` at it.

### Anywhere else

The bridge is one static binary, also available from the
[releases page](https://github.com/metapatrol/forgejo-github-bridge/releases),
configured entirely through environment variables and one YAML file. It needs
`git` on its `PATH` for pull requests, and a writable directory for its git
cache. [Configuration](configuration.md#environment) lists every variable.

### A healthy start

```text
msg="starting forgejo-github-bridge" version=… repositories="[example-org/foo=platform/foo]"
msg="applying migration" name=0001_initial.sql            (first start only)
msg="holding the leader lock; this instance does the work"
msg="identities resolved" github=<slug>[bot] forgejo=github-bridge contrib=github-contrib
msg="created the contrib fork" repository=example-org/foo fork=github-contrib/foo   (first start only)
```

## 7. Verify before the first real contribution

Run these once per repository, in this order. The second one is not optional.

1. **Issues.** Open an issue on GitHub. Within a minute it appears in Forgejo
   with the `github` label. Comment on both sides; each comment appears on the
   other. Close it in Forgejo with a comment; on GitHub the comment arrives
   before the close.
2. **The CI approval gate.** Open a pull request on GitHub from a fork. It
   appears in Forgejo opened by `github-contrib`. In the repository's
   *Actions*, its workflow run must be **blocked**, waiting for approval. If
   it runs without approval, stop: `github-contrib` has more than read access,
   or is trusted. Fix that before anything else goes through the bridge.
3. **Approve once.** Approve that run with *Approve once* and see it run.
   Never *Approve always* ([maintainers](maintainers.md#approving-ci)).
4. **Merge.** Merge the pull request in Forgejo. Within a minute of the push
   mirror running, the GitHub pull request shows the bridge's note, and is
   either marked merged (fast-forward) or closed with the commit (rebase).
5. **Privacy.** Open a Forgejo issue without the `public` label. It must not
   appear on GitHub.

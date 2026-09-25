# Installation

From nothing to a bridged repository. The examples use Forgejo at
`forgejo.example.com`, the GitHub organisation `example-org`, a namespace
`forgejo-github-bridge` in a Kubernetes cluster with CloudNativePG, and one
repository pair:

| | |
|---|---|
| canonical, in Forgejo | `platform/foo` |
| public, on GitHub | `example-org/foo` |
| contrib fork, in Forgejo | `github-contrib/foo` (only for pull requests) |

Parts 1 to 3 are done once per installation, part 4 once per maintainer,
part 5 once per repository.

## 1. Namespace and database

```bash
kubectl create namespace forgejo-github-bridge
kubectl apply -f samples/cnpg-cluster.yaml
kubectl -n forgejo-github-bridge wait \
  cluster.postgresql.cnpg.io/forgejo-github-bridge-db --for=condition=Ready --timeout=5m
```

CloudNativePG writes the connection string into the Secret
`forgejo-github-bridge-db-app` under the key `uri`, which is what the chart
reads by default. Configure backups the way the platform's other CNPG clusters
do; the bridge can rebuild most of its state without them
([Operations](./operations.md#losing-the-database)), but not all of it.

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
| Everything else | No access |
| Where can it be installed | Only on this account |

Then, on the App's page:

1. Note the **App ID**.
2. *Private keys → Generate a private key*. Keep the downloaded `.pem` out of
   chats and tickets.
3. Store it in the cluster and delete the local copy:

   ```bash
   kubectl -n forgejo-github-bridge create secret generic \
     forgejo-github-bridge-github --from-file=private-key.pem=./<app>.private-key.pem
   ```

The App does not need an OAuth client secret; if GitHub generated one, delete
it.

## 3. The Forgejo accounts

### github-bridge

The account that writes issues and comments.

1. Create a user, e.g. `github-bridge`.
2. Give it **write** access to each canonical repository that will be bridged
   (collaborator, or a team with write on issues and pull requests).
3. Create a token for it with the scopes `write:issue`, `read:repository`,
   `read:user`, and store it:

   ```bash
   kubectl -n forgejo-github-bridge create secret generic \
     forgejo-github-bridge-forgejo --from-literal=token=<token>
   ```

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
   ([Security](./security.md#ci)).
3. Create a token with `write:repository` and `read:user`, and store it:

   ```bash
   kubectl -n forgejo-github-bridge create secret generic \
     forgejo-github-bridge-contrib --from-literal=token=<token>
   ```

The bridge refuses to start if both tokens belong to the same account.

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

1. Create the public repository, e.g. `example-org/foo`, public.
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
5. Fork `platform/foo` as `github-contrib`, into `github-contrib/foo`:

   ```bash
   curl -H "Authorization: token <contrib-token>" -H 'Content-Type: application/json' \
     -X POST https://forgejo.example.com/api/v1/repos/platform/foo/forks -d '{}'
   ```

   The fork must have **no push mirror, no Actions secrets and no
   variables**. A fresh fork has none.

### In the bridge's values

```yaml
repositories:
  - github: example-org/foo
    forgejo: platform/foo
    # for pull requests:
    contrib: github-contrib/foo
    targetBranches: [develop]
    rebaseRequests: true
userMapping:
  jdoe: janedoe    # Forgejo login: GitHub login
```

## 6. Deploy

A values file, starting from
[samples/values-example.yaml](../samples/values-example.yaml):

```yaml
database:
  existingSecret: forgejo-github-bridge-db-app
github:
  appID: 123456
  existingSecret: forgejo-github-bridge-github
forgejo:
  url: http://forgejo-http.forgejo:3000
  webURL: https://forgejo.example.com
  existingSecret: forgejo-github-bridge-forgejo
  contribExistingSecret: forgejo-github-bridge-contrib
repositories:
  - github: example-org/foo
    forgejo: platform/foo
    contrib: github-contrib/foo
    targetBranches: [develop]
    rebaseRequests: true
userMapping:
  jdoe: janedoe
```

`forgejo.webURL` matters whenever `forgejo.url` is an in-cluster address:
links starting with either are recognised as internal and kept off GitHub.

```bash
helm upgrade --install forgejo-github-bridge oci://ghcr.io/metapatrol/charts/forgejo-github-bridge \
  -n forgejo-github-bridge -f values.yaml
```

The logs of a healthy start:

```text
msg="starting forgejo-github-bridge" version=… repositories="[example-org/foo=platform/foo]"
msg="applying migration" name=0001_initial.sql            (first start only)
msg="holding the leader lock; this instance does the work"
msg="identities resolved" github=<slug>[bot] forgejo=github-bridge contrib=github-contrib
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
   Never *Approve always* ([Maintainers](./maintainers.md#approving-ci)).
4. **Merge.** Merge the pull request in Forgejo. Within a minute of the push
   mirror running, the GitHub pull request shows the bridge's note, and is
   either marked merged (fast-forward) or closed with the commit (rebase).
5. **Privacy.** Open a Forgejo issue without the `public` label. It must not
   appear on GitHub.

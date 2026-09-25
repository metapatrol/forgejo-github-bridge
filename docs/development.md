# Development

## Layout

```text
cmd/forgejo-github-bridge   main: configuration, clients, leader lock, HTTP
internal/bridge             polls, queue worker, reconcilers for issues,
                            comments and pull requests
internal/translate          references, mentions, links, attribution,
                            the bridge's message templates
internal/github             the GitHub App: JWT, installation tokens, REST
internal/forgejo            the Forgejo REST client
internal/rest               the JSON client both share: pagination that never
                            leaves the configured host, the ETag cache
internal/gitops             fetch refs/pull/<n>/head, push pr/<n> with a lease
internal/store              the state's contract
internal/store/postgres       PostgreSQL, migrations, advisory locks
internal/store/memory         in-memory, for tests
internal/store/storetest      the contract as tests, run against both
internal/forge              the types the reconciler works in
internal/forge/fake           in-memory GitHub and Forgejo for tests
internal/marker             the hidden provenance line
internal/config             environment and repository file
internal/telemetry          Prometheus metrics
charts/forgejo-github-bridge
charts/forgejo-github-bridge-grafana-dashboard
                            the dashboard; dashboards/generate.py writes its JSON
tests/integration           against real PostgreSQL
tests/chart                 the dashboard against the metrics the bridge publishes
```

The reconciler knows only `internal/forge`'s types. The two clients
translate their APIs into them, so no rule in the reconciler can depend on
something only one forge has.

## Tests

```bash
make test               # everything that needs nothing: seconds
make integration-test   # starts postgres:18 in Docker, then tests/integration
make lint               # golangci-lint at the version CI pins
make helm-verify        # renders the chart, checks keys it must honour and values it must refuse
make secret-scan        # gitleaks over the working tree
make dashboard          # regenerates the dashboard JSON from dashboards/generate.py
```

What covers what:

| Suite | Covers |
|---|---|
| `internal/bridge` | the reconciler end to end against `forge/fake` and the memory store: every issue and pull request lifecycle, loop freedom, recovery after losing the database, crashes between writes, forged markers, message templates, the order of comments and closes |
| `internal/store/storetest` | the store's contract; run by `internal/store/memory` in the unit tests and by `tests/integration` against PostgreSQL, so the memory store cannot drift from the real one |
| `internal/github`, `internal/forgejo` | the clients against `httptest` servers: JWT, token scoping and reuse, pagination, ETags, JSON mapping |
| `internal/gitops` | the real git against local repositories: import, force-push, a refused lease |
| `internal/translate` | translation cases, one per rule |
| `tests/integration` | migrations, the leader lock handing over between two instances, an issue and a pull request lifecycle on PostgreSQL |
| `tests/chart` | the dashboard: every panel queries a published metric, every published metric has a panel, the datasource is a variable, panels fit the grid and do not overlap |

### The fakes

`internal/forge/fake` is a forge in memory that behaves like both real ones
where the reconciler depends on it: the forge assigns numbers and IDs, a
comment bumps its issue's update time, `since` filters on update time, a
deleted item is `ErrNotFound`. `As(login)` gives a client for another account
on the same forge — that is how `github-contrib` is modelled. Test helpers act
as users (`Open`, `Comment`, `MergePull`, `PushBranch`, `Mirror` for the push
mirror); the `forge.Client` methods act as the bridge and count its writes,
which is how tests prove there is no loop.

`fake.Git` stands in for `gitops`, with the same lease semantics.

A test that fails on a mutation is worth more than one that passes: the
reconciler tests for the mirror wait, the head freeze and the comment order
were each checked by breaking the code and watching them fail.

## Running it locally

There is no local stand-in for GitHub. To see the bridge work end to end,
give it a pair of its own to play with: a throwaway public GitHub repository
with the App installed, a Forgejo repository with a push mirror to it, and a
contrib fork. Then open issues and pull requests on both sides and watch them
cross. The tests cover the logic without any of that.

To try an unreleased image on a single-node k3s cluster without a registry:

```bash
docker build -t forgejo-github-bridge:dev .
docker save forgejo-github-bridge:dev -o /tmp/bridge.tar
sudo k3s ctr images import /tmp/bridge.tar
helm upgrade forgejo-github-bridge charts/forgejo-github-bridge -n forgejo-github-bridge \
  -f values.yaml --set image.repository=docker.io/library/forgejo-github-bridge \
  --set image.tag=dev --set image.pullPolicy=Never
```

## Changing things

- **A migration** is a new numbered file in
  `internal/store/postgres/migrations`. Never edit one that was released; the
  bridge refuses to start against a database where it was applied with other
  content.
- **A new metric** gets a panel in `dashboards/generate.py` (`make dashboard`);
  `tests/chart` fails until it has one.
- **A new message** goes into `translate.DefaultMessages` with a friendly
  default, and into the tables in [configuration](./configuration.md#messages)
  and the README.
- **A new endpoint** of either forge goes into its client, translated into
  `internal/forge`'s types, and into `forge/fake` with the behaviour the
  reconciler relies on.
- **Anything that reads text from GitHub** treats it as data. It never names
  a repository, URL, ref or command.

## Releases

Work lands on `develop` through pull requests. Merging `develop` into `main`
runs `main.yml`: the checks again, the image, then semantic-release, which
decides the version from the conventional commits since the last tag and
dispatches `release.yml` for the image, binaries and chart. See
[.forgejo/README.md](../.forgejo/README.md).

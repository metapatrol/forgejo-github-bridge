# forgejo-github-bridge

![Version: 0.1.0](https://img.shields.io/badge/Version-0.1.0-informational?style=flat-square) ![Type: application](https://img.shields.io/badge/Type-application-informational?style=flat-square) ![AppVersion: 0.1.0](https://img.shields.io/badge/AppVersion-0.1.0-informational?style=flat-square)

Keeps issues and comments in step between a private Forgejo and the public GitHub repositories it mirrors to.

## Installing

The bridge needs three Secrets that this chart does not create, because each
belongs to something else:

| Secret | key | what it is |
|---|---|---|
| `database.existingSecret` | `uri` | the PostgreSQL connection string; any Secret, e.g. the one CloudNativePG writes for its app user |
| `github.existingSecret` | `private-key.pem` | the GitHub App's private key |
| `forgejo.existingSecret` | `token` | an access token of the `github-bridge` Forgejo account |

```bash
helm install forgejo-github-bridge oci://ghcr.io/metapatrol/charts/forgejo-github-bridge \
  --namespace forgejo-github-bridge --create-namespace \
  --set database.existingSecret=forgejo-github-bridge-db-app \
  --set github.appID=123456 \
  --set github.existingSecret=forgejo-github-bridge-github \
  --set forgejo.url=http://forgejo-http.forgejo:3000 \
  --set forgejo.existingSecret=forgejo-github-bridge-forgejo \
  --set 'repositories[0].github=example-org/foo' \
  --set 'repositories[0].forgejo=platform/foo'
```

The chart refuses to render without any of these, rather than installing a
bridge that starts and then does nothing.

### One active instance

`replicaCount: 2` does not share the work. The second replica waits on a
PostgreSQL advisory lock and takes over when the first one's session ends. It
shortens a failover and nothing else.

## Values

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| affinity | object | `{}` |  |
| database.existingSecret | string | `""` | The Secret holding the PostgreSQL connection string. Required. |
| database.existingSecretKey | string | `"uri"` | The key inside that Secret. Configurable because the Secret is usually somebody else's. CloudNativePG, for example, writes the connection string of its app user under "uri". |
| database.maxConnections | int | `5` | The size of the connection pool. At least 2: the leader lock holds one connection for as long as the instance is active. |
| env | list | `[]` | Extra environment variables for the container. |
| forgejo.caSecret | string | `""` | The Secret holding the CA certificate Forgejo's own certificate is issued by, for an instance behind a private CA. Empty trusts the system roots only. |
| forgejo.caSecretKey | string | `"ca.crt"` | The key inside that Secret. |
| forgejo.contribExistingSecret | string | `""` | The Secret holding the github-contrib account's token. Required as soon as a repository sets `contrib`: imported pull requests are opened by this account, which must have no write access to the canonical repository. |
| forgejo.contribExistingSecretKey | string | `"token"` | The key inside that Secret. |
| forgejo.existingSecret | string | `""` | The Secret holding the github-bridge account's token. Required. |
| forgejo.existingSecretKey | string | `"token"` | The key inside that Secret. |
| forgejo.url | string | `""` | The Forgejo base URL the bridge talks to, e.g. the in-cluster Service. Required. |
| forgejo.webURL | string | `""` | The address people's browsers use for Forgejo, when it is not `url`. Links starting with either are recognised as internal and never reach GitHub, so this must be set whenever `url` is an in-cluster address. |
| fullnameOverride | string | `""` |  |
| gitCacheSize | string | `"1Gi"` | The size limit of the cache pull request heads pass through: one bare repository per bridged pair. Lost on restart, which only costs a fuller fetch the next time. |
| github.apiURL | string | `"https://api.github.com"` | The API base URL. Only a test would change it. |
| github.appID | string | `""` | The GitHub App's ID. Required. The App's name is not configured: the bridge reads it from GitHub. |
| github.existingSecret | string | `""` | The Secret holding the App's private key. Required. |
| github.existingSecretKey | string | `"private-key.pem"` | The key inside that Secret. |
| image.pullPolicy | string | `"IfNotPresent"` |  |
| image.repository | string | `"ghcr.io/metapatrol/forgejo-github-bridge"` |  |
| image.tag | string | `""` | Defaults to the chart's appVersion, or to the commit when the chart is read from Git by Flux. |
| imagePullSecrets | list | `[]` |  |
| logging.format | string | `"json"` | json or text. |
| logging.level | string | `"info"` | debug, info, warn or error. |
| messages | object | `{}` | What the bridge says on GitHub and in Forgejo, as Go templates. Only the messages to change need to be given; the rest keep their defaults. A name that does not exist, or a template that does not render, stops the bridge at startup rather than on a contributor's pull request.  Names: unsupportedBase, rebaseRequest, upToDate, merged, integrated, closedUpstream (said on GitHub); headFrozen, touchesCI (said in Forgejo). Fields: {{.Base}} {{.Targets}} {{.Behind}} {{.Commit}} {{.ShortCommit}} {{.Branch}} {{.Paths}} {{.Contributor}}. See the README for the defaults.    rebaseRequest: >-     Hi @{{.Contributor}}, `{{.Base}}` has moved on by {{.Behind}} commit(s).     Would you mind rebasing onto it? Thank you! |
| nameOverride | string | `""` |  |
| networkPolicy.enabled | bool | `false` | Restrict ingress to the probe and metrics port, from the namespaces below. The bridge accepts no other connection. |
| networkPolicy.metricsNamespaceSelector | object | `{"kubernetes.io/metadata.name":"monitoring"}` | Namespace labels allowed to scrape metrics. The kubelet's probes are not subject to NetworkPolicy. |
| nodeSelector | object | `{}` |  |
| podAnnotations | object | `{}` |  |
| poll.fullResync | string | `"6h"` | How often every open bridged issue is reconciled regardless of polls, which is what notices deleted comments. |
| poll.interval | string | `"60s"` | How often each repository is polled on each side. |
| port | int | `8080` | The port probes and metrics are served on. Cluster-internal; the bridge has no public endpoint. |
| replicaCount | int | `1` | One replica does the work. A second one waits on the leader lock and only shortens a failover; it never shares the work (docs/design.md §7). |
| repositories | list | `[]` | The bridged repositories. Each entry pairs a public GitHub repository with the private Forgejo repository that is its source of truth.    - github: example-org/foo      # required     forgejo: platform/foo       # required     publishLabel: public        # makes a Forgejo issue public     importLabel: github         # marks issues imported from GitHub     internalPrefix: /internal   # keeps a Forgejo comment private     importSince: "2026-09-01T00:00:00Z"     # Pull requests (phase 2), only with a contrib repository:     contrib: github-contrib/foo # the fork imported heads are pushed to     targetBranches: [develop]   # the bases a pull request may target     rebaseRequests: false       # ask contributors to rebase outdated PRs     ciPaths: [.forgejo/, .github/]     messages: {}                # per-repository overrides of `messages` |
| resources.limits.cpu | string | `"500m"` |  |
| resources.limits.memory | string | `"128Mi"` |  |
| resources.requests.cpu | string | `"10m"` |  |
| resources.requests.memory | string | `"32Mi"` |  |
| service.type | string | `"ClusterIP"` |  |
| serviceAccount.annotations | object | `{}` |  |
| serviceAccount.create | bool | `true` |  |
| serviceAccount.name | string | `""` |  |
| serviceMonitor.additionalLabels | object | `{}` | Labels the Prometheus instance selects ServiceMonitors by. |
| serviceMonitor.enabled | bool | `false` | Render a ServiceMonitor. Requires the Prometheus operator's CRDs. |
| serviceMonitor.interval | string | `"30s"` |  |
| serviceMonitor.namespace | string | `""` |  |
| serviceMonitor.scrapeTimeout | string | `""` |  |
| tolerations | list | `[]` |  |
| userMapping | object | `{}` | Forgejo logins mapped to GitHub logins, for mentions and attribution only. A maintainer who is not mapped appears on GitHub as "a maintainer". |

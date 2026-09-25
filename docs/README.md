# Documentation

forgejo-github-bridge lets a project live in a private Forgejo and still take
issues and pull requests from the public on GitHub. The pages below are
ordered by who reads them.

**Understanding it**

- **[Concept](../CONCEPT.md)** — what the bridge is for, what crosses it and
  what never does, and why; the design record
- **[Architecture](./architecture.md)** — components, the processing model,
  state, identities, and what happens to an issue and a pull request on their
  way across
- **[Security](./security.md)** — trust boundaries, what a contributor can and
  cannot make the bridge do, and the few rules operators must keep

**Setting it up and running it**

- **[Installation](./installation.md)** — from nothing to a bridged
  repository, step by step, including the checks to run before the first real
  contribution
- **[Configuration](./configuration.md)** — every environment variable, the
  repository file, the message templates, and the chart values that set them
- **[Operations](./operations.md)** — metrics and alerts, reading the logs,
  what to do when something is stuck, upgrades, credential rotation, losing
  the database, removing a repository

**Working with it**

- **[Maintainers](./maintainers.md)** — day to day in Forgejo: imported issues,
  publishing, internal comments, approving CI, reviewing, merging and closing
  imported pull requests, and what the contributor sees on GitHub

**Changing it**

- **[Development](./development.md)** — layout, the test suites and their
  fakes, running it locally, releases
- **[Chart values](../charts/forgejo-github-bridge/README.md)** — the Helm chart
- **[Grafana dashboard](../charts/forgejo-github-bridge-grafana-dashboard/README.md)** — the dashboard chart, and what each row shows
- **[CI](../.forgejo/README.md)** — what the workflows do and what they need

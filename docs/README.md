# Documentation

forgejo-github-bridge lets a project live in a private Forgejo and still take
issues and pull requests from the public on GitHub.

## Getting started

| | |
|---|---|
| [Installation](installation.md) | from nothing to a bridged repository, step by step, with the checks to run before the first real contribution |
| [Configuration](configuration.md) | every environment variable, the repository file, the message templates, the chart values |
| [FAQ](faq.md) | short answers to the questions that come up first |

## Using it

| | |
|---|---|
| [Maintainers](maintainers.md) | day to day in Forgejo: imported issues, publishing, internal comments, approving CI, reviewing, merging and closing |
| [Operations](operations.md) | health, metrics and alerts, logs, what to do when something is stuck, upgrades, credential rotation, recovery |
| [Grafana dashboard](../charts/forgejo-github-bridge-grafana-dashboard/README.md) | the dashboard chart, and what each row shows |

## Understanding it

| | |
|---|---|
| [Architecture](architecture.md) | the pieces, the processing model, state, identities, the way of an issue and a pull request |
| [Security](security.md) | the trust boundary, what a contributor cannot make the bridge do, and the rules operators keep |
| [Design](design.md) | the design record: every decision and the reasons for it |

## Changing it

| | |
|---|---|
| [Development](development.md) | layout, tests and fakes, running it locally, releases |
| [Chart values](../charts/forgejo-github-bridge/README.md) | the Helm chart's values, generated |
| [CI](../.forgejo/README.md) | what the workflows do and what they need |

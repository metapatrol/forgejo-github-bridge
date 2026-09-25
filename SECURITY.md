# Security

## Reporting a vulnerability

Please report security problems privately through GitHub's
[private vulnerability reporting](https://github.com/metapatrol/forgejo-github-bridge/security/advisories/new)
(*Security → Report a vulnerability*), not in a public issue or pull request.

Include what you found, how to reproduce it, and what an attacker could do
with it. You will get an answer as soon as a maintainer has looked at it.

## Scope

Of particular interest is anything that lets someone on GitHub:

- run code on the maintainers' CI without a maintainer's approval,
- make the bridge touch a repository, branch or remote outside its
  configuration,
- read or infer anything from the private Forgejo that was not published,
- make the bridge overwrite work it did not push.

The security model is described in [docs/security.md](docs/security.md) and
[CONCEPT.md §15–16](CONCEPT.md#15-security-properties).

## Supported versions

Fixes go into the next release. Versions are 0.x; only the latest release is
supported.

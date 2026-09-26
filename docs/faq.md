# FAQ

**Contents**

- [Why polling and not webhooks?](#why-polling-and-not-webhooks)
- [Doesn't polling cost a lot of API requests?](#doesnt-polling-cost-a-lot-of-api-requests)
- [Why does the bridge need two Forgejo accounts?](#why-does-the-bridge-need-two-forgejo-accounts)
- [Why a separate contrib fork?](#why-a-separate-contrib-fork)
- [Can a contributor run code on our CI?](#can-a-contributor-run-code-on-our-ci)
- [Why is a merged pull request sometimes "closed" on GitHub?](#why-is-a-merged-pull-request-sometimes-closed-on-github)
- [Why doesn't the bridge push to GitHub?](#why-doesnt-the-bridge-push-to-github)
- [Are Forgejo issues published?](#are-forgejo-issues-published)
- [Can we talk privately on a public issue?](#can-we-talk-privately-on-a-public-issue)
- [Who do contributors see answering them?](#who-do-contributors-see-answering-them)
- [What happens if the bridge is down?](#what-happens-if-the-bridge-is-down)
- [What if we lose the database?](#what-if-we-lose-the-database)
- [Can we run two replicas?](#can-we-run-two-replicas)
- [Does it work with Gitea?](#does-it-work-with-gitea)
- [Does the GitHub repository have to be public?](#does-the-github-repository-have-to-be-public)
- [Can we change what the bot says?](#can-we-change-what-the-bot-says)

## Why polling and not webhooks?

Webhooks need a public endpoint, and a public endpoint is the part of a
system most exposed to attack. It would also need its own deployment, TLS and
a trust relationship with the private side. What it would buy is latency, and
for open-source contributions a minute does not matter. Polling needs nothing
reachable from the internet, and there are no lost deliveries to recover
from. See [design §3](design.md#3-why-polling).

## Doesn't polling cost a lot of API requests?

No. Each poll asks only for what changed since the last one, and an unchanged
answer comes back as `304 Not Modified`, which GitHub does not count against
the rate limit. A GitHub App installation has at least 5,000 requests an
hour; a handful of repositories polled every minute use a small fraction of
that. The budget is a metric on the dashboard.

## Why does the bridge need two Forgejo accounts?

Because of CI. Forgejo runs pull request workflows without approval when their
author has write access. Imported pull requests are therefore opened by
`github-contrib`, which has only read access, so every run waits for a
maintainer. The other account, `github-bridge`, needs write access to label
and close issues. One account cannot be both. See
[design §8](design.md#8-identities-and-credentials).

## Why a separate contrib fork?

Two reasons. A branch in the canonical repository would be published to
GitHub by the push mirror, or removed by it. And a pull request from a fork is
what Forgejo treats as untrusted: no secrets, approval required. The bridge
creates the fork itself, as `github-contrib/<name>`. See
[design §11](design.md#11-pull-requests).

## Can a contributor run code on our CI?

Only after a maintainer has read the changes and approved the run, with
*Approve once*. Never use *Approve always* on an imported pull request: it
trusts `github-contrib`, the author of every imported contribution. See
[security](security.md#ci) and [maintainers](maintainers.md#approving-ci).

## Why is a merged pull request sometimes "closed" on GitHub?

GitHub marks a pull request merged when its exact commits reach the base
branch. If the pull request was up to date when you merged, Forgejo
fast-forwards, the commits keep their SHAs, and GitHub shows it merged. If
the base had moved on, Forgejo rebases the commits, they get new SHAs, and
GitHub cannot tell. The bridge then closes the pull request with a note naming
the commit. The contributor stays the author either way. To always get
"merged", turn on `rebaseRequests` and block outdated merges in the branch
protection.

## Why doesn't the bridge push to GitHub?

Forgejo's push mirror already does, and only what maintainers merged reaches
it. A bridge that could push to GitHub would be one more way for code to get
there.

## Are Forgejo issues published?

Only those you label `public`. Everything else stays in Forgejo. Removing the
label later freezes the GitHub copy; it is not deleted.

## Can we talk privately on a public issue?

Yes: start the comment with `/internal`. It stays in Forgejo. Edit the prefix
away to publish it.

## Who do contributors see answering them?

The GitHub App's bot account, with each comment naming the maintainer who
wrote it — as their GitHub account if they are in `userMapping`, otherwise as
"a maintainer". Forgejo logins never appear on GitHub.

## What happens if the bridge is down?

Nothing is lost. Changes wait on either side; when the bridge is back, it
polls from where it stopped and catches up. Failed work is retried with
backoff.

## What if we lose the database?

Restore it from backup. Without one, the bridge rebuilds its mappings from
hidden markers in the issues and comments it wrote. See
[operations](operations.md#losing-the-database).

## Can we run two replicas?

Yes, for faster failover. Only one works at a time; the second waits on a
PostgreSQL lock and takes over within seconds when the first stops.

## Does it work with Gitea?

It is built and tested against Forgejo 16. Gitea shares much of Forgejo's
API, but the bridge is not tested against it, and the CI approval gate it
relies on is Forgejo's.

## Does the GitHub repository have to be public?

No. The bridge fetches a pull request's commits anonymously first, which is
all a public repository needs. For a private one it retries with the GitHub
App's installation token, restricted to that repository; the App then needs
the *Contents: read* permission. Contributors to a private repository need
to be able to fork it, which is an organisation setting.

## Can we change what the bot says?

Yes. Every message is a template you can override, globally or per
repository. See [configuration](configuration.md#messages).

# Contributing

Thanks for taking the time.

## How this repository works

forgejo-github-bridge is developed in a private Forgejo instance and
push-mirrored here. It is also its own first user: the issues and pull
requests you open on GitHub are carried into Forgejo by the bridge, where the
maintainers work, and everything they say comes back here. From your side it
is an ordinary GitHub project; a bot account posts the maintainers' replies,
naming who wrote them.

## Issues

Open an issue for bugs, questions and ideas. Please include the bridge's
version (the `version` in its first log line, or the image tag), what you
expected, what happened, and the relevant log lines — with tokens, keys and
DSNs removed.

## Pull requests

- Target **`develop`**. Pull requests against other branches are not picked
  up; the bot will say so.
- Use [Conventional Commits](https://www.conventionalcommits.org/)
  (`feat:`, `fix:`, `docs:`, …). Versions and the changelog are derived from
  them.
- Run the checks before pushing:

  ```bash
  make test lint helm-verify
  make integration-test   # needs Docker
  ```

- Keep the comment style of the surrounding code: comments explain *why*.
- If you change CI configuration (`.forgejo/`, `.github/`), say why in the
  description. Such pull requests are labelled and read with particular care
  before any workflow of theirs runs.

### What happens after you open one

1. Your branch is imported into the maintainers' Forgejo. CI runs there once a
   maintainer has read your changes and approved the run.
2. Review comments appear on your pull request, posted by the bot and
   attributed to the maintainer.
3. If `develop` moves on while you wait, the bot may ask you to rebase. Once is
   enough; it updates that one comment instead of adding more.
4. When it is merged, the bot says so. If your commits were merged as they
   are, GitHub shows the pull request as merged. If they had to be rebased,
   the bot closes it with a note naming the commit — your authorship is kept
   on every commit either way.

## Security

Please do not report security problems in public issues. See
[SECURITY.md](SECURITY.md).

## License

By contributing you agree that your contributions are licensed under the
[Apache License 2.0](LICENSE).

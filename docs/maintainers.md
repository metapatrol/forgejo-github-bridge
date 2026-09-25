# Working with the bridge

For maintainers, who work in Forgejo. Nothing here needs GitHub: the bridge
carries what matters there and back. Contributors, on the other side, use
GitHub as they would for any project and need not know Forgejo exists.

## Issues

### Issues from GitHub

An issue opened on GitHub appears in Forgejo within a minute or two, with the
label `github`. It starts with who reported it and a link to the original:

> Reported by [alice](https://github.com/alice) via GitHub: https://github.com/metapatrol/foo/issues/42

The imported issue is yours to triage. Labels, milestones, assignees and
projects you set in Forgejo stay in Forgejo and are never overwritten. Title
and description follow GitHub: an edit to them in Forgejo is replaced by the
GitHub text on the next change.

Everything you write on the issue in Forgejo appears on GitHub under your
name (if you are in `userMapping`, otherwise as "a maintainer"), and
everything written on GitHub appears in Forgejo.

### Keeping a comment internal

Start the comment with `/internal`:

```text
/internal this is the customer from last week, see #12
```

It stays in Forgejo. Edit the prefix away and it is published; edit it in and
the published copy is withdrawn from GitHub.

### Publishing a Forgejo issue

Issues opened in Forgejo are **private by default**. To publish one, add the
label `public`. The bridge creates it on GitHub and from then on bridges it
like an imported one, with Forgejo as the source of title and description.

Removing the label again does not delete the GitHub issue. It freezes it:
nothing crosses any more in either direction.

Closed issues are never published or imported.

### Closing

Close on either side and the other follows; reopening works the same way.
Use **Close with comment** to explain: the comment arrives on GitHub before
the close, in that order. Closing first and commenting afterwards is carried
across in that order too.

## References, mentions and links

The bridge rewrites them so they mean the same on GitHub and leak nothing:

| You write in Forgejo | On GitHub |
|---|---|
| `#12`, where #12 is bridged | its GitHub number |
| `#13`, where #13 is private | `` `internal reference` `` |
| `@denis` (mapped) | `@denisneuling` |
| `@alice`, who takes part in the GitHub issue | `@alice`, and she is notified |
| `@bob`, who does not | `bob`, and nobody is notified |
| a link to a bridged Forgejo issue | the GitHub issue's link |
| any other link into Forgejo | its link text, or `` `internal link` `` |
| an attachment | `` `attachment not available publicly` `` |

Code spans and code blocks are never touched.

On the way in, `#42` in a GitHub comment becomes the Forgejo number if #42 is
bridged, and a link to GitHub otherwise; a GitHub `@mention` becomes a link to
the profile, never a Forgejo mention.

A reference to a private issue that is published later is updated on GitHub
the next time the referring issue changes, or at the latest at the next full
resync (every six hours by default).

## Pull requests

### What arrives

A contributor opens a pull request on GitHub against a branch that takes
contributions (`targetBranches`, usually `develop`). In Forgejo it appears:

- opened by **`github-contrib`**, from the branch `pr/<GitHub number>` of the
  contrib fork,
- with the label `github`, and `touches-ci` if it changes `.forgejo/` or
  `.github/`, plus a warning comment listing those files,
- with a description that starts with who opened it and a link to GitHub.

A pull request against any other branch is not imported. The contributor
gets one friendly explanation on GitHub; once they change the base branch, it
is picked up.

New commits the contributor pushes, including force-pushes after a rebase,
arrive in the same Forgejo pull request within a minute or two.

### Approving CI

Workflow runs of an imported pull request **wait for your approval**, because
they run code and workflow files from outside.

1. Read the changes first — all of them, and every file under `.forgejo/` and
   `.github/` in particular. The `touches-ci` warning lists those.
2. Approve with **Approve once**.
3. **Never use "Approve always"** on an imported pull request. It trusts the
   pull request's author, and that author is `github-contrib` — the account
   behind *every* imported contribution, from anyone. One click would let
   every future contributor run CI unreviewed. If it happens, revoke the trust
   in the repository settings straight away.

Every new push needs a new approval. That is the point.

### Reviewing and talking

Comment in the Forgejo pull request as usual; it appears on GitHub under your
name. Submitted reviews appear on GitHub as a comment: "requested changes",
"approved these changes" or "reviewed", with the review's text. Line comments
stay in Forgejo for now.

### Pushing to the contrib branch

Don't, unless you mean it. The bridge owns `pr/<n>`. If you push a fixup
there, the bridge will not overwrite it: it stops importing the contributor's
new commits into that pull request and says so in a comment. Prefer asking
the contributor, or a follow-up commit after merging.

### Outdated pull requests

With `rebaseRequests` on for the repository, a pull request that falls behind
its base gets one comment on GitHub asking the contributor to rebase. The
contributor is notified once each time the pull request falls behind; while
it stays behind, the comment is only updated; once it is rebased, the comment
says thanks. There is never more than one such comment.

### Merging

Merge in Forgejo with the repository's merge style, *rebase, then
fast-forward*. What the contributor sees on GitHub depends on whether the
rebase had anything to do:

| At merge time | On GitHub |
|---|---|
| up to date with its base | a plain fast-forward: GitHub marks the pull request **merged** itself; the bridge adds a thank-you with the commit |
| behind its base | the commits are rebased and get new SHAs, which GitHub cannot recognise; the bridge comments with the commit and **closes** the pull request |

Either way the contributor is the author of every commit, and you are the
committer — with the primary e-mail of your Forgejo account, published by the
push mirror. Make sure it is an address of your GitHub account
([Installation](./installation.md#4-per-maintainer)).

The report on GitHub appears once the push mirror has brought the merge
there, usually within a minute.

### Closing without merging

**Close with comment** in Forgejo: your comment appears on GitHub, then a
short note that the maintainers closed it, then the close. The contrib branch
is deleted.

If the contributor withdraws the pull request on GitHub, the Forgejo pull
request is closed. If they reopen it, it is imported again.

### Your own pull requests

Pull requests you open in Forgejo stay in Forgejo, labelled `public` or not.
Only their result reaches GitHub, through the push mirror.

## What the contributor sees

On GitHub, the bridge is the App's bot account. From the contributor's side:

- Their issue gets answers from the maintainers, quoted with the maintainer's
  GitHub account, or "a maintainer".
- Their pull request gets the maintainers' comments and reviews, perhaps a
  friendly request to rebase, and finally a thank-you: marked merged, or
  closed with the commit their work landed as.
- They never see Forgejo, its addresses, its private issues or anyone's
  internal comments.

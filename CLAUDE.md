# bbkt — Bitbucket Data Center pull request CLI

## What this talks to

**Bitbucket Data Center REST API 1.0** (`<host>/rest/api/1.0`), not Bitbucket
Cloud's REST 2.0. The two are incompatible and almost every example online is for
Cloud. When adding an endpoint, read the Data Center reference:
<https://developer.atlassian.com/server/bitbucket/rest/>

That reference's OpenAPI models an activity with only its base fields, so the
`comment`, `commentAction` and `commentAnchor` a comment activity carries are
absent from it, and it types an anchor's `path` as an object. The legacy 7.21
reference prints real example payloads, a string `path` among them:
<https://docs.atlassian.com/bitbucket-server/rest/7.21.0/bitbucket-rest.html>.
`bitbucket.Path` decodes either form.

## Constraints that shaped the design

- **No local repository registry.** Repo-scoped commands parse the git remote;
  cross-repo views use Bitbucket's server-side `/inbox/pull-requests` and
  `/dashboard/pull-requests`. Bitbucket answers "what needs me" itself, so there
  is nothing to keep in sync.
- **No diff command.** `git diff origin/target...origin/source` is the same
  merge-base diff the web view renders. Fetching it over the API would be a worse
  copy of something git already does. Every hint bbkt prints uses
  **remote-tracking refs** on the remote `--remote` names, never the bare
  `DisplayID`: a pull request's target is usually a long-lived branch
  (`develop`/`uat`/`prod`) that is never checked out,
  so a bare `develop` either fails to resolve or resolves to a local copy pinned
  at whenever it was created — silently diffing against a stale target.
  `pr comment create` reads one file's diff over the API to place a comment, and
  never prints it.
- **A comment's line type is read from Bitbucket's diff, never asked for.** An
  anchor needs `lineType` (ADDED, REMOVED, CONTEXT) beside the line, and a wrong
  one puts the comment somewhere other than the line meant. The user gives a
  path, a line and a side; `ResolveAnchor` finds the rest in the pull request's
  own effective diff, the one Bitbucket validates the anchor against. Rejected:
  `--line-type`/`--file-type` flags, which hand the user a fact they can get
  wrong, and the local `git diff`, which `-R` cannot reach and whose rename
  detection need not match Bitbucket's.
- **Merge reads the version first.** Bitbucket uses `version` for optimistic
  concurrency; a stale one must fail rather than merge commits the caller never
  saw. Never cache it across commands.
- **Approve goes through `/participants/{user}`,** not the deprecated
  `/approve`. The current user comes from the `X-AUSERNAME` response header —
  REST 1.0 has no `/myself` endpoint.

## Tested against the published payloads

`bitbucket/client_test.go` drives an `httptest` server with payloads built from
the Data Center reference, and that is where an endpoint's shape gets pinned.
That is the whole verification a change needs. Nothing waits on a run against a
live instance: the first one after a release is ordinary use. A payload it shows
to differ from the reference is a bug, and its fix moves the fixture to the real
shape.

bbkt is installed with `go install …@latest`, which applies no ldflags, so
`buildVersion` reads the module version from build info. It runs under WSL as well
as on native Linux and macOS, which is why `pr open` tries `wslview` before
`xdg-open`.

## Gotchas

- **Project/repository-scoped tokens cannot merge.** A merge creates a commit,
  which needs a real user identity. Only personal tokens work for `pr merge`.
- **An anchor's FROM and TO run opposite to a pull request's fromRef and
  toRef.** FROM is the file before the pull request, where a removed line lives;
  TO is the file after it, which is the source branch's version. The CLI says
  `old` and `new` so nobody has to hold that inversion.
- **Every diff line carries a number on both sides.** An added line's `source`
  is a position in the old file, not a line of it. Only CONTEXT segments and the
  side's own segment type count when matching a line (`segmentOnSide`), and
  `TestResolveAnchorReadsTheLineTypeFromTheDiff` uses a diff whose sides number
  later lines differently so a swap fails it.
- **The changes resource ignores `start`** and returns one page. Walking it with
  `paged` would read that page forever, so `fileChange` makes one request at
  `changesLimit`.
- **Branch names containing underscores** break a Jira-key regex that ends in
  `\b` — `_` is a word character. `SplitIssueKey` relies on a greedy `\d+`
  instead; `TestSplitIssueKey` pins it.

## Never write the breaking-change trailer in a commit message

The words `BREAKING CHANGE` — either number, colon or not, subject or body — cut a major release
here, and a major on this repo is an outage rather than a version. `commit-analyzer-cz` matches
them unanchored against the raw message and ORs the result with the configured major rules, so
`.semrelrc` cannot stop it and it majors even a `fix:` commit.

The module path carries no `/vN` suffix, so once a major exists `go install …@latest` cannot see it
and silently resolves the highest v1 instead — an installer comparing versions reports the tool
stale forever while installing nothing. Every already-installed binary is stranded too:
`goselfupdate` refuses a lower version and reports "already up to date". Recovery is a reinstall on
each machine, and it is not a rewrite — branch protection refuses one on `main`, and the offending
commit re-cuts the major on every push until a tag above it takes it out of range.

**The ban covers a commit that merely discusses the trailer.** One explaining this exact caveat cut
a fresh major on push. Name it some other way — "that marker" — and never quote it.

Deliberate majors use `chore(release-major)`, the one subject `.semrelrc` leaves as a major. The
release carrying one also adds the `/vN` suffix to the module path, so `go install <module>/vN@latest`
resolves the tag. Without the suffix, a deliberate major is the same outage.

Resetting after an accidental major goes in this order, because deleting the tags is what makes the
analyzer resume from the v1 line:

1. Delete the vN GitHub releases and their tags, remote and local.
2. Tag a baseline on `main` above the offending commit, and push it. Without it the commit stays in
   the analyzed range and the next push re-cuts the major. Find the commit with
   `rg 'BREAKING CHANGES?'` over `git log <highest-v1-tag>..origin/main --format='%H %s%n%b'`. The
   narrower `^BREAKING CHANGE:` misses a mid-line hit and the plural.
3. Confirm with `go list -m github.com/datapointchris/bbkt@latest`, which must answer the v1 tag.
4. Reinstall on every machine. Self-update cannot recover a stranded binary.

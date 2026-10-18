# bbkt — Bitbucket Data Center pull request CLI

This file holds only what is specific to bbkt: the API it talks to, the constraints
behind its design, how it is tested, its gotchas, and the commit-message marker
that cuts a major.

## What this talks to

**Bitbucket Data Center REST API 1.0** (`<host>/rest/api/1.0`), not Bitbucket
Cloud's REST 2.0. The two are incompatible and almost every example online is for
Cloud. When adding an endpoint, read the Data Center reference:
<https://developer.atlassian.com/server/bitbucket/rest/>

## Constraints that shaped the design

- **No local repository registry.** Repo-scoped commands parse the git remote;
  cross-repo views use Bitbucket's server-side `/inbox/pull-requests` and
  `/dashboard/pull-requests`. Bitbucket answers "what needs me" itself, so there
  is nothing to keep in sync.
- **No diff command.** `git diff origin/target...origin/source` is the same
  merge-base diff the web view renders. Fetching it over the API would be a worse
  copy of something git already does. Every hint bbkt prints uses
  **remote-tracking refs** on the remote `--remote` names, never the bare
  `DisplayID`: a pull request's target is
  usually a long-lived branch (`develop`/`uat`/`prod`) that is never checked out,
  so a bare `develop` either fails to resolve or resolves to a local copy pinned
  at whenever it was created — silently diffing against a stale target.
- **Merge reads the version first.** Bitbucket uses `version` for optimistic
  concurrency; a stale one must fail rather than merge commits the caller never
  saw. Never cache it across commands.
- **Approve goes through `/participants/{user}`,** not the deprecated
  `/approve`. The current user comes from the `X-AUSERNAME` response header —
  REST 1.0 has no `/myself` endpoint.

## Tested without an instance

Development has no Data Center instance to reach, so nothing here runs against a
real one. `bitbucket/client_test.go` drives an `httptest` server with realistic
Data Center payloads instead, and that is where an endpoint's shape gets pinned.

bbkt is installed with `go install …@latest`, which applies no ldflags, so
`buildVersion` reads the module version from build info. It runs under WSL as well
as on native Linux and macOS, which is why `pr open` tries `wslview` before
`xdg-open`.

## Gotchas

- **Project/repository-scoped tokens cannot merge.** A merge creates a commit,
  which needs a real user identity. Only personal tokens work for `pr merge`.
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

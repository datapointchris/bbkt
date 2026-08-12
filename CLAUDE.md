# bbkt — Bitbucket Data Center pull request CLI

Universal rules live in `~/.claude/CLAUDE.md`; fleet standards in `standards/`
(`go.md`, `cli-design.md`, `release.md`, `repo-structure.md`). This file holds only
what is specific to bbkt.

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
  **remote-tracking refs**, never the bare `DisplayID`: a pull request's target is
  usually a long-lived branch (`develop`/`uat`/`prod`) that is never checked out,
  so a bare `develop` either fails to resolve or resolves to a local copy pinned
  at whenever it was created — silently diffing against a stale target.
- **Merge reads the version first.** Bitbucket uses `version` for optimistic
  concurrency; a stale one must fail rather than merge commits the caller never
  saw. Never cache it across commands.
- **Approve goes through `/participants/{user}`,** not the deprecated
  `/approve`. The current user comes from the `X-AUSERNAME` response header —
  REST 1.0 has no `/myself` endpoint.

## Where it runs

The work box only (WSL Ubuntu, corporate network, VPN). It is deployed there via
`go install` from the `go_tools` list in
`~/dotfiles/install/manifests/wsl-work-workstation.yml`. It cannot be tested
against a real instance from any personal machine — `bitbucket/client_test.go`
drives an `httptest` server with realistic Data Center payloads instead, and that
is where a new endpoint's shape gets pinned.

## Gotchas

- **Project/repository-scoped tokens cannot merge.** A merge creates a commit,
  which needs a real user identity. Only personal tokens work for `pr merge`.
- **Branch names containing underscores** break a Jira-key regex that ends in
  `\b` — `_` is a word character. `SplitIssueKey` relies on a greedy `\d+`
  instead; `TestSplitIssueKey` pins it.

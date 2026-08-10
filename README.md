# bbkt

Pull request lifecycle for **self-hosted Bitbucket Data Center**, from the terminal.

Bitbucket Cloud and Bitbucket Data Center are different products with incompatible
APIs — Cloud is REST 2.0 at `api.bitbucket.org`, Data Center is REST 1.0 at
`<your-host>/rest/api/1.0`. Every maintained Bitbucket CLI targets Cloud, which
is why this exists.

## Install

```bash
go install github.com/datapointchris/bbkt/v2@latest
```

## Configure

First create an HTTP access token in Bitbucket: **profile picture → Manage
account → HTTP access tokens**. It must be a **personal** (user-level) token —
project- and repository-scoped tokens cannot merge a pull request, because a
merge creates a commit and that needs a real user identity.

Then:

```bash
bbkt config init    # prompts for the URL and the token, writes both files
bbkt config check   # connects and reports who you are
```

`config init` writes `~/.config/bbkt/config.toml` and a `0600` token file beside
it, and never overwrites either. In a script, pipe the token in instead:

```bash
pass show work/bitbucket | bbkt config init --no-input
```

`bbkt config example` prints the annotated config with every option, and
`bbkt config edit` opens the real one.

### Where a value came from

`BBKT_URL`, `BBKT_TOKEN`, `BBKT_TOKEN_FILE`, and `BBKT_CA_FILE` each override the
file, so the value in use is not always the one written down. `bbkt config show`
reports both, and never prints the token itself:

```yaml
SETTING                VALUE                          SOURCE
url                    https://bitbucket.corp.example config.toml
token_file             ~/.config/bbkt/token           config.toml
default_target_branch  develop                        config.toml
ca_file                /etc/ssl/certs/corp-ca.pem     $BBKT_CA_FILE
token                  found                          ~/.config/bbkt/token
```

There is deliberately no `--token` flag: flag values land in `ps` output and
shell history.

## Use

```bash
bbkt pr list                 # open PRs in this repo
bbkt pr list --reviewing     # awaiting your review, across every repo
bbkt pr list --mine          # authored by you, across every repo
bbkt pr view 42
bbkt pr create               # from the checked-out branch
bbkt pr approve              # defaults to this branch's PR
bbkt pr merge 42
bbkt pr open --print
```

The project key and repository slug come from the git remote, so commands need no
arguments inside a clone. `--repo PROJECT/SLUG` targets another repository from
anywhere; `--json` is on every read.

`--reviewing` and `--mine` hit Bitbucket's own `/inbox` and `/dashboard`
endpoints, which already know every repository on the instance. There is no local
repository registry to maintain.

## Reviewing the diff

`bbkt` deliberately has no diff command. Bitbucket's web view renders a merge-base
diff, and git produces exactly that locally:

```bash
git fetch origin
git diff origin/develop...HEAD | delta        # scan your own PR
nvim -c 'DiffviewOpen origin/develop...HEAD'  # review it side by side
```

`bbkt pr view` prints the corresponding command for the branches the pull request
actually uses, ready to paste.

Three dots, not two: `origin/develop...HEAD` diffs from the **merge base** — what
your branch adds since it diverged. Two dots (`origin/develop..HEAD`) compares the
tips, so every commit that landed on `develop` after you branched shows up
backwards, as if you had deleted it.

Always the `origin/` ref, never a bare `develop`. A pull request's target is
usually a long-lived branch you never check out, so a bare name either fails to
resolve or resolves to a local copy pinned at whenever you made it — silently
diffing against a stale target. `git fetch` keeps `origin/develop` current with no
local branch and nothing to maintain.

## Jira linkage

Bitbucket Data Center pushes branches, commits, and pull requests to Jira, which
links them by finding a work item key in the branch name, commit message, or PR
title. `bbkt pr create` derives the title from the branch name and preserves the
key, so `feature/PROJ-123-fix-the-parser` becomes `PROJ-123 fix the parser` and
lands in the Jira development panel.

## Not implemented

Inline review comments. Reading them means recursing `/activities`, and writing
them means constructing a nested `anchor` — worth doing, but only once the rest
of the workflow has proven itself.

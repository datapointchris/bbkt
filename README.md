# bbkt

Pull request lifecycle for **self-hosted Bitbucket Data Center**, from the terminal.

Bitbucket Cloud and Bitbucket Data Center are different products with incompatible
APIs — Cloud is REST 2.0 at `api.bitbucket.org`, Data Center is REST 1.0 at
`<your-host>/rest/api/1.0`. Every maintained Bitbucket CLI targets Cloud, which
is why this exists.

## Install

```bash
go install github.com/datapointchris/bbkt@latest
```

## Configure

Create an HTTP access token in Bitbucket: **Profile picture → Manage account →
HTTP access tokens**. A personal (user-level) token is required — project- and
repository-scoped tokens cannot merge a pull request, because a merge creates a
commit and that needs a real user identity.

`~/.config/bbkt/config.toml`:

```toml
url = "https://bitbucket.corp.example"
token_file = "~/.config/bbkt/token"
default_target_branch = "main"

# Only when the instance uses an internally-issued TLS certificate.
# ca_file = "/etc/ssl/certs/corp-root-ca.pem"
```

```bash
install -m 600 /dev/null ~/.config/bbkt/token
printf '%s' 'YOUR_TOKEN' > ~/.config/bbkt/token
```

`BBKT_URL`, `BBKT_TOKEN`, `BBKT_TOKEN_FILE`, and `BBKT_CA_FILE` override the file.
There is deliberately no `--token` flag: flag values land in `ps` output and shell
history.

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
git diff origin/main...HEAD | delta      # scan
nvim -c 'DiffviewOpen origin/main...HEAD'  # review
```

`bbkt pr view` prints the corresponding command for the branches the pull request
actually uses.

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

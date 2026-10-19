# bbkt

Pull requests on **self-hosted Bitbucket Data Center**, worked from the terminal. It shows what
waits on your review and where your own pull requests stand. It reads and writes their review
comments, down to one line of the diff. It creates, approves and merges them.

Bitbucket Cloud and Bitbucket Data Center are different products with incompatible APIs. Cloud is
REST 2.0 at `api.bitbucket.org`. Data Center is REST 1.0 at `<your-host>/rest/api/1.0`. Every
maintained Bitbucket CLI targets Cloud, which is why this exists.

The boundary is git. bbkt asks Bitbucket what only Bitbucket knows: reviewers, approvals, merge
checks, and which pull requests need you. Fetching, diffing and pushing stay in git.

## Install

```bash
go install github.com/datapointchris/bbkt@latest  # builds from source; needs a Go toolchain
bbkt update                                       # later: the latest release, checked against its checksums
```

Each GitHub release also carries prebuilt archives for Linux and macOS, so `bbkt update` needs no Go
toolchain. On a terminal, bbkt checks for a newer release once a day, and prints one line when it
finds one. Setting `BBKT_NO_AUTO_UPDATE` to any value turns that check off.

## Starting from scratch

```bash
bbkt config init          # asks for the instance URL and the token; never overwrites either file
bbkt config check         # signs in and names you; exits 1 naming the step that failed
bbkt config show          # each setting, and whether the file or the environment supplied it

bbkt pr list --reviewing  # what waits on your review, answered from any directory
cd path/to/a/clone        # any clone whose remote is the Data Center instance
bbkt pr list              # open pull requests in that repository: proves the remote parsed
```

**The token is the one input no command creates.** Make it in Bitbucket under **profile picture →
Manage account → HTTP access tokens**. It must be a **personal** token. A project- or
repository-scoped token cannot merge a pull request, because a merge creates a commit and that needs
a real user identity.

`config init` writes `~/.config/bbkt/config.toml`, or the same path under `$XDG_CONFIG_HOME`, and a
`0600` token file beside it. A file that already exists is reported and left as it was. The config
is the only record of the instance URL, and the token cannot be regenerated. In a script, pipe the
token in:

```bash
pass show work/bitbucket | bbkt config init --no-input
```

Piped, the URL is written commented out. Set it afterwards with `bbkt config edit`, or supply it as
`BBKT_URL`.

`config check` is the only check that means anything. Whether the URL is reachable, the CA is right
and the token is accepted are answered by the round trip, never by reading the file. It also warns
when the token file is readable by other users.

## The grammar

Commands take the shape `bbkt pr VERB [ID]`. The noun is always `pr`, so a create → review → merge
loop changes only the verb. Review comments sit one level down, as `bbkt pr comment VERB [ID]`.

A verb whose usage shows `[id]` takes it optionally. Given, it targets that pull request. Omitted,
it targets the open pull request whose source is the checked-out branch. `pr view` is the exception
and requires the id.

Where you are is inferred rather than configured. The project key and repository slug come from the
git remote, `origin` unless `--remote` names another. These remote forms parse:

- an HTTP clone URL under `/scm/`
- an `ssh://` URL such as `ssh://git@host:7999/PROJECT/slug.git`
- git's scp form, `git@host:PROJECT/slug.git`, which an instance serving SSH on port 22 hands out

`-R PROJECT/SLUG` targets another repository from anywhere.

`pr list`, `pr view`, `pr create`, the three `pr comment` verbs and `config show` take `--json`.
Progress and confirmations go to stderr, so stdout carries only the answer. `--no-input` never
prompts, and fails naming the flag that would have answered. Any partial command run with no
arguments prints what comes next.

## Which surface answers which question

| Ask | Surface | Reads |
| --- | --- | --- |
| What waits on my review | `pr list --reviewing` | Bitbucket's inbox, across every repository |
| Where do my own pull requests stand | `pr list --mine` | Bitbucket's dashboard, narrowed to what you authored |
| What is open in this repository | `pr list` | the repository the git remote names |
| What does this pull request say, and how do I diff it | `pr view <id>` | the pull request; the diff itself stays in git |
| Where is it in the web view | `pr open` | the pull request's own link |
| What have reviewers said, and on which lines | `pr comment list` | the pull request's activity stream, threads nested |
| Say something on a line, a file or a thread | `pr comment create`, `pr comment reply` | the pull request's own diff, to place the comment |
| Can it merge, and merge it | `pr merge` | Bitbucket's merge checks, then the pull request's version |
| Which value is bbkt actually using | `config show` | `config.toml` and the `BBKT_*` environment |
| Does the config reach Bitbucket | `config check` | the instance, signed in as you |

## `pr list` — Bitbucket already knows what needs you, so nothing is kept locally

```bash
bbkt pr list --reviewing         # awaiting your review, across every repository
bbkt pr list --mine              # authored by you, across every repository
bbkt pr list                     # open pull requests in this clone's repository
bbkt pr view 42 -R PROJ/service  # a cross-repo row, followed up with the REPO it named
```

`--reviewing` and `--mine` ask Bitbucket's own `/inbox/pull-requests` and
`/dashboard/pull-requests` endpoints, which already cover every repository on the instance. Both
ignore the remote and answer from any directory. There is no local repository list to keep in sync.

A cross-repo row adds a REPO column, because its id belongs to that repository. The command that
follows it names the repository with `-R`. A bare `pr list` answers for the current clone's
repository alone.

## `pr view` — the diff stays in git, which already renders it

```bash
bbkt pr view 42                               # title, branches, reviewers, and the diff command
git fetch origin                              # keeps origin/develop current with no local branch
git diff origin/develop...HEAD | delta        # scan your own pull request
nvim -c 'DiffviewOpen origin/develop...HEAD'  # review it side by side
```

bbkt has no diff command. Bitbucket's web view renders a merge-base diff, and git produces exactly
that locally. A copy fetched over the API would be a worse version of what git already makes.
`pr view` ends on the `git diff origin/<target>...origin/<source>` line for the branches the pull
request uses, ready to paste. Its refs name the remote `--remote` picks, which is `origin` unless
you name another.

Three dots, not two. `origin/develop...HEAD` diffs from the **merge base**: what your branch adds
since it diverged. Two dots (`origin/develop..HEAD`) compares the tips. Every commit that landed on
`develop` after you branched then shows up backwards, as if you had deleted it.

Always the `origin/` ref, never a bare `develop`. A pull request's target is usually a long-lived
branch you never check out. A bare name either fails to resolve or resolves to a local copy pinned
at whenever you made it, which silently diffs against a stale target. `git fetch` keeps
`origin/develop` current with no local branch and nothing to maintain.

## `pr comment` — Bitbucket's own diff decides which side a line comment lands on

```bash
bbkt pr comment list 42                                              # every thread, with ids
bbkt pr comment create 42 --path src/parser.go --line 40             # a line as the PR leaves it
bbkt pr comment create 42 --path src/parser.go --line 37 --side old  # a removed line
bbkt pr comment reply 42 --to 1001                                   # answer a thread
```

`list` reads the pull request's activity stream, where each comment arrives with its replies nested
beneath it. Comments on the pull request itself come first, then file and line comments by path and
line. The number beside each comment is its id, which `reply --to` takes. `--json` prints each
thread as a comment object holding its id, version, text, author, dates, severity, state and
anchor, with its replies nested under `comments`.

A line comment sits on one side of the diff. `new` is the file as the pull request leaves it, where
an added line lives. `old` is the file before the pull request, where a removed line lives. An
unchanged line is on both sides, and its number can differ between them. `--line` counts on the new
side unless `--side old` says otherwise. On the new side, that is the number your editor shows on
the pull request's branch.

Bitbucket also needs to know whether the line was added, removed or unchanged. A wrong answer puts
the comment somewhere other than the line meant, so bbkt never asks for it. It reads that file's
diff from Bitbucket and takes the answer from there, and the comment lands where the web view draws
the line. A line the diff does not show is refused. A path the pull request leaves alone is refused
with the paths it does change, counted like `--path` from the repository root. Both are refused
before the editor opens.

Without `--path`, the comment goes on the pull request itself. With `--path` and no `--line`, it
goes on the file.

The body comes from `--body TEXT`, `--body-file FILE`, or `--body-file -` for stdin. Without one,
on a terminal, `$VISUAL` or `$EDITOR` opens on a template, the way `git commit` does. The template
quotes the line or comment being answered below git's scissors line, and everything from that line
down is left out. An empty comment posts nothing. If Bitbucket refuses the post, what you wrote is
kept in a file and the error names it. Off a terminal, or under `--no-input`, a missing body exits 2
before anything is sent.

## `pr create` — the branch name becomes the title, and the Jira key survives it

```bash
bbkt pr create               # from the checked-out branch into default_target_branch
bbkt pr create --target uat  # another target, for this pull request only
```

Bitbucket Data Center pushes branches, commits and pull requests to Jira. Jira links each one by
finding a work item key in the branch name, a commit message or the pull request title. Without
`--title`, `pr create` builds the title from the branch's last path segment and keeps the key
intact. `feature/PROJ-123-fix-the-parser` becomes `PROJ-123 fix the parser` and lands in the Jira
development panel. An underscore separates words too, so `bugfix/DATA-7_null_handling` becomes
`DATA-7 null handling`.

Without `--target`, the pull request goes to `default_target_branch` from the config, which is
`main` unless set. An instance running gitflow usually wants `develop` there.

## `pr approve`, `pr needs-work`, `pr unapprove` — one participant status, visible to everyone

```bash
bbkt pr approve        # the open pull request from the checked-out branch
bbkt pr needs-work 42  # a specific one
bbkt pr unapprove 42   # withdraw either verdict
```

Bitbucket records each of these as your participant status on the pull request. Each one replaces
the status you set earlier, and everyone on the pull request sees it.

## `pr merge` — Bitbucket's veto first, the version read last

```bash
bbkt pr merge           # this branch's pull request, after a y/N prompt
bbkt pr merge 42 --yes  # no prompt, which is what a script passes
```

Merge asks Bitbucket first whether the pull request can merge. Conflicts, unmet approval
requirements and failed builds are refused with Bitbucket's own reasons, before anything is written.

Then it reads the pull request's version. Bitbucket uses the version for optimistic concurrency, so
a stale one fails rather than merging commits you never saw. The version is read immediately before
the prompt and never kept between commands.

Without a terminal, or under `--no-input`, merge refuses to prompt. It exits 2 naming `--yes`,
before anything is asked of Bitbucket. It needs the personal token from the first run, since a
scoped token cannot create the merge commit.

## `config` — a value can come from two places, and `show` says which won

```bash
bbkt config show     # each setting, its value, and the input that supplied it
bbkt config edit     # config.toml in $VISUAL or $EDITOR, created first if absent
bbkt config example  # every option, annotated; reference rather than a scaffold
```

`BBKT_URL`, `BBKT_TOKEN_FILE` and `BBKT_CA_FILE` each override the file's field. `BBKT_TOKEN`
supplies the token itself and wins over any token file. So the value in use is not always the one
written down. `config show` reports both, and never prints the token:

```text
config  /home/you/.config/bbkt/config.toml

SETTING                VALUE                           SOURCE
url                    https://bitbucket.corp.example  config.toml
token_file             ~/.config/bbkt/token            config.toml
default_target_branch  develop                         config.toml
ca_file                /etc/ssl/certs/corp-ca.pem      $BBKT_CA_FILE
token                  found                           /home/you/.config/bbkt/token
```

A missing config file is not an error. A box with only `BBKT_URL` and `BBKT_TOKEN` set runs every
command.

There is deliberately no `--token` flag: a flag value lands in `ps` output and shell history.

`ca_file` is for an instance that terminates TLS with an internally issued certificate. That CA is
added to the system pool for bbkt's requests. There is no option to skip verification.

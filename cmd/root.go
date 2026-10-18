package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/datapointchris/goclikit"
	"github.com/datapointchris/goselfupdate/autoupdate"
	"github.com/datapointchris/gotermstyle/cobrahelp"
	"github.com/spf13/cobra"

	"github.com/datapointchris/bbkt/bitbucket"
	"github.com/datapointchris/bbkt/config"
)

var (
	repoSpec   string
	remoteName string
	noInput    bool
)

var rootCmd = &cobra.Command{
	Use:   "bbkt",
	Short: "Work Bitbucket Data Center pull requests from the terminal",
	Long: `bbkt drives the pull request lifecycle on a self-hosted Bitbucket Data Center
instance.

Start with bbkt pr list --reviewing for what waits on your review, or
bbkt pr list --mine for where your own pull requests stand. Both ask Bitbucket's
cross-repo views and answer from any directory; a bare bbkt pr list shows only the
repository you are standing in. A cross-repo row's id belongs to its REPO
column, so follow it with -R: bbkt pr view 42 -R PROJ/service.

Commands follow the shape: bbkt pr VERB [ID] — the noun is always "pr", so a
create → review → merge loop changes only the verb.

One rule: an id in brackets is optional. Given, it targets that pull request;
omitted, it targets the open pull request for the checked-out branch. view <id>
is the one verb that requires it.

Where you are is inferred, not configured. The project key and repository slug
come from the git remote, so nothing needs a list of repositories. Every level
is self-documenting: run any partial command with no arguments or --help to see
what comes next.`,
	Example: `  bbkt pr list --reviewing   what is waiting on your review, across every repo
  bbkt pr list --mine        your own open pull requests, across every repo
  bbkt pr create             open a pull request from the checked-out branch
  bbkt pr view 42            title, reviewers, and the command to diff it
  bbkt pr merge              merge the pull request for this branch`,
	SilenceErrors: true,
	SilenceUsage:  true,
}

func Execute() {
	if err := run(rootCmd, autoupdate.Config{Update: updateConfig()}); err != nil {
		if !errors.Is(err, goclikit.ErrReported) {
			fmt.Fprintln(os.Stderr, err)
		}
		// 2 says the command line was wrong rather than the run, which is the
		// only failure a caller should retry with different arguments.
		if errors.Is(err, goclikit.ErrUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// run drives root through goclikit's bootstrap and returns its error. Separate
// from Execute, which exits, so a test can drive the real tree with the version
// check suppressed and read the error.
func run(root *cobra.Command, config autoupdate.Config) error {
	return goclikit.Execute(context.Background(), root, config, goclikit.WithNotFound(notFound))
}

// Command groups keep the help readable as verbs accumulate: a flat list mixes
// the domain with the plumbing, and the domain is what someone is looking for.
const (
	groupPullRequests = "pull-requests"
	groupTool         = "tool"
)

func init() {
	rootCmd.AddGroup(
		&cobra.Group{ID: groupPullRequests, Title: "Pull requests"},
		&cobra.Group{ID: groupTool, Title: "Managing bbkt itself"},
	)
	update := goclikit.UpdateCommand(updateConfig())
	update.GroupID = groupTool
	rootCmd.AddCommand(prCmd, configCmd, update, versionCmd)
	cobrahelp.Install(rootCmd)

	rootCmd.PersistentFlags().StringVarP(&repoSpec, "repo", "R", "", "target PROJECT/SLUG instead of the current directory's remote")
	rootCmd.PersistentFlags().StringVar(&remoteName, "remote", "origin", "git remote to read the repository from")
	rootCmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "never prompt; fail naming the flag that would have answered")
}

func newClient() (*bitbucket.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	token, err := cfg.Token()
	if err != nil {
		return nil, err
	}
	return newClientFrom(cfg, token)
}

// newClientFrom builds the client from an already-resolved config, so
// `config check` can report each input as it resolves rather than failing the
// whole setup on one line.
func newClientFrom(cfg *config.Config, token config.Token) (*bitbucket.Client, error) {
	return bitbucket.NewClient(bitbucket.Options{
		BaseURL: cfg.URL,
		Token:   token.Value,
		CAFile:  cfg.CAFile,
	})
}

// resolveRepo prefers an explicit --repo, falling back to the git remote.
func resolveRepo() (bitbucket.Repo, error) {
	if repoSpec != "" {
		return bitbucket.ParseSpec(repoSpec)
	}
	return bitbucket.CurrentRepo(remoteName)
}

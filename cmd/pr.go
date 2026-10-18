package cmd

import (
	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"
)

// Verb groups within `pr`: reading is safe and browsing-shaped, acting changes
// state on the server. Separating them makes the destructive half visible.
const (
	groupRead = "read"
	groupAct  = "act"
)

// A namespace: bare, it teaches the verbs rather than erroring on a missing
// subcommand, and a word naming no verb exits 2 with the verbs near it.
var prCmd = goclikit.AsNamespace(&cobra.Command{
	Use:     "pr",
	GroupID: groupPullRequests,
	Short:   "Create, review, and merge pull requests",
	Long: `Pull request commands.

The project key and repository slug come from the current directory's git
remote; -R PROJECT/SLUG targets another repository from anywhere.

A bare list answers for this repository alone. --reviewing and --mine ignore the
remote and answer across every repository, so take a row's id together with
the REPO it names: bbkt pr view 42 -R PROJ/service.

Every verb listed with [id] also works without one, in which case it targets the
open pull request for the checked-out branch — so the common case is the short
one. view <id> is the exception, and needs the id.`,
	Example: `  bbkt pr list                    open pull requests in this repository
  bbkt pr list --reviewing        awaiting your review, across every repository
  bbkt pr view 42                 one pull request in detail
  bbkt pr approve                 approve this branch's pull request
  bbkt pr merge 42 --yes          merge without the confirmation prompt`,
})

func init() {
	prCmd.AddGroup(
		&cobra.Group{ID: groupRead, Title: "Reading"},
		&cobra.Group{ID: groupAct, Title: "Acting"},
	)
	goclikit.WithRecoveryHints(prCmd, prRecoveryHints...)
	rootCmd.AddCommand(prCmd)
}

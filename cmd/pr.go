package cmd

import "github.com/spf13/cobra"

// Verb groups within `pr`: reading is safe and browsing-shaped, acting changes
// state on the server. Separating them makes the destructive half visible.
const (
	groupRead = "read"
	groupAct  = "act"
)

var prCmd = &cobra.Command{
	Use:     "pr",
	GroupID: groupPullRequests,
	Short:   "Create, review, and merge pull requests",
	Long: `Pull request commands.

The project key and repository slug come from the current directory's git
remote; -R PROJECT/SLUG targets another repository from anywhere.

Every verb that takes an id also works without one, in which case it targets the
open pull request for the checked-out branch — so the common case is the short
one.`,
	Example: `  bbkt pr list                    open pull requests in this repository
  bbkt pr list --reviewing        awaiting your review, across every repository
  bbkt pr view 42                 one pull request in detail
  bbkt pr approve                 approve this branch's pull request
  bbkt pr merge 42 --yes          merge without the confirmation prompt`,
	// Help is never wrong; a bare `bbkt pr` teaches the verbs rather than
	// erroring on a missing subcommand.
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	prCmd.AddGroup(
		&cobra.Group{ID: groupRead, Title: "Reading"},
		&cobra.Group{ID: groupAct, Title: "Acting"},
	)
	rootCmd.AddCommand(prCmd)
}

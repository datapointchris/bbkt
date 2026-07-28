package cmd

import "github.com/spf13/cobra"

var prCmd = &cobra.Command{
	Use:   "pr",
	Short: "Create, review, and merge pull requests",
	Long: "Pull request commands. The project and repository come from the current\n" +
		"directory's git remote unless --repo is given.",
	// Help is never wrong; a bare `bbkt pr` should teach the verbs rather than
	// error on a missing subcommand.
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(prCmd)
}

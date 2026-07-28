package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"
)

var viewJSON bool

var prViewCmd = &cobra.Command{
	Use:   "view <id>",
	Short: "Show a pull request",
	Long: "Shows title, branches, reviewers, and approval state.\n\n" +
		"The diff is deliberately not fetched — `git diff target...source` renders the\n" +
		"same merge-base diff Bitbucket's web view shows, locally and without the API.",
	Example: "  bbkt pr view 42\n" +
		"  bbkt pr view 42 --json",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("pull request id must be a number, got %q", args[0])
		}

		repo, err := resolveRepo()
		if err != nil {
			return err
		}
		client, err := newClient()
		if err != nil {
			return err
		}
		pr, err := client.GetPullRequest(repo, id)
		if err != nil {
			return err
		}

		if viewJSON {
			return emitJSON(cmd, pr)
		}

		out := cmd.OutOrStdout()
		approved, total := pr.ApprovalCount()
		writef(out, "#%d  %s\n", pr.ID, pr.Title)
		writef(out, "%s → %s\n", pr.FromRef.DisplayID, pr.ToRef.DisplayID)
		writef(out, "%s by %s · updated %s\n", pr.State, pr.Author.User.Name, pr.Updated().Format("2006-01-02 15:04"))
		writef(out, "approvals %d/%d\n", approved, total)

		if len(pr.Reviewers) > 0 {
			writef(out, "\n")
			table := newTable(out)
			writef(table, "REVIEWER\tSTATUS\n")
			for _, r := range pr.Reviewers {
				status := r.Status
				if status == "" {
					status = "UNAPPROVED"
				}
				writef(table, "%s\t%s\n", r.User.Name, status)
			}
			if err := table.Flush(); err != nil {
				return err
			}
		}

		if pr.Description != "" {
			writef(out, "\n%s\n", pr.Description)
		}
		if url := pr.URL(); url != "" {
			writef(out, "\n%s\n", url)
		}
		// The merge-base diff is what Bitbucket's web view renders, and git
		// produces it locally without touching the API.
		writef(out, "\ndiff: git diff %s...%s\n", pr.ToRef.DisplayID, pr.FromRef.DisplayID)
		return nil
	},
}

func init() {
	prViewCmd.Flags().BoolVar(&viewJSON, "json", false, "Output the pull request as JSON to stdout")
	prCmd.AddCommand(prViewCmd)
}

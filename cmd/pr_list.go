package cmd

import (
	"fmt"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"

	"github.com/datapointchris/bbkt/bitbucket"
)

var (
	listJSON      bool
	listLimit     int
	listState     string
	listReviewing bool
	listMine      bool
)

var prListCmd = &cobra.Command{
	Use:     "list",
	GroupID: groupRead,
	Short:   "List pull requests",
	Long: "Lists pull requests for the current repository.\n\n" +
		"--reviewing and --mine query Bitbucket's own cross-repository views, so they\n" +
		"work from anywhere and cover every repository on the instance.",
	Example: "  bbkt pr list\n" +
		"  bbkt pr list --reviewing\n" +
		"  bbkt pr list --mine --state MERGED -n 5\n" +
		"  bbkt pr list -R PROJ/service --json",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if listReviewing && listMine {
			return goclikit.UsageError(fmt.Errorf("--reviewing and --mine are mutually exclusive"))
		}
		// Ahead of newClient, so a cap the command cannot use is refused before
		// the token is read and the VPN round trip is spent on it.
		if listLimit < 0 {
			return goclikit.UsageError(fmt.Errorf("--limit cannot be negative; the smallest cap is 0"))
		}

		client, err := newClient()
		if err != nil {
			return err
		}

		var prs []bitbucket.PullRequest
		crossRepo := listReviewing || listMine
		switch {
		case listReviewing:
			prs, err = client.ListInbox(listLimit)
		case listMine:
			prs, err = client.ListDashboard(listState, "AUTHOR", listLimit)
		default:
			var repo bitbucket.Repo
			repo, err = resolveRepo()
			if err != nil {
				return err
			}
			prs, err = client.ListPullRequests(repo, bitbucket.ListOptions{State: listState, Limit: listLimit})
		}
		if err != nil {
			return err
		}

		if listJSON {
			return emitJSON(cmd, prs)
		}

		if len(prs) == 0 {
			// Nothing was requested when the cap is zero, so nothing was
			// measured either. "No pull requests" would report the instance as
			// bare on the strength of a question never asked.
			if listLimit == 0 {
				infof(cmd, "--limit 0 asked for no pull requests.")
				return nil
			}
			infof(cmd, "No pull requests.")
			return nil
		}

		table := newTable(cmd.OutOrStdout())
		if crossRepo {
			writef(table, "ID\tREPO\tTITLE\tAUTHOR\tAPPROVALS\tBRANCH\n")
		} else {
			writef(table, "ID\tTITLE\tAUTHOR\tAPPROVALS\tBRANCH\n")
		}
		for _, pr := range prs {
			approved, total := pr.ApprovalCount()
			approvals := fmt.Sprintf("%d/%d", approved, total)
			if crossRepo {
				writef(table, "%d\t%s\t%s\t%s\t%s\t%s\n",
					pr.ID, pr.Repo(), clip(pr.Title, 50), pr.Author.User.Name, approvals, pr.FromRef.DisplayID)
				continue
			}
			writef(table, "%d\t%s\t%s\t%s\t%s\n",
				pr.ID, clip(pr.Title, 60), pr.Author.User.Name, approvals, pr.FromRef.DisplayID)
		}
		if err := table.Flush(); err != nil {
			return err
		}

		// A full page is the one screen that cannot say whether it is the whole
		// answer, and the row count alone reads as the total. The hint goes to
		// stderr so it reaches a person and not a pipe.
		if len(prs) == listLimit {
			infof(cmd, "\nStopped at the %d-row cap; -n raises it.", listLimit)
		}
		return nil
	},
}

func init() {
	prListCmd.Flags().BoolVar(&listJSON, "json", false, "Output pull requests as JSON to stdout")
	prListCmd.Flags().IntVarP(&listLimit, "limit", "n", 25, "Maximum number of pull requests to show")
	prListCmd.Flags().StringVarP(&listState, "state", "s", "OPEN", "OPEN, MERGED, DECLINED, or ALL")
	prListCmd.Flags().BoolVar(&listReviewing, "reviewing", false, "pull requests awaiting your review, across every repository")
	prListCmd.Flags().BoolVar(&listMine, "mine", false, "pull requests you authored, across every repository")
	prCmd.AddCommand(prListCmd)
}

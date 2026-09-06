package cmd

import (
	"fmt"
	"strings"

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
		// Both answers to a --limit the command cannot use are given here,
		// ahead of newClient. A negative is a typing mistake; a zero is a
		// complete request whose answer is known. Neither owes a token, a git
		// remote or a round trip, and reaching newClient for either turns a
		// question that cannot fail into an exit 1.
		if listLimit < 0 {
			return goclikit.UsageError(fmt.Errorf("--limit cannot be negative; 0 is the smallest it takes"))
		}
		if listLimit == 0 {
			if listJSON {
				return emitJSON(cmd, []bitbucket.PullRequest{})
			}
			infof(cmd, "--limit 0 asked for no pull requests; bbkt pr list -n 25 shows the first 25.")
			return nil
		}

		client, err := newClient()
		if err != nil {
			return err
		}

		var prs []bitbucket.PullRequest
		var truncated bool
		crossRepo := listReviewing || listMine
		switch {
		case listReviewing:
			prs, truncated, err = client.ListInbox(&listLimit)
		case listMine:
			prs, truncated, err = client.ListDashboard(listState, "AUTHOR", &listLimit)
		default:
			var repo bitbucket.Repo
			repo, err = resolveRepo()
			if err != nil {
				return err
			}
			prs, truncated, err = client.ListPullRequests(repo,
				bitbucket.ListOptions{State: listState, Limit: &listLimit})
		}
		if err != nil {
			return err
		}

		if listJSON {
			return emitJSON(cmd, prs)
		}

		if len(prs) == 0 {
			infof(cmd, "%s", noPullRequests(listState))
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

		// The server said whether rows were left behind, so this reports a fact
		// rather than a guess from the row count. That guess is wrong exactly
		// where a repository holds as many pull requests as the limit: it
		// announces hidden rows and raising --limit produces none.
		if truncated {
			infof(cmd, "\nMore pull requests exist beyond --limit %d; bbkt pr list -n %d shows more.",
				listLimit, listLimit*2)
		}
		return nil
	},
}

// noPullRequests names the flag that widens the question. An empty listing is
// read by someone who expected rows, and --state is what they have not tried;
// with every state already asked for there is nothing further to offer.
func noPullRequests(state string) string {
	if strings.EqualFold(state, "ALL") {
		return "No pull requests."
	}
	return fmt.Sprintf("No %s pull requests; bbkt pr list --state ALL includes every state.",
		strings.ToLower(state))
}

func init() {
	prListCmd.Flags().BoolVar(&listJSON, "json", false, "Output pull requests as JSON to stdout")
	prListCmd.Flags().IntVarP(&listLimit, "limit", "n", 25, "Maximum number of pull requests to show")
	prListCmd.Flags().StringVarP(&listState, "state", "s", "OPEN", "OPEN, MERGED, DECLINED, or ALL")
	prListCmd.Flags().BoolVar(&listReviewing, "reviewing", false, "pull requests awaiting your review, across every repository")
	prListCmd.Flags().BoolVar(&listMine, "mine", false, "pull requests you authored, across every repository")
	prCmd.AddCommand(prListCmd)
}

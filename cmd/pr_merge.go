package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

var mergeYes bool

var prMergeCmd = &cobra.Command{
	Use:     "merge [id]",
	GroupID: groupAct,
	Short:   "Merge a pull request",
	Long: "Merges a pull request after checking Bitbucket's own merge veto — conflicts,\n" +
		"unmet approval requirements, and failed builds all surface before anything is\n" +
		"written.\n\n" +
		"With no id, targets the pull request for the checked-out branch.",
	Example: "  bbkt pr merge 42\n" +
		"  bbkt pr merge 42 --yes",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, err := resolveRepo()
		if err != nil {
			return err
		}
		client, err := newClient()
		if err != nil {
			return err
		}
		id, err := resolvePullRequestID(client, repo, args)
		if err != nil {
			return err
		}

		status, err := client.CanMerge(repo, id)
		if err != nil {
			return err
		}
		if !status.CanMerge {
			if status.Conflicted {
				return fmt.Errorf("#%d has conflicts and cannot be merged", id)
			}
			reasons := make([]string, 0, len(status.Vetoes))
			for _, v := range status.Vetoes {
				reasons = append(reasons, v.SummaryMessage)
			}
			if len(reasons) == 0 {
				return fmt.Errorf("#%d cannot be merged", id)
			}
			return fmt.Errorf("#%d cannot be merged: %s", id, strings.Join(reasons, "; "))
		}

		// The version is read immediately before the merge: Bitbucket uses it for
		// optimistic concurrency, so a stale one fails rather than merging commits
		// that landed since the pull request was last read.
		pr, err := client.GetPullRequest(repo, id)
		if err != nil {
			return err
		}

		if !mergeYes {
			ok, err := confirm(cmd, fmt.Sprintf("Merge #%d %q into %s?", pr.ID, pr.Title, pr.ToRef.DisplayID))
			if err != nil {
				return err
			}
			if !ok {
				infof(cmd, "Aborted.")
				return nil
			}
		}

		merged, err := client.Merge(repo, id, pr.Version)
		if err != nil {
			return err
		}
		infof(cmd, "Merged #%d into %s", merged.ID, merged.ToRef.DisplayID)
		return nil
	},
}

// confirm prompts on a TTY and otherwise fails naming the flag that would have
// answered, because a prompt on a closed stdin deadlocks the caller with no
// output and no exit code.
func confirm(cmd *cobra.Command, question string) (bool, error) {
	if noInput || !isatty.IsTerminal(os.Stdin.Fd()) {
		return false, fmt.Errorf("refusing to prompt without a terminal; pass --yes to merge")
	}
	writef(cmd.ErrOrStderr(), "%s [y/N] ", question)
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false, err
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

func init() {
	prMergeCmd.Flags().BoolVar(&mergeYes, "yes", false, "merge without confirmation")
	prCmd.AddCommand(prMergeCmd)
}

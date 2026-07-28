package cmd

import (
	"fmt"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/datapointchris/bbkt/bitbucket"
)

func reviewCommand(use, short, status string) *cobra.Command {
	return &cobra.Command{
		Use:   use + " [id]",
		Short: short,
		Long:  short + ".\n\nWith no id, targets the pull request for the checked-out branch.",
		Args:  cobra.MaximumNArgs(1),
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
			if err := client.SetReviewStatus(repo, id, status); err != nil {
				return err
			}
			infof(cmd, "#%d marked %s", id, status)
			return nil
		},
	}
}

// resolvePullRequestID takes an explicit id when given and otherwise finds the
// open pull request for the checked-out branch.
func resolvePullRequestID(client *bitbucket.Client, repo bitbucket.Repo, args []string) (int, error) {
	if len(args) == 1 {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return 0, fmt.Errorf("pull request id must be a number, got %q", args[0])
		}
		return id, nil
	}
	pr, err := pullRequestForCurrentBranch(client, repo)
	if err != nil {
		return 0, err
	}
	return pr.ID, nil
}

func pullRequestForCurrentBranch(client *bitbucket.Client, repo bitbucket.Repo) (*bitbucket.PullRequest, error) {
	branch, err := bitbucket.CurrentBranch()
	if err != nil {
		return nil, err
	}
	prs, err := client.ListPullRequests(repo, bitbucket.ListOptions{State: "OPEN"})
	if err != nil {
		return nil, err
	}
	for i := range prs {
		if prs[i].FromRef.DisplayID == branch {
			return &prs[i], nil
		}
	}
	return nil, fmt.Errorf("no open pull request from branch %q in %s", branch, repo)
}

func init() {
	prCmd.AddCommand(reviewCommand("approve", "Approve a pull request", "APPROVED"))
	prCmd.AddCommand(reviewCommand("unapprove", "Withdraw approval from a pull request", "UNAPPROVED"))
	prCmd.AddCommand(reviewCommand("needs-work", "Mark a pull request as needing work", "NEEDS_WORK"))
}

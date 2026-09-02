package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"

	"github.com/datapointchris/bbkt/bitbucket"
	"github.com/datapointchris/bbkt/config"
)

var (
	createTitle     string
	createBody      string
	createBodyFile  string
	createTarget    string
	createSource    string
	createReviewers []string
	createJSON      bool
)

var prCreateCmd = &cobra.Command{
	Use:     "create",
	GroupID: groupAct,
	Short:   "Open a pull request from the current branch",
	Long: "Creates a pull request from the checked-out branch.\n\n" +
		"When --title is omitted the branch name becomes the title, with any Jira work\n" +
		"item key preserved — Bitbucket's Jira integration links the pull request by\n" +
		"finding that key in the branch name or title.",
	Example: "  bbkt pr create\n" +
		"  bbkt pr create --title 'PROJ-123 fix the parser' --reviewer gopal\n" +
		"  bbkt pr create --body-file notes.md --target develop",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if createBody != "" && createBodyFile != "" {
			return goclikit.UsageError(fmt.Errorf("--body and --body-file are mutually exclusive"))
		}

		repo, err := resolveRepo()
		if err != nil {
			return err
		}

		source := createSource
		if source == "" {
			source, err = bitbucket.CurrentBranch()
			if err != nil {
				return err
			}
		}

		target := createTarget
		if target == "" {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			target = cfg.DefaultTargetBranch
		}
		if source == target {
			return fmt.Errorf("source and target are both %q", source)
		}

		body := createBody
		if createBodyFile != "" {
			data, err := os.ReadFile(createBodyFile)
			if err != nil {
				return fmt.Errorf("reading --body-file: %w", err)
			}
			body = string(data)
		}

		title := createTitle
		if title == "" {
			title = titleFromBranch(source)
		}

		client, err := newClient()
		if err != nil {
			return err
		}
		pr, err := client.CreatePullRequest(repo, bitbucket.CreateOptions{
			Title:        title,
			Description:  body,
			SourceBranch: source,
			TargetBranch: target,
			Reviewers:    createReviewers,
		})
		if err != nil {
			return err
		}

		if createJSON {
			return emitJSON(cmd, pr)
		}
		infof(cmd, "Created #%d  %s → %s", pr.ID, pr.FromRef.DisplayID, pr.ToRef.DisplayID)
		outf(cmd, "%s", pr.URL())
		return nil
	},
}

var branchWords = strings.NewReplacer("-", " ", "_", " ")

// titleFromBranch turns feature/PROJ-123-fix-the-parser into
// "PROJ-123 fix the parser", keeping the work item key intact so the Jira
// development panel picks the pull request up.
func titleFromBranch(branch string) string {
	name := branch
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}

	key, rest := bitbucket.SplitIssueKey(name)
	words := strings.TrimSpace(branchWords.Replace(strings.Trim(rest, "-_ ")))
	if key == "" {
		return words
	}
	return strings.TrimSpace(key + " " + words)
}

func init() {
	prCreateCmd.Flags().StringVarP(&createTitle, "title", "t", "", "pull request title (default: derived from the branch name)")
	prCreateCmd.Flags().StringVarP(&createBody, "body", "b", "", "pull request description")
	prCreateCmd.Flags().StringVar(&createBodyFile, "body-file", "", "read the description from a file")
	prCreateCmd.Flags().StringVar(&createTarget, "target", "", "target branch (default: default_target_branch from config)")
	prCreateCmd.Flags().StringVar(&createSource, "source", "", "source branch (default: the checked-out branch)")
	prCreateCmd.Flags().StringSliceVar(&createReviewers, "reviewer", nil, "reviewer username, repeatable")
	prCreateCmd.Flags().BoolVar(&createJSON, "json", false, "Output the created pull request as JSON to stdout")
	prCmd.AddCommand(prCreateCmd)
}

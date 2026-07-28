package cmd

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"

	"github.com/spf13/cobra"
)

var printOnly bool

var prOpenCmd = &cobra.Command{
	Use:     "open [id]",
	GroupID: groupRead,
	Short:   "Open a pull request in the browser",
	Long: "Opens the pull request in a browser. With no id, opens the pull request for\n" +
		"the checked-out branch.",
	Example: "  bbkt pr open 42\n" +
		"  bbkt pr open --print",
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

		var url string
		if len(args) == 1 {
			id, err := strconv.Atoi(args[0])
			if err != nil {
				return fmt.Errorf("pull request id must be a number, got %q", args[0])
			}
			pr, err := client.GetPullRequest(repo, id)
			if err != nil {
				return err
			}
			url = pr.URL()
		} else {
			pr, err := pullRequestForCurrentBranch(client, repo)
			if err != nil {
				return err
			}
			url = pr.URL()
		}

		if url == "" {
			return fmt.Errorf("bitbucket returned no browser link for the pull request")
		}
		if printOnly {
			outf(cmd, "%s", url)
			return nil
		}
		if err := browse(url); err != nil {
			// Printing the URL keeps the command useful on a headless WSL box
			// where no browser opener is installed.
			outf(cmd, "%s", url)
			return err
		}
		return nil
	},
}

func browse(url string) error {
	var candidates [][]string
	switch runtime.GOOS {
	case "darwin":
		candidates = [][]string{{"open", url}}
	case "windows":
		candidates = [][]string{{"rundll32", "url.dll,FileProtocolHandler", url}}
	default:
		// wslview comes from wslu and is what actually reaches the Windows
		// browser from WSL; xdg-open is the native-Linux path.
		candidates = [][]string{{"wslview", url}, {"xdg-open", url}}
	}
	for _, c := range candidates {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		return exec.Command(c[0], c[1:]...).Start()
	}
	return fmt.Errorf("no browser opener found (tried %s)", candidateNames(candidates))
}

func candidateNames(candidates [][]string) string {
	names := make([]string, 0, len(candidates))
	for _, c := range candidates {
		names = append(names, c[0])
	}
	return fmt.Sprint(names)
}

func init() {
	prOpenCmd.Flags().BoolVar(&printOnly, "print", false, "Print the URL to stdout instead of opening a browser")
	prCmd.AddCommand(prOpenCmd)
}

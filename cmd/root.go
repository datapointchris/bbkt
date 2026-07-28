package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/datapointchris/goselfupdate/autoupdate"
	"github.com/datapointchris/goselfupdate/cobracmd"
	"github.com/spf13/cobra"

	"github.com/datapointchris/bbkt/bitbucket"
	"github.com/datapointchris/bbkt/config"
)

var (
	repoSpec   string
	remoteName string
	noInput    bool
)

var rootCmd = &cobra.Command{
	Use:   "bbkt",
	Short: "Work Bitbucket Data Center pull requests from the terminal",
	Long: "bbkt drives the pull request lifecycle on a self-hosted Bitbucket Data Center\n" +
		"instance. The project and repository are read from the git remote, so most\n" +
		"commands need no arguments inside a clone.",
	SilenceErrors: true,
	SilenceUsage:  true,
}

func Execute() {
	autoConfig := autoupdate.Config{Update: updateConfig()}
	if err := cobracmd.Execute(context.Background(), rootCmd, autoConfig); err != nil {
		if !errors.Is(err, cobracmd.ErrReported) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVarP(&repoSpec, "repo", "R", "", "target PROJECT/SLUG instead of the current directory's remote")
	rootCmd.PersistentFlags().StringVar(&remoteName, "remote", "origin", "git remote to read the repository from")
	rootCmd.PersistentFlags().BoolVar(&noInput, "no-input", false, "never prompt; fail naming the flag that would have answered")
}

func newClient() (*bitbucket.Client, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}
	token, err := cfg.Token()
	if err != nil {
		return nil, err
	}
	return bitbucket.NewClient(bitbucket.Options{
		BaseURL: cfg.URL,
		Token:   token,
		CAFile:  cfg.CAFile,
	})
}

// resolveRepo prefers an explicit --repo, falling back to the git remote.
func resolveRepo() (bitbucket.Repo, error) {
	if repoSpec != "" {
		return bitbucket.ParseSpec(repoSpec)
	}
	return bitbucket.CurrentRepo(remoteName)
}

package cmd

import (
	"github.com/datapointchris/goselfupdate"
	"github.com/datapointchris/goselfupdate/cobracmd"
)

// updateConfig describes where bbkt's releases come from. Shared by the `update`
// command and the daily check in Execute, so the two cannot point at different
// releases.
func updateConfig() goselfupdate.Config {
	return goselfupdate.Config{
		Owner:   "datapointchris",
		Repo:    "bbkt",
		Binary:  "bbkt",
		Version: buildVersion(),
	}
}

func init() {
	rootCmd.AddCommand(cobracmd.New(updateConfig()))
}

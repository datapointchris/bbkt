package cmd

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/datapointchris/goclikit"
	"github.com/datapointchris/goselfupdate/autoupdate"
)

// runLine drives the real tree through run, the path the shipped binary takes,
// with the version check suppressed. goclikit.Execute resolves the command from
// os.Args, so the line goes there rather than through SetArgs.
func runLine(t *testing.T, args ...string) error {
	t.Helper()
	original := os.Args
	os.Args = append([]string{"bbkt"}, args...)
	rootCmd.SetOut(io.Discard)
	rootCmd.SetErr(io.Discard)
	t.Cleanup(func() {
		os.Args = original
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	return run(rootCmd, autoupdate.Config{Suppress: true})
}

// Cobra parses a group's flags and answers --help before it validates the
// group's arguments, so a mistyped verb followed by either would be answered
// as the flag, or with the group's help and exit 0.
func TestAnUnknownVerbIsRefusedWhateverFollowsIt(t *testing.T) {
	for _, args := range [][]string{
		{"pr", "bogus"},
		{"pr", "bogus", "--json"},
		{"pr", "bogus", "--help"},
		{"config", "bogus", "--json"},
		{"config", "bogus", "--help"},
	} {
		err := runLine(t, args...)
		if !errors.Is(err, goclikit.ErrUsage) {
			t.Errorf("%v answered %v, want a usage error", args, err)
			continue
		}
		if want := `unknown command "bogus" for "bbkt ` + args[0] + `"`; !strings.HasPrefix(err.Error(), want) {
			t.Errorf("%v answered %q, want it to open %q", args, err, want)
		}
	}
}

// A mistyped verb is answered with the verbs near it.
func TestAMistypedVerbNamesTheNearOnes(t *testing.T) {
	err := runLine(t, "pr", "mrege")
	if err == nil || !strings.Contains(err.Error(), "\tmerge") {
		t.Errorf("pr mrege answered %v, want it to name merge", err)
	}
}

func TestABareGroupShowsItsHelp(t *testing.T) {
	for _, group := range []string{"pr", "config"} {
		if err := runLine(t, group); err != nil {
			t.Errorf("bare %s failed: %v", group, err)
		}
	}
}

package cmd

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/datapointchris/goclikit"
	"github.com/datapointchris/goselfupdate/autoupdate"
	"github.com/spf13/cobra"
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

// Every command that would prompt, given --no-input, exits 2 before the first
// request. The URL is a closed port, so a refusal that came after a request
// would surface as a connection error.
func TestAPromptWithoutATerminalIsAUsageErrorBeforeAnyRequest(t *testing.T) {
	t.Setenv("BBKT_URL", "http://127.0.0.1:9")
	t.Setenv("BBKT_TOKEN", "unused")
	t.Cleanup(func() { noInput, repoSpec, replyCommentTo = false, "", 0 })
	for _, args := range [][]string{
		{"pr", "comment", "create", "42", "--path", "README.md", "--line", "3"},
		{"pr", "comment", "reply", "42", "--to", "1001"},
		{"pr", "merge", "42"},
	} {
		err := runLine(t, append(args, "-R", "EX/demo", "--no-input")...)
		if !errors.Is(err, goclikit.ErrUsage) {
			t.Errorf("%v answered %v, want a usage error", args, err)
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

// An example is lifted verbatim more often than it is read, so every command
// line on the screens that teach where a session starts must name a real
// command and real flags. A typo here is copied into someone's shell.
func TestEveryGuideExampleIsARealInvocation(t *testing.T) {
	for _, screen := range []*cobra.Command{rootCmd, prCmd} {
		for _, line := range strings.Split(screen.Example, "\n") {
			invocation, _, _ := strings.Cut(strings.TrimSpace(line), "  ")
			words := strings.Fields(invocation)
			if len(words) == 0 || words[0] != "bbkt" {
				t.Errorf("%s example %q does not start with bbkt", screen.Name(), line)
				continue
			}
			found, rest, err := rootCmd.Find(words[1:])
			if err != nil || found == rootCmd {
				t.Errorf("%s example %q names no command: %v", screen.Name(), invocation, err)
				continue
			}
			for _, word := range rest {
				if !strings.HasPrefix(word, "-") {
					continue
				}
				name := strings.TrimLeft(word, "-")
				known := found.Flags().Lookup(name) != nil || found.InheritedFlags().Lookup(name) != nil
				if len(name) == 1 {
					known = found.Flags().ShorthandLookup(name) != nil || found.InheritedFlags().ShorthandLookup(name) != nil
				}
				if !known {
					t.Errorf("%s example %q passes %s, which %s does not take", screen.Name(), invocation, word, found.CommandPath())
				}
			}
		}
	}
}

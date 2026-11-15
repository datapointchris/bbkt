package cmd

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/datapointchris/goclikit"
	"github.com/datapointchris/goselfupdate/autoupdate"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
// group's arguments. Without the namespace check, `pr bogus --json` fails on
// the unknown flag and `pr bogus --help` prints pr's help and exits 0.
func TestAnUnknownVerbIsRefusedWhateverFollowsIt(t *testing.T) {
	for _, args := range [][]string{
		{"pr", "bogus"},
		{"pr", "bogus", "--json"},
		{"pr", "bogus", "--help"},
		{"config", "bogus", "--json"},
		{"config", "bogus", "--help"},
	} {
		if err := runLine(t, args...); !errors.Is(err, goclikit.ErrUsage) {
			t.Errorf("%v answered %v, want a usage error", args, err)
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

// The token comes only from BBKT_TOKEN or a token file. A flag value lands in
// ps output and shell history, where anyone on the host can read it. Walking
// the tree covers every command added later as well.
func TestNoCommandTakesTheTokenAsAFlag(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, set := range []*pflag.FlagSet{c.LocalFlags(), c.InheritedFlags()} {
			if set.Lookup("token") != nil {
				t.Errorf("%s takes --token", c.CommandPath())
			}
		}
		for _, child := range c.Commands() {
			walk(child)
		}
	}
	walk(rootCmd)
}

// A hint an error prints breaks when a neighbor is renamed, and no test of the
// failing command covers that. So every string literal in the source that
// names a bbkt command is resolved against the real tree. Find stops at a group
// when the next word is no verb of it, so a group with words left over is a
// verb that does not exist. Where a sentence names the tool rather than a
// command, the word after bbkt is one prose holds, and the match is skipped.
func TestEveryCommandTheSourceNamesExists(t *testing.T) {
	prose := map[string]bool{
		"cannot": true, "drives": true, "has": true, "itself": true,
		"keeps": true, "reads": true, "resolved": true,
	}
	invocation := regexp.MustCompile(`\bbbkt((?: [a-z][a-z-]*)+)`)
	fset := token.NewFileSet()
	for _, dir := range []string{".", "../config"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			ast.Inspect(file, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				text, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", fset.Position(lit.Pos()), err)
				}
				for _, match := range invocation.FindAllStringSubmatch(text, -1) {
					words := strings.Fields(match[1])
					if prose[words[0]] {
						continue
					}
					found, rest, err := rootCmd.Find(words)
					if err != nil || found == rootCmd || (len(rest) > 0 && found.HasSubCommands()) {
						t.Errorf("%s names %q, which is no bbkt command", fset.Position(lit.Pos()), "bbkt"+match[1])
					}
				}
				return true
			})
		}
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

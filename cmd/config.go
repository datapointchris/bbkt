package cmd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/datapointchris/bbkt/config"
)

var configCmd = &cobra.Command{
	Use:     "config",
	GroupID: groupTool,
	Short:   "Set up and inspect bbkt's configuration",
	Long: `The config file, the token it points at, and what they resolve to.

bbkt reads one TOML file, and every field in it can be overridden by an
environment variable — so the value a command used is not always the one written
in the file. "config show" is the only place that reports which input won, and
"config check" proves the result actually reaches Bitbucket.

Starting from nothing: run "config init", then "config check".`,
	Example: `  bbkt config init     write the config and token files, prompting for both
  bbkt config check    prove the config reaches Bitbucket as you
  bbkt config show     the effective settings, and where each one came from
  bbkt config example  every option, annotated`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create the config file and the token file it points at",
	Long: `Write a starter config at the path bbkt reads, and a 0600 token file beside it.

On a terminal it prompts for the instance URL and the token; the token prompt
does not echo. With --no-input, or when stdin is a pipe, the URL is left
commented out for you to fill in and the token is read from stdin:

  pass show work/bitbucket | bbkt config init --no-input

Idempotent. A file that already exists is reported and left exactly as it was —
config.toml is the only record of the instance URL, and the token file is the
one thing here that cannot be regenerated.`,
	Args: cobra.NoArgs,
	RunE: runConfigInit,
}

var configExampleCmd = &cobra.Command{
	Use:   "example",
	Short: "Print an annotated config showing every option",
	Long: `Print a fully annotated config, every option exercised.

Reference material, not a scaffold: "bbkt config init" writes a minimal starter
instead, because a generated file you have to delete most of is worse than a
short one. Read this alongside the real file while editing it.`,
	Args: cobra.NoArgs,
	RunE: runConfigExample,
}

var configPathCmd = &cobra.Command{
	Use:   "path [config|token]",
	Short: "Print the resolved config and token paths",
	Long: `Print where bbkt reads its config and its token, whether or not they exist.

Naming one prints it bare, for shell substitution:
  chmod 600 "$(bbkt config path token)"`,
	Args:      cobra.MaximumNArgs(1),
	ValidArgs: []string{"config", "token"},
	RunE:      runConfigPath,
}

var configShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show the effective settings and where each came from",
	Long: `Show what bbkt resolved, and which input supplied it.

The source column is the point. A field can come from the config file, from its
environment variable, or from a built-in default, and once resolved they are
indistinguishable — this is the only command that says which one won.

The token is never printed, only where it was found.`,
	Args: cobra.NoArgs,
	RunE: runConfigShow,
}

var configEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the config file in $VISUAL or $EDITOR",
	Long: `Open the config in your editor, creating a starter one first if none exists.

The editor runs in the foreground, so a terminal editor blocks until you close
it; a GUI editor returns immediately unless configured to wait (EDITOR="code --wait").`,
	Args: cobra.NoArgs,
	RunE: runConfigEdit,
}

var configCheckCmd = &cobra.Command{
	Use:   "check",
	Short: "Prove the config reaches Bitbucket as you",
	Long: `Resolve the config, then use it: connect to the instance and report who you are.

The only check that means anything here. Nothing about a four-field TOML can be
validated by reading it — whether the URL is reachable, the CA is the right one,
and the token is a personal token that has not expired are all answered by the
round trip and nothing else.

Exits non-zero on the first step that fails, naming that step.`,
	Args: cobra.NoArgs,
	RunE: runConfigCheck,
}

var configJSON bool

func init() {
	configShowCmd.Flags().BoolVar(&configJSON, "json", false, "output as JSON")
	configCmd.AddCommand(configInitCmd, configCheckCmd, configShowCmd, configPathCmd, configExampleCmd, configEditCmd)
	rootCmd.AddCommand(configCmd)
}

func runConfigInit(cmd *cobra.Command, args []string) error {
	if err := ensureConfigFile(cmd); err != nil {
		return err
	}
	// Resolved after the write, so a config created a moment ago is what names
	// the token path — not the absence it would have been resolved against.
	cfg, err := config.Resolve()
	if err != nil {
		return err
	}
	if err := ensureTokenFile(cmd, cfg); err != nil {
		return err
	}
	infof(cmd, "")
	infof(cmd, "next: bbkt config check")
	return nil
}

func ensureConfigFile(cmd *cobra.Command) error {
	switch _, err := os.Stat(config.Path()); {
	case err == nil:
		infof(cmd, "config already exists at %s — left as is", config.Path())
		return nil
	case !os.IsNotExist(err):
		return err
	}

	url, err := promptLine(cmd, fmt.Sprintf("Bitbucket URL (e.g. %s): ", config.URLPlaceholder))
	if err != nil {
		return err
	}
	url = strings.TrimRight(strings.TrimSpace(url), "/")

	if err := config.Init(url, config.DefaultTokenPath()); err != nil {
		return err
	}
	infof(cmd, "created config at %s", config.Path())
	if url == "" {
		infof(cmd, "  no url set — uncomment and fill it in: bbkt config edit")
	}
	return nil
}

func ensureTokenFile(cmd *cobra.Command, cfg *config.Config) error {
	if os.Getenv("BBKT_TOKEN") != "" {
		infof(cmd, "$BBKT_TOKEN is set and wins over any token file — nothing to create")
		return nil
	}

	path, err := cfg.TokenPath()
	if err != nil {
		return err
	}
	if path == "" {
		infof(cmd, "no token_file configured — set one: bbkt config edit")
		return nil
	}

	switch _, err := os.Stat(path); {
	case err == nil:
		infof(cmd, "token file already exists at %s — left as is", path)
		return nil
	case !os.IsNotExist(err):
		return err
	}

	token, err := readToken(cmd)
	if err != nil {
		return err
	}
	if token == "" {
		infof(cmd, "no token given — create one in Bitbucket under:")
		infof(cmd, "  profile picture → Manage account → HTTP access tokens")
		infof(cmd, "it must be a personal token; project- and repository-scoped tokens cannot merge")
		infof(cmd, "then: printf '%%s' TOKEN > %s && chmod 600 %s", path, path)
		return nil
	}
	if err := config.WriteToken(path, token); err != nil {
		return err
	}
	infof(cmd, "wrote token to %s (mode 0600)", path)
	return nil
}

// promptLine asks on a terminal and returns empty everywhere else, so the same
// code path serves an interactive setup and a scripted one.
func promptLine(cmd *cobra.Command, question string) (string, error) {
	if noInput || !isatty.IsTerminal(os.Stdin.Fd()) {
		return "", nil
	}
	infof(cmd, "%s", question)
	var answer string
	if _, err := fmt.Fscanln(os.Stdin, &answer); err != nil {
		// A bare newline is a declined prompt, not a failure — every field this
		// asks about can be filled in later with `config edit`.
		return "", nil
	}
	return answer, nil
}

// readToken takes the token from a pipe when there is one and prompts without
// echo otherwise. Never a flag or an argument: both land in ps output and shell
// history, which is the whole reason bbkt has no --token.
func readToken(cmd *cobra.Command) (string, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		piped, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(piped)), nil
	}
	if noInput {
		return "", nil
	}

	infof(cmd, "HTTP access token (input hidden, empty to skip): ")
	entered, err := term.ReadPassword(int(os.Stdin.Fd()))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(entered)), nil
}

func runConfigExample(cmd *cobra.Command, args []string) error {
	outf(cmd, "%s", strings.TrimRight(config.Example, "\n"))
	if isatty.IsTerminal(os.Stdout.Fd()) {
		// A template on screen with no path is the half-answer: it says what to
		// write and never where. On stderr, so a redirect captures only the file.
		infof(cmd, "\nreference only — the file bbkt reads is %s; open it with `bbkt config edit`", config.Path())
	}
	return nil
}

func runConfigPath(cmd *cobra.Command, args []string) error {
	cfg, err := config.Resolve()
	if err != nil {
		return err
	}
	tokenPath, err := cfg.TokenPath()
	if err != nil {
		return err
	}
	if tokenPath == "" {
		tokenPath = config.DefaultTokenPath()
	}

	if len(args) == 1 {
		if args[0] == "token" {
			outf(cmd, "%s", tokenPath)
			return nil
		}
		outf(cmd, "%s", config.Path())
		return nil
	}

	table := newTable(cmd.OutOrStdout())
	writef(table, "config\t%s\n", config.Path())
	writef(table, "token\t%s\n", tokenPath)
	return table.Flush()
}

type settingReport struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Source string `json:"source"`
}

// tokenReport carries where the token came from and never the token itself.
type tokenReport struct {
	Found  bool   `json:"found"`
	Source string `json:"source,omitempty"`
	Error  string `json:"error,omitempty"`
}

func runConfigShow(cmd *cobra.Command, args []string) error {
	cfg, err := config.Resolve()
	if err != nil {
		return err
	}

	_, statErr := os.Stat(config.Path())
	exists := statErr == nil

	settings := []settingReport{
		{"url", cfg.URL, cfg.Sources.URL},
		{"token_file", cfg.TokenFile, cfg.Sources.TokenFile},
		{"default_target_branch", cfg.DefaultTargetBranch, cfg.Sources.DefaultTargetBranch},
		{"ca_file", cfg.CAFile, cfg.Sources.CAFile},
	}

	token := tokenReport{Found: true}
	if resolved, err := cfg.Token(); err != nil {
		token = tokenReport{Error: err.Error()}
	} else {
		token.Source = resolved.Source
	}

	if configJSON {
		return emitJSON(cmd, struct {
			ConfigFile   string          `json:"config_file"`
			ConfigExists bool            `json:"config_exists"`
			Settings     []settingReport `json:"settings"`
			Token        tokenReport     `json:"token"`
		}{config.Path(), exists, settings, token})
	}

	note := ""
	if !exists {
		note = "  (absent — reading the environment only)"
	}
	outf(cmd, "config  %s%s", config.Path(), note)
	outf(cmd, "")

	table := newTable(cmd.OutOrStdout())
	writef(table, "SETTING\tVALUE\tSOURCE\n")
	for _, setting := range settings {
		value := setting.Value
		if value == "" {
			value = "-"
		}
		writef(table, "%s\t%s\t%s\n", setting.Name, value, setting.Source)
	}
	source := token.Source
	if !token.Found {
		source = token.Error
	}
	writef(table, "token\t%s\t%s\n", boolWord(token.Found, "found", "not found"), source)
	if err := table.Flush(); err != nil {
		return err
	}

	if cfg.URL == "" {
		outf(cmd, "")
		next := "bbkt config init"
		if exists {
			next = "bbkt config edit"
		}
		infof(cmd, "no url — bbkt cannot reach an instance until one is set: %s", next)
	}
	return nil
}

func boolWord(cond bool, yes, no string) string {
	if cond {
		return yes
	}
	return no
}

func runConfigEdit(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(config.Path()); os.IsNotExist(err) {
		if err := config.Init("", config.DefaultTokenPath()); err != nil {
			return err
		}
		infof(cmd, "no config found — created a starter one at %s", config.Path())
	} else if err != nil {
		return err
	}

	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("no editor configured — set $VISUAL or $EDITOR")
	}

	// $EDITOR may carry arguments ("code --wait", "emacsclient -nw"), so split it
	// and resolve the binary rather than exec'ing the whole string.
	parts := strings.Fields(editor)
	binary, err := exec.LookPath(parts[0])
	if err != nil {
		return fmt.Errorf("editor not found on PATH: %s", parts[0])
	}

	editing := exec.Command(binary, append(parts[1:], config.Path())...)
	editing.Stdin, editing.Stdout, editing.Stderr = os.Stdin, os.Stdout, os.Stderr
	return editing.Run()
}

func runConfigCheck(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	outf(cmd, "url        %s  (%s)", cfg.URL, cfg.Sources.URL)

	token, err := cfg.Token()
	if err != nil {
		return err
	}
	outf(cmd, "token      found  (%s)", token.Source)

	if cfg.CAFile != "" {
		if _, err := os.Stat(cfg.CAFile); err != nil {
			return fmt.Errorf("ca_file: %w", err)
		}
		outf(cmd, "ca_file    %s  (%s)", cfg.CAFile, cfg.Sources.CAFile)
	}

	client, err := newClientFrom(cfg, token)
	if err != nil {
		return err
	}
	user, err := client.Whoami()
	if err != nil {
		return fmt.Errorf("reaching %s: %w", cfg.URL, err)
	}

	outf(cmd, "")
	outf(cmd, "ok — authenticated to %s as %s", cfg.URL, user)

	if tokenPath, _ := cfg.TokenPath(); tokenPath != "" && token.Source == tokenPath {
		if info, err := os.Stat(tokenPath); err == nil && info.Mode().Perm()&0o077 != 0 {
			infof(cmd, "\ntoken file is %04o and readable by others: chmod 600 %s", info.Mode().Perm(), tokenPath)
		}
	}
	return nil
}

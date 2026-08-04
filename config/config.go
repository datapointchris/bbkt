package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk shape of ~/.config/bbkt/config.toml.
type Config struct {
	URL                 string `toml:"url"`
	TokenFile           string `toml:"token_file"`
	DefaultTargetBranch string `toml:"default_target_branch"`
	CAFile              string `toml:"ca_file"`

	Sources Sources `toml:"-"`
}

// Sources records which input supplied each field. Five inputs — the file and
// four environment variables — collapse into one value with no trace of which
// won, and that is the question `bbkt config show` exists to answer.
type Sources struct {
	URL                 string
	TokenFile           string
	DefaultTargetBranch string
	CAFile              string
}

// What Sources reports for an origin that is not an environment variable; those
// name themselves.
const (
	SourceFile    = "config.toml"
	SourceDefault = "built-in default"
	SourceUnset   = "unset"
)

// ErrNoURL is a sentinel; the path in the wrapped message moves with
// XDG_CONFIG_HOME, so it cannot be baked into the error at package init.
var ErrNoURL = errors.New("no Bitbucket URL configured")

// Path reports where bbkt reads its config.
//
// XDG_CONFIG_HOME is read directly rather than through os.UserConfigDir, which
// returns ~/Library/Application Support on darwin — the fleet puts every tool's
// config under ~/.config on every platform, and a config path that moves with
// the OS would make a synced dotfile wrong on one of them.
func Path() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "bbkt", "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return filepath.Join(home, ".config", "bbkt", "config.toml")
}

// Dir is the directory the config file lives in, and where `config init` puts
// the token file it writes.
func Dir() string { return filepath.Dir(Path()) }

// DefaultTokenPath is the token_file a config written by `config init` points at.
func DefaultTokenPath() string { return filepath.Join(Dir(), "token") }

// Resolve reads the config file if present and applies environment overrides,
// requiring nothing to be set. A missing file is not an error — every field has
// an env equivalent, so a fresh machine can run entirely from the environment.
//
// Separate from Load because `config show` has to describe a config that does
// not work yet: refusing to resolve an incomplete one would fail exactly when
// someone is trying to find out what is incomplete about it.
func Resolve() (*Config, error) {
	cfg := &Config{
		DefaultTargetBranch: "main",
		Sources: Sources{
			URL:                 SourceUnset,
			TokenFile:           SourceUnset,
			DefaultTargetBranch: SourceDefault,
			CAFile:              SourceUnset,
		},
	}

	data, err := os.ReadFile(Path())
	switch {
	case err == nil:
		// Decode rather than Unmarshal for the MetaData: a key present in the
		// file and a key left at its default are indistinguishable in the struct
		// once decoded, and `default_target_branch = "main"` is exactly that case.
		meta, err := toml.Decode(string(data), cfg)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", Path(), err)
		}
		for key, source := range map[string]*string{
			"url":                   &cfg.Sources.URL,
			"token_file":            &cfg.Sources.TokenFile,
			"default_target_branch": &cfg.Sources.DefaultTargetBranch,
			"ca_file":               &cfg.Sources.CAFile,
		} {
			if meta.IsDefined(key) {
				*source = SourceFile
			}
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("reading %s: %w", Path(), err)
	}

	for _, override := range []struct {
		env    string
		value  *string
		source *string
	}{
		{"BBKT_URL", &cfg.URL, &cfg.Sources.URL},
		{"BBKT_TOKEN_FILE", &cfg.TokenFile, &cfg.Sources.TokenFile},
		{"BBKT_CA_FILE", &cfg.CAFile, &cfg.Sources.CAFile},
	} {
		if v := os.Getenv(override.env); v != "" {
			*override.value = v
			*override.source = "$" + override.env
		}
	}

	cfg.URL = strings.TrimRight(cfg.URL, "/")
	return cfg, nil
}

// Load resolves the config and requires the one field every command that talks
// to Bitbucket needs.
func Load() (*Config, error) {
	cfg, err := Resolve()
	if err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		// A config that exists is one `config init` refuses to touch, so pointing
		// at init would send someone to a command that declines to help them.
		next := "run `bbkt config init` to create " + Path()
		if _, err := os.Stat(Path()); err == nil {
			next = "set url in " + Path() + " (`bbkt config edit`)"
		}
		return nil, fmt.Errorf("%w: %s, or set BBKT_URL", ErrNoURL, next)
	}
	return cfg, nil
}

// Token is a resolved access token and the input that supplied it, so
// `bbkt config show` can report where it came from without printing the value.
type Token struct {
	Value  string
	Source string
}

// Token resolves the HTTP access token. BBKT_TOKEN wins so a shell can supply it
// per-invocation; otherwise it comes from a file. There is deliberately no flag —
// a flag value lands in ps output and shell history.
func (c *Config) Token() (Token, error) {
	if v := os.Getenv("BBKT_TOKEN"); v != "" {
		return Token{Value: strings.TrimSpace(v), Source: "$BBKT_TOKEN"}, nil
	}

	path, err := c.TokenPath()
	if err != nil {
		return Token{}, err
	}
	if path == "" {
		return Token{}, fmt.Errorf("no token: run `bbkt config init`, or set BBKT_TOKEN or token_file in %s", Path())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Token{}, fmt.Errorf("reading token_file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return Token{}, fmt.Errorf("token_file %s is empty", path)
	}
	return Token{Value: token, Source: path}, nil
}

// TokenPath is token_file with ~ expanded, or empty when none is configured.
func (c *Config) TokenPath() (string, error) {
	if c.TokenFile == "" {
		return "", nil
	}
	return ExpandHome(c.TokenFile)
}

// ExpandHome resolves a leading ~/ against the home directory. Only the leading
// form — a ~ anywhere else in a path is a literal character, and treating it
// otherwise would mangle a legitimate filename.
func ExpandHome(path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, path[2:]), nil
}

// Init writes a starter config at Path. It never rewrites an existing file:
// config.toml is the only place the instance URL is recorded, and re-scaffolding
// it would be data loss. Callers check for the file first and report; this
// checks again because the answer to "does it exist" goes stale.
func Init(url, tokenPath string) error {
	if _, err := os.Stat(Path()); err == nil {
		return fmt.Errorf("%s already exists", Path())
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	// A URL nobody supplied is written commented out rather than empty, so the
	// file names the thing to fill in and `config show` still reports it unset.
	urlLine := fmt.Sprintf("url = %q", url)
	if url == "" {
		urlLine = "# url = " + strconv.Quote(URLPlaceholder)
	}
	starter := fmt.Sprintf(starterTemplate, urlLine, shortenHome(tokenPath))
	return os.WriteFile(Path(), []byte(starter), 0o600)
}

// WriteToken writes the token file at 0600, creating its directory. Bitbucket
// rejects nothing about a world-readable token, so nothing but this enforces it.
func WriteToken(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strings.TrimSpace(token)+"\n"), 0o600)
}

// shortenHome writes a path back in ~/ form, so a generated config reads like a
// hand-written one and survives being copied to a machine with a different home.
func shortenHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || !strings.HasPrefix(path, home+string(filepath.Separator)) {
		return path
	}
	return "~" + path[len(home):]
}

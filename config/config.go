package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk shape of ~/.config/bbkt/config.toml.
type Config struct {
	URL                 string `toml:"url"`
	TokenFile           string `toml:"token_file"`
	DefaultTargetBranch string `toml:"default_target_branch"`
	CAFile              string `toml:"ca_file"`
}

var ErrNoURL = errors.New("no Bitbucket URL configured: set url in " + Path() + " or BBKT_URL")

// Path reports where bbkt reads its config, honoring XDG_CONFIG_HOME.
func Path() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return filepath.Join(os.Getenv("HOME"), ".config", "bbkt", "config.toml")
	}
	return filepath.Join(dir, "bbkt", "config.toml")
}

// Load reads the config file if present and applies environment overrides. A
// missing file is not an error — every field has an env equivalent, so a fresh
// machine can run entirely from the environment.
func Load() (*Config, error) {
	cfg := &Config{DefaultTargetBranch: "main"}

	data, err := os.ReadFile(Path())
	switch {
	case err == nil:
		if err := toml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing %s: %w", Path(), err)
		}
	case !os.IsNotExist(err):
		return nil, fmt.Errorf("reading %s: %w", Path(), err)
	}

	if v := os.Getenv("BBKT_URL"); v != "" {
		cfg.URL = v
	}
	if v := os.Getenv("BBKT_TOKEN_FILE"); v != "" {
		cfg.TokenFile = v
	}
	if v := os.Getenv("BBKT_CA_FILE"); v != "" {
		cfg.CAFile = v
	}

	cfg.URL = strings.TrimRight(cfg.URL, "/")
	if cfg.URL == "" {
		return nil, ErrNoURL
	}
	return cfg, nil
}

// Token resolves the HTTP access token. BBKT_TOKEN wins so a shell can supply it
// per-invocation; otherwise it comes from a file. There is deliberately no flag —
// a flag value lands in ps output and shell history.
func (c *Config) Token() (string, error) {
	if v := os.Getenv("BBKT_TOKEN"); v != "" {
		return strings.TrimSpace(v), nil
	}
	if c.TokenFile == "" {
		return "", fmt.Errorf("no token: set BBKT_TOKEN or token_file in %s", Path())
	}

	path := c.TokenFile
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, path[2:])
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("reading token_file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", fmt.Errorf("token_file %s is empty", path)
	}
	return token, nil
}

package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig points the whole package at a temp XDG_CONFIG_HOME and puts body
// at the path bbkt reads. An empty body writes no file, which is the
// fresh-machine case.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	for _, name := range []string{"BBKT_URL", "BBKT_TOKEN", "BBKT_TOKEN_FILE", "BBKT_CA_FILE"} {
		t.Setenv(name, "")
	}
	if body != "" {
		if err := os.MkdirAll(Dir(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(Path(), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestResolveReportsSources(t *testing.T) {
	writeConfig(t, `
url = "https://bitbucket.corp.example/"
token_file = "~/.config/bbkt/token"
`)
	t.Setenv("BBKT_CA_FILE", "/etc/ssl/corp.pem")

	cfg, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}

	// The trailing slash is stripped so the API path concatenation never doubles it.
	if cfg.URL != "https://bitbucket.corp.example" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.Sources.URL != SourceFile {
		t.Errorf("Sources.URL = %q, want %q", cfg.Sources.URL, SourceFile)
	}
	if cfg.Sources.CAFile != "$BBKT_CA_FILE" {
		t.Errorf("Sources.CAFile = %q", cfg.Sources.CAFile)
	}
	// The distinction `config show` exists for: "main" written in the file and
	// "main" from the built-in default are the same value from different inputs.
	if cfg.DefaultTargetBranch != "main" || cfg.Sources.DefaultTargetBranch != SourceDefault {
		t.Errorf("DefaultTargetBranch = %q (%s)", cfg.DefaultTargetBranch, cfg.Sources.DefaultTargetBranch)
	}
}

func TestResolveDistinguishesExplicitDefault(t *testing.T) {
	writeConfig(t, "url = \"https://x\"\ndefault_target_branch = \"main\"\n")

	cfg, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Sources.DefaultTargetBranch != SourceFile {
		t.Errorf("Sources.DefaultTargetBranch = %q, want %q", cfg.Sources.DefaultTargetBranch, SourceFile)
	}
}

func TestEnvironmentOverridesFile(t *testing.T) {
	writeConfig(t, "url = \"https://from-file\"\n")
	t.Setenv("BBKT_URL", "https://from-env/")

	cfg, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://from-env" {
		t.Errorf("URL = %q", cfg.URL)
	}
	if cfg.Sources.URL != "$BBKT_URL" {
		t.Errorf("Sources.URL = %q", cfg.Sources.URL)
	}
}

func TestResolveWithoutFile(t *testing.T) {
	writeConfig(t, "")

	cfg, err := Resolve()
	if err != nil {
		t.Fatalf("a missing config must resolve, not error: %v", err)
	}
	if cfg.URL != "" || cfg.Sources.URL != SourceUnset {
		t.Errorf("URL = %q (%s)", cfg.URL, cfg.Sources.URL)
	}

	// Load is the strict one, and its message has to name the path it looked at.
	_, err = Load()
	if !errors.Is(err, ErrNoURL) {
		t.Fatalf("Load error = %v, want ErrNoURL", err)
	}
	if !strings.Contains(err.Error(), Path()) {
		t.Errorf("Load error does not name %s: %v", Path(), err)
	}
}

func TestTokenPrefersEnvironment(t *testing.T) {
	writeConfig(t, "url = \"https://x\"\ntoken_file = \"/nonexistent/token\"\n")
	t.Setenv("BBKT_TOKEN", "  from-env\n")

	cfg, err := Resolve()
	if err != nil {
		t.Fatal(err)
	}
	token, err := cfg.Token()
	if err != nil {
		t.Fatal(err)
	}
	if token.Value != "from-env" {
		t.Errorf("Value = %q", token.Value)
	}
	if token.Source != "$BBKT_TOKEN" {
		t.Errorf("Source = %q", token.Source)
	}
}

func TestTokenFromFile(t *testing.T) {
	root := writeConfig(t, "")
	tokenPath := filepath.Join(root, "token")
	if err := os.WriteFile(tokenPath, []byte("secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &Config{TokenFile: tokenPath}
	token, err := cfg.Token()
	if err != nil {
		t.Fatal(err)
	}
	if token.Value != "secret" || token.Source != tokenPath {
		t.Errorf("token = %+v", token)
	}
}

func TestTokenRejectsEmptyFile(t *testing.T) {
	root := writeConfig(t, "")
	tokenPath := filepath.Join(root, "token")
	if err := os.WriteFile(tokenPath, []byte("\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// An empty token file authenticates as anonymous rather than failing at the
	// client, which surfaces as a confusing 401 from an unrelated command.
	if _, err := (&Config{TokenFile: tokenPath}).Token(); err == nil {
		t.Fatal("expected an error for an empty token_file")
	}
}

func TestInitWritesLoadableConfig(t *testing.T) {
	writeConfig(t, "")

	if err := Init("https://bitbucket.corp.example", DefaultTokenPath()); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load()
	if err != nil {
		t.Fatalf("a config written by Init must load: %v", err)
	}
	if cfg.URL != "https://bitbucket.corp.example" {
		t.Errorf("URL = %q", cfg.URL)
	}
	tokenPath, err := cfg.TokenPath()
	if err != nil {
		t.Fatal(err)
	}
	if tokenPath != DefaultTokenPath() {
		t.Errorf("TokenPath = %q, want %q", tokenPath, DefaultTokenPath())
	}

	info, err := os.Stat(Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config mode = %04o, want 0600", perm)
	}
}

func TestInitWithoutURLStillParses(t *testing.T) {
	writeConfig(t, "")

	if err := Init("", DefaultTokenPath()); err != nil {
		t.Fatal(err)
	}
	cfg, err := Resolve()
	if err != nil {
		t.Fatalf("the placeholder config must parse: %v", err)
	}
	if cfg.URL != "" || cfg.Sources.URL != SourceUnset {
		t.Errorf("URL = %q (%s), want unset — the placeholder is commented out", cfg.URL, cfg.Sources.URL)
	}
}

func TestInitNeverOverwrites(t *testing.T) {
	writeConfig(t, "url = \"https://original\"\n")

	if err := Init("https://replacement", DefaultTokenPath()); err == nil {
		t.Fatal("Init overwrote an existing config")
	}
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URL != "https://original" {
		t.Errorf("URL = %q", cfg.URL)
	}
}

func TestWriteTokenIsPrivate(t *testing.T) {
	tokenPath := filepath.Join(t.TempDir(), "nested", "token")
	if err := WriteToken(tokenPath, " secret \n"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token mode = %04o, want 0600", perm)
	}
	token, err := (&Config{TokenFile: tokenPath}).Token()
	if err != nil {
		t.Fatal(err)
	}
	if token.Value != "secret" {
		t.Errorf("Value = %q", token.Value)
	}
}

func TestExampleIsLoadable(t *testing.T) {
	writeConfig(t, Example)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("`config example` must be a valid config: %v", err)
	}
	if cfg.URL == "" || cfg.TokenFile == "" || cfg.DefaultTargetBranch == "" {
		t.Errorf("example leaves a documented field unset: %+v", cfg)
	}
	// ca_file is commented out, because most instances do not need one and an
	// active path to a certificate that is not there fails every command.
	if cfg.CAFile != "" {
		t.Errorf("CAFile = %q, want it commented out in the example", cfg.CAFile)
	}
}

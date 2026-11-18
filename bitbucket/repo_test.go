package bitbucket

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The parsers above are pure, so they say nothing about what git prints for the
// arguments git() hands it. This drives the wrapper against a real repository
// the test creates, with the machine's git config shut out so a user's settings
// cannot change what git answers.
func TestTheGitWrapperReadsARealRepository(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	// A git hook running this suite exports GIT_DIR and its siblings, which would
	// point every git call below at the repository being committed to. git names
	// those variables itself.
	localVars, err := exec.CommandContext(t.Context(), "git", "rev-parse", "--local-env-vars").Output()
	if err != nil {
		t.Fatalf("git rev-parse --local-env-vars: %v", err)
	}
	for _, name := range strings.Fields(string(localVars)) {
		t.Setenv(name, "")
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for _, name := range []string{"GIT_AUTHOR_NAME", "GIT_COMMITTER_NAME"} {
		t.Setenv(name, "bbkt test")
	}
	for _, name := range []string{"GIT_AUTHOR_EMAIL", "GIT_COMMITTER_EMAIL"} {
		t.Setenv(name, "test@example.com")
	}

	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("remote", "add", "origin", "ssh://git@bitbucket.corp.example:7999/DATA/pipeline.git")
	run("commit", "-q", "--allow-empty", "-m", "first")
	run("switch", "-q", "-c", "DATA-7_null_handling")

	sub := filepath.Join(root, "src", "etl")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(sub)
	ctx := t.Context()

	repo, err := CurrentRepo(ctx, "origin")
	if err != nil {
		t.Fatalf("CurrentRepo: %v", err)
	}
	if want := (Repo{Project: "DATA", Slug: "pipeline"}); repo != want {
		t.Errorf("CurrentRepo = %v, want %v", repo, want)
	}
	if _, err := CurrentRepo(ctx, "upstream"); err == nil {
		t.Error("CurrentRepo found a remote the repository does not have")
	}

	branch, err := CurrentBranch(ctx)
	if err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if branch != "DATA-7_null_handling" {
		t.Errorf("CurrentBranch = %q", branch)
	}

	if prefix := WorkTreePrefix(ctx); prefix != "src/etl/" {
		t.Errorf("WorkTreePrefix = %q, want %q", prefix, "src/etl/")
	}

	run("switch", "-q", "--detach")
	if branch, err := CurrentBranch(ctx); err == nil {
		t.Errorf("CurrentBranch on a detached HEAD = %q, want an error", branch)
	}
}

func TestParseRemote(t *testing.T) {
	tests := []struct {
		name    string
		remote  string
		want    Repo
		wantErr bool
	}{
		{
			name:   "https clone url",
			remote: "https://bitbucket.corp.example/scm/DATA/pipeline.git",
			want:   Repo{Project: "DATA", Slug: "pipeline"},
		},
		{
			name:   "https without .git suffix",
			remote: "https://bitbucket.corp.example/scm/DATA/pipeline",
			want:   Repo{Project: "DATA", Slug: "pipeline"},
		},
		{
			name:   "https with embedded username",
			remote: "https://chris@bitbucket.corp.example/scm/DATA/pipeline.git",
			want:   Repo{Project: "DATA", Slug: "pipeline"},
		},
		{
			name:   "https with port",
			remote: "https://bitbucket.corp.example:8443/scm/DATA/pipeline.git",
			want:   Repo{Project: "DATA", Slug: "pipeline"},
		},
		{
			name:   "ssh clone url on the bitbucket port",
			remote: "ssh://git@bitbucket.corp.example:7999/DATA/pipeline.git",
			want:   Repo{Project: "DATA", Slug: "pipeline"},
		},
		{
			name:   "ssh without port",
			remote: "ssh://git@bitbucket.corp.example/DATA/pipeline.git",
			want:   Repo{Project: "DATA", Slug: "pipeline"},
		},
		{
			name:   "personal fork project key",
			remote: "https://bitbucket.corp.example/scm/~chris/pipeline.git",
			want:   Repo{Project: "~chris", Slug: "pipeline"},
		},
		{
			name:   "repo slug containing a hyphen",
			remote: "https://bitbucket.corp.example/scm/DATA/etl-common-lib.git",
			want:   Repo{Project: "DATA", Slug: "etl-common-lib"},
		},
		{
			name:   "scp-style ssh, as an instance on port 22 hands out",
			remote: "git@bitbucket.corp.example:DATA/pipeline.git",
			want:   Repo{Project: "DATA", Slug: "pipeline"},
		},
		{
			name:    "github remote is not bitbucket",
			remote:  "https://github.com/datapointchris/bbkt.git",
			wantErr: true,
		},
		{
			name:    "empty",
			remote:  "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseRemote(tt.remote)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseSpec(t *testing.T) {
	got, err := ParseSpec("DATA/pipeline")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if want := (Repo{Project: "DATA", Slug: "pipeline"}); got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	for _, bad := range []string{"pipeline", "DATA/", "/pipeline", "A/B/C", ""} {
		if _, err := ParseSpec(bad); err == nil {
			t.Errorf("expected an error for %q", bad)
		}
	}
}

func TestSplitIssueKey(t *testing.T) {
	tests := []struct {
		branch   string
		wantKey  string
		wantRest string
	}{
		{"PROJ-123-fix-the-parser", "PROJ-123", "-fix-the-parser"},
		{"proj-123-fix-the-parser", "PROJ-123", "-fix-the-parser"},
		{"DATA-7", "DATA-7", ""},
		{"no-key-here", "", "no-key-here"},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.branch, func(t *testing.T) {
			key, rest := SplitIssueKey(tt.branch)
			if key != tt.wantKey || rest != tt.wantRest {
				t.Errorf("got (%q, %q), want (%q, %q)", key, rest, tt.wantKey, tt.wantRest)
			}
		})
	}
}

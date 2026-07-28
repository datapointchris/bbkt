package bitbucket

import "testing"

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
			name:    "github remote is not bitbucket",
			remote:  "git@github.com:datapointchris/bbkt.git",
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

package cmd

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/datapointchris/goclikit"
)

// Bitbucket's merge check is the gate between a conflicted or vetoed pull
// request and a merge commit on the target branch. Each refusal it can give
// must stop the command before the merge request is sent.
func TestAMergeBitbucketRefusesSendsNoMerge(t *testing.T) {
	for name, status := range map[string]string{
		"conflicted": `{"canMerge": false, "conflicted": true, "vetoes": []}`,
		"vetoed": `{"canMerge": false, "conflicted": false, "vetoes": [` +
			`{"summaryMessage": "Not all required builds are successful yet", "detailedMessage": "1 of 2 builds failed"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			var checked, merged bool
			// The pull request is served too, so a command that skipped the
			// check would read its version and reach the merge.
			const pullRequest = "/rest/api/1.0/projects/EX/repos/demo/pull-requests/42"
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch {
				case r.URL.Path == pullRequest:
					_, _ = w.Write([]byte(`{"id": 42, "version": 7, "title": "Fix the parser", "state": "OPEN"}`))
				case r.URL.Path == pullRequest+"/merge" && r.Method == http.MethodPost:
					merged = true
					_, _ = w.Write([]byte(`{"id": 42, "version": 8, "state": "MERGED"}`))
				case r.URL.Path == pullRequest+"/merge":
					checked = true
					_, _ = w.Write([]byte(status))
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(server.Close)

			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("BBKT_URL", server.URL)
			t.Setenv("BBKT_TOKEN", "unused")
			t.Cleanup(func() { mergeYes, repoSpec = false, "" })

			err := runLine(t, "pr", "merge", "42", "--yes", "-R", "EX/demo")
			if err == nil || errors.Is(err, goclikit.ErrUsage) {
				t.Fatalf("pr merge answered %v, want a refusal", err)
			}
			if !checked {
				t.Error("pr merge never asked Bitbucket whether it can merge")
			}
			if merged {
				t.Error("pr merge sent the merge after Bitbucket refused it")
			}
		})
	}
}

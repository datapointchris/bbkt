package cmd

import (
	"errors"
	"net/http"
	"testing"

	"github.com/datapointchris/goclikit"
	"github.com/spf13/cobra"

	"github.com/datapointchris/bbkt/bitbucket"
)

func TestNotFoundCarriesBitbucketsOwnMessage(t *testing.T) {
	t.Parallel()

	err := &bitbucket.APIError{
		Status:  http.StatusNotFound,
		Path:    "/projects/EX/repos/demo/pull-requests/999",
		Message: "Pull request 999 does not exist in repository demo.",
	}

	subject, ok := notFound(err)
	if !ok {
		t.Fatal("notFound() = false, want true for a 404 carrying a message")
	}
	if subject != err.Message {
		t.Errorf("subject = %q, want the server's own message", subject)
	}
	// The REST path is plumbing. A subject built from Error() would put it in
	// front of the reader, which is what taking Message avoids.
	if subject == err.Error() {
		t.Error("subject is the full Error(), which names the REST path")
	}
}

func TestNotFoundDeclinesA404ThatSaysNothing(t *testing.T) {
	t.Parallel()

	// goclikit reads an empty subject as false, leaving the error alone rather
	// than rewriting it into a resource claim the server never made.
	err := &bitbucket.APIError{Status: http.StatusNotFound, Path: "/projects/EX"}
	if _, ok := notFound(err); ok {
		t.Error("notFound() = true for a 404 with no message")
	}
}

func TestNotFoundDeclinesAnythingThatIsNotA404(t *testing.T) {
	t.Parallel()

	// A permission failure that arrives as 403 is a different remedy and must
	// not be dressed as a missing pull request.
	forbidden := &bitbucket.APIError{Status: http.StatusForbidden, Message: "You are not permitted"}
	if _, ok := notFound(forbidden); ok {
		t.Error("notFound() = true for a 403")
	}
	if _, ok := notFound(errors.New("dial tcp: connection refused")); ok {
		t.Error("notFound() = true for a transport error")
	}
}

func TestIdTakingCommandsReachHintsAndConfigDoesNot(t *testing.T) {
	t.Parallel()

	reaches := func(cmd *cobra.Command) bool {
		for current := cmd; current != nil; current = current.Parent() {
			if current.Annotations[goclikit.RecoveryHintsAnnotation] != "" {
				return true
			}
		}
		return false
	}

	for _, path := range [][]string{{"pr", "view"}, {"pr", "merge"}, {"pr", "open"}} {
		command, _, err := rootCmd.Find(path)
		if err != nil {
			t.Fatalf("Find(%v) = %v", path, err)
		}
		if !reaches(command) {
			t.Errorf("%v reaches no recovery hints", path)
		}
	}

	// config takes no id, so pointing it at `bbkt pr list` would name the wrong
	// noun. The hints sit on pr for exactly this reason.
	command, _, err := rootCmd.Find([]string{"config", "check"})
	if err != nil {
		t.Fatalf("Find(config check) = %v", err)
	}
	if reaches(command) {
		t.Error("config check inherits the pull-request hints")
	}
}

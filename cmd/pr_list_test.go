package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/datapointchris/goclikit"
)

// runPrList drives the real command with the flags set and both streams
// captured. Every case here returns before newClient, so none of it needs a
// token, a git remote or a server.
func runPrList(t *testing.T, limit int, state string, asJSON bool) (stdout, stderr string, err error) {
	t.Helper()

	previousLimit, previousState, previousJSON := listLimit, listState, listJSON
	t.Cleanup(func() { listLimit, listState, listJSON = previousLimit, previousState, previousJSON })
	listLimit, listState, listJSON = limit, state, asJSON

	var out, errOut bytes.Buffer
	prListCmd.SetOut(&out)
	prListCmd.SetErr(&errOut)
	t.Cleanup(func() {
		prListCmd.SetOut(nil)
		prListCmd.SetErr(nil)
	})

	err = prListCmd.RunE(prListCmd, nil)
	return out.String(), errOut.String(), err
}

// A negative is a typing mistake rather than a request, so it is answered
// before the token is read: exit 2, and no network.
func TestANegativeLimitIsAUsageError(t *testing.T) {
	_, _, err := runPrList(t, -1, "OPEN", false)
	if err == nil {
		t.Fatal("--limit -1 succeeded, want a usage error")
	}
	if !errors.Is(err, goclikit.ErrUsage) {
		t.Errorf("error is not ErrUsage, so it exits 1 rather than 2: %v", err)
	}
	if !strings.Contains(err.Error(), "--limit") {
		t.Errorf("the refusal does not name the flag: %v", err)
	}
}

// A cap of zero is a complete request whose answer is known, so it is answered
// without a token or a git remote. Reaching either would turn a question that
// cannot fail into an exit 1 for anyone outside a repository.
func TestALimitOfZeroAnswersWithoutAClient(t *testing.T) {
	stdout, stderr, err := runPrList(t, 0, "OPEN", false)
	if err != nil {
		t.Fatalf("--limit 0 failed: %v", err)
	}
	if stdout != "" {
		t.Errorf("--limit 0 wrote to stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "--limit 0") {
		t.Errorf("the empty state does not name the cap: %q", stderr)
	}
	// An empty state is read by someone who is stuck, so it ends on something
	// they can run.
	if !strings.Contains(stderr, "bbkt pr list -n") {
		t.Errorf("the empty state names no command to type next: %q", stderr)
	}
}

// A script reads --json, and what it must not get is a document jq cannot
// iterate. A nil slice marshals to null, which fails `jq '.[]'` at exit 5.
func TestALimitOfZeroEmitsAnEmptyArray(t *testing.T) {
	stdout, _, err := runPrList(t, 0, "OPEN", true)
	if err != nil {
		t.Fatalf("--limit 0 --json failed: %v", err)
	}
	if strings.TrimSpace(stdout) == "null" {
		t.Fatalf("--limit 0 --json emitted null, which jq cannot iterate")
	}

	var decoded []map[string]any
	if err := json.Unmarshal([]byte(stdout), &decoded); err != nil {
		t.Fatalf("--limit 0 --json emitted %q, which does not decode as a list: %v", stdout, err)
	}
	if len(decoded) != 0 {
		t.Errorf("--limit 0 --json returned %d rows", len(decoded))
	}
}

// An empty listing is read by someone who expected rows, and --state is the
// question they have not widened. With every state already asked for there is
// nothing further to offer, so the sentence stops rather than pointing at a
// flag that would change nothing.
func TestAnEmptyListingNamesTheFlagThatWidensIt(t *testing.T) {
	if got := noPullRequests("OPEN"); !strings.Contains(got, "--state ALL") {
		t.Errorf("noPullRequests(OPEN) = %q, want it to name --state ALL", got)
	}
	if got := noPullRequests("ALL"); strings.Contains(got, "--state ALL") {
		t.Errorf("noPullRequests(ALL) = %q, and --state ALL is what was already asked", got)
	}
}

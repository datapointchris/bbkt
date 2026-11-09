package cmd

import (
	"testing"

	"github.com/datapointchris/bbkt/bitbucket"
)

// A bare target branch resolves to a stale local copy, or to nothing, so the
// diff hint must name both branches as remote-tracking refs on the remote the
// repository was read from. The remote here is not origin, so a hint that
// hard-coded it would fail too.
func TestTheDiffHintNamesBothBranchesOnTheChosenRemote(t *testing.T) {
	pr := &bitbucket.PullRequest{
		FromRef: bitbucket.Ref{DisplayID: "feature/DATA-7_nulls"},
		ToRef:   bitbucket.Ref{DisplayID: "develop"},
	}
	target, source := diffRefs("upstream", pr)
	if target != "upstream/develop" || source != "upstream/feature/DATA-7_nulls" {
		t.Errorf("diffRefs = %q, %q; want upstream/develop, upstream/feature/DATA-7_nulls", target, source)
	}
}

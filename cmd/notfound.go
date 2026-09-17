package cmd

import (
	"errors"

	"github.com/datapointchris/bbkt/bitbucket"
)

// notFound is the classifier [goclikit.WithNotFound] calls: Bitbucket's
// not-found, and the line naming what was missing.
//
// The subject is the server's own message. Data Center writes one on a 404 —
// "Pull request 999 does not exist in repository X" — and it is more specific
// than anything composable here, because it names the repository the id was
// looked for in and the command's arguments do not.
//
// An empty message returns false. A 404 that says nothing leaves the error
// alone rather than being rewritten into a claim the server did not make.
func notFound(err error) (string, bool) {
	var apiErr *bitbucket.APIError
	if !errors.As(err, &apiErr) || !apiErr.NotFound() || apiErr.Message == "" {
		return "", false
	}
	return apiErr.Message, true
}

// prRecoveryHints are the commands a not-found under `pr` names.
//
// They sit on `pr` rather than on the root because `config` is the other half
// of this tool and takes no id — a config failure pointed at `bbkt pr list`
// would send someone to the wrong noun entirely.
//
// **Phrased for what a 404 actually establishes.** Data Center answers 404 both
// for a pull request that is absent and for one the token cannot see, and the
// response does not say which. "the ones you can see" is true either way: an
// invisible pull request is missing from the list too, and the reader is not
// told it does not exist when it may only be hidden. A hint reading "it does
// not exist, list them" would be wrong half the time and give no way to find
// out which half.
var prRecoveryHints = []string{
	"List the pull requests you can see: bbkt pr list",
	"Check which account the token authenticates as: bbkt config check",
}

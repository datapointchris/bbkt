package bitbucket

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// Repo identifies a repository within a Bitbucket Data Center instance. Unlike
// GitHub's two-level owner/name, Bitbucket is three-level: host, project key,
// repository slug.
type Repo struct {
	Project string
	Slug    string
}

func (r Repo) String() string { return r.Project + "/" + r.Slug }

// Bitbucket Data Center serves HTTP clones under /scm/ and SSH on port 7999.
// Personal forks use a ~username project key, which is a valid key and needs no
// special handling beyond surviving the character class.
var remotePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^(?:https?://)?(?:[^@/]+@)?[^/]+/scm/([^/]+)/([^/]+?)(?:\.git)?$`),
	regexp.MustCompile(`^ssh://(?:[^@]+@)?[^/]+(?::\d+)?/([^/]+)/([^/]+?)(?:\.git)?$`),
}

// ParseRemote extracts the project key and repository slug from a Bitbucket
// remote URL.
func ParseRemote(remote string) (Repo, error) {
	remote = strings.TrimSpace(remote)
	for _, re := range remotePatterns {
		if m := re.FindStringSubmatch(remote); m != nil {
			return Repo{Project: m[1], Slug: m[2]}, nil
		}
	}
	return Repo{}, fmt.Errorf("cannot parse Bitbucket project/slug from remote %q", remote)
}

// ParseSpec reads an explicit PROJECT/SLUG override.
func ParseSpec(spec string) (Repo, error) {
	parts := strings.Split(spec, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return Repo{}, fmt.Errorf("expected PROJECT/SLUG, got %q", spec)
	}
	return Repo{Project: parts[0], Slug: parts[1]}, nil
}

// CurrentRepo resolves the repository from the git remote in the working
// directory.
func CurrentRepo(remoteName string) (Repo, error) {
	out, err := git("remote", "get-url", remoteName)
	if err != nil {
		return Repo{}, fmt.Errorf("no git remote %q in the current directory", remoteName)
	}
	return ParseRemote(out)
}

// CurrentBranch reports the checked-out branch, erroring on a detached HEAD
// rather than returning the literal "HEAD" as a branch name.
func CurrentBranch() (string, error) {
	out, err := git("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", fmt.Errorf("not a git repository")
	}
	if out == "HEAD" {
		return "", fmt.Errorf("HEAD is detached; check out a branch first")
	}
	return out, nil
}

// No trailing \b: an underscore is a word character, so it would refuse to match
// DATA-7 in DATA-7_null_handling. The greedy \d+ already stops the match at the
// end of the number.
var issueKeyPattern = regexp.MustCompile(`(?i)\b[a-z][a-z0-9]*-\d+`)

// SplitIssueKey separates a Jira work item key from the rest of a branch name,
// returning the key uppercased. Bitbucket Data Center's Jira integration links a
// pull request by finding the key in the branch name, commit message, or PR
// title, so carrying it through to the title is what populates the development
// panel. An empty key means the branch carries none.
func SplitIssueKey(name string) (key, rest string) {
	loc := issueKeyPattern.FindStringIndex(name)
	if loc == nil {
		return "", name
	}
	return strings.ToUpper(name[loc[0]:loc[1]]), name[loc[1]:]
}

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

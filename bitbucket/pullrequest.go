package bitbucket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type User struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

type Participant struct {
	User     User   `json:"user"`
	Role     string `json:"role"`
	Approved bool   `json:"approved"`
	Status   string `json:"status"`
}

type Ref struct {
	ID         string  `json:"id"`
	DisplayID  string  `json:"displayId"`
	Repository RepoRef `json:"repository"`
}

type RepoRef struct {
	Slug    string `json:"slug"`
	Project struct {
		Key string `json:"key"`
	} `json:"project"`
}

type PullRequest struct {
	ID          int           `json:"id"`
	Version     int           `json:"version"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	State       string        `json:"state"`
	Open        bool          `json:"open"`
	CreatedDate int64         `json:"createdDate"`
	UpdatedDate int64         `json:"updatedDate"`
	FromRef     Ref           `json:"fromRef"`
	ToRef       Ref           `json:"toRef"`
	Author      Participant   `json:"author"`
	Reviewers   []Participant `json:"reviewers"`
	Links       Links         `json:"links"`
}

type Links struct {
	Self []struct {
		Href string `json:"href"`
	} `json:"self"`
}

// URL returns the browser URL Bitbucket reports for the pull request.
func (p PullRequest) URL() string {
	if len(p.Links.Self) > 0 {
		return p.Links.Self[0].Href
	}
	return ""
}

// Repo reports the repository the pull request targets, which is the one that
// owns it even when the source is a fork.
func (p PullRequest) Repo() Repo {
	return Repo{Project: p.ToRef.Repository.Project.Key, Slug: p.ToRef.Repository.Slug}
}

func (p PullRequest) Updated() time.Time {
	return time.UnixMilli(p.UpdatedDate)
}

// ApprovalCount reports approvals and total reviewers.
func (p PullRequest) ApprovalCount() (approved, total int) {
	for _, r := range p.Reviewers {
		total++
		if r.Approved {
			approved++
		}
	}
	return approved, total
}

func prPath(r Repo) string {
	return fmt.Sprintf("/projects/%s/repos/%s/pull-requests", r.Project, r.Slug)
}

// ListOptions narrows a listing of pull requests.
//
// Limit is how many rows to return. A nil Limit returns every row, which is the
// ordinary reading of an option nobody set, and it is the only way to say so —
// no number means "all". A Limit of zero returns none and issues no request.
//
// The pointer is what keeps those two answers apart. An int would spend its
// zero on one of them, and whichever it spent would silently become the answer
// for every caller that left the field out of a struct literal.
type ListOptions struct {
	State string
	Role  string
	Limit *int
}

// ListPullRequests returns pull requests for one repository, and reports
// whether opts.Limit left rows behind.
func (c *Client) ListPullRequests(r Repo, opts ListOptions) ([]PullRequest, bool, error) {
	q := url.Values{}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	if opts.Role != "" {
		q.Set("role", opts.Role)
	}
	return c.collectPullRequests(prPath(r), q, opts.Limit)
}

// ListInbox returns pull requests across every repository where the
// authenticated user is a reviewer, and reports whether limit left rows behind.
// Bitbucket answers this server-side, so no local repository registry is needed.
//
// A nil limit returns every row; a limit of zero returns none.
func (c *Client) ListInbox(limit *int) ([]PullRequest, bool, error) {
	return c.collectPullRequests("/inbox/pull-requests", url.Values{}, limit)
}

// ListDashboard returns pull requests across every repository the authenticated
// user authored or reviews, and reports whether limit left rows behind.
//
// A nil limit returns every row; a limit of zero returns none.
func (c *Client) ListDashboard(state string, role string, limit *int) ([]PullRequest, bool, error) {
	q := url.Values{}
	if state != "" {
		q.Set("state", state)
	}
	if role != "" {
		q.Set("role", role)
	}
	return c.collectPullRequests("/dashboard/pull-requests", q, limit)
}

// collectPullRequests decodes each page into one slice, stopping at the limit.
//
// The slice starts empty rather than nil, because a nil one marshals to JSON's
// null and a caller's `jq '.[]'` cannot iterate that. A list read answers with a
// list whether or not it found anything.
func (c *Client) collectPullRequests(path string, q url.Values, limit *int) ([]PullRequest, bool, error) {
	out := []PullRequest{}
	truncated, err := c.paged(path, q, limit, func(raw json.RawMessage) (int, int, error) {
		var batch []PullRequest
		if err := json.Unmarshal(raw, &batch); err != nil {
			return 0, 0, err
		}
		kept := 0
		for _, pr := range batch {
			if limit != nil && len(out) >= *limit {
				break
			}
			out = append(out, pr)
			kept++
		}
		return len(batch), kept, nil
	})
	return out, truncated, err
}

func (c *Client) GetPullRequest(r Repo, id int) (*PullRequest, error) {
	var pr PullRequest
	path := fmt.Sprintf("%s/%d", prPath(r), id)
	if err := c.do(http.MethodGet, path, nil, nil, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

type CreateOptions struct {
	Title        string
	Description  string
	SourceBranch string
	TargetBranch string
	Reviewers    []string
}

func (c *Client) CreatePullRequest(r Repo, opts CreateOptions) (*PullRequest, error) {
	type refBody struct {
		ID         string  `json:"id"`
		Repository RepoRef `json:"repository"`
	}
	repoRef := RepoRef{Slug: r.Slug}
	repoRef.Project.Key = r.Project

	reviewers := make([]Participant, 0, len(opts.Reviewers))
	for _, name := range opts.Reviewers {
		reviewers = append(reviewers, Participant{User: User{Name: name}})
	}

	body := map[string]any{
		"title":       opts.Title,
		"description": opts.Description,
		"fromRef":     refBody{ID: "refs/heads/" + opts.SourceBranch, Repository: repoRef},
		"toRef":       refBody{ID: "refs/heads/" + opts.TargetBranch, Repository: repoRef},
		"reviewers":   reviewers,
	}

	var pr PullRequest
	if err := c.do(http.MethodPost, prPath(r), nil, body, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

// SetReviewStatus sets the authenticated user's participant status. The older
// POST .../approve endpoint is deprecated in favor of this one, which also
// expresses UNAPPROVED and NEEDS_WORK.
func (c *Client) SetReviewStatus(r Repo, id int, status string) error {
	user, err := c.Whoami()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("%s/%d/participants/%s", prPath(r), id, user)
	return c.do(http.MethodPut, path, nil, map[string]string{"status": status}, nil)
}

// Merge merges a pull request. Bitbucket uses the version for optimistic
// concurrency: passing a stale one fails rather than merging changes the caller
// never saw, so it is always read immediately beforehand.
func (c *Client) Merge(r Repo, id int, version int) (*PullRequest, error) {
	q := url.Values{"version": []string{strconv.Itoa(version)}}
	path := fmt.Sprintf("%s/%d/merge", prPath(r), id)
	var pr PullRequest
	if err := c.do(http.MethodPost, path, q, nil, &pr); err != nil {
		return nil, err
	}
	return &pr, nil
}

type MergeStatus struct {
	CanMerge   bool `json:"canMerge"`
	Conflicted bool `json:"conflicted"`
	Vetoes     []struct {
		SummaryMessage  string `json:"summaryMessage"`
		DetailedMessage string `json:"detailedMessage"`
	} `json:"vetoes"`
}

func (c *Client) CanMerge(r Repo, id int) (*MergeStatus, error) {
	var status MergeStatus
	path := fmt.Sprintf("%s/%d/merge", prPath(r), id)
	if err := c.do(http.MethodGet, path, nil, nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

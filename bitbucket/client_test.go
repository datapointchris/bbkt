package bitbucket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(Options{BaseURL: server.URL, Token: "test-token"})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, server
}

func TestListPullRequestsPaginates(t *testing.T) {
	var seenStarts []string
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		start := r.URL.Query().Get("start")
		seenStarts = append(seenStarts, start)

		w.Header().Set("X-AUSERNAME", "chris")
		switch start {
		case "0":
			writeJSON(w, map[string]any{
				"size": 2, "isLastPage": false, "nextPageStart": 2,
				"values": []map[string]any{prFixture(1, "first"), prFixture(2, "second")},
			})
		default:
			writeJSON(w, map[string]any{
				"size": 1, "isLastPage": true,
				"values": []map[string]any{prFixture(3, "third")},
			})
		}
	})

	client, _ := newTestClient(t, mux)
	repo := Repo{Project: "DATA", Slug: "pipeline"}

	// No limit at all, which is what every caller that leaves the field out of
	// the literal asks for. The walk stops on isLastPage and both pages run.
	prs, truncated, err := client.ListPullRequests(repo, ListOptions{State: "OPEN"})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 3 {
		t.Fatalf("got %d pull requests, want 3", len(prs))
	}
	if truncated {
		t.Error("an uncapped walk reported rows left behind")
	}
	if len(seenStarts) != 2 || seenStarts[0] != "0" || seenStarts[1] != "2" {
		t.Errorf("pagination walked starts %v, want [0 2]", seenStarts)
	}
	if prs[0].FromRef.DisplayID != "feature/PROJ-1" {
		t.Errorf("fromRef displayId = %q", prs[0].FromRef.DisplayID)
	}
}

// The zero value of ListOptions is what a caller writes when they have no
// opinion about how many rows they want, and every literal that omits Limit
// writes it. Reading that as "no rows" is invisible in Go: the compiler sees a
// well-typed call and no site reads as changed.
//
// This is the shape that broke pr approve, pr unapprove, pr needs-work and the
// no-id forms of pr merge and pr open, all of which find their pull request by
// listing every open one and matching the branch.
func TestAnOmittedLimitReturnsEveryRow(t *testing.T) {
	requests := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		requests++
		writeJSON(w, map[string]any{
			"size": 1, "isLastPage": true,
			"values": []map[string]any{prFixture(7, "seventh")},
		})
	})

	client, _ := newTestClient(t, mux)
	prs, _, err := client.ListPullRequests(Repo{Project: "DATA", Slug: "pipeline"}, ListOptions{State: "OPEN"})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if requests != 1 {
		t.Errorf("issued %d requests, want 1", requests)
	}
	if len(prs) != 1 {
		t.Fatalf("got %d pull requests, want 1", len(prs))
	}
}

func TestListPullRequestsRespectsLimit(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Errorf("page size = %q, want 2", got)
		}
		writeJSON(w, map[string]any{
			"size": 2, "isLastPage": false, "nextPageStart": 2,
			"values": []map[string]any{prFixture(1, "first"), prFixture(2, "second")},
		})
	})

	client, _ := newTestClient(t, mux)
	two := 2
	prs, truncated, err := client.ListPullRequests(Repo{Project: "DATA", Slug: "pipeline"}, ListOptions{Limit: &two})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d, want 2 — limit must stop the walk", len(prs))
	}
	if !truncated {
		t.Error("a walk stopped by the limit reported no rows left behind")
	}
}

// The walk stopping at the limit is not the same fact as rows being left
// behind, and they part company exactly where a repository holds as many rows
// as the limit. Re-deriving truncation from the row count cannot tell them
// apart, so the server's own isLastPage is what answers.
func TestALimitEqualToTheRowCountLeavesNothingBehind(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"size": 3, "isLastPage": true,
			"values": []map[string]any{
				prFixture(1, "first"), prFixture(2, "second"), prFixture(3, "third"),
			},
		})
	})

	client, _ := newTestClient(t, mux)
	three := 3
	prs, truncated, err := client.ListPullRequests(Repo{Project: "DATA", Slug: "pipeline"}, ListOptions{Limit: &three})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 3 {
		t.Fatalf("got %d, want 3", len(prs))
	}
	if truncated {
		t.Error("a complete listing was reported as truncated")
	}
}

// The cap landing mid-page leaves rows behind even when that page is the last
// one, so the page's own row count has to be weighed against what was kept.
func TestALimitInsideTheLastPageLeavesRowsBehind(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"size": 3, "isLastPage": true,
			"values": []map[string]any{
				prFixture(1, "first"), prFixture(2, "second"), prFixture(3, "third"),
			},
		})
	})

	client, _ := newTestClient(t, mux)
	two := 2
	prs, truncated, err := client.ListPullRequests(Repo{Project: "DATA", Slug: "pipeline"}, ListOptions{Limit: &two})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d, want 2", len(prs))
	}
	if !truncated {
		t.Error("a listing cut inside the last page reported nothing left behind")
	}
}

// A list read answers with a list. A nil slice marshals to JSON's null, which a
// consumer's `jq '.[]'` cannot iterate, so the type a caller decodes would flip
// between array and null on whether anything was found.
func TestAnEmptyListingMarshalsToAnEmptyArray(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"size": 0, "isLastPage": true, "values": []map[string]any{}})
	})

	client, _ := newTestClient(t, mux)
	prs, _, err := client.ListPullRequests(Repo{Project: "DATA", Slug: "pipeline"}, ListOptions{State: "OPEN"})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}

	encoded, err := json.Marshal(prs)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(encoded) != "[]" {
		t.Errorf("an empty listing encoded as %s, want []", encoded)
	}
}

// A cap of zero is a request for no rows, and the cheapest way to serve it is
// not to ask. The handler fails the test if it is reached, which is the only
// assertion that can tell "asked and discarded" from "never asked".
func TestACapOfZeroIssuesNoRequest(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a cap of zero reached the server at %s", r.URL)
		writeJSON(w, map[string]any{
			"size": 1, "isLastPage": true,
			"values": []map[string]any{prFixture(1, "first")},
		})
	})

	client, _ := newTestClient(t, mux)
	zero := 0
	prs, _, err := client.ListPullRequests(Repo{Project: "DATA", Slug: "pipeline"}, ListOptions{Limit: &zero})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 0 {
		t.Fatalf("got %d pull requests, want none", len(prs))
	}
}

// A negative is not a number of rows, so it collects nothing for the same
// reason a zero does. The trap it stands in front of is a `> 0` comparison: a
// negative falls straight through one into an unbounded walk, which reads every
// page of a repository's history and returns a list that looks entirely
// plausible.
func TestANegativeCapCollectsNothing(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("a negative cap reached the server at %s", r.URL)
	})

	client, _ := newTestClient(t, mux)
	negative := -1
	prs, _, err := client.ListPullRequests(Repo{Project: "DATA", Slug: "pipeline"}, ListOptions{Limit: &negative})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 0 {
		t.Fatalf("got %d pull requests, want none", len(prs))
	}
}

func TestApprovalCount(t *testing.T) {
	pr := PullRequest{Reviewers: []Participant{
		{Approved: true}, {Approved: false}, {Approved: true},
	}}
	approved, total := pr.ApprovalCount()
	if approved != 2 || total != 3 {
		t.Errorf("got %d/%d, want 2/3", approved, total)
	}
}

func TestMergeSendsCurrentVersion(t *testing.T) {
	var mergedVersion string
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests/42/merge", func(w http.ResponseWriter, r *http.Request) {
		mergedVersion = r.URL.Query().Get("version")
		writeJSON(w, prFixture(42, "merged"))
	})

	client, _ := newTestClient(t, mux)
	if _, err := client.Merge(Repo{Project: "DATA", Slug: "pipeline"}, 42, 7); err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if mergedVersion != "7" {
		t.Errorf("merge sent version=%q, want 7", mergedVersion)
	}
}

func TestSetReviewStatusUsesAuthenticatedUser(t *testing.T) {
	var gotPath, gotStatus string
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/inbox/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-AUSERNAME", "chris")
		writeJSON(w, map[string]any{"size": 0, "isLastPage": true, "values": []any{}})
	})
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests/42/participants/chris",
		func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			gotStatus = body["status"]
			writeJSON(w, map[string]any{})
		})

	client, _ := newTestClient(t, mux)
	if err := client.SetReviewStatus(Repo{Project: "DATA", Slug: "pipeline"}, 42, "APPROVED"); err != nil {
		t.Fatalf("SetReviewStatus: %v", err)
	}
	if gotPath == "" {
		t.Fatal("participant endpoint was never called")
	}
	if gotStatus != "APPROVED" {
		t.Errorf("status = %q, want APPROVED", gotStatus)
	}
}

func TestAPIErrorSurfacesBitbucketMessage(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests/9", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		writeJSON(w, map[string]any{
			"errors": []map[string]any{{"message": "Pull request 9 does not exist in DATA/pipeline."}},
		})
	})

	client, _ := newTestClient(t, mux)
	_, err := client.GetPullRequest(Repo{Project: "DATA", Slug: "pipeline"}, 9)
	if err == nil {
		t.Fatal("expected an error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("got %T, want *APIError", err)
	}
	if apiErr.Status != 404 {
		t.Errorf("status = %d, want 404", apiErr.Status)
	}
	if apiErr.Message != "Pull request 9 does not exist in DATA/pipeline." {
		t.Errorf("message = %q — Bitbucket's own text must survive", apiErr.Message)
	}
}

func TestCreatePullRequestBuildsRefs(t *testing.T) {
	var body map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		writeJSON(w, prFixture(11, "created"))
	})

	client, _ := newTestClient(t, mux)
	_, err := client.CreatePullRequest(Repo{Project: "DATA", Slug: "pipeline"}, CreateOptions{
		Title:        "PROJ-1 do the thing",
		SourceBranch: "feature/PROJ-1",
		TargetBranch: "main",
		Reviewers:    []string{"gopal"},
	})
	if err != nil {
		t.Fatalf("CreatePullRequest: %v", err)
	}

	from := body["fromRef"].(map[string]any)
	if from["id"] != "refs/heads/feature/PROJ-1" {
		t.Errorf("fromRef.id = %v, want refs/heads/feature/PROJ-1", from["id"])
	}
	to := body["toRef"].(map[string]any)
	if to["id"] != "refs/heads/main" {
		t.Errorf("toRef.id = %v, want refs/heads/main", to["id"])
	}
	project := from["repository"].(map[string]any)["project"].(map[string]any)
	if project["key"] != "DATA" {
		t.Errorf("fromRef repository project key = %v, want DATA", project["key"])
	}
	reviewers := body["reviewers"].([]any)
	if len(reviewers) != 1 {
		t.Fatalf("got %d reviewers, want 1", len(reviewers))
	}
	if name := reviewers[0].(map[string]any)["user"].(map[string]any)["name"]; name != "gopal" {
		t.Errorf("reviewer name = %v, want gopal", name)
	}
}

func prFixture(id int, title string) map[string]any {
	return map[string]any{
		"id":      id,
		"version": 0,
		"title":   title,
		"state":   "OPEN",
		"open":    true,
		"fromRef": map[string]any{
			"id":         "refs/heads/feature/PROJ-" + strconv.Itoa(id),
			"displayId":  "feature/PROJ-" + strconv.Itoa(id),
			"repository": map[string]any{"slug": "pipeline", "project": map[string]any{"key": "DATA"}},
		},
		"toRef": map[string]any{
			"id":         "refs/heads/main",
			"displayId":  "main",
			"repository": map[string]any{"slug": "pipeline", "project": map[string]any{"key": "DATA"}},
		},
		"author":    map[string]any{"user": map[string]any{"name": "chris"}},
		"reviewers": []any{},
		"links":     map[string]any{"self": []map[string]any{{"href": fmt.Sprintf("https://bitbucket.example/projects/DATA/repos/pipeline/pull-requests/%d", id)}}},
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

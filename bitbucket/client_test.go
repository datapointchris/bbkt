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

	prs, err := client.ListPullRequests(repo, ListOptions{State: "OPEN", Limit: 0})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 3 {
		t.Fatalf("got %d pull requests, want 3", len(prs))
	}
	if len(seenStarts) != 2 || seenStarts[0] != "0" || seenStarts[1] != "2" {
		t.Errorf("pagination walked starts %v, want [0 2]", seenStarts)
	}
	if prs[0].FromRef.DisplayID != "feature/PROJ-1" {
		t.Errorf("fromRef displayId = %q", prs[0].FromRef.DisplayID)
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
	prs, err := client.ListPullRequests(Repo{Project: "DATA", Slug: "pipeline"}, ListOptions{Limit: 2})
	if err != nil {
		t.Fatalf("ListPullRequests: %v", err)
	}
	if len(prs) != 2 {
		t.Fatalf("got %d, want 2 — limit must stop the walk", len(prs))
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

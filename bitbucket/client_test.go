package bitbucket

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
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

// The activity stream mixes every action, carries one COMMENTED activity per
// comment including each reply, and nests replies under the comment they answer.
// Releases before 8 anchor a thread on the activity with a string path; the
// current schema puts the anchor on the comment with a path object.
func TestCommentThreadsAreReadFromTheActivityStream(t *testing.T) {
	user := func(name string) map[string]any {
		return map[string]any{"name": name, "displayName": name, "slug": name, "type": "NORMAL"}
	}
	reply := map[string]any{
		"id": 1002, "version": 0, "text": "Fixed in the next push.", "author": user("chris"),
		"createdDate": 1760000300000, "comments": []any{}, "severity": "NORMAL", "state": "OPEN",
	}
	lineThread := map[string]any{
		"id": 1001, "version": 1, "text": "This misses the empty case.", "author": user("gopal"),
		"createdDate": 1760000200000, "comments": []any{reply}, "severity": "NORMAL", "state": "OPEN",
	}
	general := map[string]any{
		"id": 1003, "version": 0, "text": "Looks good overall.\n\nOne question below.", "author": user("gopal"),
		"createdDate": 1760000400000, "comments": []any{}, "severity": "NORMAL", "state": "OPEN",
	}
	currentShape := map[string]any{
		"id": 1004, "version": 0, "text": "Rename this.", "author": user("gopal"),
		"createdDate": 1760000100000, "severity": "BLOCKER", "state": "OPEN", "threadResolved": true,
		"anchor": map[string]any{
			"path": map[string]any{"components": []string{"src", "parser.go"}, "name": "parser.go", "parent": "src"},
			"line": 40, "lineType": "CONTEXT", "fileType": "FROM", "diffType": "EFFECTIVE",
		},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests/42/activities", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"size": 6, "limit": 25, "isLastPage": true, "start": 0,
			"values": []map[string]any{
				{"id": 6, "action": "COMMENTED", "commentAction": "ADDED", "user": user("gopal"), "comment": general},
				{"id": 5, "action": "COMMENTED", "commentAction": "REPLIED", "user": user("chris"), "comment": reply},
				{"id": 4, "action": "APPROVED", "user": user("gopal")},
				{
					"id": 3, "action": "COMMENTED", "commentAction": "ADDED", "user": user("gopal"), "comment": lineThread,
					"commentAnchor": map[string]any{"line": 10, "lineType": "ADDED", "fileType": "TO", "path": "README.md", "srcPath": "README.md"},
				},
				{"id": 2, "action": "RESCOPED", "fromHash": "abc", "added": map[string]any{"commits": []any{}, "total": 0}},
				{"id": 1, "action": "COMMENTED", "commentAction": "ADDED", "user": user("gopal"), "comment": currentShape},
			},
		})
	})

	client, _ := newTestClient(t, mux)
	threads, err := client.ListCommentThreads(Repo{Project: "DATA", Slug: "pipeline"}, 42)
	if err != nil {
		t.Fatalf("ListCommentThreads: %v", err)
	}

	var ids []int
	for _, thread := range threads {
		ids = append(ids, thread.ID)
	}
	if fmt.Sprint(ids) != "[1003 1001 1004]" {
		t.Fatalf("threads %v, want [1003 1001 1004]: the pull request's own first, then by path and line, and no reply as a thread", ids)
	}
	if len(threads[1].Comments) != 1 || threads[1].Comments[0].ID != 1002 {
		t.Errorf("thread 1001 replies = %+v, want only 1002", threads[1].Comments)
	}
	if a := threads[1].Anchor; a == nil || a.Path != "README.md" || a.Line != 10 || a.LineType != "ADDED" || a.FileType != FileTo {
		t.Errorf("thread 1001 anchor = %+v, want README.md:10 ADDED TO from the activity", a)
	}
	if a := threads[2].Anchor; a == nil || a.Path != "src/parser.go" || a.FileType != FileFrom {
		t.Errorf("thread 1004 anchor = %+v, want src/parser.go on FROM from the comment's own path object", a)
	}
	if !threads[2].ThreadResolved || threads[2].Severity != "BLOCKER" {
		t.Errorf("thread 1004 = %+v, want a resolved task", threads[2])
	}

	encoded, err := json.Marshal(threads)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(encoded), `"comments":null`) {
		t.Errorf("a comment with no replies encoded its replies as null: %s", encoded)
	}
}

// The line type and the numbering are read from the pull request's own diff.
// The fixture replaces old line 10 with new lines 10 and 11, so the two sides
// number every later line differently. Data Center also writes a positional
// number on the other side of each added or removed line, which must never be
// read as a line of the old file. Here the added lines carry source 11 and 12.
func TestResolveAnchorReadsTheLineTypeFromTheDiff(t *testing.T) {
	pathObject := func(p string) map[string]any {
		parts := strings.Split(p, "/")
		return map[string]any{"components": parts, "name": parts[len(parts)-1], "toString": p}
	}
	diffLine := func(source, destination int, text string) map[string]any {
		return map[string]any{"source": source, "destination": destination, "line": text, "truncated": false}
	}
	var diffQueries []string
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests/42/changes", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"size": 2, "limit": 1000, "isLastPage": true, "start": 0,
			"values": []map[string]any{
				{"path": pathObject("README.md"), "type": "MODIFY", "nodeType": "FILE"},
				{"path": pathObject("src/new.go"), "srcPath": pathObject("src/old.go"), "type": "MOVE", "nodeType": "FILE"},
			},
		})
	})
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests/42/diff/{path...}", func(w http.ResponseWriter, r *http.Request) {
		diffQueries = append(diffQueries, r.PathValue("path")+"?"+r.URL.Query().Get("srcPath"))
		writeJSON(w, map[string]any{
			"fromHash": "a0f224f", "toHash": "dc93f22", "contextLines": 3, "whitespace": "SHOW",
			"diffs": []map[string]any{{
				"source": pathObject("README.md"), "destination": pathObject("README.md"),
				"hunks": []map[string]any{{
					"sourceLine": 7, "sourceSpan": 7, "destinationLine": 7, "destinationSpan": 8,
					"segments": []map[string]any{
						{"type": "CONTEXT", "lines": []any{diffLine(7, 7, "seven"), diffLine(8, 8, "eight"), diffLine(9, 9, "nine")}},
						{"type": "REMOVED", "lines": []any{diffLine(10, 10, "old ten")}},
						{"type": "ADDED", "lines": []any{diffLine(11, 10, "new ten"), diffLine(12, 11, "new eleven")}},
						{"type": "CONTEXT", "lines": []any{diffLine(11, 12, "eleven"), diffLine(12, 13, "twelve"), diffLine(13, 14, "thirteen")}},
					},
				}},
			}},
		})
	})

	client, _ := newTestClient(t, mux)
	repo := Repo{Project: "DATA", Slug: "pipeline"}
	for _, tt := range []struct {
		line     int
		fileType string
		lineType string
		text     string
	}{
		{10, FileTo, "ADDED", "new ten"},
		{11, FileTo, "ADDED", "new eleven"},
		{12, FileTo, "CONTEXT", "eleven"},
		{10, FileFrom, "REMOVED", "old ten"},
		{11, FileFrom, "CONTEXT", "eleven"},
		{8, FileFrom, "CONTEXT", "eight"},
	} {
		anchor, text, err := client.ResolveAnchor(repo, 42, "README.md", tt.line, tt.fileType)
		if err != nil {
			t.Errorf("line %d on %s: %v", tt.line, tt.fileType, err)
			continue
		}
		want := Anchor{Path: "README.md", Line: tt.line, LineType: tt.lineType, FileType: tt.fileType}
		if *anchor != want || text != tt.text {
			t.Errorf("line %d on %s = %+v %q, want %+v %q", tt.line, tt.fileType, *anchor, text, want, tt.text)
		}
	}

	var notInDiff *LineNotInDiffError
	if _, _, err := client.ResolveAnchor(repo, 42, "README.md", 40, FileTo); !errors.As(err, &notInDiff) {
		t.Errorf("a line outside every hunk answered %v, want a LineNotInDiffError", err)
	}

	anchor, _, err := client.ResolveAnchor(repo, 42, "src/old.go", 0, FileTo)
	if err != nil {
		t.Fatalf("a moved file found by its old path: %v", err)
	}
	if *anchor != (Anchor{Path: "src/new.go", SrcPath: "src/old.go"}) {
		t.Errorf("file anchor on a moved file = %+v, want path src/new.go and srcPath src/old.go", *anchor)
	}
	if _, _, err := client.ResolveAnchor(repo, 42, "src/new.go", 3, FileTo); !errors.As(err, &notInDiff) {
		t.Fatalf("a line of a moved file: %v", err)
	}
	if last := diffQueries[len(diffQueries)-1]; last != "src/new.go?src/old.go" {
		t.Errorf("diff of a moved file requested as %q, want its new path with srcPath set to the old one", last)
	}

	var notChanged *PathNotChangedError
	_, _, err = client.ResolveAnchor(repo, 42, "docs/untouched.md", 0, FileTo)
	if !errors.As(err, &notChanged) {
		t.Fatalf("a path the pull request leaves alone answered %v, want a PathNotChangedError", err)
	}
	if want := []string{"README.md", "src/new.go"}; !slices.Equal(notChanged.Changed, want) {
		t.Errorf("a path the pull request leaves alone carried %q, want the paths it changes, %q", notChanged.Changed, want)
	}
}

// A line comment sends its anchor with a string path and no hashes, which Data
// Center resolves to the effective diff. A reply sends only its parent's id.
func TestCreateCommentSendsTheAnchorOrTheParent(t *testing.T) {
	var bodies []map[string]any
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/1.0/projects/DATA/repos/pipeline/pull-requests/42/comments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 1005 + len(bodies), "version": 0, "text": body["text"],
			"author": map[string]any{"name": "chris"}, "comments": []any{},
		})
	})

	client, _ := newTestClient(t, mux)
	repo := Repo{Project: "DATA", Slug: "pipeline"}
	created, err := client.CreateComment(repo, 42, NewComment{
		Text:   "Guard the empty case.",
		Anchor: &Anchor{Path: "README.md", Line: 11, LineType: "ADDED", FileType: FileTo},
	})
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if created.ID != 1006 {
		t.Errorf("created id = %d, want 1006 from the response", created.ID)
	}
	if _, err := client.CreateComment(repo, 42, NewComment{Text: "Done.", Parent: 1001}); err != nil {
		t.Fatalf("CreateComment reply: %v", err)
	}

	line, _ := json.Marshal(bodies[0])
	if want := `{"anchor":{"fileType":"TO","line":11,"lineType":"ADDED","path":"README.md"},"text":"Guard the empty case."}`; string(line) != want {
		t.Errorf("line comment body = %s, want %s", line, want)
	}
	reply, _ := json.Marshal(bodies[1])
	if want := `{"parent":{"id":1001},"text":"Done."}`; string(reply) != want {
		t.Errorf("reply body = %s, want %s", reply, want)
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

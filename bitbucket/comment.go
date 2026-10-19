package bitbucket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Path is a file path in the repository. Data Center writes it two ways: a
// plain string in a comment anchor, and an object carrying toString and
// components in a change or a diff. Both decode, and it always encodes as the
// string the comment endpoint takes.
type Path string

func (p *Path) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*p = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*p = Path(s)
		return nil
	}
	var object struct {
		ToString   string   `json:"toString"`
		Components []string `json:"components"`
	}
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("path is neither a string nor a path object: %w", err)
	}
	if object.ToString != "" {
		*p = Path(object.ToString)
		return nil
	}
	*p = Path(strings.Join(object.Components, "/"))
	return nil
}

// The two files of a pull request's diff, as an anchor's fileType names them.
// FROM is the file before the pull request, which is where a removed line
// lives. TO is the file after it. They run opposite to the pull request's own
// fromRef and toRef: the source branch's version of a file is the diff's TO.
const (
	FileFrom = "FROM"
	FileTo   = "TO"
)

// Anchor places a comment in the diff. Path alone anchors to the file; Line,
// LineType and FileType together anchor to one line on one side.
type Anchor struct {
	Path     Path   `json:"path"`
	SrcPath  Path   `json:"srcPath,omitempty"`
	Line     int    `json:"line,omitempty"`
	LineType string `json:"lineType,omitempty"`
	FileType string `json:"fileType,omitempty"`
	Orphaned bool   `json:"orphaned,omitempty"`
}

// Comment is one comment and the replies beneath it, which nest to any depth.
type Comment struct {
	ID             int       `json:"id"`
	Version        int       `json:"version"`
	Text           string    `json:"text"`
	Author         User      `json:"author"`
	CreatedDate    int64     `json:"createdDate"`
	UpdatedDate    int64     `json:"updatedDate"`
	Severity       string    `json:"severity,omitempty"`
	State          string    `json:"state,omitempty"`
	ThreadResolved bool      `json:"threadResolved,omitempty"`
	Anchor         *Anchor   `json:"anchor,omitempty"`
	Comments       []Comment `json:"comments"`
}

func (c Comment) Created() time.Time { return time.UnixMilli(c.CreatedDate) }

// commentActivity is the part of an activity a comment thread is read from.
// Every other action shares the stream and decodes with a nil Comment.
type commentActivity struct {
	Action        string   `json:"action"`
	Comment       *Comment `json:"comment"`
	CommentAnchor *Anchor  `json:"commentAnchor"`
}

// ListCommentThreads returns every comment thread on a pull request, read from
// its activity stream, in reading order: comments on the pull request itself
// oldest first, then file comments by path and line.
//
// The stream carries one COMMENTED activity per comment, a reply included, and
// each activity's comment arrives with its replies already nested beneath it.
// A thread's root is therefore the one comment no other comment contains.
// Picking roots that way, rather than trusting commentAction to say which
// activity opened a thread, keeps a reply from showing twice whatever actions
// an instance records for edits.
//
// Data Center releases before 8 put the anchor on the activity alone, and later
// ones put it on the comment as well, so the comment's own wins when present.
func (c *Client) ListCommentThreads(r Repo, id int) ([]Comment, error) {
	var candidates []Comment
	path := fmt.Sprintf("%s/%d/activities", prPath(r), id)
	_, err := c.paged(path, nil, nil, func(raw json.RawMessage) (int, int, error) {
		var batch []commentActivity
		if err := json.Unmarshal(raw, &batch); err != nil {
			return 0, 0, err
		}
		for _, a := range batch {
			if a.Action != "COMMENTED" || a.Comment == nil {
				continue
			}
			comment := *a.Comment
			if comment.Anchor == nil {
				comment.Anchor = a.CommentAnchor
			}
			candidates = append(candidates, comment)
		}
		return len(batch), len(batch), nil
	})
	if err != nil {
		return nil, err
	}

	nested := map[int]bool{}
	for _, candidate := range candidates {
		markReplies(candidate.Comments, nested)
	}
	threads := []Comment{}
	seen := map[int]bool{}
	for _, candidate := range candidates {
		if nested[candidate.ID] || seen[candidate.ID] {
			continue
		}
		seen[candidate.ID] = true
		threads = append(threads, withListReplies(candidate))
	}
	sort.SliceStable(threads, func(i, j int) bool { return readsBefore(threads[i], threads[j]) })
	return threads, nil
}

func markReplies(replies []Comment, nested map[int]bool) {
	for _, reply := range replies {
		nested[reply.ID] = true
		markReplies(reply.Comments, nested)
	}
}

// withListReplies gives every level an empty list rather than nil, so --json
// answers a comment with no replies with [] and never null.
func withListReplies(c Comment) Comment {
	replies := make([]Comment, 0, len(c.Comments))
	for _, reply := range c.Comments {
		replies = append(replies, withListReplies(reply))
	}
	c.Comments = replies
	return c
}

func readsBefore(a, b Comment) bool {
	if (a.Anchor == nil) != (b.Anchor == nil) {
		return a.Anchor == nil
	}
	if a.Anchor != nil {
		if a.Anchor.Path != b.Anchor.Path {
			return a.Anchor.Path < b.Anchor.Path
		}
		if a.Anchor.Line != b.Anchor.Line {
			return a.Anchor.Line < b.Anchor.Line
		}
	}
	return a.CreatedDate < b.CreatedDate
}

// GetComment returns one comment and its replies.
func (c *Client) GetComment(r Repo, id, commentID int) (*Comment, error) {
	var comment Comment
	path := fmt.Sprintf("%s/%d/comments/%d", prPath(r), id, commentID)
	if err := c.do(http.MethodGet, path, nil, nil, &comment); err != nil {
		return nil, err
	}
	return &comment, nil
}

// NewComment is a comment to post. A nil Anchor and a zero Parent post on the
// pull request itself. A Parent posts a reply, which takes its thread's place
// and so carries no anchor of its own.
type NewComment struct {
	Text   string
	Anchor *Anchor
	Parent int
}

// CreateComment posts a comment. No diffType and no hashes go with an anchor,
// which Data Center resolves to the pull request's effective diff: the one its
// web view shows.
func (c *Client) CreateComment(r Repo, id int, comment NewComment) (*Comment, error) {
	body := map[string]any{"text": comment.Text}
	if comment.Anchor != nil {
		body["anchor"] = comment.Anchor
	}
	if comment.Parent != 0 {
		body["parent"] = map[string]int{"id": comment.Parent}
	}
	var created Comment
	path := fmt.Sprintf("%s/%d/comments", prPath(r), id)
	if err := c.do(http.MethodPost, path, nil, body, &created); err != nil {
		return nil, err
	}
	return &created, nil
}

type change struct {
	Path    Path   `json:"path"`
	SrcPath Path   `json:"srcPath"`
	Type    string `json:"type"`
}

// changesLimit is how many changes one request asks for. The changes resource
// ignores start and answers with a single page, so walking it would read that
// page forever; one request as large as the server allows is the whole answer.
const changesLimit = 1000

// fileChange finds the change a pull request makes to path, matching either
// its current path or, for a move or copy, the one it had before.
func (c *Client) fileChange(r Repo, id int, path string) (*change, error) {
	var p page
	q := url.Values{"limit": []string{strconv.Itoa(changesLimit)}, "withComments": []string{"false"}}
	if err := c.do(http.MethodGet, fmt.Sprintf("%s/%d/changes", prPath(r), id), q, nil, &p); err != nil {
		return nil, err
	}
	var changes []change
	if err := json.Unmarshal(p.Values, &changes); err != nil {
		return nil, err
	}
	changed := make([]string, 0, len(changes))
	for i := range changes {
		if string(changes[i].Path) == path || string(changes[i].SrcPath) == path {
			return &changes[i], nil
		}
		changed = append(changed, string(changes[i].Path))
	}
	return nil, &PathNotChangedError{ID: id, Path: path, Changed: changed, Listed: p.IsLastPage}
}

// PathNotChangedError says a pull request changes nothing at a path, and
// carries the paths it does change. Listed is false when Bitbucket stopped the
// list at its limit, so the path may be among the changes it left out.
type PathNotChangedError struct {
	ID      int
	Path    string
	Changed []string
	Listed  bool
}

func (e *PathNotChangedError) Error() string {
	if !e.Listed {
		return fmt.Sprintf("%s is not among the %d changed files Bitbucket lists for #%d, which is as many as it returns",
			e.Path, len(e.Changed), e.ID)
	}
	return fmt.Sprintf("#%d does not change %s", e.ID, e.Path)
}

type fileDiff struct {
	Diffs []struct {
		Hunks []struct {
			Segments []struct {
				Type  string `json:"type"`
				Lines []struct {
					Source      int    `json:"source"`
					Destination int    `json:"destination"`
					Line        string `json:"line"`
				} `json:"lines"`
			} `json:"segments"`
		} `json:"hunks"`
	} `json:"diffs"`
}

// ResolveAnchor builds the anchor for a comment on path, and returns the text
// of the line it lands on. A line of zero anchors to the file.
//
// The line type is read from the pull request's own diff rather than asked
// for, because it is a fact about that diff and a wrong one puts the comment
// somewhere other than the line meant. fileType chooses the numbering: FROM
// counts lines in the file before the pull request, TO in the file after it.
// Each diff line carries a number on both sides, and only the side whose file
// holds the line counts — an added line's FROM number is a position, not a
// line of the old file.
func (c *Client) ResolveAnchor(r Repo, id int, path string, line int, fileType string) (*Anchor, string, error) {
	if line != 0 && fileType != FileFrom && fileType != FileTo {
		return nil, "", fmt.Errorf("fileType %q is neither %s nor %s", fileType, FileFrom, FileTo)
	}
	changed, err := c.fileChange(r, id, path)
	if err != nil {
		return nil, "", err
	}
	anchor := &Anchor{Path: changed.Path}
	if changed.SrcPath != "" && changed.SrcPath != changed.Path {
		anchor.SrcPath = changed.SrcPath
	}
	if line == 0 {
		return anchor, "", nil
	}

	q := url.Values{"withComments": []string{"false"}}
	if anchor.SrcPath != "" {
		q.Set("srcPath", string(anchor.SrcPath))
	}
	var diff fileDiff
	if err := c.do(http.MethodGet, fmt.Sprintf("%s/%d/diff/%s", prPath(r), id, escapePath(string(anchor.Path))), q, nil, &diff); err != nil {
		return nil, "", err
	}

	for _, d := range diff.Diffs {
		for _, hunk := range d.Hunks {
			for _, segment := range hunk.Segments {
				if !segmentOnSide(segment.Type, fileType) {
					continue
				}
				for _, l := range segment.Lines {
					number := l.Destination
					if fileType == FileFrom {
						number = l.Source
					}
					if number == line {
						anchor.Line, anchor.LineType, anchor.FileType = line, segment.Type, fileType
						return anchor, l.Line, nil
					}
				}
			}
		}
	}
	return nil, "", &LineNotInDiffError{Path: string(anchor.Path), Line: line, FileType: fileType}
}

// segmentOnSide reports whether a segment's lines exist in the file fileType
// names: added lines only after the pull request, removed lines only before.
func segmentOnSide(segmentType, fileType string) bool {
	switch segmentType {
	case "CONTEXT":
		return true
	case "ADDED":
		return fileType == FileTo
	case "REMOVED":
		return fileType == FileFrom
	}
	return false
}

// LineNotInDiffError says a line is not one the pull request's diff shows on
// the side asked for, which is the only place Bitbucket draws a line comment.
type LineNotInDiffError struct {
	Path     string
	Line     int
	FileType string
}

func (e *LineNotInDiffError) Error() string {
	return fmt.Sprintf("line %d of %s is not in the pull request's diff on the %s side", e.Line, e.Path, e.FileType)
}

// escapePath escapes each segment of a repository path and keeps the slashes,
// which the diff resource reads as the path's own structure.
func escapePath(path string) string {
	segments := strings.Split(path, "/")
	for i, s := range segments {
		segments[i] = url.PathEscape(s)
	}
	return strings.Join(segments, "/")
}

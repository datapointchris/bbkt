package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/datapointchris/goclikit"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/datapointchris/bbkt/bitbucket"
)

var prCommentCmd = goclikit.AsNamespace(&cobra.Command{
	Use:     "comment",
	GroupID: groupComment,
	Short:   "Read and post review comments",
	Long: `Review comments on a pull request: on the pull request itself, on a file, or
on one line of its diff.

Each verb takes the pull request's [id] the way the other pr verbs do, and
without one targets the open pull request for the checked-out branch. A
comment's own id is the number comment list prints beside it; reply takes it
as --to.

A line comment sits on one side of the diff. new is the file as the pull request
leaves it, where an added line lives. old is the file before it, where a removed
line lives. An unchanged line is on both, numbered on each.

The body comes from --body-file, or --body-file - for stdin. Without it, on a
terminal, $VISUAL or $EDITOR opens the way git commit does. Off a terminal, or
under --no-input, a missing --body-file is refused before anything is sent.`,
	Example: `  bbkt pr comment list 42                          every thread and its ids
  bbkt pr comment create 42 --path a.go --line 40  line 40 of the new a.go
  bbkt pr comment reply 42 --to 1001               answer comment 1001`,
})

func init() {
	prCommentCmd.AddCommand(prCommentListCmd, prCommentCreateCmd, prCommentReplyCmd)
	goclikit.WithRecoveryHints(prCommentCmd, prCommentRecoveryHints...)
}

// prCommentRecoveryHints replace pr's own under comment, because a 404 here
// can be the comment a reply answers as well as the pull request.
var prCommentRecoveryHints = []string{
	"List the pull requests you can see: bbkt pr list",
	"List a pull request's comments and their ids: bbkt pr comment list <id>",
	"Check which account the token authenticates as: bbkt config check",
}

var commentListJSON bool

var prCommentListCmd = &cobra.Command{
	Use:   "list [id]",
	Short: "Show a pull request's comment threads",
	Long: "Shows every comment thread on a pull request, with each reply under the comment\n" +
		"it answers. Comments on the pull request itself come first, then comments on files\n" +
		"and lines, ordered by path and line under the place each one is anchored.\n\n" +
		"The number on each comment's first row is its id, which reply takes as --to.\n" +
		"A marker after the date says the comment is a task, resolved, pending (a draft\n" +
		"only you see), or outdated (its line changed after it was written).",
	Example: "  bbkt pr comment list          the pull request for this branch\n" +
		"  bbkt pr comment list 42\n" +
		"  bbkt pr comment list 42 --json",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repo, err := resolveRepo()
		if err != nil {
			return err
		}
		client, err := newClient()
		if err != nil {
			return err
		}
		id, err := resolvePullRequestID(client, repo, args)
		if err != nil {
			return err
		}
		threads, err := client.ListCommentThreads(repo, id)
		if err != nil {
			return err
		}

		if commentListJSON {
			return emitJSON(cmd, threads)
		}
		if len(threads) == 0 {
			infof(cmd, "No comments on #%d.", id)
			return nil
		}
		writeThreads(cmd.OutOrStdout(), threads)
		return nil
	},
}

func writeThreads(out io.Writer, threads []bitbucket.Comment) {
	place := ""
	for i, thread := range threads {
		if i > 0 {
			writef(out, "\n")
		}
		if p := placeOf(thread.Anchor); i == 0 || p != place {
			writef(out, "%s\n", p)
			place = p
		}
		writeComment(out, thread, 1)
	}
}

func writeComment(out io.Writer, c bitbucket.Comment, depth int) {
	indent := strings.Repeat("  ", depth)
	writef(out, "%s%d  %s  %s", indent, c.ID, c.Author.Name, c.Created().Format("2006-01-02 15:04"))
	if marks := commentMarkers(c); len(marks) > 0 {
		writef(out, "  %s", strings.Join(marks, ", "))
	}
	writef(out, "\n")
	for _, line := range strings.Split(c.Text, "\n") {
		writef(out, "%s\n", strings.TrimRight(indent+"  "+line, " "))
	}
	for _, reply := range c.Comments {
		writeComment(out, reply, depth+1)
	}
}

func commentMarkers(c bitbucket.Comment) []string {
	var marks []string
	if c.Severity == "BLOCKER" {
		marks = append(marks, "task")
	}
	switch {
	case c.State == "RESOLVED" || c.ThreadResolved:
		marks = append(marks, "resolved")
	case c.State == "PENDING":
		marks = append(marks, "pending")
	}
	if c.Anchor != nil && c.Anchor.Orphaned {
		marks = append(marks, "outdated")
	}
	return marks
}

func placeOf(a *bitbucket.Anchor) string {
	switch {
	case a == nil:
		return "On the pull request"
	case a.Line == 0:
		return string(a.Path)
	default:
		return fmt.Sprintf("%s:%d (%s)", a.Path, a.Line, sideOf(a.FileType))
	}
}

// sides maps the --side words to the anchor's fileType. The words are the
// diff's own old and new, because Bitbucket's FROM and TO run opposite to the
// pull request's from and to branches and read backwards to anyone who knows
// those.
var sides = map[string]string{"new": bitbucket.FileTo, "old": bitbucket.FileFrom}

func sideOf(fileType string) string {
	if fileType == bitbucket.FileFrom {
		return "old"
	}
	return "new"
}

var (
	createCommentPath     string
	createCommentLine     int
	createCommentSide     string
	createCommentBodyFile string
	createCommentJSON     bool
)

var prCommentCreateCmd = &cobra.Command{
	Use:   "create [id]",
	Short: "Comment on a pull request, a file, or a line of its diff",
	Long: "Posts a comment. With no --path it goes on the pull request itself. --path puts\n" +
		"it on a file the pull request changes, and --line on one line of that file's diff.\n\n" +
		"--line counts in the new file unless --side old says the file before the pull\n" +
		"request, which is where a removed line is numbered. Whether the line was added,\n" +
		"removed or left unchanged is read from Bitbucket's own diff, so the comment lands\n" +
		"where its web view draws that line. A line outside the diff is refused before the\n" +
		"editor opens, as is a path the pull request does not change.\n\n" +
		"--path is the file's path in the repository, as git diff prints it. A moved file\n" +
		"is found by either of its paths.",
	Example: "  bbkt pr comment create 42\n" +
		"  bbkt pr comment create 42 --path src/parser.go\n" +
		"  bbkt pr comment create 42 --path src/parser.go --line 40\n" +
		"  bbkt pr comment create 42 --path src/parser.go --line 37 --side old\n" +
		"  bbkt pr comment create --path README.md --body-file notes.md",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		hasLine := cmd.Flags().Changed("line")
		switch {
		case cmd.Flags().Changed("path") && createCommentPath == "":
			return goclikit.UsageError(fmt.Errorf("--path is empty; omit it to comment on the pull request itself"))
		case hasLine && createCommentPath == "":
			return goclikit.UsageError(fmt.Errorf("--line needs --path, the file the line is in"))
		case hasLine && createCommentLine < 1:
			return goclikit.UsageError(fmt.Errorf("--line counts from 1, got %d", createCommentLine))
		case cmd.Flags().Changed("side") && !hasLine:
			return goclikit.UsageError(fmt.Errorf("--side needs --line; a comment on a whole file has no side"))
		}
		fileType, ok := sides[createCommentSide]
		if !ok {
			return goclikit.UsageError(fmt.Errorf("--side takes new or old, got %q", createCommentSide))
		}
		body, err := prepareCommentBody(cmd, createCommentBodyFile)
		if err != nil {
			return err
		}

		repo, err := resolveRepo()
		if err != nil {
			return err
		}
		client, err := newClient()
		if err != nil {
			return err
		}
		id, err := resolvePullRequestID(client, repo, args)
		if err != nil {
			return err
		}

		var anchor *bitbucket.Anchor
		about := []string{fmt.Sprintf("A comment on pull request #%d.", id)}
		if createCommentPath != "" {
			filePath := path.Clean(createCommentPath)
			var lineText string
			anchor, lineText, err = client.ResolveAnchor(repo, id, filePath, createCommentLine, fileType)
			var notInDiff *bitbucket.LineNotInDiffError
			if errors.As(err, &notInDiff) {
				return lineNotInDiff(id, filePath, createCommentLine, createCommentSide)
			}
			if err != nil {
				return err
			}
			about = []string{fmt.Sprintf("A comment on %s in pull request #%d.", anchor.Path, id)}
			if anchor.Line != 0 {
				about = []string{
					fmt.Sprintf("A comment on %s in pull request #%d,", placeOf(anchor), id),
					fmt.Sprintf("a line the diff shows as %s:", strings.ToLower(anchor.LineType)),
					"    " + lineText,
				}
			}
		}

		return postComment(cmd, client, repo, id, body, about, createCommentJSON,
			bitbucket.NewComment{Anchor: anchor},
			func(c *bitbucket.Comment) string {
				where := "the pull request"
				if anchor != nil {
					where = placeOf(anchor)
				}
				return fmt.Sprintf("Posted comment %d on #%d, on %s", c.ID, id, where)
			})
	},
}

// lineNotInDiff names the ways out of a line Bitbucket would not draw a
// comment on: see the diff, try the other side, or comment on the file.
func lineNotInDiff(id int, filePath string, line int, side string) error {
	view := fmt.Sprintf("bbkt pr view %d", id)
	if repoSpec != "" {
		view += " -R " + repoSpec
	}
	otherSide := "A removed line is numbered on the old side: --side old"
	if side == "old" {
		otherSide = "An added line is numbered on the new side: drop --side old"
	}
	return fmt.Errorf("line %d of %s is not in #%d's diff on the %s side\n"+
		"  Bitbucket draws a line comment only on a line its diff shows\n"+
		"  Print the command that shows the diff: %s\n"+
		"  %s\n"+
		"  Drop --line to comment on the whole file",
		line, filePath, id, side, view, otherSide)
}

var (
	replyCommentTo       int
	replyCommentBodyFile string
	replyCommentJSON     bool
)

var prCommentReplyCmd = &cobra.Command{
	Use:   "reply [id]",
	Short: "Reply to a comment thread",
	Long: "Posts a reply to the comment --to names, which bbkt pr comment list prints beside\n" +
		"each comment. A reply sits in its thread, so it takes no --path or --line.\n\n" +
		"The comment is read before the editor opens, so a wrong --to is refused before\n" +
		"you write anything, and the editor shows what you are answering.",
	Example: "  bbkt pr comment reply 42 --to 1001\n" +
		"  bbkt pr comment reply --to 1001 --body-file -",
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if replyCommentTo < 1 {
			return goclikit.UsageError(fmt.Errorf("--to takes the id of the comment to answer; bbkt pr comment list prints each one"))
		}
		body, err := prepareCommentBody(cmd, replyCommentBodyFile)
		if err != nil {
			return err
		}

		repo, err := resolveRepo()
		if err != nil {
			return err
		}
		client, err := newClient()
		if err != nil {
			return err
		}
		id, err := resolvePullRequestID(client, repo, args)
		if err != nil {
			return err
		}
		parent, err := client.GetComment(repo, id, replyCommentTo)
		if err != nil {
			return err
		}

		about := []string{fmt.Sprintf("A reply in pull request #%d to comment %d by %s:", id, parent.ID, parent.Author.Name)}
		for _, line := range strings.Split(parent.Text, "\n") {
			about = append(about, "    "+line)
		}
		return postComment(cmd, client, repo, id, body, about, replyCommentJSON,
			bitbucket.NewComment{Parent: parent.ID},
			func(c *bitbucket.Comment) string {
				return fmt.Sprintf("Posted comment %d on #%d, replying to %d", c.ID, id, parent.ID)
			})
	},
}

// commentBody is where a comment's text comes from. It is settled before the
// first request, so every refusal that does not need the server comes before
// any work does.
type commentBody struct {
	text   string
	editor []string
}

func prepareCommentBody(cmd *cobra.Command, bodyFile string) (commentBody, error) {
	if bodyFile != "" {
		text, err := readBodyFile(cmd, bodyFile)
		if err != nil {
			return commentBody{}, err
		}
		return commentBody{text: text}, nil
	}
	if noInput || !isatty.IsTerminal(os.Stdin.Fd()) {
		return commentBody{}, goclikit.UsageError(fmt.Errorf(
			"no comment body without a terminal; pass --body-file FILE, or --body-file - to read stdin"))
	}
	editor, err := editorCommand()
	if err != nil {
		return commentBody{}, fmt.Errorf("%w, or pass --body-file", err)
	}
	return commentBody{editor: editor}, nil
}

func readBodyFile(cmd *cobra.Command, bodyFile string) (string, error) {
	var data []byte
	var err error
	if bodyFile == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
	} else {
		data, err = os.ReadFile(bodyFile)
	}
	if err != nil {
		return "", fmt.Errorf("reading --body-file: %w", err)
	}
	text := trimBody(string(data))
	if text == "" {
		return "", fmt.Errorf("--body-file %s is empty; nothing to post", bodyFile)
	}
	return text, nil
}

// trimBody drops blank lines before the text and whitespace after it. Spaces
// opening the first line stay, since Markdown reads them as a code block.
func trimBody(text string) string {
	text = strings.TrimRight(text, " \t\r\n")
	for {
		first, rest, found := strings.Cut(text, "\n")
		if !found || strings.TrimSpace(first) != "" {
			return text
		}
		text = rest
	}
}

// scissors is git's own cut line. Everything from it down is left out of the
// comment, which lets the template quote what is being answered in Markdown,
// where a # line is a heading and cannot be the comment marker git uses.
const scissors = "# ------------------------ >8 ------------------------"

// composeInEditor opens a template in the editor and returns the comment and
// the file it was written in, which the caller removes once the comment is
// posted and keeps when it is not.
func composeInEditor(editor []string, about []string) (text, file string, err error) {
	f, err := os.CreateTemp("", "bbkt-comment-*.md")
	if err != nil {
		return "", "", err
	}
	file = f.Name()

	var template strings.Builder
	template.WriteString("\n" + scissors + "\n")
	template.WriteString("# Everything from the line above down is left out of the comment.\n")
	template.WriteString("# An empty comment posts nothing.\n#\n")
	for _, line := range about {
		template.WriteString(strings.TrimRight("# "+line, " ") + "\n")
	}
	if _, err := f.WriteString(template.String()); err != nil {
		_ = f.Close()
		_ = os.Remove(file)
		return "", "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(file)
		return "", "", err
	}

	if err := runEditor(editor, file); err != nil {
		_ = os.Remove(file)
		return "", "", fmt.Errorf("the editor exited with an error, so nothing was posted: %w", err)
	}
	written, err := os.ReadFile(file)
	if err != nil {
		return "", "", err
	}
	kept, _, _ := strings.Cut(string(written), scissors)
	text = trimBody(kept)
	if text == "" {
		_ = os.Remove(file)
		return "", "", fmt.Errorf("the comment is empty, so nothing was posted")
	}
	return text, file, nil
}

// postComment composes the body if the editor writes it, posts the comment and
// reports it. A failed post after an editing session keeps what was written,
// and says where, so the work survives the refusal.
func postComment(
	cmd *cobra.Command,
	client *bitbucket.Client,
	repo bitbucket.Repo,
	id int,
	body commentBody,
	about []string,
	asJSON bool,
	comment bitbucket.NewComment,
	posted func(*bitbucket.Comment) string,
) error {
	comment.Text = body.text
	file := ""
	if body.editor != nil {
		var err error
		comment.Text, file, err = composeInEditor(body.editor, about)
		if err != nil {
			return err
		}
	}

	created, err := client.CreateComment(repo, id, comment)
	if err != nil {
		if file != "" {
			if writeErr := os.WriteFile(file, []byte(comment.Text+"\n"), 0o600); writeErr == nil {
				infof(cmd, "The comment is kept in %s; post it with --body-file %s", file, file)
			}
		}
		return err
	}
	if file != "" {
		_ = os.Remove(file)
	}

	if asJSON {
		return emitJSON(cmd, created)
	}
	infof(cmd, "%s", posted(created))
	return nil
}

func init() {
	prCommentListCmd.Flags().BoolVar(&commentListJSON, "json", false, "Output the comment threads as JSON to stdout")

	prCommentCreateCmd.Flags().StringVar(&createCommentPath, "path", "", "the file to comment on, as git diff names it")
	prCommentCreateCmd.Flags().IntVar(&createCommentLine, "line", 0, "the line of --path to comment on")
	prCommentCreateCmd.Flags().StringVar(&createCommentSide, "side", "new", "which file --line counts in: new, or old for a removed line")
	prCommentCreateCmd.Flags().StringVar(&createCommentBodyFile, "body-file", "", "read the comment from a file, or - for stdin (default: $EDITOR)")
	prCommentCreateCmd.Flags().BoolVar(&createCommentJSON, "json", false, "Output the posted comment as JSON to stdout")

	prCommentReplyCmd.Flags().IntVar(&replyCommentTo, "to", 0, "the id of the comment to answer")
	prCommentReplyCmd.Flags().StringVar(&replyCommentBodyFile, "body-file", "", "read the reply from a file, or - for stdin (default: $EDITOR)")
	prCommentReplyCmd.Flags().BoolVar(&replyCommentJSON, "json", false, "Output the posted reply as JSON to stdout")
}

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// editorCommand resolves $VISUAL, then $EDITOR, to a command line whose binary
// is on PATH. It runs before anything the editor would be opened for, so a
// missing editor is refused before any work rests on it.
//
// The variable may carry arguments ("code --wait", "emacsclient -nw"), so it is
// split and only its first word is looked up.
func editorCommand() ([]string, error) {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		return nil, fmt.Errorf("no editor configured — set $VISUAL or $EDITOR")
	}
	binary, err := exec.LookPath(parts[0])
	if err != nil {
		return nil, fmt.Errorf("editor not found on PATH: %s", parts[0])
	}
	return append([]string{binary}, parts[1:]...), nil
}

// runEditor opens path in the editor in the foreground. A terminal editor
// blocks until it closes; a GUI editor returns at once unless told to wait.
func runEditor(editor []string, path string) error {
	editing := exec.Command(editor[0], append(editor[1:], path)...)
	editing.Stdin, editing.Stdout, editing.Stderr = os.Stdin, os.Stdout, os.Stderr
	return editing.Run()
}

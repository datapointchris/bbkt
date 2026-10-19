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

// openTerminal opens the controlling terminal for the editor to draw on and
// read from. bbkt's own stdout may be a pipe carrying --json, and an editor
// drawing there would hide itself from the user and corrupt the output.
func openTerminal() (*os.File, error) {
	return os.OpenFile("/dev/tty", os.O_RDWR, 0)
}

// runEditor opens path in the editor in the foreground, on tty. A terminal
// editor blocks until it closes; a GUI editor returns at once unless told to
// wait.
func runEditor(editor []string, tty *os.File, path string) error {
	editing := exec.Command(editor[0], append(editor[1:], path)...)
	editing.Stdin, editing.Stdout, editing.Stderr = tty, tty, tty
	return editing.Run()
}

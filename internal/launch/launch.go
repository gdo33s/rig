// Package launch shows files and changes outside rig's own pane: in rig's
// viewer, as a popup over the tab in Rex, or in place elsewhere. The
// viewer hands a file to the editor when it is to be changed.
package launch

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gdo33s/rig/internal/rex"
)

// Popup labels. A view replaces an earlier view, but never a popup where
// the file is being edited, so unsaved work is not lost.
const (
	ViewLabel = "rig view: "
	EditLabel = "rig edit: "
)

// Target is a file to show: its working copy at a line, its change against
// HEAD, or its change in a single commit.
type Target struct {
	Root   string
	Path   string
	Line   int
	Diff   bool
	Commit string
}

// DoneMsg reports that the file was shown, or why it could not be.
type DoneMsg struct{ Err error }

// Open shows t in rig's viewer.
func Open(t Target) tea.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return DoneMsg{err} }
	}
	argv := ViewArgv(exe, t)
	if target, ok := rex.Current(); ok {
		return func() tea.Msg {
			_, err := rex.OpenPopup(context.Background(), target, rex.Popup{
				Label: ViewLabel + t.Path, Command: argv, Cwd: t.Root,
				Replaces: ViewLabel, Fresh: true,
			})
			return DoneMsg{err}
		}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = t.Root
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return DoneMsg{err} })
}

// ViewArgv is `rig view` for t, run from t.Root.
func ViewArgv(rig string, t Target) []string {
	argv := []string{rig, "view"}
	switch {
	case t.Commit != "":
		argv = append(argv, "--commit", t.Commit)
	case t.Diff:
		argv = append(argv, "--diff")
	case t.Line > 0:
		argv = append(argv, "--line", strconv.Itoa(t.Line))
	}
	return append(argv, "--", t.Path)
}

// Argv is the editor opening t's file, at its line where the editor takes
// one, run from t.Root.
func Argv(t Target) []string {
	editor := Editor()
	if t.Line > 0 && takesPlusLine(editor) {
		return []string{"sh", "-c", "exec " + editor + ` "+$2" "$1"`, "sh", t.Path, strconv.Itoa(t.Line)}
	}
	return []string{"sh", "-c", "exec " + editor + ` "$1"`, "sh", t.Path}
}

// takesPlusLine reports editors that open at a line given as +N.
func takesPlusLine(editor string) bool {
	name := filepath.Base(strings.Fields(editor + " x")[0])
	switch name {
	case "nvim", "vim", "vi", "nano", "micro", "emacs", "emacsclient", "kak", "mg", "joe":
		return true
	}
	return false
}

// Editor is $VISUAL or $EDITOR, else the best editor installed. It may
// include arguments, so it is run through sh.
func Editor() string {
	for _, v := range []string{"VISUAL", "EDITOR"} {
		if e := os.Getenv(v); e != "" {
			return e
		}
	}
	for _, e := range []string{"nvim", "vim"} {
		if _, err := exec.LookPath(e); err == nil {
			return e
		}
	}
	return "vi"
}

// Find opens rig's file search over the tab, in a popup in Rex.
func Find(root string) tea.Cmd {
	exe, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return DoneMsg{err} }
	}
	argv := []string{exe, "find", root}
	if target, ok := rex.Current(); ok {
		return func() tea.Msg {
			_, err := rex.OpenPopup(context.Background(), target, rex.Popup{Label: "rig find", Command: argv, Cwd: root})
			return DoneMsg{err}
		}
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = root
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return DoneMsg{err} })
}

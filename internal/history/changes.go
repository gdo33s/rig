package history

import (
	"fmt"
	"path"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/launch"
	"github.com/gdo33s/rig/internal/ui"
)

// view is what the pane shows: the files changed since the last commit,
// as a source control panel does, or the commits before them.
type view int

const (
	changesView view = iota
	historyView
)

var viewNames = [...]string{"Changes", "History"}

// fileRow is one changed file in the working tree or index.
type fileRow struct {
	path   string
	change *git.Change
}

// setChanges lists st's files by path, keeping the cursor on the file it
// was on.
func (m *Model) setChanges(st *git.Status) {
	var keep string
	if m.cscroll.Cursor < len(m.changes) {
		keep = m.changes[m.cscroll.Cursor].path
	}
	m.changes = m.changes[:0]
	if st != nil {
		for p, c := range st.Changes {
			m.changes = append(m.changes, fileRow{p, c})
		}
	}
	sort.Slice(m.changes, func(i, j int) bool { return m.changes[i].path < m.changes[j].path })
	for i, f := range m.changes {
		if f.path == keep {
			m.cscroll.Cursor = i
		}
	}
	m.cscroll.Clamp(len(m.changes), m.listHeight())
}

func (m Model) changesKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.cscroll.Move(msg.String(), len(m.changes), m.listHeight()) {
		return m, nil
	}
	switch msg.String() {
	case "enter", "l", "right", "d":
		return m.openChange()
	}
	return m, nil
}

// openChange shows the change to the file under the cursor since the
// last commit.
func (m Model) openChange() (Model, tea.Cmd) {
	if m.opening || m.cscroll.Cursor >= len(m.changes) {
		return m, nil
	}
	m.opening = true
	return m, launch.Open(launch.Target{Root: m.repo.Root, Path: m.changes[m.cscroll.Cursor].path, Diff: true})
}

func (m Model) changesMouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if m.cscroll.Wheel(msg, len(m.changes), m.listHeight()) {
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	i := m.cscroll.Offset + msg.Y - 1
	if i < 0 || i >= len(m.changes) {
		return m, nil
	}
	if i == m.cscroll.Cursor {
		return m.openChange()
	}
	m.cscroll.Cursor = i
	return m, nil
}

// changesLines draws the changed files: the name in its change's colour
// (bold when staged), its folder after it, and its line delta and kind.
func (m Model) changesLines() []string {
	if m.statusErr != nil {
		return []string{ui.Error.Render(m.statusErr.Error())}
	}
	if len(m.changes) == 0 {
		return []string{ui.Dim.Render("No changes")}
	}
	var lines []string
	start, end := m.cscroll.Visible(len(m.changes), m.listHeight())
	for i := start; i < end; i++ {
		f := m.changes[i]
		dir, name := path.Split(f.path)
		style := ui.KindStyle(f.change.Kind()).Bold(f.change.IsStaged())
		left := " " + style.Render(name)
		if dir != "" {
			left += " " + ui.Dim.Render(strings.TrimSuffix(dir, "/"))
		}
		var right string
		if m.width >= 24 {
			right = ui.ChangeStats(f.change)
		}
		line := ui.Line(left, right, m.width)
		if i == m.cscroll.Cursor {
			line = ui.Selected.Render(ui.Plain(line))
		}
		lines = append(lines, line)
	}
	return lines
}

// tabs names both views, the shown one bright, so it is clear what Tab
// switches to.
func (m Model) tabs() string {
	var out []string
	for i, name := range viewNames {
		if view(i) == m.view {
			out = append(out, ui.Heading.Render(name))
		} else {
			out = append(out, ui.Dim.Render(name))
		}
	}
	return strings.Join(out, " ")
}

func (m Model) changesSummary() string {
	switch n := len(m.changes); n {
	case 0:
		return ""
	case 1:
		return ui.Dim.Render("1 file")
	default:
		return ui.Dim.Render(fmt.Sprintf("%d files", n))
	}
}

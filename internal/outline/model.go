// Package outline is the Git-aware file tree pane.
package outline

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/launch"
	"github.com/gdo33s/rig/internal/ui"
)

const (
	minInterval = 1500 * time.Millisecond
	loadTimeout = 30 * time.Second
)

type Model struct {
	dir  string
	repo git.Repo
	gen  int // bumps when the directory changes, so stale loads are dropped

	files   []string
	stamp   string
	status  *git.Status
	root    *node
	err     error
	locked  bool
	ready   bool
	notice  error // a file that failed to open; cleared by the next key
	opening bool  // one open at a time, so a double-click opens one popup

	loading bool
	rows    []row
	toggled map[string]bool
	scroll  ui.Scroll

	searching bool
	query     string

	width, height int
}

func New(dir string) Model {
	return Model{dir: dir, toggled: map[string]bool{}}
}

// SetDir points the pane at another directory, as when the shell it
// follows changes folder. It is a no-op inside the same repository.
func (m *Model) SetDir(dir string) tea.Cmd {
	if dir == m.dir {
		return nil
	}
	if m.repo.Root != "" && within(dir, m.repo.Root) {
		m.dir = dir
		return nil
	}
	*m = Model{dir: dir, gen: m.gen + 1, toggled: map[string]bool{}, width: m.width, height: m.height}
	return m.Init()
}

func within(dir, root string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func (m Model) Init() tea.Cmd { return m.load() }

type loadedMsg struct {
	gen    int
	repo   git.Repo
	files  []string
	stamp  string
	status *git.Status
	err    error
	locked bool
	took   time.Duration
}

type tickMsg struct{ gen int }

// load reads the repository off the UI goroutine. The tracked file list is
// reused until the index changes.
func (m *Model) load() tea.Cmd {
	m.loading = true
	gen, dir, repo, files, stamp := m.gen, m.dir, m.repo, m.files, m.stamp
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		msg := loadedMsg{gen: gen}
		if repo.Root == "" {
			r, err := git.Open(ctx, dir)
			if err != nil {
				msg.err = err
				return msg
			}
			repo = r
		}
		msg.repo = repo
		if repo.Locked() {
			msg.locked = true
			msg.took = time.Since(start)
			return msg
		}
		msg.stamp = repo.IndexStamp()
		msg.files = files
		if msg.stamp == "" || msg.stamp != stamp {
			msg.files, msg.err = repo.Files(ctx)
		}
		if msg.err == nil {
			msg.status, msg.err = repo.Status(ctx)
		}
		msg.took = time.Since(start)
		return msg
	}
}

func (m *Model) schedule(took time.Duration) tea.Cmd {
	gen := m.gen
	return tea.Tick(max(minInterval, 4*took), func(time.Time) tea.Msg { return tickMsg{gen} })
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()

	case loadedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		m.locked = msg.locked
		m.repo = msg.repo
		if msg.err != nil && msg.err != git.ErrNotRepo && m.ready {
			// Keep showing the last good answer; say what went wrong.
			m.err = msg.err
			return m, m.schedule(msg.took)
		}
		m.err = msg.err
		if msg.err == git.ErrNotRepo {
			m.ready = true
			return m, nil
		}
		if msg.status != nil {
			m.files, m.stamp, m.status = msg.files, msg.stamp, msg.status
			m.root = build(m.files, m.status.Changes)
			m.ready = true
			m.refresh()
		}
		return m, m.schedule(msg.took)

	case tickMsg:
		if msg.gen == m.gen && !m.loading {
			return m, m.load()
		}

	case launch.DoneMsg:
		m.notice = msg.Err
		m.opening = false

	case tea.KeyMsg:
		m.notice = nil
		return m.key(msg)

	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return m, nil
}

// refresh rebuilds the visible rows, keeping the cursor on the same path.
func (m *Model) refresh() {
	var keep string
	if m.scroll.Cursor < len(m.rows) {
		keep = m.rows[m.scroll.Cursor].node.path
	}
	if m.root == nil {
		m.rows = nil
	} else if m.searching && m.query != "" {
		m.rows = search(m.root, m.query)
	} else {
		m.rows = flatten(m.root, m.toggled)
	}
	for i, r := range m.rows {
		if r.node.path == keep {
			m.scroll.Cursor = i
			break
		}
	}
	m.clamp()
}

func (m *Model) listHeight() int {
	h := m.height - 1 // header
	if m.searching {
		h--
	}
	return max(h, 1)
}

func (m *Model) clamp() { m.scroll.Clamp(len(m.rows), m.listHeight()) }

func (m Model) key(msg tea.KeyMsg) (Model, tea.Cmd) {
	if keys := ui.Keys(msg); len(keys) > 1 {
		var cmds []tea.Cmd
		for _, k := range keys {
			var cmd tea.Cmd
			m, cmd = m.key(k)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}
	if m.searching && msg.Alt {
		// Esc followed quickly by a key reads as Alt+key: leave search, then act.
		m.searching, m.query = false, ""
		m.refresh()
		msg.Alt = false
		return m.key(msg)
	}
	if m.searching {
		switch msg.Type {
		case tea.KeyEsc:
			m.searching, m.query = false, ""
			m.refresh()
			return m, nil
		case tea.KeyBackspace:
			if m.query == "" {
				m.searching = false
			} else {
				_, size := lastRune(m.query)
				m.query = m.query[:len(m.query)-size]
			}
			m.scroll.Cursor = 0
			m.refresh()
			return m, nil
		case tea.KeyRunes, tea.KeySpace:
			m.query += string(msg.Runes)
			if msg.Type == tea.KeySpace {
				m.query += " "
			}
			m.scroll.Cursor = 0
			m.refresh()
			return m, nil
		}
	}

	if m.scroll.Move(msg.String(), len(m.rows), m.listHeight()) {
		return m, nil
	}
	switch msg.String() {
	case "enter", "l", "right":
		return m.activate(false)
	case "h", "left":
		m.collapse()
	case " ":
		if r, ok := m.current(); ok && r.node.dir {
			m.toggled[r.node.path] = !r.open
			m.refresh()
		}
	case "d":
		return m.activate(true)
	case "/":
		m.searching, m.query, m.scroll.Cursor = true, "", 0
		m.refresh()
	case "esc":
		m.toggled = map[string]bool{}
		m.refresh()
	case "f":
		if m.repo.Root != "" {
			return m, launch.Find(m.repo.Root)
		}
	case "r":
		if !m.loading {
			return m, m.load()
		}
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	m.clamp()
	return m, nil
}

func lastRune(s string) (rune, int) {
	r := []rune(s)
	last := r[len(r)-1]
	return last, len(string(last))
}

func (m *Model) current() (row, bool) {
	if m.scroll.Cursor < 0 || m.scroll.Cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.scroll.Cursor], true
}

// activate opens a folder or shows a file.
func (m Model) activate(diff bool) (Model, tea.Cmd) {
	r, ok := m.current()
	if !ok {
		return m, nil
	}
	if r.node.dir {
		m.toggled[r.node.path] = !r.open
		m.refresh()
		return m, nil
	}
	t := launch.Target{Root: m.repo.Root, Path: r.node.path}
	if diff || (r.node.change != nil && r.node.change.Kind() == 'D') {
		if r.node.change == nil {
			return m, nil
		}
		t.Diff = true
	}
	if m.opening {
		return m, nil
	}
	m.opening = true
	return m, launch.Open(t)
}

// collapse closes the folder under the cursor, or moves to its parent.
func (m *Model) collapse() {
	r, ok := m.current()
	if !ok || m.searching {
		return
	}
	if r.node.dir && r.open {
		m.toggled[r.node.path] = false
		m.refresh()
		return
	}
	parent, _ := splitPath(r.node.path)
	for i := m.scroll.Cursor - 1; i >= 0; i-- {
		if m.rows[i].node.path == parent {
			m.scroll.Cursor = i
			return
		}
	}
}

func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if m.scroll.Wheel(msg, len(m.rows), m.listHeight()) {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		i := m.scroll.Offset + msg.Y - 1
		if i < 0 || i >= len(m.rows) {
			return m, nil
		}
		if i == m.scroll.Cursor {
			return m.activate(false)
		}
		m.scroll.Cursor = i
	}
	m.clamp()
	return m, nil
}

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	lines := make([]string, 0, m.height)
	lines = append(lines, m.header())

	switch {
	case !m.ready:
		lines = append(lines, ui.Dim.Render("Reading…"))
	case m.err == git.ErrNotRepo:
		lines = append(lines, ui.Dim.Render("Not a Git repository"))
	case len(m.rows) == 0 && m.searching:
		lines = append(lines, ui.Dim.Render("No matches"))
	}
	end := min(m.scroll.Offset+m.listHeight(), len(m.rows))
	for i := m.scroll.Offset; i < end && m.ready; i++ {
		lines = append(lines, m.renderRow(m.rows[i], i == m.scroll.Cursor))
	}
	for len(lines) < m.height-btoi(m.searching) {
		lines = append(lines, "")
	}
	if m.searching {
		lines = append(lines, ui.Line("/"+m.query+"█", "", m.width))
	}
	return strings.Join(lines[:min(len(lines), m.height)], "\n")
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (m Model) header() string {
	if m.notice != nil {
		return ui.Line(ui.Error.Render(m.notice.Error()), "", m.width)
	}
	left := ui.Heading.Render(filepath.Base(m.repo.Root))
	if m.repo.Root == "" {
		left = ui.Heading.Render(filepath.Base(m.dir))
	}
	var right string
	if st := m.status; st != nil {
		b := st.Branch
		branch := b.Head
		if branch == "" {
			branch = "detached"
		}
		left += " " + ui.Dim.Render(branch)
		if b.Ahead > 0 {
			left += ui.Dim.Render(fmt.Sprintf(" ↑%d", b.Ahead))
		}
		if b.Behind > 0 {
			left += ui.Dim.Render(fmt.Sprintf(" ↓%d", b.Behind))
		}
		if n := len(st.Changes); n > 0 {
			right = ui.Dim.Render(fmt.Sprintf("%d changed", n))
		}
	}
	switch {
	case m.locked:
		right = ui.Dim.Render("waiting for git")
	case m.err != nil && m.err != git.ErrNotRepo:
		right = ui.Error.Render("git error")
	}
	return ui.Line(left, right, m.width)
}

func (m Model) renderRow(r row, selected bool) string {
	n := r.node
	indent := strings.Repeat("  ", r.depth)
	var glyph, name, right string

	if n.dir {
		glyph = "▸ "
		if r.open {
			glyph = "▾ "
		}
		name = n.name + "/"
		if n.staged {
			name = ui.Bold.Render(name)
		}
		if n.changed > 0 && m.width >= 32 {
			right = ui.Count(n.added, n.deleted, n.pending)
		}
	} else {
		glyph = "  "
		name = n.name
		if m.searching {
			name = n.path
		}
		if c := n.change; c != nil {
			style := ui.KindStyle(c.Kind())
			if c.IsStaged() {
				style = style.Bold(true)
			}
			name = style.Render(name)
			if m.width >= 32 {
				right = ui.ChangeStats(c)
			}
		}
	}

	line := ui.Line(indent+ui.Dim.Render(glyph)+name, right, m.width)
	if selected {
		return ui.Selected.Render(ui.Plain(line))
	}
	return line
}

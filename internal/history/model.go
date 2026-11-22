// Package history is the pane for what changed: the files changed since
// the last commit, and, a Tab away, the commit history.
package history

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/launch"
	"github.com/gdo33s/rig/internal/ui"
)

const (
	limit       = 300
	minInterval = 2 * time.Second
	loadTimeout = 30 * time.Second
)

// row is a commit, or a line under it: one of its files, a line of its
// message, or a note such as "Reading files…".
type row struct {
	commit *git.Commit
	file   *git.FileChange
	info   string
	note   string
}

func (r row) isCommit() bool { return r.commit != nil && r.file == nil && r.info == "" && r.note == "" }

// changed holds a commit's files once read. Commits never change, so the
// answer is kept for as long as the pane points at this repository.
type changed struct {
	files []git.FileChange
	err   error
}

type message struct {
	git.Message
	err error
}

type Model struct {
	dir  string
	repo git.Repo
	gen  int

	view      view
	changes   []fileRow
	statusErr error
	cscroll   ui.Scroll

	commits  []git.Commit
	tracking git.Tracking
	unpushed map[string]bool
	err      error
	ready    bool
	loading  bool

	open     map[string]bool // commits showing their files
	files    map[string]*changed
	info     map[string]bool // commits showing their message
	messages map[string]*message

	rows   []row
	scroll ui.Scroll

	notice  error
	opening bool

	width, height int
}

func New(dir string) Model {
	return Model{dir: dir, open: map[string]bool{}, files: map[string]*changed{},
		info: map[string]bool{}, messages: map[string]*message{}}
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
	next := New(dir)
	next.gen, next.width, next.height, next.view = m.gen+1, m.width, m.height, m.view
	*m = next
	return m.Init()
}

func within(dir, root string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

func (m Model) Init() tea.Cmd { return m.load() }

type loadedMsg struct {
	gen       int
	repo      git.Repo
	commits   []git.Commit
	tracking  git.Tracking
	unpushed  map[string]bool
	status    *git.Status
	statusErr error
	err       error
	took      time.Duration
}

type filesMsg struct {
	gen  int
	hash string
	changed
}

type messageMsg struct {
	gen  int
	hash string
	message
}

type tickMsg struct{ gen int }

func (m *Model) load() tea.Cmd {
	m.loading = true
	gen, dir, repo := m.gen, m.dir, m.repo
	return func() tea.Msg {
		start := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		msg := loadedMsg{gen: gen}
		defer func() { msg.took = time.Since(start) }()
		if repo.Root == "" {
			r, err := git.Open(ctx, dir)
			if err != nil {
				msg.err = err
				return msg
			}
			repo = r
		}
		msg.repo = repo
		msg.status, msg.statusErr = repo.Status(ctx)
		if msg.commits, msg.err = repo.Log(ctx, limit); msg.err != nil {
			return msg
		}
		if msg.tracking, msg.err = repo.Tracking(ctx); msg.err != nil {
			return msg
		}
		msg.unpushed, msg.err = repo.Unpushed(ctx, msg.tracking.Base, limit)
		return msg
	}
}

func (m *Model) loadFiles(c git.Commit) tea.Cmd {
	gen, repo := m.gen, m.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		files, err := repo.Changed(ctx, c)
		return filesMsg{gen: gen, hash: c.Hash, changed: changed{files, err}}
	}
}

func (m *Model) loadMessage(hash string) tea.Cmd {
	gen, repo := m.gen, m.repo
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
		defer cancel()
		msg, err := repo.Message(ctx, hash)
		return messageMsg{gen: gen, hash: hash, message: message{msg, err}}
	}
}

func (m *Model) schedule(took time.Duration) tea.Cmd {
	gen := m.gen
	return tea.Tick(max(minInterval, 4*took), func(time.Time) tea.Msg { return tickMsg{gen} })
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		rewrap := msg.Width != m.width
		m.width, m.height = msg.Width, msg.Height
		if rewrap {
			m.refresh()
		}
		m.clamp()

	case loadedMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		m.loading = false
		m.repo = msg.repo
		if msg.err == git.ErrNotRepo {
			m.err, m.ready = msg.err, true
			return m, nil
		}
		m.err, m.statusErr = msg.err, msg.statusErr
		m.setChanges(msg.status)
		if msg.err == nil {
			m.commits, m.tracking, m.unpushed = msg.commits, msg.tracking, msg.unpushed
			m.ready = true
			m.refresh()
		}
		return m, m.schedule(msg.took)

	case filesMsg:
		if msg.gen == m.gen {
			c := msg.changed
			m.files[msg.hash] = &c
			m.refresh()
		}

	case messageMsg:
		if msg.gen == m.gen {
			got := msg.message
			m.messages[msg.hash] = &got
			m.refresh()
		}

	case tickMsg:
		if msg.gen == m.gen && !m.loading {
			return m, m.load()
		}

	case launch.DoneMsg:
		m.notice, m.opening = msg.Err, false

	case tea.KeyMsg:
		m.notice = nil
		var cmds []tea.Cmd
		for _, k := range ui.Keys(msg) {
			var cmd tea.Cmd
			m, cmd = m.key(k)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case tea.MouseMsg:
		return m.mouse(msg)
	}
	return m, nil
}

// refresh rebuilds the rows, keeping the cursor on the same row.
func (m *Model) refresh() {
	keep, _ := m.current()
	m.rows = nil
	for i := range m.commits {
		c := &m.commits[i]
		m.rows = append(m.rows, row{commit: c})
		if m.info[c.Hash] {
			m.rows = append(m.rows, m.infoRows(c)...)
		}
		if !m.open[c.Hash] {
			continue
		}
		got := m.files[c.Hash]
		switch {
		case got == nil:
			m.rows = append(m.rows, row{commit: c, note: "Reading files…"})
		case got.err != nil:
			m.rows = append(m.rows, row{commit: c, note: got.err.Error()})
		case len(got.files) == 0:
			m.rows = append(m.rows, row{commit: c, note: "No files changed"})
		}
		if got != nil {
			for j := range got.files {
				m.rows = append(m.rows, row{commit: c, file: &got.files[j]})
			}
		}
	}
	if len(m.commits) == limit {
		m.rows = append(m.rows, row{note: "Older commits not shown"})
	}
	if keep.commit != nil {
		for i, r := range m.rows {
			if r.commit != nil && r.commit.Hash == keep.commit.Hash &&
				(r.file == nil) == (keep.file == nil) &&
				(r.file == nil || r.file.Path == keep.file.Path) {
				m.scroll.Cursor = i
				break
			}
		}
	}
	m.clamp()
}

// infoRows lays out a commit's author, date and message body, wrapped to
// the pane.
func (m *Model) infoRows(c *git.Commit) []row {
	const indent = "    "
	got := m.messages[c.Hash]
	switch {
	case got == nil:
		return []row{{commit: c, note: "Reading message…"}}
	case got.err != nil:
		return []row{{commit: c, note: got.err.Error()}}
	}
	byline := got.Author + " · " + got.When.Local().Format("Mon 2 Jan 2006 15:04")
	rows := []row{{commit: c, info: indent + ui.Dim.Render(byline)}, {commit: c, info: " "}}
	// The full subject too: the commit's own row truncates it.
	text := c.Subject
	if got.Body != "" {
		text += "\n\n" + got.Body
	}
	width := max(m.width-len(indent), 10)
	for _, line := range strings.Split(ansi.Wrap(text, width, ""), "\n") {
		if line == "" {
			line = " "
		}
		rows = append(rows, row{commit: c, info: indent + line})
	}
	return rows
}

func (m *Model) listHeight() int { return max(m.height-1, 1) }

func (m *Model) clamp() { m.scroll.Clamp(len(m.rows), m.listHeight()) }

func (m *Model) current() (row, bool) {
	if m.scroll.Cursor < 0 || m.scroll.Cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.scroll.Cursor], true
}

func (m Model) key(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "shift+tab":
		m.view = 1 - m.view
		return m, nil
	case "f":
		if m.repo.Root != "" {
			return m, launch.Find(m.repo.Root)
		}
		return m, nil
	case "r":
		if !m.loading {
			return m, m.load()
		}
		return m, nil
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	if m.view == changesView {
		return m.changesKey(msg)
	}
	if m.scroll.Move(msg.String(), len(m.rows), m.listHeight()) {
		return m, nil
	}
	switch msg.String() {
	case "enter", "l", "right", "d":
		return m.activate()
	case " ":
		if r, ok := m.current(); ok && r.isCommit() {
			return m.toggle(r.commit)
		}
	case "i":
		if r, ok := m.current(); ok && r.commit != nil {
			return m.toggleInfo(r.commit)
		}
	case "h", "left":
		m.collapse()
	case "esc":
		m.open, m.info = map[string]bool{}, map[string]bool{}
		m.refresh()
	}
	return m, nil
}

// activate expands a commit, or shows a file's change in that commit.
func (m Model) activate() (Model, tea.Cmd) {
	r, ok := m.current()
	if !ok || r.commit == nil || r.note != "" || r.info != "" {
		return m, nil
	}
	if r.file == nil {
		return m.toggle(r.commit)
	}
	if m.opening {
		return m, nil
	}
	m.opening = true
	return m, launch.Open(launch.Target{Root: m.repo.Root, Path: r.file.Path, Commit: r.commit.Hash})
}

func (m Model) toggle(c *git.Commit) (Model, tea.Cmd) {
	m.open[c.Hash] = !m.open[c.Hash]
	var cmd tea.Cmd
	if m.open[c.Hash] && m.files[c.Hash] == nil {
		cmd = m.loadFiles(*c)
	}
	m.refresh()
	return m, cmd
}

func (m Model) toggleInfo(c *git.Commit) (Model, tea.Cmd) {
	m.info[c.Hash] = !m.info[c.Hash]
	var cmd tea.Cmd
	if m.info[c.Hash] && m.messages[c.Hash] == nil {
		cmd = m.loadMessage(c.Hash)
	}
	m.moveTo(c)
	m.refresh()
	return m, cmd
}

// moveTo puts the cursor on c's own row.
func (m *Model) moveTo(c *git.Commit) {
	for i, r := range m.rows {
		if r.commit == c && r.isCommit() {
			m.scroll.Cursor = i
			return
		}
	}
}

// collapse closes the commit under the cursor, or the one a line under it
// belongs to: its files and its message.
func (m *Model) collapse() {
	r, ok := m.current()
	if !ok || r.commit == nil || !(m.open[r.commit.Hash] || m.info[r.commit.Hash]) {
		return
	}
	m.open[r.commit.Hash], m.info[r.commit.Hash] = false, false
	for i, other := range m.rows {
		if other.commit == r.commit && other.isCommit() {
			m.scroll.Cursor = i
			break
		}
	}
	m.refresh()
}

func (m Model) mouse(msg tea.MouseMsg) (Model, tea.Cmd) {
	if msg.Y == 0 && msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		m.view = 1 - m.view
		return m, nil
	}
	if m.view == changesView {
		return m.changesMouse(msg)
	}
	if m.scroll.Wheel(msg, len(m.rows), m.listHeight()) {
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	i := m.scroll.Offset + msg.Y - 1
	if i < 0 || i >= len(m.rows) {
		return m, nil
	}
	if i == m.scroll.Cursor {
		return m.activate()
	}
	m.scroll.Cursor = i
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
	case m.view == changesView:
		lines = append(lines, m.changesLines()...)
	case len(m.commits) == 0 && m.err == nil:
		lines = append(lines, ui.Dim.Render("No commits yet"))
	}
	if m.ready && m.view == historyView && m.err != git.ErrNotRepo {
		now := time.Now()
		marks := len(m.unpushed) > 0
		start, end := m.scroll.Visible(len(m.rows), m.listHeight())
		for i := start; i < end; i++ {
			lines = append(lines, m.renderRow(m.rows[i], i == m.scroll.Cursor, marks, now))
		}
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.height], "\n")
}

func (m Model) header() string {
	if m.notice != nil {
		return ui.Line(ui.Error.Render(m.notice.Error()), "", m.width)
	}
	left := m.tabs()
	t := m.tracking
	var right string
	switch {
	case m.err != nil && m.err != git.ErrNotRepo:
		right = ui.Error.Render("git error")
	case !m.ready || m.err != nil:
	case m.view == changesView:
		right = m.changesSummary()
	default:
		switch {
		case !t.Remote:
			right = ui.Dim.Render("no remote")
		case t.Base == "":
			right = ui.Dim.Render("no upstream")
		default:
			if t.Ahead > 0 {
				right += ui.Added.Render(fmt.Sprintf("↑%d", t.Ahead))
			}
			if t.Behind > 0 {
				right += ui.Dim.Render(fmt.Sprintf(" ↓%d", t.Behind))
			}
			if !t.Upstream {
				right += ui.Dim.Render(" vs " + t.Base)
			}
		}
	}
	return ui.Line(left, strings.TrimSpace(right), m.width)
}

func (m Model) renderRow(r row, selected, marks bool, now time.Time) string {
	var line string
	switch {
	case r.note != "" && r.commit == nil:
		line = ui.Dim.Render(r.note)
	case r.note != "":
		line = "    " + ui.Dim.Render(r.note)
	case r.info != "":
		line = ui.Line(r.info, "", m.width)
	case r.file != nil:
		line = m.renderFile(r.file)
	default:
		line = m.renderCommit(r.commit, marks, now)
	}
	if selected {
		return ui.Selected.Render(ui.Plain(line))
	}
	return line
}

func (m Model) renderCommit(c *git.Commit, marks bool, now time.Time) string {
	glyph := "▸ "
	if m.open[c.Hash] {
		glyph = "▾ "
	}
	left := ui.Dim.Render(glyph)
	if marks {
		if m.unpushed[c.Hash] {
			left += ui.Added.Render("↑ ")
		} else {
			left += "  "
		}
	}
	hash := ui.Hash
	if m.unpushed[c.Hash] {
		hash = hash.Bold(true)
	}
	left += hash.Render(c.Short) + " " + c.Subject
	return ui.Line(left, ui.Dim.Render(ui.Age(c.When, now)), m.width)
}

func (m Model) renderFile(f *git.FileChange) string {
	left := "    " + ui.KindStyle(f.Kind).Render(string(f.Kind)+" "+f.Path)
	var right string
	if m.width >= 32 {
		if f.Binary {
			right = ui.Dim.Render("bin")
		} else {
			right = ui.Count(f.Added, f.Deleted, false)
		}
	}
	return ui.Line(left, right, m.width)
}

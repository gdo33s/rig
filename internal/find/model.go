// Package find is rig's file search: fuzzy file names and text in files,
// ranked by fff, opening the chosen result in the editor popup.
package find

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/fff"
	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/launch"
	"github.com/gdo33s/rig/internal/ui"
)

// mode is what a query searches: names and text together, or one of them.
type mode int

const (
	all mode = iota
	files
	text
)

var modeNames = []string{"All", "Files", "Text"}

const (
	// limit is how many results are asked for: more than a popup shows, so
	// scrolling down finds more.
	limit = 200
	// allFiles is how many file names lead the All results, so the matches
	// in text still show without scrolling.
	allFiles = 6
	// minText is the shortest query that All also searches text for: a
	// letter or two matches nearly every line.
	minText = 3
)

// row is one line of results: a section title, a file, or a matching line
// (one with a line number).
type row struct {
	title string
	path  string
	line  int
	text  string
}

func (r row) selectable() bool { return r.title == "" }

type Model struct {
	root    string
	client  *fff.Client
	started bool  // fff has started, or failed to
	err     error // fff could not start, or a search failed

	mode    mode
	query   string
	seq     int // the latest search; older answers are dropped
	files   []string
	matches []fff.Match
	rows    []row
	ready   bool // the first answer for this query has come

	changes map[string]*git.Change
	scroll  ui.Scroll

	opening bool
	notice  error

	width, height int
}

func New(root string) Model { return Model{root: root} }

// Run shows the search in this terminal until a result is opened or it is
// closed, then stops fff.
func Run(dir string) error {
	// Search the whole repository, even from a folder inside it.
	root := dir
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if repo, err := git.Open(ctx, dir); err == nil {
		root = repo.Root
	}
	cancel()
	final, err := tea.NewProgram(New(root), tea.WithAltScreen()).Run()
	if m, ok := final.(Model); ok && m.client != nil {
		m.client.Close()
	}
	return err
}

type startedMsg struct {
	client *fff.Client
	err    error
}

type changesMsg map[string]*git.Change

type resultsMsg struct {
	seq     int
	files   []string
	matches []fff.Match
	err     error
}

func (m Model) Init() tea.Cmd {
	root := m.root
	start := func() tea.Msg {
		c, err := fff.Start(root)
		return startedMsg{c, err}
	}
	status := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		repo, err := git.Open(ctx, root)
		if err != nil {
			return changesMsg(nil)
		}
		st, err := repo.Status(ctx)
		if err != nil {
			return changesMsg(nil)
		}
		return changesMsg(st.Changes)
	}
	return tea.Batch(start, status)
}

// search asks fff for the current query. Each keystroke starts one; only
// the answer to the latest is shown.
func (m *Model) search() tea.Cmd {
	m.seq++
	seq, c, q, md := m.seq, m.client, strings.TrimSpace(m.query), m.mode
	if c == nil {
		return nil
	}
	wantFiles := md != text
	wantText := md == text && q != "" || md == all && len([]rune(q)) >= minText
	if !wantFiles && !wantText {
		m.files, m.matches, m.ready = nil, nil, true
		m.layout()
		return nil
	}
	fileLimit := limit
	if md == all && wantText {
		fileLimit = allFiles
	}
	return func() tea.Msg {
		var res resultsMsg
		res.seq = seq
		if wantFiles {
			res.files, res.err = c.Files(q, fileLimit)
		}
		if wantText && res.err == nil {
			res.matches, res.err = c.Grep(q, limit)
		}
		return res
	}
}

// layout turns the results into rows: in All, files and matching lines,
// each under a title. Files lead only when one plainly contains the
// query; otherwise the text, being the better answer, comes first.
func (m *Model) layout() {
	m.rows = nil
	titled := m.mode == all && len(m.files) > 0 && len(m.matches) > 0
	var fileRows, textRows []row
	for _, f := range m.files {
		fileRows = append(fileRows, row{path: f})
	}
	for _, hit := range m.matches {
		textRows = append(textRows, row{path: hit.Path, line: hit.Line, text: hit.Text})
	}
	if titled {
		fileRows = append([]row{{title: "Files"}}, fileRows...)
		textRows = append([]row{{title: "Text"}}, textRows...)
	}
	if titled && !m.filesLead() {
		m.rows = append(textRows, fileRows...)
	} else {
		m.rows = append(fileRows, textRows...)
	}
	m.scroll = ui.Scroll{}
	m.step(1)
}

func (m *Model) filesLead() bool {
	for _, f := range m.files {
		if namesMatch(f, m.query) {
			return true
		}
	}
	return false
}

// step moves the cursor by delta rows, past any title, and keeps it in
// view.
func (m *Model) step(delta int) {
	n := len(m.rows)
	if n == 0 {
		return
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	at := m.scroll.Cursor
	if at < n && !m.rows[at].selectable() && delta > 0 {
		delta-- // from a title, the first move lands on the row below it
	}
	at = min(max(at+delta, 0), n-1)
	for at >= 0 && at < n && !m.rows[at].selectable() {
		at += dir
	}
	if at < 0 || at >= n {
		return // nothing to select that way
	}
	m.scroll.Cursor = at
	m.clamp()
	// Keep a title in view above the first row under it.
	if at > 0 && !m.rows[at-1].selectable() && m.scroll.Offset == at {
		m.scroll.Offset--
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()

	case startedMsg:
		m.client, m.err, m.started = msg.client, msg.err, true
		return m, m.search()

	case changesMsg:
		m.changes = msg

	case resultsMsg:
		if msg.seq != m.seq {
			return m, nil
		}
		m.files, m.matches, m.err, m.ready = msg.files, rankMatches(msg.matches, m.query), msg.err, true
		m.layout()

	case launch.DoneMsg:
		m.opening = false
		if msg.Err != nil {
			m.notice = msg.Err
			return m, nil
		}
		return m, tea.Quit

	case tea.KeyMsg:
		m.notice = nil
		return m.key(msg)
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.open()
	case "tab":
		m.mode = (m.mode + 1) % 3
		m.ready = false
		return m, m.search()
	case "shift+tab":
		m.mode = (m.mode + 2) % 3
		m.ready = false
		return m, m.search()
	case "up", "ctrl+p", "ctrl+k":
		m.step(-1)
		return m, nil
	case "down", "ctrl+n", "ctrl+j":
		m.step(1)
		return m, nil
	case "pgup":
		m.step(-m.listHeight() / 2)
		return m, nil
	case "pgdown":
		m.step(m.listHeight() / 2)
		return m, nil
	case "backspace":
		if m.query == "" {
			return m, nil
		}
		r := []rune(m.query)
		m.query = string(r[:len(r)-1])
	case "ctrl+w", "alt+backspace":
		m.query = strings.TrimRightFunc(m.query, unicode.IsSpace)
		m.query = strings.TrimRightFunc(m.query, func(r rune) bool { return !unicode.IsSpace(r) })
	case "ctrl+u":
		m.query = ""
	default:
		if msg.Type != tea.KeyRunes && msg.Type != tea.KeySpace {
			return m, nil
		}
		if msg.Type == tea.KeySpace {
			m.query += " "
		} else {
			m.query += string(msg.Runes)
		}
	}
	return m, m.search()
}

// open shows the chosen result in the editor popup, at its line for a
// match, then closes the search once it is up.
func (m Model) open() (tea.Model, tea.Cmd) {
	if m.opening || m.scroll.Cursor >= len(m.rows) || !m.rows[m.scroll.Cursor].selectable() {
		return m, nil
	}
	r := m.rows[m.scroll.Cursor]
	m.opening = true
	return m, launch.Open(launch.Target{Root: m.root, Path: r.path, Line: r.line})
}

// The layout: the query row, a rule, the results, then a footer.
func (m Model) listHeight() int { return max(m.height-3, 1) }

func (m *Model) clamp() { m.scroll.Clamp(len(m.rows), m.listHeight()) }

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	lines := []string{m.queryLine(), ui.Dim.Render(strings.Repeat("─", m.width))}
	body := m.results()
	for len(body) < m.listHeight() {
		body = append(body, "")
	}
	lines = append(lines, body[:m.listHeight()]...)
	lines = append(lines, m.footer())
	return strings.Join(lines[:min(len(lines), m.height)], "\n")
}

func (m Model) queryLine() string {
	var tabs []string
	for i, name := range modeNames {
		if mode(i) == m.mode {
			tabs = append(tabs, ui.Bold.Render(name))
		} else {
			tabs = append(tabs, ui.Dim.Render(name))
		}
	}
	right := strings.Join(tabs, ui.Dim.Render(" │ ")) + " "
	prompt := ui.Accent.Render(" 󰍉 ")
	if m.query == "" {
		hint := [...]string{"Search file names and text", "Search file names", "Search text in files"}[m.mode]
		return ui.Line(prompt+ui.Selected.Render(" ")+ui.Dim.Render(hint), right, m.width)
	}
	return ui.Line(prompt+m.query+ui.Selected.Render(" "), right, m.width)
}

func (m Model) results() []string {
	q := strings.TrimSpace(m.query)
	switch {
	case m.err != nil && m.client == nil:
		return []string{" " + ui.Error.Render(m.err.Error()), ui.Dim.Render(" Run rig install to download it")}
	case m.err != nil:
		return []string{" " + ui.Error.Render(m.err.Error())}
	case !m.started:
		return []string{ui.Dim.Render(" Indexing…")}
	case !m.ready:
		return nil
	case len(m.rows) == 0 && m.mode == text && q == "":
		return nil
	case len(m.rows) == 0:
		return []string{ui.Dim.Render(" No matches")}
	}
	start, end := m.scroll.Visible(len(m.rows), m.listHeight())
	var lines []string
	for i := start; i < end; i++ {
		r := m.rows[i]
		var line string
		switch {
		case r.title != "":
			line = m.titleLine(r.title)
		case r.line > 0:
			line = m.matchLine(r)
		default:
			line = m.fileLine(r.path)
		}
		if i == m.scroll.Cursor && r.selectable() {
			line = ui.Selected.Render(ui.Plain(line) + strings.Repeat(" ", max(m.width-ansi.StringWidth(ui.Plain(line)), 0)))
		}
		lines = append(lines, line)
	}
	return lines
}

// titleLine heads a section of All, with how many it holds.
func (m Model) titleLine(title string) string {
	n := len(m.files)
	count := plural(n, "file", "files")
	if title == "Text" {
		count = plural(len(m.matches), "match", "matches")
	}
	return ui.Line(" "+ui.Heading.Render(title), ui.Dim.Render(count)+" ", m.width)
}

// fileLine is the file name, its folder after it, and its git state at
// the end, as Outline shows it.
func (m Model) fileLine(p string) string {
	dir, name := path.Split(p)
	style := ui.Bold
	var right string
	if c := m.changes[p]; c != nil {
		style = ui.KindStyle(c.Kind()).Bold(c.IsStaged())
		right = ui.KindStyle(c.Kind()).Render(string(c.Kind()))
	}
	left := " " + highlight(name, m.query, style)
	if dir != "" {
		left += "  " + ui.Dim.Render(strings.TrimSuffix(dir, "/"))
	}
	return ui.Line(left, right+" ", m.width)
}

// matchLine is the matching line with the query marked, and where it is.
func (m Model) matchLine(r row) string {
	where := ui.Dim.Render(fmt.Sprintf("%s:%d", r.path, r.line))
	body := " " + mark(strings.TrimSpace(r.text), strings.TrimSpace(m.query))
	return ui.Line(body, where+" ", m.width)
}

func (m Model) footer() string {
	var left string
	switch {
	case m.notice != nil:
		left = ui.Error.Render(m.notice.Error())
	case m.ready && m.mode == text && strings.TrimSpace(m.query) != "":
		left = ui.Dim.Render(plural(len(m.matches), "match", "matches"))
	case m.ready && m.mode == files:
		left = ui.Dim.Render(plural(len(m.files), "file", "files"))
	case m.ready && m.mode == all && len(m.matches) > 0:
		left = ui.Dim.Render(plural(len(m.files), "file", "files") + " · " + plural(len(m.matches), "match", "matches"))
	case m.ready && m.mode == all:
		left = ui.Dim.Render(plural(len(m.files), "file", "files"))
	}
	keys := ui.Dim.Render("↵ open  ⇥ all/files/text  esc close")
	return ui.Line(" "+left, keys+" ", m.width)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	if n >= limit {
		return fmt.Sprintf("%d+ %s", n, many)
	}
	return fmt.Sprintf("%d %s", n, many)
}

// highlight draws name in style, underlining in order the letters of query
// that a fuzzy match would use, so it is clear why a file matched.
func highlight(name, query string, style lipgloss.Style) string {
	q := []rune(strings.ToLower(strings.ReplaceAll(query, " ", "")))
	hit := style.Foreground(lipgloss.Color("6")).Underline(true)
	var b strings.Builder
	for _, r := range name {
		if len(q) > 0 && unicode.ToLower(r) == q[0] {
			b.WriteString(hit.Render(string(r)))
			q = q[1:]
			continue
		}
		b.WriteString(style.Render(string(r)))
	}
	return b.String()
}

// mark colours each place query appears in line, ignoring case.
func mark(line, query string) string {
	if query == "" {
		return line
	}
	lower, q := strings.ToLower(line), strings.ToLower(query)
	var b strings.Builder
	for {
		i := strings.Index(lower, q)
		if i < 0 {
			b.WriteString(line)
			return b.String()
		}
		b.WriteString(line[:i])
		b.WriteString(ui.Accent.Bold(true).Render(line[i : i+len(q)]))
		line, lower = line[i+len(q):], lower[i+len(q):]
	}
}

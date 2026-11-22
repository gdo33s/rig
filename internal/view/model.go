// Package view is rig's read-only file viewer: a file with its syntax
// coloured, its changes marked and a line to start at, or a diff drawn the
// same way. e hands the file to the editor when it needs changing.
package view

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/launch"
	"github.com/gdo33s/rig/internal/rex"
	"github.com/gdo33s/rig/internal/syntax"
	"github.com/gdo33s/rig/internal/ui"
)

// maxBytes is the largest file shown; past it, the editor is better.
const maxBytes = 4 << 20

// Options say what to show.
type Options struct {
	Root   string // the repository, or the folder Path is relative to
	Path   string // relative to Root
	Line   int    // the line to start at, for a file
	Diff   bool   // the change against HEAD rather than the file
	Commit string // the change in one commit
}

func (o Options) diff() bool { return o.Diff || o.Commit != "" }

// line is one row of the document: a line of the file, or of a diff.
type line struct {
	num    int    // the file line it shows, 0 for none
	old    int    // its line before, for a removed line in a diff
	mark   byte   // a change: git.LineAdded and friends, or '+', '-', '@'
	text   string // coloured
	plain  string // as typed, for search
	starts bool   // the first row of a change

	// A side-by-side row has a cell per side; either may be missing.
	split       bool
	left, right *cell
}

// cell is one side of a side-by-side row.
type cell struct {
	num   int
	kind  byte // ' ', '-' or '+'
	spans []syntax.Span
	words []bool // the runes that changed, when worth marking
}

// errNoChanges means a diff was asked for a file with none.
var errNoChanges = errors.New("no changes since the last commit")

type Model struct {
	unified []line // a diff in one column
	split   []line // and side by side, shown when the pane is wide

	opt      Options
	showDiff bool // the change rather than the file; d switches
	pending  int  // a file line to land on once the other view loads
	doc      []line
	lang     string
	added    int
	gone     int
	fresh    bool
	err      error
	ready    bool

	scroll ui.Scroll
	hx     int // columns scrolled right

	searching bool
	query     string
	found     string // the last search, for n and N

	notice string

	width, height int
}

func New(opt Options) Model { return Model{opt: opt, showDiff: opt.diff()} }

// current is what is on show: the file, or its change (in the commit
// given, else since the last commit).
func (m Model) current() Options {
	o := m.opt
	if !m.showDiff {
		o.Diff, o.Commit = false, ""
	} else if o.Commit == "" {
		o.Diff = true
	}
	return o
}

// Run shows the viewer in this terminal until it is closed.
func Run(opt Options) error {
	_, err := tea.NewProgram(New(opt), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

type loadedMsg struct {
	doc         []line
	split       []line // the same diff side by side
	lang        string
	added, gone int
	fresh       bool // a file not in the last commit
	err         error
}

func (m Model) Init() tea.Cmd { return load(m.current()) }

func load(opt Options) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if opt.diff() {
			return loadDiff(ctx, opt)
		}
		return loadFile(ctx, opt)
	}
}

func loadFile(ctx context.Context, opt Options) loadedMsg {
	full := filepath.Join(opt.Root, opt.Path)
	info, err := os.Stat(full)
	if err != nil {
		return loadedMsg{err: err}
	}
	if info.Size() > maxBytes {
		return loadedMsg{err: fmt.Errorf("%s is too large to show here; press e to edit it", humanSize(info.Size()))}
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return loadedMsg{err: err}
	}
	if strings.IndexByte(string(data[:min(len(data), 8000)]), 0) >= 0 {
		return loadedMsg{err: errors.New("binary file")}
	}
	src := string(data)
	lexer := syntax.Lexer(opt.Path, src)
	coloured := syntax.Lines(lexer, src)
	raw := strings.Split(strings.TrimSuffix(strings.ReplaceAll(src, "\r\n", "\n"), "\n"), "\n")

	var marks map[int]byte
	if repo, err := git.Open(ctx, opt.Root); err == nil {
		marks, _ = repo.LineChanges(ctx, opt.Path)
	}
	msg := loadedMsg{lang: syntax.Language(opt.Path, src)}
	// A file that is new all through is not marked line by line.
	if len(marks) == len(coloured) && marks[1] == git.LineAdded && marks[len(coloured)] == git.LineAdded {
		marks, msg.fresh = nil, true
	}
	for i := range coloured {
		l := line{num: i + 1, mark: marks[i+1], text: coloured[i], plain: syntax.ExpandTabs(raw[i])}
		l.starts = l.mark != 0 && (i == 0 || marks[i] == 0 || marks[i] == git.LineDeleted)
		switch l.mark {
		case git.LineAdded, git.LineModified:
			msg.added++
		}
		msg.doc = append(msg.doc, l)
	}
	return msg
}

func loadDiff(ctx context.Context, opt Options) loadedMsg {
	repo, err := git.Open(ctx, opt.Root)
	if err != nil {
		return loadedMsg{err: err}
	}
	d, err := repo.FileDiff(ctx, opt.Path, opt.Commit)
	if err != nil {
		return loadedMsg{err: err}
	}
	if d.Binary {
		return loadedMsg{err: errors.New("binary file")}
	}
	if len(d.Lines) == 0 {
		return loadedMsg{err: errNoChanges}
	}
	var sample strings.Builder
	for _, dl := range d.Lines {
		if dl.Kind != '-' && dl.Kind != '@' {
			sample.WriteString(dl.Text + "\n")
		}
	}
	lexer := syntax.Lexer(opt.Path, sample.String())
	// Highlight both versions in full, so strings and comments that span
	// lines are right on both sides; a line the full text cannot vouch for
	// is highlighted on its own.
	oldSrc, newSrc := repo.Sides(ctx, opt.Path, opt.Commit)
	oldSpans, newSpans := syntax.Spans(lexer, oldSrc), syntax.Spans(lexer, newSrc)
	spansAt := func(all [][]syntax.Span, n int, text string) []syntax.Span {
		if n >= 1 && n <= len(all) && plainOf(all[n-1]) == text {
			return all[n-1]
		}
		return syntax.Spans(lexer, text)[0]
	}

	msg := loadedMsg{lang: syntax.Language(opt.Path, sample.String())}
	prev := byte('@')
	for _, dl := range d.Lines {
		l := line{mark: dl.Kind, plain: syntax.ExpandTabs(dl.Text)}
		switch dl.Kind {
		case '@':
			l.text = heading(dl)
			l.num = dl.New
		case '-':
			l.old = dl.Old
			l.text = removed.Render(l.plain)
			msg.gone++
		case '+':
			l.num = dl.New
			l.text = syntax.Render(spansAt(newSpans, dl.New, l.plain))
			msg.added++
		default:
			l.num = dl.New
			l.text = syntax.Render(spansAt(newSpans, dl.New, l.plain))
		}
		l.starts = (dl.Kind == '+' || dl.Kind == '-') && (prev == ' ' || prev == '@')
		prev = dl.Kind
		msg.doc = append(msg.doc, l)
	}

	changed := false
	for _, p := range pairs(d) {
		if p.heading != nil {
			msg.split = append(msg.split, line{mark: '@', num: p.heading.New, text: heading(*p.heading), split: true})
			changed = false
			continue
		}
		l := line{split: true}
		var texts []string
		if p.left != nil {
			text := syntax.ExpandTabs(p.left.Text)
			l.left = &cell{num: p.left.Old, kind: p.left.Kind, spans: spansAt(oldSpans, p.left.Old, text), words: p.leftWords}
			l.old = p.left.Old
			texts = append(texts, text)
		}
		if p.right != nil {
			text := syntax.ExpandTabs(p.right.Text)
			l.right = &cell{num: p.right.New, kind: p.right.Kind, spans: spansAt(newSpans, p.right.New, text), words: p.rightWords}
			l.num = p.right.New
			texts = append(texts, text)
		}
		l.plain = strings.Join(texts, "  ")
		isChange := p.left != p.right
		l.starts = isChange && !changed
		changed = isChange
		msg.split = append(msg.split, l)
	}
	return msg
}

func heading(dl git.DiffLine) string {
	at := fmt.Sprintf("line %d", dl.New)
	if dl.Text != "" {
		at += "  " + dl.Text
	}
	return ui.Dim.Render("⋯ " + at)
}

func plainOf(spans []syntax.Span) string {
	var b strings.Builder
	for _, sp := range spans {
		b.WriteString(sp.Text)
	}
	return b.String()
}

var (
	removed  = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Faint(true)
	addMark  = ui.Added.Render("▎")
	modMark  = ui.KindStyle('M').Render("▎")
	delMark  = ui.Deleted.Render("▁")
	plusMark = ui.Added.Render("+")
	subMark  = ui.Deleted.Render("-")
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		first := m.width == 0
		m.width, m.height = msg.Width, msg.Height
		switch {
		case first && m.ready:
			m.relayout()
			m.goToLine(m.opt.Line)
		case m.ready:
			m.relayout()
		}
		m.clamp()

	case loadedMsg:
		if msg.err == errNoChanges && m.pending != 0 {
			// Switched to a diff of a file with none: stay on the file.
			m.showDiff, m.pending, m.notice = false, 0, "No changes since the last commit"
			return m, nil
		}
		cursor := m.scroll.Cursor
		reloaded := m.ready
		m.unified, m.split = msg.doc, msg.split
		m.lang, m.added, m.gone, m.fresh, m.err, m.ready = msg.lang, msg.added, msg.gone, msg.fresh, msg.err, true
		m.doc = m.unified
		if m.wantSplit() {
			m.doc = m.split
		}
		switch {
		case m.pending != 0:
			m.goToLine(m.pending)
			m.pending = 0
		case reloaded:
			m.scroll.Cursor = cursor
			m.clamp()
		default:
			m.goToLine(m.opt.Line)
		}

	case editedMsg:
		return m, load(m.current())

	case tea.KeyMsg:
		m.notice = ""
		if m.searching {
			return m.searchKey(msg)
		}
		return m.key(msg)

	case tea.MouseMsg:
		if m.scroll.Wheel(msg, len(m.doc), m.bodyHeight()) {
			return m, nil
		}
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.scroll.Move(msg.String(), len(m.doc), m.bodyHeight()) {
		return m, nil
	}
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "]", "tab":
		m.jumpChange(1)
	case "[", "shift+tab":
		m.jumpChange(-1)
	case "/":
		m.searching, m.query = true, ""
	case "n":
		m.find(m.found, 1)
	case "N":
		m.find(m.found, -1)
	case "right", "l":
		m.hx += 8
	case "left", "h":
		m.hx = max(m.hx-8, 0)
	case "e":
		return m, m.edit()
	case "d":
		return m, m.toggle()
	}
	return m, nil
}

// splitMin is the narrowest pane a diff is shown side by side in; below
// it each side would be too cramped to read.
const splitMin = 100

func (m Model) wantSplit() bool { return m.showDiff && len(m.split) > 0 && m.width >= splitMin }

func (m Model) isSplit() bool { return len(m.doc) > 0 && m.doc[0].split }

// relayout shows a diff side by side or in one column as the width
// allows, keeping the cursor on the same file line.
func (m *Model) relayout() {
	if m.wantSplit() == m.isSplit() {
		return
	}
	at := m.fileLine()
	if m.wantSplit() {
		m.doc = m.split
	} else {
		m.doc = m.unified
	}
	m.goToLine(at)
}

// toggle switches between the file and its change, landing on the same
// file line, or the nearest one the other view has.
func (m *Model) toggle() tea.Cmd {
	m.pending = max(m.fileLine(), 1)
	m.showDiff = !m.showDiff
	return load(m.current())
}

// fileLine is the file line at the cursor. A removed line in a diff has
// none, so it takes the next line that does.
func (m Model) fileLine() int {
	for i := m.scroll.Cursor; i < len(m.doc); i++ {
		if m.doc[i].num > 0 {
			return m.doc[i].num
		}
	}
	for i := min(m.scroll.Cursor, len(m.doc)-1); i >= 0; i-- {
		if m.doc[i].num > 0 {
			return m.doc[i].num
		}
	}
	return 0
}

func (m Model) searchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.searching = false
	case tea.KeyEnter:
		m.searching, m.found = false, m.query
		m.find(m.found, 1)
	case tea.KeyBackspace:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		m.query += " "
	case tea.KeyRunes:
		m.query += string(msg.Runes)
	}
	return m, nil
}

// goToLine puts the cursor on a file line, or the nearest one shown,
// centred. With none given, a diff opens on its first change.
func (m *Model) goToLine(n int) {
	m.scroll = ui.Scroll{}
	if n == 0 {
		if m.showDiff {
			m.jumpChange(1)
		}
		m.clamp()
		return
	}
	best, gap := -1, 0
	for i, l := range m.doc {
		if l.num == 0 || l.mark == '@' {
			continue
		}
		d := l.num - n
		if d < 0 {
			d = -d
		}
		if best < 0 || d < gap {
			best, gap = i, d
		}
	}
	if best >= 0 {
		m.scroll.Cursor = best
		m.scroll.Offset = max(best-m.bodyHeight()/2, 0)
	}
	m.clamp()
}

func (m *Model) jumpChange(dir int) {
	for i := m.scroll.Cursor + dir; i >= 0 && i < len(m.doc); i += dir {
		if m.doc[i].starts {
			m.scroll.Cursor = i
			if i < m.scroll.Offset || i >= m.scroll.Offset+m.bodyHeight() {
				m.scroll.Offset = max(i-m.bodyHeight()/3, 0)
			}
			m.clamp()
			return
		}
	}
	m.notice = "No more changes"
}

// find moves to the next line containing q, ignoring case, wrapping round.
func (m *Model) find(q string, dir int) {
	if q == "" || len(m.doc) == 0 {
		return
	}
	q = strings.ToLower(q)
	n := len(m.doc)
	for step := 1; step <= n; step++ {
		i := ((m.scroll.Cursor+dir*step)%n + n) % n
		if strings.Contains(strings.ToLower(m.doc[i].plain), q) {
			m.scroll.Cursor = i
			if i < m.scroll.Offset || i >= m.scroll.Offset+m.bodyHeight() {
				m.scroll.Offset = max(i-m.bodyHeight()/2, 0)
			}
			m.clamp()
			if col := strings.Index(strings.ToLower(m.doc[i].plain), q); col < m.hx || col > m.hx+m.textWidth()-len(q) {
				m.hx = max(col-8, 0)
			}
			return
		}
	}
	m.notice = "Not found: " + q
}

type editedMsg struct{}

// edit hands the file to the editor at the cursor's line, then shows it
// again. In Rex the popup is relabelled while editing, so a newer view does
// not replace it and lose unsaved work.
func (m Model) edit() tea.Cmd {
	n := 1
	if m.scroll.Cursor < len(m.doc) {
		if l := m.doc[m.scroll.Cursor]; l.num > 0 {
			n = l.num
		}
	}
	argv := launch.Argv(launch.Target{Root: m.opt.Root, Path: m.opt.Path, Line: n})
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = m.opt.Root
	label := func(prefix string) {
		if t, ok := rex.Current(); ok {
			rex.SetLabel(context.Background(), t, prefix+m.opt.Path)
		}
	}
	label(launch.EditLabel)
	return tea.ExecProcess(cmd, func(error) tea.Msg {
		label(launch.ViewLabel)
		return editedMsg{}
	})
}

// Layout: a title row, a rule, the document, then a footer.
func (m Model) bodyHeight() int { return max(m.height-3, 1) }

func (m *Model) clamp() { m.scroll.Clamp(len(m.doc), m.bodyHeight()) }

func (m Model) gutterWidth() int {
	widest := 1
	for _, l := range m.doc {
		widest = max(widest, len(fmt.Sprint(max(l.num, l.old))))
	}
	return 1 + widest + 1
}

func (m Model) textWidth() int { return max(m.width-m.gutterWidth()-1, 1) }

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	out := []string{m.title(), ui.Dim.Render(strings.Repeat("─", m.width))}
	switch {
	case !m.ready:
		out = append(out, ui.Dim.Render(" Reading…"))
	case m.err != nil:
		out = append(out, " "+ui.Error.Render(m.err.Error()))
	default:
		start, end := m.scroll.Visible(len(m.doc), m.bodyHeight())
		for i := start; i < end; i++ {
			out = append(out, m.row(i))
		}
	}
	for len(out) < m.height-1 {
		out = append(out, "")
	}
	out = append(out[:m.height-1], m.footer())
	return strings.Join(out, "\n")
}

func (m Model) title() string {
	dir, name := path.Split(m.opt.Path)
	left := " " + ui.Bold.Render(name)
	if dir != "" {
		left += "  " + ui.Dim.Render(strings.TrimSuffix(dir, "/"))
	}
	var facts []string
	switch {
	case m.showDiff && m.opt.Commit != "":
		facts = append(facts, "change in "+short(m.opt.Commit))
	case m.showDiff:
		facts = append(facts, "change since last commit")
	}
	if m.lang != "" {
		facts = append(facts, m.lang)
	}
	if m.ready && m.err == nil && !m.showDiff {
		facts = append(facts, fmt.Sprintf("%d lines", len(m.doc)))
		if m.fresh {
			facts = append(facts, "new file")
		} else if m.added > 0 {
			facts = append(facts, fmt.Sprintf("%d changed", m.added))
		}
	}
	right := ui.Dim.Render(strings.Join(facts, " · "))
	if count := ui.Count(m.added, m.gone, false); count != "" && m.showDiff {
		right += "  " + count
	}
	return ui.Spread(left, right+" ", m.width)
}

func short(commit string) string { return commit[:min(len(commit), 7)] }

func (m Model) row(i int) string {
	l := m.doc[i]
	if l.split {
		return m.splitRow(l, i == m.scroll.Cursor)
	}
	var mark string
	switch l.mark {
	case git.LineAdded:
		mark = addMark
	case git.LineModified:
		mark = modMark
	case git.LineDeleted:
		mark = delMark
	case '+':
		mark = plusMark
	case '-':
		mark = subMark
	default:
		mark = " "
	}
	digits := m.gutterWidth() - 2
	num := strings.Repeat(" ", digits)
	switch {
	case l.mark == '@':
	case l.num > 0:
		num = fmt.Sprintf("%*d", digits, l.num)
	case l.old > 0:
		num = fmt.Sprintf("%*d", digits, l.old)
	}
	if i == m.scroll.Cursor {
		num = ui.Selected.Render(num)
	} else {
		num = ui.Dim.Render(num)
	}
	text := ansi.Cut(l.text, m.hx, m.hx+m.textWidth())
	return mark + num + "  " + text
}

func (m Model) footer() string {
	if m.searching {
		return ui.Line(" "+ui.Accent.Render("/")+m.query+ui.Selected.Render(" "), ui.Dim.Render("↵ find  esc cancel")+" ", m.width)
	}
	var left string
	switch {
	case m.notice != "":
		left = ui.KindStyle('M').Render(m.notice)
	case m.ready && len(m.doc) > 0 && m.scroll.Cursor < len(m.doc):
		l := m.doc[m.scroll.Cursor]
		if l.num > 0 {
			left = ui.Dim.Render(fmt.Sprintf("line %d", l.num))
		}
	}
	other := "d diff"
	if m.showDiff {
		other = "d file"
	}
	keys := "↑↓ move  ] [ changes  " + other + "  / find  e edit  q close"
	if m.width < 76 {
		keys = "] [ changes  " + other + "  e edit  q close"
	}
	return ui.Spread(" "+left, ui.Dim.Render(keys)+" ", m.width)
}

func humanSize(n int64) string {
	return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
}

package mentions

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/launch"
	"github.com/gdo33s/rig/internal/rex"
	"github.com/gdo33s/rig/internal/ui"
)

// Source is the terminal whose files are listed, and the folder its
// program runs in, which relative paths start from.
type Source struct {
	Session, Block string
	Dir            string
}

type Model struct {
	src   Source
	read  func(context.Context, Source) (string, error)
	all   []Mention
	shown []Mention
	ready bool
	err   error

	root    string // the project, which changes describe
	changes map[string]*git.Change
	query   string
	scroll  ui.Scroll

	opening bool
	notice  error

	width, height int
}

func New(src Source) Model { return Model{src: src, read: capture} }

// Run shows the files src has mentioned until one is opened or the list
// is closed.
func Run(src Source) error {
	_, err := tea.NewProgram(program{New(src)}, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// program is Model as Bubble Tea runs it.
type program struct{ Model }

func (p program) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	p.Model, cmd = p.Model.Update(msg)
	return p, cmd
}

func capture(ctx context.Context, src Source) (string, error) {
	return rex.Capture(ctx, src.Session, src.Block)
}

type loadedMsg struct {
	root     string
	mentions []Mention
	changes  map[string]*git.Change
	err      error
}

func (m Model) Init() tea.Cmd {
	src, read := m.src, m.read
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		text, err := read(ctx, src)
		if err != nil {
			return loadedMsg{err: err}
		}
		ix := &Index{Cwd: src.Dir}
		var changes map[string]*git.Change
		if repo, err := git.Open(ctx, src.Dir); err == nil {
			ix.Root = repo.Root
			if real, err := filepath.EvalSymlinks(ix.Root); err == nil {
				ix.Root = real
			}
			ix.Files, _ = repo.Project(ctx)
			if st, err := repo.Status(ctx); err == nil {
				changes = st.Changes
			}
		}
		if real, err := filepath.EvalSymlinks(ix.Cwd); err == nil {
			ix.Cwd = real
		}
		return loadedMsg{root: ix.Root, mentions: ix.Extract(text), changes: changes}
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.clamp()

	case loadedMsg:
		m.root, m.all, m.changes, m.err, m.ready = msg.root, msg.mentions, msg.changes, msg.err, true
		m.filter()

	case launch.DoneMsg:
		m.opening = false
		if msg.Err != nil {
			m.notice = msg.Err
			return m, nil
		}
		return m, tea.Quit

	case tea.MouseMsg:
		m.scroll.Wheel(msg, len(m.shown), m.listHeight())

	case tea.KeyMsg:
		m.notice = nil
		var cmd tea.Cmd
		for _, k := range ui.Keys(msg) {
			if m, cmd = m.key(k); cmd != nil {
				return m, cmd
			}
		}
		return m, nil
	}
	return m, nil
}

func (m Model) key(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+c":
		return m, tea.Quit
	case "enter":
		return m.open(false)
	case "tab":
		return m.open(true)
	case "up", "ctrl+p", "ctrl+k":
		m.scroll.Cursor--
	case "down", "ctrl+n", "ctrl+j":
		m.scroll.Cursor++
	case "pgup":
		m.scroll.Cursor -= max(m.listHeight()/2, 1)
	case "pgdown":
		m.scroll.Cursor += max(m.listHeight()/2, 1)
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
			m.filter()
		}
	case "ctrl+u":
		m.query = ""
		m.filter()
	default:
		if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
			m.query += string(msg.Runes)
			if msg.Type == tea.KeySpace {
				m.query += " "
			}
			m.filter()
		}
	}
	m.clamp()
	return m, nil
}

// filter keeps the files whose path holds every word of the query.
func (m *Model) filter() {
	words := strings.Fields(strings.ToLower(m.query))
	m.shown = m.shown[:0]
	for _, f := range m.all {
		p := strings.ToLower(f.Path)
		keep := true
		for _, w := range words {
			if !strings.Contains(p, w) {
				keep = false
				break
			}
		}
		if keep {
			m.shown = append(m.shown, f)
		}
	}
	m.scroll = ui.Scroll{}
	m.clamp()
}

// open shows the chosen file at the line it was mentioned at, or its
// change, then closes the list once it is up.
func (m Model) open(diff bool) (Model, tea.Cmd) {
	if m.opening || m.scroll.Cursor >= len(m.shown) {
		return m, nil
	}
	f := m.shown[m.scroll.Cursor]
	m.opening = true
	t := launch.Target{Root: f.Root, Path: f.Path, Line: f.Line, Diff: diff}
	if diff {
		t.Line = 0
	}
	return m, launch.Open(t)
}

// The layout: the filter row, a rule, the files, then a footer.
func (m Model) listHeight() int { return max(m.height-3, 1) }

func (m *Model) clamp() { m.scroll.Clamp(len(m.shown), m.listHeight()) }

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	lines := []string{m.queryLine(), ui.Dim.Render(strings.Repeat("─", m.width))}
	body := m.rows()
	for len(body) < m.listHeight() {
		body = append(body, "")
	}
	lines = append(lines, body[:m.listHeight()]...)
	lines = append(lines, m.footer())
	return strings.Join(lines[:min(len(lines), m.height)], "\n")
}

func (m Model) queryLine() string {
	prompt := ui.Accent.Render(" 󰈙 ")
	right := ui.Dim.Render("Mentioned here") + " "
	if m.query == "" {
		return ui.Line(prompt+ui.Selected.Render(" ")+ui.Dim.Render("Filter files"), right, m.width)
	}
	return ui.Line(prompt+m.query+ui.Selected.Render(" "), right, m.width)
}

func (m Model) rows() []string {
	switch {
	case m.err != nil:
		return []string{" " + ui.Error.Render(m.err.Error())}
	case !m.ready:
		return []string{ui.Dim.Render(" Reading the pane…")}
	case len(m.all) == 0:
		return []string{ui.Dim.Render(" No files mentioned in this pane yet")}
	case len(m.shown) == 0:
		return []string{ui.Dim.Render(" No matches")}
	}
	start, end := m.scroll.Visible(len(m.shown), m.listHeight())
	var lines []string
	for i := start; i < end; i++ {
		line := m.fileLine(m.shown[i])
		if i == m.scroll.Cursor {
			plain := ui.Plain(line)
			line = ui.Selected.Render(plain + strings.Repeat(" ", max(m.width-ansi.StringWidth(plain), 0)))
		}
		lines = append(lines, line)
	}
	return lines
}

// fileLine is the file name and line, its folder after it, and its git
// state at the end, as Find shows a file.
func (m Model) fileLine(f Mention) string {
	dir, name := path.Split(filepath.ToSlash(f.Path))
	style := ui.Bold
	var right string
	if c := m.changes[f.Path]; c != nil && f.Root == m.root {
		style = ui.KindStyle(c.Kind()).Bold(true)
		right = ui.KindStyle(c.Kind()).Render(string(c.Kind()))
	}
	left := " " + style.Render(name)
	if f.Line > 0 {
		left += ui.Dim.Render(fmt.Sprintf(":%d", f.Line))
	}
	if dir != "" {
		left += "  " + ui.Dim.Render(strings.TrimSuffix(dir, "/"))
	} else if f.Root != m.root {
		left += "  " + ui.Dim.Render(tilde(f.Root))
	}
	return ui.Line(left, right+" ", m.width)
}

// tilde writes a path in the home folder from ~.
func tilde(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

func (m Model) footer() string {
	var left string
	switch {
	case m.notice != nil:
		left = ui.Error.Render(m.notice.Error())
	case m.ready && len(m.all) > 0:
		n := len(m.shown)
		left = ui.Dim.Render(fmt.Sprintf("%d %s", n, map[bool]string{true: "file", false: "files"}[n == 1]))
	}
	keys := ui.Dim.Render("↵ open  ⇥ diff  esc close")
	return ui.Line(" "+left, keys+" ", m.width)
}

package usage

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/git"
	"github.com/gdo33s/rig/internal/kitty"
	"github.com/gdo33s/rig/internal/ui"
)

const (
	interval    = 2 * time.Second
	loadTimeout = 20 * time.Second
	// A reading this old may no longer be true: nothing has reported it
	// since, so it is dimmed.
	staleAfter = 5 * time.Minute
	// A session quiet for longer than this no longer has its context shown.
	activeFor = 30 * time.Minute
)

// Model is the Usage pane: a strip of Claude Code's 5-hour and weekly
// limits and the current session's context, and below it a strip of what
// the current branch changes. Each entry is an icon and a figure.
type Model struct {
	dir       string // the project; a session here is preferred for context
	statePath string
	icons     iconMode
	state     State
	err       error
	repo      git.Repo
	branch    *git.BranchDiff // nil outside a repository
	ready     bool
	loading   bool
	now       time.Time
	width     int
	height    int
}

func New(dir string) Model {
	return Model{dir: dir, statePath: StatePath(), icons: detectIcons()}
}

// SetDir points the pane at another project, as when its shell changes
// folder.
func (m *Model) SetDir(dir string) {
	m.dir = dir
	if m.repo.Root != "" && !within(dir, m.repo.Root) {
		m.repo, m.branch = git.Repo{}, nil
	}
}

type loadedMsg struct {
	state  State
	err    error
	repo   git.Repo
	branch *git.BranchDiff
	now    time.Time
	took   time.Duration
}

type tickMsg struct{}

func (m Model) Init() tea.Cmd { return m.load() }

// load reads the recorded readings and the branch off the UI goroutine.
func (m *Model) load() tea.Cmd {
	m.loading = true
	path, dir, repo := m.statePath, m.dir, m.repo
	return func() tea.Msg {
		start := time.Now()
		msg := loadedMsg{now: start}
		msg.state, msg.err = Load(path)
		if dir != "" {
			ctx, cancel := context.WithTimeout(context.Background(), loadTimeout)
			defer cancel()
			if repo.Root == "" {
				repo, _ = git.Open(ctx, dir)
			}
			if repo.Root != "" {
				if d, err := repo.BranchDiff(ctx); err == nil {
					msg.repo, msg.branch = repo, &d
				}
			}
		}
		msg.took = time.Since(start)
		return msg
	}
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case loadedMsg:
		m.loading, m.ready = false, true
		m.state, m.err, m.now = msg.state, msg.err, msg.now
		m.repo, m.branch = msg.repo, msg.branch
		// A slow repository is read less often.
		return m, tea.Tick(max(interval, 4*msg.took), func(time.Time) tea.Msg { return tickMsg{} })
	case tickMsg:
		if !m.loading {
			return m, m.load()
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "r":
			if !m.loading {
				return m, m.load()
			}
		case "q", "ctrl+c":
			return m, tea.Quit
		}
	}
	return m, nil
}

// item is one entry: an icon, a figure, and a detail under the figure.
type item struct {
	id     int    // its image, when icons are images
	icon   string // its Nerd Font glyph
	label  string // shown when icons are text; empty when the figure says enough
	name   string // its row's name in a table
	figure string
	value  string // the figure in a table, when shorter than figure
	style  lipgloss.Style
	detail string
	title  bool // in a table, this item is its group's title row
}

var (
	fiveHour  = item{id: 1, icon: "󰔟", label: "5h", name: "5-hour"}
	weekly    = item{id: 2, icon: "󰨳", label: "7d", name: "Weekly"}
	ctxItem   = item{id: 3, icon: "󰧑", label: "ctx", name: "Context"}
	branchIt  = item{id: 4, icon: "󰘬", name: "Branch", title: true}
	linesItem = item{id: 5, icon: "󰦓", name: "Lines"}
	filesItem = item{id: 6, icon: "󰈢", name: "Files"}

	allItems = []item{fiveHour, weekly, ctxItem, branchIt, linesItem, filesItem}
)

func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	groups := [][]item{m.usageItems()}
	if m.branch != nil {
		groups = append(groups, m.branchItems())
	}
	lines := m.arrange(groups)
	if note := m.errNote(); note != "" && len(lines) > 0 {
		lines[0] = ui.Line(ansi.Truncate(lines[0], m.width-2, ""), note, m.width)
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	return strings.Join(lines[:m.height], "\n")
}

// form is one way to draw a group of items.
type form struct {
	icons   iconMode
	table   bool // a titled table, one item per row, for narrow panes
	details bool // a row form can leave out details to save a row
}

// arrange stacks the groups, separated by blank rows, in the largest form
// that fits them all. A wide pane puts each group on one row, with image
// icons when it can; a narrow one draws each group as a small table in the
// style of the panes beside it. Only when even the smallest form is too
// tall does it drop the last group, and failing everything it shows as
// much of the usage group as fits.
func (m Model) arrange(groups [][]item) []string {
	small := m.icons
	if small == imageIcons {
		small = fontIcons
	}
	var forms []form
	if m.icons == imageIcons {
		forms = append(forms, form{imageIcons, false, true})
	}
	forms = append(forms, form{small, true, true}, form{small, false, true}, form{small, false, false})

	for n := len(groups); n > 0; n-- {
	forms:
		for _, f := range forms {
			var lines []string
			for i, g := range groups[:n] {
				rows := m.render(g, f)
				if rows == nil {
					continue forms
				}
				if i > 0 {
					lines = append(lines, "")
				}
				lines = append(lines, rows...)
			}
			if len(lines) <= m.height {
				return lines
			}
		}
	}
	// Nothing fits whole: the compact row, cut to the pane.
	rows := m.row(groups[0], form{small, false, false}, false)
	return rows[:min(len(rows), m.height)]
}

const itemGap = "   "

func (m Model) render(items []item, f form) []string {
	if f.table {
		return m.table(items, f.icons)
	}
	return m.row(items, f, true)
}

// icon draws an item's icon for one row of its height: an image row, a
// glyph on the first row, or a label padded to labelW.
func icon(it item, mode iconMode, row, labelW int) string {
	switch mode {
	case imageIcons:
		return kitty.Row(it.id, row, iconCols)
	case fontIcons:
		if row > 0 {
			return strings.Repeat(" ", ansi.StringWidth(it.icon))
		}
		return it.icon
	}
	if labelW == 0 {
		return ""
	}
	if row > 0 {
		return strings.Repeat(" ", labelW)
	}
	return pad(it.label, labelW)
}

// row puts the whole group on one line, each figure beside the top of its
// icon and its detail beside the bottom. With fit, it returns nil if that
// is wider than the pane; otherwise the line is cut to the pane.
func (m Model) row(items []item, f form, fit bool) []string {
	top, bottom := " ", " "
	for i, it := range items {
		if i > 0 {
			top, bottom = top+itemGap, bottom+itemGap
		}
		if ic := icon(it, f.icons, 0, ansi.StringWidth(it.label)); ic != "" {
			top += ic + " "
			bottom += icon(it, f.icons, 1, ansi.StringWidth(it.label)) + " "
		}
		w := ansi.StringWidth(it.figure)
		if f.details {
			w = max(w, ansi.StringWidth(it.detail))
		}
		top += pad(it.style.Render(it.figure), w)
		bottom += pad(ui.Dim.Render(it.detail), w)
	}
	if fit && ansi.StringWidth(top) > m.width {
		return nil
	}
	if !f.details {
		return []string{ui.Line(top, "", m.width)}
	}
	return []string{ui.Line(top, "", m.width), ui.Line(bottom, "", m.width)}
}

// table draws a group as a titled table: a bold title row, then one row
// per item with its icon and name on the left, and on the right a dim note
// and the figure, right-aligned in a column. The usage group is titled
// Usage; the branch group is titled by its branch.
func (m Model) table(items []item, mode iconMode) []string {
	head := ui.Heading.Render("Usage")
	headNote := m.age()
	var rows []item
	for _, it := range items {
		if it.title {
			head = ui.Heading.Render(it.name) + " " + it.figure
			headNote = it.detail
			continue
		}
		rows = append(rows, it)
	}
	lines := []string{m.spread(head, ui.Dim.Render(headNote))}

	nameW, minV, maxV := 0, math.MaxInt, 0
	for _, it := range rows {
		nameW = max(nameW, ansi.StringWidth(it.name))
		w := ansi.StringWidth(it.tableValue())
		minV, maxV = min(minV, w), max(maxV, w)
	}
	// Figures of a like width, such as percentages, share a column. A short
	// figure beside a long one keeps its note close instead.
	valueW := 0
	if maxV-minV <= 2 {
		valueW = maxV
	}
	for _, it := range rows {
		left := pad(it.name, nameW)
		if mode != textIcons {
			left = it.icon + " " + left
		}
		value := it.style.Render(it.tableValue())
		value = strings.Repeat(" ", max(valueW-ansi.StringWidth(it.tableValue()), 0)) + value
		right := value
		if it.detail != "" {
			right = ui.Dim.Render(it.detail) + "  " + value
		}
		// Without room for the note, the figure still shows.
		if ansi.StringWidth(left)+1+ansi.StringWidth(right) > m.width {
			right = value
		}
		lines = append(lines, m.spread(left, right))
	}
	return lines
}

func (it item) tableValue() string {
	if it.value != "" {
		return it.value
	}
	return it.figure
}

// spread puts left and right at the two ends of a line, truncating left
// when they do not both fit.
func (m Model) spread(left, right string) string {
	rw := ansi.StringWidth(right)
	if rw > 0 && rw+2 > m.width {
		right, rw = "", 0
	}
	room := m.width - rw
	if rw > 0 {
		room--
	}
	left = ansi.Truncate(left, room, "…")
	return left + strings.Repeat(" ", max(m.width-ansi.StringWidth(left)-rw, 0)) + right
}

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(width-ansi.StringWidth(s), 0))
}

func (m Model) errNote() string {
	if m.err != nil {
		return ui.Error.Render("!")
	}
	return ""
}

func (m Model) usageItems() []item {
	items := []item{m.window("five_hour", fiveHour), m.window("seven_day", weekly), m.context()}
	if !m.ready {
		for i := range items {
			items[i].figure, items[i].style, items[i].detail = "…", ui.Dim, ""
		}
	}
	return items
}

// window fills in a limit: the percentage used, coloured by whether it is
// being used faster than it renews.
func (m Model) window(name string, it item) item {
	it.figure, it.style = "–", ui.Dim
	w, ok := m.state.Windows[name]
	if !ok {
		return it
	}
	resets := time.Unix(w.ResetsAt, 0)
	if !resets.After(m.now) {
		it.detail = "reset"
		return it
	}
	used := math.Max(0, w.UsedPercentage)
	it.figure = fmt.Sprintf("%.0f%%", used)
	elapsed := 1 - resets.Sub(m.now).Seconds()/windowLength[name].Seconds()
	it.style = paceStyle(used/100, elapsed)
	it.detail = until(resets, m.now)
	return it
}

// age says how old the readings are, once the newest of them is older than
// staleAfter: Claude Code reports only while a session is active, so usage
// elsewhere may not be counted yet.
func (m Model) age() string {
	var newest int64
	for _, w := range m.state.Windows {
		newest = max(newest, w.SeenAt)
	}
	for _, s := range m.state.Sessions {
		newest = max(newest, s.SeenAt)
	}
	seen := time.Unix(newest, 0)
	if newest == 0 || m.now.Sub(seen) <= staleAfter {
		return ""
	}
	return ui.Age(seen, m.now) + " ago"
}

// paceStyle colours a limit by whether it will last: red when nearly gone,
// yellow when used faster than the window renews, green otherwise.
func paceStyle(used, elapsed float64) lipgloss.Style {
	switch {
	case used >= 0.9:
		return ui.Deleted
	case used > elapsed+0.1:
		return ui.KindStyle('M')
	}
	return ui.Added
}

// context fills in the context window of the session this pane belongs to:
// the most recent one in its directory, else the most recent anywhere.
func (m Model) context() item {
	it := ctxItem
	it.figure, it.style = "–", ui.Dim
	var best Session
	var bestHere, found bool
	for _, s := range m.state.Sessions {
		if m.now.Sub(time.Unix(s.SeenAt, 0)) > activeFor {
			continue
		}
		here := m.dir != "" && (within(m.dir, s.Dir) || within(s.Dir, m.dir))
		if !found || here && !bestHere || here == bestHere && s.SeenAt > best.SeenAt {
			best, bestHere, found = s, here, true
		}
	}
	if !found {
		return it
	}
	used := math.Max(0, math.Min(100, best.UsedPercentage))
	it.figure = fmt.Sprintf("%.0f%%", used)
	it.style = contextStyle(used)
	it.detail = sessionName(best)
	return it
}

func sessionName(s Session) string {
	if s.Name != "" {
		return s.Name
	}
	return filepath.Base(s.Dir)
}

// contextStyle warns as a session's context nears the point where Claude
// Code compacts it.
func contextStyle(used float64) lipgloss.Style {
	switch {
	case used >= 80:
		return ui.Deleted
	case used >= 60:
		return ui.KindStyle('M')
	}
	return ui.Added
}

const maxBranchName = 24

// branchItems shows what the current branch changes: its name and base,
// the lines it adds and removes, and the files it touches.
func (m Model) branchItems() []item {
	d := m.branch
	br, lines, files := branchIt, linesItem, filesItem

	br.figure = d.Branch
	if br.figure == "" {
		br.figure = "detached"
	}
	br.figure = ansi.Truncate(br.figure, maxBranchName, "…")
	br.style = ui.Bold
	br.detail = "uncommitted"
	if d.Base != "" {
		br.detail = "vs " + d.Base
	}

	lines.figure = ui.Count(d.Added, d.Deleted, false)
	if lines.figure == "" {
		lines.figure = ui.Dim.Render("no changes")
	}
	if d.Base != "" {
		lines.detail = plural(d.Commits, "commit")
	}

	files.figure = plural(d.Files, "file")
	files.value = fmt.Sprint(d.Files)
	if d.Files == 0 {
		files.style = ui.Dim
	}
	return []item{br, lines, files}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func within(dir, root string) bool {
	rel, err := filepath.Rel(root, dir)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, "../")
}

// until is the time left before t: "42m", "2h14m", or a day and time when
// it is more than a day away.
func until(t, now time.Time) string {
	d := t.Sub(now)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(math.Ceil(d.Minutes())))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return t.Local().Format("Mon 15:04")
}

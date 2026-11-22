package find

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/fff"
)

func withResults(md mode, files []string, matches []fff.Match) Model {
	m := Model{root: "/repo", mode: md, query: "wind", started: true, ready: true, width: 70, height: 10}
	m.files, m.matches = files, matches
	m.layout()
	return m
}

var (
	someFiles   = []string{"src/wind.go", "docs/windy.md"}
	someMatches = []fff.Match{{Path: "src/weather.go", Line: 12, Text: "  windKph := 18"}, {Path: "README.md", Line: 3, Text: "Wind speed"}}
)

func lines(m Model) []string {
	out := strings.Split(ansi.Strip(m.View()), "\n")
	for i := range out {
		out[i] = strings.TrimRight(out[i], " ")
	}
	return out
}

func TestAllShowsFilesThenText(t *testing.T) {
	m := withResults(all, someFiles, someMatches)
	got := lines(m)
	for i, want := range []string{"Files", "wind.go", "windy.md", "Text", "windKph := 18", "Wind speed"} {
		if !strings.Contains(got[i+2], want) {
			t.Fatalf("row %d = %q, want %q\n%s", i, got[i+2], want, strings.Join(got, "\n"))
		}
	}
	if !strings.HasSuffix(got[6], "src/weather.go:12") {
		t.Fatalf("match row = %q", got[6])
	}
	if !strings.Contains(got[len(got)-1], "2 files · 2 matches") {
		t.Fatalf("footer = %q", got[len(got)-1])
	}
	if m.scroll.Cursor != 1 {
		t.Fatalf("cursor starts on %d, want the first file", m.scroll.Cursor)
	}
}

func TestMovingSkipsTitles(t *testing.T) {
	m := withResults(all, someFiles, someMatches)
	var visited []string
	for range 5 {
		r := m.rows[m.scroll.Cursor]
		visited = append(visited, r.path)
		m.step(1)
	}
	want := "src/wind.go docs/windy.md src/weather.go README.md README.md"
	if got := strings.Join(visited, " "); got != want {
		t.Fatalf("down: %s", got)
	}
	for range 2 {
		m.step(-1)
	}
	if r := m.rows[m.scroll.Cursor]; r.path != "docs/windy.md" {
		t.Fatalf("up landed on %+v", r)
	}
}

func TestOpenGoesToTheLine(t *testing.T) {
	m := withResults(all, someFiles, someMatches)
	m.step(2) // past the Text title to the first match
	if r := m.rows[m.scroll.Cursor]; r.path != "src/weather.go" || r.line != 12 {
		t.Fatalf("selected %+v", r)
	}
	next, cmd := m.open()
	if cmd == nil || !next.(Model).opening {
		t.Fatal("did not open")
	}
}

func TestSingleSectionsHaveNoTitles(t *testing.T) {
	if got := lines(withResults(files, someFiles, nil)); strings.Contains(got[2], "Files") || !strings.Contains(got[2], "wind.go") {
		t.Fatalf("files mode:\n%s", strings.Join(got, "\n"))
	}
	if got := lines(withResults(all, someFiles, nil)); strings.Contains(got[2], "Files") {
		t.Fatalf("all with names only should not title them:\n%s", strings.Join(got, "\n"))
	}
}

func TestTabCyclesModes(t *testing.T) {
	m := Model{}
	var seen []mode
	for range 4 {
		next, _ := m.key(tea.KeyMsg{Type: tea.KeyTab})
		m = next.(Model)
		seen = append(seen, m.mode)
	}
	if seen[0] != files || seen[1] != text || seen[2] != all || seen[3] != files {
		t.Fatalf("modes = %v", seen)
	}
}

func TestWeakFileNamesGoBelowText(t *testing.T) {
	m := Model{root: "/repo", mode: all, query: "func run", started: true, ready: true, width: 80, height: 14}
	m.files = []string{"internal/launch/launch_test.go", "internal/git/branch.go"}
	m.matches = rankMatches([]fff.Match{
		{Path: "internal/find/model.go", Line: 84, Text: "func Run(dir string) error {"},
		{Path: "cmd/rig/main.go", Line: 44, Text: "func run(args []string) error {"},
	}, m.query)
	m.layout()
	got := lines(m)
	if !strings.HasPrefix(strings.TrimSpace(got[2]), "Text") || !strings.Contains(got[3], "func run(args") || !strings.HasPrefix(strings.TrimSpace(got[5]), "Files") {
		t.Fatalf("layout:\n%s", strings.Join(got, "\n"))
	}
	if r := m.rows[m.scroll.Cursor]; r.path != "cmd/rig/main.go" || r.line != 44 {
		t.Fatalf("cursor on %+v, want the exact match", r)
	}
}

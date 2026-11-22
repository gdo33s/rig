package view

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// repo makes a repository with a committed Go file of 40 lines, then
// changes line 10 and adds a line after 30.
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=rig", "-c", "user.email=rig@example.com"}, args...)...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	var lines []string
	for i := 1; i <= 40; i++ {
		lines = append(lines, "var line"+strings.Repeat("x", i%3)+" = "+strconv.Itoa(i))
	}
	write := func() {
		os.WriteFile(filepath.Join(root, "main.go"), []byte("package main\n\n"+strings.Join(lines, "\n")+"\n"), 0o644)
	}
	write()
	git("init", "-q")
	git("add", ".")
	git("commit", "-qm", "base")
	lines[7] = "var changed = 10"                                                   // file line 10
	lines = append(lines[:28], append([]string{"var added = 1"}, lines[28:]...)...) // file line 31
	write()
	return root
}

func open(t *testing.T, opt Options) Model {
	t.Helper()
	m := New(opt)
	next, _ := m.Update(m.Init()())
	next, _ = next.Update(tea.WindowSizeMsg{Width: 60, Height: 12})
	return next.(Model)
}

func cursorLine(m Model) int { return m.doc[m.scroll.Cursor].num }

func TestFileOpensAtLineWithChangesMarked(t *testing.T) {
	m := open(t, Options{Root: repo(t), Path: "main.go", Line: 25})
	if m.err != nil {
		t.Fatal(m.err)
	}
	if cursorLine(m) != 25 {
		t.Fatalf("cursor on %d", cursorLine(m))
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "25  var line") || !strings.Contains(view, "main.go") || !strings.Contains(view, "Go · 43 lines · 2 changed") {
		t.Fatalf("view:\n%s", view)
	}
	if m.doc[9].mark != 'M' || m.doc[30].mark != 'A' {
		t.Fatalf("marks: line 10 %q, line 31 %q", m.doc[9].mark, m.doc[30].mark)
	}

	next, _ := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("]")})
	if got := cursorLine(next.(Model)); got != 31 {
		t.Fatalf("] went to %d, want 31", got)
	}
	next, _ = next.(Model).key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	next, _ = next.(Model).key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[")})
	if got := cursorLine(next.(Model)); got != 10 {
		t.Fatalf("[[ went to %d, want 10", got)
	}
}

func TestSearchWraps(t *testing.T) {
	m := open(t, Options{Root: repo(t), Path: "main.go", Line: 40})
	m.find("changed", 1)
	if got := cursorLine(m); got != 10 {
		t.Fatalf("found on %d, want 10 (wrapping past the end)", got)
	}
	m.find("no such text", 1)
	if m.notice == "" {
		t.Fatal("no notice for a failed search")
	}
}

func TestDiffOpensOnFirstChange(t *testing.T) {
	m := open(t, Options{Root: repo(t), Path: "main.go", Diff: true})
	if m.err != nil {
		t.Fatal(m.err)
	}
	l := m.doc[m.scroll.Cursor]
	if l.mark != '-' || l.plain != "var linexx = 8" || l.old != 10 {
		t.Fatalf("diff starts on %+v", l)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "change since last commit") || !strings.Contains(view, "+2 -1") || !strings.Contains(view, "⋯ line 7") {
		t.Fatalf("view:\n%s", view)
	}
}

func TestMissingFile(t *testing.T) {
	m := open(t, Options{Root: t.TempDir(), Path: "nope.go"})
	if m.err == nil || !strings.Contains(ansi.Strip(m.View()), "no such file") {
		t.Fatalf("view:\n%s", ansi.Strip(m.View()))
	}
}

// press sends a key and runs any load it starts, as the program would.
func press(t *testing.T, m Model, key string) Model {
	t.Helper()
	next, cmd := m.key(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
	if cmd != nil {
		next, _ = next.(Model).Update(cmd())
	}
	return next.(Model)
}

func TestDSwitchesBetweenFileAndDiffKeepingPlace(t *testing.T) {
	m := open(t, Options{Root: repo(t), Path: "main.go", Line: 31})
	m = press(t, m, "d")
	if !m.showDiff || m.doc[m.scroll.Cursor].num != 31 || m.doc[m.scroll.Cursor].mark != '+' {
		t.Fatalf("diff cursor on %+v", m.doc[m.scroll.Cursor])
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "change since last commit") || !strings.Contains(v, "d file") {
		t.Fatalf("view:\n%s", v)
	}
	m = press(t, m, "d")
	if m.showDiff || cursorLine(m) != 31 {
		t.Fatalf("back on the file at %d", cursorLine(m))
	}
	// A line far from any change lands on the nearest line the diff has.
	m.goToLine(20)
	m = press(t, m, "d")
	if n := m.doc[m.scroll.Cursor].num; n < 7 || n > 34 {
		t.Fatalf("landed on %d", n)
	}
}

func TestDOnAnUnchangedFileStays(t *testing.T) {
	root := repo(t)
	cmd := exec.Command("git", "-c", "user.name=rig", "-c", "user.email=rig@example.com", "commit", "-qam", "all")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	m := open(t, Options{Root: root, Path: "main.go", Line: 5})
	m = press(t, m, "d")
	if m.showDiff || m.err != nil || cursorLine(m) != 5 || !strings.Contains(m.notice, "No changes") {
		t.Fatalf("showDiff=%v err=%v line=%d notice=%q", m.showDiff, m.err, cursorLine(m), m.notice)
	}
}

func TestWideDiffIsSideBySide(t *testing.T) {
	m := New(Options{Root: repo(t), Path: "main.go", Diff: true})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	next, _ = next.Update(m.Init()())
	m = next.(Model)
	if !m.isSplit() {
		t.Fatal("a wide diff should be side by side")
	}
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	var changed, added string
	for _, r := range rows {
		switch {
		case strings.Contains(r, "linexx = 8"):
			changed = r
		case strings.Contains(r, "var added = 1"):
			added = r
		}
	}
	// The changed line: old on the left, new on the right of the divider.
	if l, r, ok := strings.Cut(changed, "│"); !ok || !strings.Contains(l, "10 var linexx = 8") || !strings.Contains(r, "10 var changed = 10") {
		t.Fatalf("changed row = %q", changed)
	}
	// The added line has nothing opposite: a stripe on the left.
	if l, r, _ := strings.Cut(added, "│"); !strings.Contains(l, "╱╱╱") || !strings.Contains(r, "31 var added = 1") {
		t.Fatalf("added row = %q", added)
	}
	// Narrowing the pane goes back to one column, on the same line.
	at := m.fileLine()
	next, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 14})
	m = next.(Model)
	if m.isSplit() || m.fileLine() != at {
		t.Fatalf("narrow: split=%v line=%d, want %d", m.isSplit(), m.fileLine(), at)
	}
}

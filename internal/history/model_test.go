package history

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gdo33s/rig/internal/ui"
)

func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	write := func(p, s string) {
		os.MkdirAll(filepath.Join(dir, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(dir, p), []byte(s), 0o644)
	}
	git("init", "-q")
	write("src/app.go", "package app\n")
	write("README.md", "hi\n")
	git("add", ".")
	git("commit", "-qm", "First commit")
	write("src/app.go", "package app\n\nfunc main() {}\n")
	write("notes.txt", "new\n")
	write("README.md", "hello\n")
	git("add", "README.md")
	return dir
}

func loaded(t *testing.T, dir string) Model {
	t.Helper()
	m := New(dir)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 60, Height: 10})
	m, _ = m.Update(m.load()())
	return m
}

func TestOpensOnChangedFiles(t *testing.T) {
	m := loaded(t, repo(t))
	view := ui.Plain(m.View())
	lines := strings.Split(view, "\n")
	if !strings.HasPrefix(lines[0], "Changes History") || !strings.Contains(lines[0], "3 files") {
		t.Fatalf("header = %q", lines[0])
	}
	for i, want := range []string{"README.md", "notes.txt", "app.go src"} {
		if !strings.Contains(lines[i+1], want) {
			t.Fatalf("line %d = %q, want %q\n%s", i+1, lines[i+1], want, view)
		}
	}
	if !strings.Contains(lines[3], "+2 M") || !strings.Contains(lines[2], "+1 ?") {
		t.Fatalf("stats missing:\n%s", view)
	}
}

func TestTabSwitchesToHistoryAndBack(t *testing.T) {
	m := loaded(t, repo(t))
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	view := ui.Plain(m.View())
	if !strings.Contains(view, "First commit") || strings.Contains(view, "notes.txt") {
		t.Fatalf("history view:\n%s", view)
	}
	m, _ = m.Update(tea.MouseMsg{X: 2, Y: 0, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if !strings.Contains(ui.Plain(m.View()), "notes.txt") {
		t.Fatalf("header click did not switch back:\n%s", ui.Plain(m.View()))
	}
}

func TestCleanTree(t *testing.T) {
	dir := repo(t)
	cmd := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qam", "all")
	cmd.Dir = dir
	cmd.Run()
	os.Remove(filepath.Join(dir, "notes.txt"))
	if v := ui.Plain(loaded(t, dir).View()); !strings.Contains(v, "No changes") {
		t.Fatalf("clean view:\n%s", v)
	}
}

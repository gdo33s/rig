package mentions

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gdo33s/rig/internal/ui"
)

func repoWith(t *testing.T, files ...string) string {
	t.Helper()
	ix := project(t, files...)
	for _, args := range [][]string{{"init", "-q"}, {"add", "."}, {"-c", "user.name=t", "-c", "user.email=t@t", "commit", "-qm", "init"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = ix.Root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	return ix.Root
}

func loaded(t *testing.T, root, text string) Model {
	t.Helper()
	m := New(Source{Dir: root})
	m.read = func(context.Context, Source) (string, error) { return text, nil }
	m, _ = m.Update(tea.WindowSizeMsg{Width: 80, Height: 12})
	m, _ = m.Update(m.Init()())
	return m
}

func TestListsNewestFirstWithChanges(t *testing.T) {
	root := repoWith(t, "internal/view/split.go", "cmd/rig/main.go")
	os.WriteFile(filepath.Join(root, "cmd/rig/main.go"), []byte("changed\n"), 0o644)
	m := loaded(t, root, "⏺ Read(cmd/rig/main.go)\n⏺ Update(internal/view/split.go)\nsee split.go:184")
	view := ui.Plain(m.View())
	first := strings.Index(view, "split.go:184")
	second := strings.Index(view, "main.go")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("order wrong:\n%s", view)
	}
	if !strings.Contains(view, "main.go  cmd/rig") || !strings.Contains(view, " M") || !strings.Contains(view, "2 files") {
		t.Fatalf("missing folder, change or count:\n%s", view)
	}
}

func TestFilterAndEmpty(t *testing.T) {
	root := repoWith(t, "a/one.go", "b/two.go")
	m := loaded(t, root, "a/one.go b/two.go")
	for _, r := range "two" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.shown) != 1 || m.shown[0].Path != "b/two.go" {
		t.Fatalf("shown = %+v", m.shown)
	}
	empty := loaded(t, root, "nothing here")
	if !strings.Contains(ui.Plain(empty.View()), "No files mentioned") {
		t.Fatalf("empty view:\n%s", ui.Plain(empty.View()))
	}
}

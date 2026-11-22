package mentions

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func project(t *testing.T, files ...string) *Index {
	t.Helper()
	root := t.TempDir()
	for _, f := range files {
		p := filepath.Join(root, filepath.FromSlash(f))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root, _ = filepath.EvalSymlinks(root)
	return &Index{Root: root, Cwd: root, Files: files}
}

func paths(ms []Mention) string {
	var out []string
	for _, m := range ms {
		s := m.Path
		if m.Line > 0 {
			s += ":" + strconv.Itoa(m.Line)
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

func TestExtractClaudeCodeTranscript(t *testing.T) {
	ix := project(t, "internal/view/split.go", "internal/view/model.go", "cmd/rig/main.go",
		"internal/find/model.go", "README.md")
	text := `
⏺ Read(cmd/rig/main.go)
  ⎿  Read 285 lines (ctrl+o to expand)

⏺ Update(internal/view/split.go)
  ⎿  Updated internal/view/split.go with 4 additions

  The pairing is in split.go:184, and see https://github.com/jnsahaj/lumen for
  the design, e.g. lipgloss.Style or v1.3.10. model.go is ambiguous, but
  …/view/model.go is not. Also nothing.go and ./missing/file.txt.
`
	got := paths(ix.Extract(text))
	want := "internal/view/model.go internal/view/split.go:184 cmd/rig/main.go"
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestExtractKeepsLineAndTrimsPunctuation(t *testing.T) {
	ix := project(t, "a/b.go", "README.md")
	got := paths(ix.Extract("see a/b.go#L12, then README.md. Finally a/b.go again."))
	if got != "a/b.go:12 README.md" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractAbsoluteAndOutsideProject(t *testing.T) {
	ix := project(t, "x.go")
	other := t.TempDir()
	other, _ = filepath.EvalSymlinks(other)
	outside := filepath.Join(other, "notes.txt")
	os.WriteFile(outside, []byte("n"), 0o644)
	ms := ix.Extract(filepath.Join(ix.Root, "x.go") + ":3 and " + outside)
	if len(ms) != 2 {
		t.Fatalf("got %+v", ms)
	}
	if ms[0].Root != other || ms[0].Path != "notes.txt" {
		t.Fatalf("outside = %+v", ms[0])
	}
	if ms[1].Root != ix.Root || ms[1].Path != "x.go" || ms[1].Line != 3 {
		t.Fatalf("inside = %+v", ms[1])
	}
}

func TestExtractFromSubfolder(t *testing.T) {
	ix := project(t, "web/src/app.ts")
	ix.Cwd = filepath.Join(ix.Root, "web")
	if got := paths(ix.Extract("edited src/app.ts:9")); got != "web/src/app.ts:9" {
		t.Fatalf("got %q", got)
	}
}

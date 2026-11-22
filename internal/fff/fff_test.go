package fff

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// These are answers fff-mcp 0.11.0 gave, kept verbatim.
const filesAnswer = `→ Read internal/outline/model.go (best match — Read this file directly)
5/29 matches
internal/outline/model.go
internal/outline/tree.go - frequent git:untracked
internal/outline/tree_test.go git:staged_modified
internal/usage/model_test.go
internal/usage/model.go
cursor: 1`

const grepAnswer = `→ Read PLAN.md (best match)
4/7 matches shown
internal/usage/model.go
 54: func (m *Model) SetDir(dir string) {
  55| m.dir = dir
PLAN.md
 130: **Follow cwd**: ` + "`rig sidebar`" + ` is given the shell block…
internal/history/model.go
 79: // SetDir points the pane at another directory, as when the shell it
`

func TestParseFiles(t *testing.T) {
	want := []string{
		"internal/outline/model.go", "internal/outline/tree.go", "internal/outline/tree_test.go",
		"internal/usage/model_test.go", "internal/usage/model.go",
	}
	if got := parseFiles(filesAnswer); !reflect.DeepEqual(got, want) {
		t.Fatalf("files = %q", got)
	}
	for name, want := range map[string]string{
		"docs/a - b.md":               "docs/a - b.md",
		"notes/my recent ideas.md":    "notes/my recent ideas.md",
		"src/x.go - recent":           "src/x.go",
		"src/y.go git:untracked":      "src/y.go",
		"notes/git: a guide.md":       "notes/git: a guide.md",
		"src/z.go - frequent git:new": "src/z.go",
	} {
		if got := parseFiles(name + "\n"); got[0] != want {
			t.Errorf("%q parsed as %q, want %q", name, got[0], want)
		}
	}
	if got := parseFiles("0/0 matches\n"); len(got) != 0 {
		t.Fatalf("no matches = %q", got)
	}
}

func TestParseGrep(t *testing.T) {
	got := parseGrep(grepAnswer)
	want := []Match{
		{"internal/usage/model.go", 54, "func (m *Model) SetDir(dir string) {"},
		{"PLAN.md", 130, "**Follow cwd**: `rig sidebar` is given the shell block…"},
		{"internal/history/model.go", 79, "// SetDir points the pane at another directory, as when the shell it"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches = %+v", got)
	}
}

func TestPruneLogs(t *testing.T) {
	dir := t.TempDir()
	for i := range keepLogs + 3 {
		name := filepath.Join(dir, "fff_mcp+"+strings.Repeat("1", i+1)+"+1.log")
		os.WriteFile(name, nil, 0o644)
	}
	pruneLogs(dir)
	logs, _ := filepath.Glob(filepath.Join(dir, "fff_mcp+*.log"))
	if len(logs) != keepLogs {
		t.Fatalf("kept %d logs", len(logs))
	}
}

// TestLive talks to a real fff-mcp, when one is installed.
func TestLive(t *testing.T) {
	if _, err := Binary(); err != nil {
		t.Skip("fff-mcp not installed")
	}
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "weather.go"), []byte("package src\n\nfunc WindSpeed() int { return 18 }\n"), 0o644)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# Tides\n"), 0o644)

	c, err := Start(root)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	files, err := c.Files("wethr", 10)
	if err != nil || len(files) == 0 || files[0] != "src/weather.go" {
		t.Fatalf("files = %q, %v", files, err)
	}
	matches, err := c.Grep("WindSpeed", 10)
	if err != nil || len(matches) != 1 || matches[0].Path != "src/weather.go" || matches[0].Line != 3 {
		t.Fatalf("matches = %+v, %v", matches, err)
	}
}

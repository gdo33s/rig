package syntax

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func init() { lipgloss.SetColorProfile(termenv.ANSI) }

func TestLinesKeepsTextAndLineCount(t *testing.T) {
	src := "package main\n\n// hello\nfunc main() {\n\ts := `multi\nline`\n}\n"
	l := Lexer("main.go", src)
	got := Lines(l, src)
	want := []string{"package main", "", "// hello", "func main() {", "    s := `multi", "line`", "}"}
	if len(got) != len(want) {
		t.Fatalf("lines = %d, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if ansi.Strip(got[i]) != want[i] {
			t.Errorf("line %d = %q, want %q", i, ansi.Strip(got[i]), want[i])
		}
	}
	if !strings.Contains(got[0], "\x1b[35m") {
		t.Errorf("keyword not coloured: %q", got[0])
	}
	if !strings.Contains(got[5], "\x1b[32m") {
		t.Errorf("the second line of a raw string should stay a string: %q", got[5])
	}
}

func TestLanguage(t *testing.T) {
	if got := Language("model.go", ""); got != "Go" {
		t.Errorf("go = %q", got)
	}
	if got := Language("notes.unknownext", "just words"); got != "" {
		t.Errorf("plain = %q", got)
	}
}

func TestExpandTabs(t *testing.T) {
	if got := ExpandTabs("a\tbc\td"); got != "a   bc  d" {
		t.Fatalf("got %q", got)
	}
}

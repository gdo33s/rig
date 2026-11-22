package find

import (
	"strings"
	"testing"

	"github.com/gdo33s/rig/internal/fff"
)

func TestTier(t *testing.T) {
	for line, want := range map[string]int{
		"func run(args []string) error {":   exactWord,
		"func runPane(m tea.Model) error {": exact,
		"func Run(dir string) error {":      foldedWord,
		"func RunThen(command string) {":    folded,
		"// functions that run in the pane": loose,
	} {
		if got := tier(line, "func run"); got != want {
			t.Errorf("tier(%q) = %d, want %d", line, got, want)
		}
	}
}

func TestRankMatchesFromTheScreenshot(t *testing.T) {
	fffOrder := []fff.Match{
		{Path: "internal/find/model.go", Line: 84, Text: "func Run(dir string) error {"},
		{Path: "cmd/rig/main.go", Line: 44, Text: "func run(args []string) error {"},
		{Path: "cmd/rig/main.go", Line: 164, Text: "func runPane(m tea.Model) error {"},
		{Path: "internal/usage/statusline.go", Line: 81, Text: "func runThen(command string, input []byte, stdout io.Writer) error {"},
		{Path: "internal/git/git.go", Line: 86, Text: "func run(ctx context.Context, dir string, args ...string) ([]byte, error) {"},
	}
	var got []string
	for _, m := range rankMatches(fffOrder, "func run") {
		got = append(got, m.Text[:strings.IndexByte(m.Text, '(')])
	}
	want := "func run func run func runPane func runThen func Run"
	if strings.Join(got, " ") != want {
		t.Fatalf("order = %q", got)
	}
}

func TestNamesMatch(t *testing.T) {
	for file, want := range map[string]bool{
		"internal/launch/launch_test.go": false, // only scattered letters of "funcrun"
		"internal/find/run.go":           false,
		"cmd/func_runner/main.go":        false, // "func_run" is not "funcrun"
		"src/funcrun.go":                 true,
	} {
		if got := namesMatch(file, "func run"); got != want {
			t.Errorf("namesMatch(%q) = %v, want %v", file, got, want)
		}
	}
	if !namesMatch("internal/outline/model.go", "outline/mod") {
		t.Error("a path fragment should count")
	}
}

func TestWholeWordsNotFooledByEarlierPartialMatch(t *testing.T) {
	// The first "run" is inside "runner"; the second stands alone.
	if got := tier("runner := run(x)", "run"); got != exactWord {
		t.Fatalf("tier = %d", got)
	}
}

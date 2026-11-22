package outline

import (
	"strings"
	"testing"

	"github.com/gdo33s/rig/internal/git"
)

func names(rows []row) string {
	var parts []string
	for _, r := range rows {
		s := strings.Repeat(" ", r.depth) + r.node.name
		if r.node.dir {
			s += "/"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "|")
}

func changed(added, deleted int) *git.Change {
	return &git.Change{Staged: git.None, Unstaged: 'M', Added: added, Deleted: deleted}
}

func TestBuildSortsFoldersFirstAndMergesChanges(t *testing.T) {
	files := []string{"z.go", "b/y.go", "a/x.go", "README"}
	changes := map[string]*git.Change{
		"a/x.go":   changed(3, 1),
		"a/new.go": {Staged: git.None, Unstaged: git.None, Untracked: true, Added: 10},
	}
	root := build(files, changes)

	got := names(flatten(root, map[string]bool{"a": true}))
	want := "a/| new.go| x.go|b/|README|z.go"
	if got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
	a := root.children[0]
	if a.changed != 2 || a.added != 13 || a.deleted != 1 || a.pending {
		t.Fatalf("rollup = %+v", a)
	}
}

func TestFoldersStartClosed(t *testing.T) {
	root := build([]string{"a/x.go", "b/y.go"}, map[string]*git.Change{"a/x.go": changed(1, 0)})
	got := names(flatten(root, map[string]bool{"b": true}))
	if want := "a/|b/| y.go"; got != want {
		t.Fatalf("rows = %q, want %q", got, want)
	}
}

func TestPendingCountsRollUp(t *testing.T) {
	root := build(nil, map[string]*git.Change{
		"d/e/f.txt": {Untracked: true, Added: -1},
	})
	if d := root.children[0]; !d.pending || d.children[0].changed != 1 {
		t.Fatalf("d = %+v", d)
	}
}

func TestSearchMatchesAllTermsChangedFirst(t *testing.T) {
	root := build(
		[]string{"cmd/rig/main.go", "internal/outline/model.go", "internal/outline/tree.go"},
		map[string]*git.Change{"internal/outline/tree.go": changed(1, 1)},
	)
	got := names(search(root, "OUT go"))
	if want := "tree.go|model.go"; got != want {
		t.Fatalf("hits = %q, want %q", got, want)
	}
}

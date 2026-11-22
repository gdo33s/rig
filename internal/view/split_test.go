package view

import (
	"testing"

	"github.com/gdo33s/rig/internal/git"
)

func marked(s string, m []bool) string {
	if m == nil {
		return "<none>"
	}
	out := []rune{}
	for i, r := range []rune(s) {
		if m[i] {
			out = append(out, r)
		} else {
			out = append(out, '.')
		}
	}
	return string(out)
}

func TestWordChanges(t *testing.T) {
	old, new := `	return { outlook, wind: 12 }`, `	return { outlook, windKph: 18 }`
	l, r := wordChanges(old, new)
	if got := marked(old, l); got != "...................wind..12.." {
		t.Errorf("old marks = %q", got)
	}
	if got := marked(new, r); got != "...................windKph..18.." {
		t.Errorf("new marks = %q", got)
	}
	// Lines with almost nothing in common get no word marks at all.
	if l, r := wordChanges("x := compute(a, b)", "// TODO: remove"); l != nil || r != nil {
		t.Errorf("unrelated lines marked: %q %q", marked("x := compute(a, b)", l), marked("// TODO: remove", r))
	}
}

func TestPairsZipRunsAndLeaveGaps(t *testing.T) {
	d := git.FileDiff{Lines: []git.DiffLine{
		{Kind: '@', Old: 1, New: 1},
		{Kind: ' ', Old: 1, New: 1, Text: "a"},
		{Kind: '-', Old: 2, Text: "b"},
		{Kind: '-', Old: 3, Text: "c"},
		{Kind: '+', New: 2, Text: "B"},
		{Kind: ' ', Old: 4, New: 3, Text: "d"},
		{Kind: '+', New: 4, Text: "e"},
	}}
	got := pairs(d)
	if len(got) != 6 {
		t.Fatalf("rows = %d", len(got))
	}
	if got[0].heading == nil || got[1].left != got[1].right {
		t.Fatalf("heading and context: %+v %+v", got[0], got[1])
	}
	if got[2].left.Text != "b" || got[2].right.Text != "B" {
		t.Fatalf("first pair: %+v", got[2])
	}
	if got[3].left.Text != "c" || got[3].right != nil {
		t.Fatalf("removed line left over: %+v", got[3])
	}
	if got[5].left != nil || got[5].right.Text != "e" {
		t.Fatalf("added line alone: %+v", got[5])
	}
}

package git

import (
	"context"
	"reflect"
	"testing"
)

func TestLineChanges(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "1\n2\n3\n4\n5\n6\n")
	f.git("add", ".")
	f.git("commit", "-qm", "base")
	f.write("a.txt", "1\nTWO\n3\nnew\n4\n6\n") // 2 modified, "new" added after 3, 5 removed

	marks, err := f.repo().LineChanges(context.Background(), "a.txt")
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]byte{2: LineModified, 4: LineAdded, 5: LineDeleted}
	if !reflect.DeepEqual(marks, want) {
		t.Fatalf("marks = %v, want %v", marks, want)
	}
}

func TestFileDiffWorkTreeAndCommit(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\ntwo\nthree\n")
	f.git("add", ".")
	f.git("commit", "-qm", "base")
	f.write("a.txt", "one\n2\nthree\n")
	ctx := context.Background()
	r := f.repo()

	d, err := r.FileDiff(ctx, "a.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	want := []DiffLine{
		{Kind: '@', Old: 1, New: 1},
		{Kind: ' ', Old: 1, New: 1, Text: "one"},
		{Kind: '-', Old: 2, Text: "two"},
		{Kind: '+', New: 2, Text: "2"},
		{Kind: ' ', Old: 3, New: 3, Text: "three"},
	}
	if !reflect.DeepEqual(d.Lines, want) {
		t.Fatalf("diff = %+v", d.Lines)
	}

	f.git("commit", "-qam", "change")
	log, _ := r.Log(ctx, 1)
	d, err = r.FileDiff(ctx, "a.txt", log[0].Hash)
	if err != nil || !reflect.DeepEqual(d.Lines, want) {
		t.Fatalf("commit diff = %+v, %v", d.Lines, err)
	}
}

func TestFileDiffUntrackedAndInitial(t *testing.T) {
	f := newFixture(t)
	f.write("new.txt", "a\nb\n")
	d, err := f.repo().FileDiff(context.Background(), "new.txt", "")
	if err != nil || len(d.Lines) != 3 || d.Lines[2] != (DiffLine{Kind: '+', New: 2, Text: "b"}) {
		t.Fatalf("untracked = %+v, %v", d, err)
	}
	f.git("add", "new.txt")
	d, err = f.repo().FileDiff(context.Background(), "new.txt", "")
	if err != nil || len(d.Lines) != 3 {
		t.Fatalf("staged before the first commit = %+v, %v", d, err)
	}
}

func TestHunkHeader(t *testing.T) {
	a, b, c, d, heading, ok := hunkHeader("@@ -12 +14,0 @@ func main() {")
	if !ok || a != 12 || b != 1 || c != 14 || d != 0 || heading != "func main() {" {
		t.Fatalf("got %d %d %d %d %q %v", a, b, c, d, heading, ok)
	}
}

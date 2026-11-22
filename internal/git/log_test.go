package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLogEmptyRepo(t *testing.T) {
	f := newFixture(t)
	log, err := f.repo().Log(context.Background(), 10)
	if err != nil || len(log) != 0 {
		t.Fatalf("log = %v, %v", log, err)
	}
}

func TestLogAndChanged(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "1\n2\n")
	f.write("old.txt", "keep this content\nacross the rename\n")
	f.git("add", ".")
	f.git("commit", "-qm", "first")
	f.write("a.txt", "1\nTWO\n3\n")
	f.git("mv", "old.txt", "new.txt")
	f.write("bin.dat", "\x00\x01")
	f.git("add", ".")
	f.git("commit", "-qm", "second: with spaces\tand tab")

	ctx := context.Background()
	r := f.repo()
	log, err := r.Log(ctx, 10)
	if err != nil || len(log) != 2 {
		t.Fatalf("log = %v, %v", log, err)
	}
	if log[0].Subject != "second: with spaces\tand tab" || len(log[0].Parents) != 1 || log[1].Subject != "first" {
		t.Fatalf("log = %+v", log)
	}

	files, err := r.Changed(ctx, log[0])
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]FileChange{}
	for _, fc := range files {
		got[fc.Path] = fc
	}
	if a := got["a.txt"]; a.Kind != 'M' || a.Added != 2 || a.Deleted != 1 {
		t.Fatalf("a.txt = %+v", a)
	}
	if n := got["new.txt"]; n.Kind != 'R' || n.From != "old.txt" {
		t.Fatalf("new.txt = %+v", n)
	}
	if b := got["bin.dat"]; b.Kind != 'A' || !b.Binary {
		t.Fatalf("bin.dat = %+v", b)
	}

	msg, err := r.Message(ctx, log[1].Hash)
	if err != nil || msg.Author != "rig" || msg.Email != "rig@example.com" || msg.Body != "" || msg.When.IsZero() {
		t.Fatalf("message = %+v, %v", msg, err)
	}

	root, err := r.Changed(ctx, log[1])
	if err != nil || len(root) != 2 {
		t.Fatalf("root commit files = %+v, %v", root, err)
	}
}

func TestTrackingAheadOfUpstream(t *testing.T) {
	remote := newFixture(t)
	remote.write("a.txt", "1\n")
	remote.git("add", ".")
	remote.git("commit", "-qm", "base")

	f := newFixture(t)
	f.git("remote", "add", "origin", remote.root)
	f.git("fetch", "-q", "origin")
	f.git("checkout", "-q", "-b", "main", "--track", "origin/main")
	f.write("b.txt", "2\n")
	f.git("add", ".")
	f.git("commit", "-qm", "local")

	ctx := context.Background()
	r := f.repo()
	tr, err := r.Tracking(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Branch != "main" || !tr.Remote || !tr.Upstream || tr.Base != "origin/main" || tr.Ahead != 1 || tr.Behind != 0 {
		t.Fatalf("tracking = %+v", tr)
	}
	log, _ := r.Log(ctx, 10)
	unpushed, err := r.Unpushed(ctx, tr.Base, 10)
	if err != nil || len(unpushed) != 1 || !unpushed[log[0].Hash] {
		t.Fatalf("unpushed = %v, %v", unpushed, err)
	}
}

func TestTrackingNoRemote(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "1\n")
	f.git("add", ".")
	f.git("commit", "-qm", "base")
	tr, err := f.repo().Tracking(context.Background())
	if err != nil || tr.Remote || tr.Base != "" || tr.Branch != "main" {
		t.Fatalf("tracking = %+v, %v", tr, err)
	}
}

func TestChangedShallowBoundary(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "1\n")
	f.git("add", ".")
	f.git("commit", "-qm", "base")
	r := f.repo()
	log, _ := r.Log(context.Background(), 1)
	if err := os.WriteFile(filepath.Join(f.root, ".git", "shallow"), []byte(log[0].Hash+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Changed(context.Background(), log[0]); err != ErrShallow {
		t.Fatalf("err = %v", err)
	}
}

func TestMessageBody(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "1\n")
	f.git("add", ".")
	f.git("commit", "-qm", "subject line", "-m", "First paragraph.\nStill first.", "-m", "Second paragraph.")
	r := f.repo()
	log, _ := r.Log(context.Background(), 1)
	msg, err := r.Message(context.Background(), log[0].Hash)
	if err != nil {
		t.Fatal(err)
	}
	if want := "First paragraph.\nStill first.\n\nSecond paragraph."; msg.Body != want {
		t.Fatalf("body = %q, want %q", msg.Body, want)
	}
}

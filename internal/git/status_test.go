package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// fixture is a throwaway repository with isolated git config.
type fixture struct {
	t    *testing.T
	root string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "rig")
	t.Setenv("GIT_AUTHOR_EMAIL", "rig@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "rig")
	t.Setenv("GIT_COMMITTER_EMAIL", "rig@example.com")
	f := &fixture{t: t, root: root}
	f.git("init", "-q", "-b", "main")
	return f
}

func (f *fixture) git(args ...string) {
	f.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = f.root
	if out, err := cmd.CombinedOutput(); err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func (f *fixture) write(path, content string) {
	f.t.Helper()
	full := filepath.Join(f.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) repo() Repo {
	f.t.Helper()
	r, err := Open(context.Background(), filepath.Join(f.root))
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

func TestStatusInitialRepo(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "one\ntwo\n")
	f.write("dir/b.txt", "x")
	f.git("add", "a.txt")

	st, err := f.repo().Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !st.Branch.Initial || st.Branch.Head != "main" {
		t.Fatalf("branch = %+v", st.Branch)
	}
	a := st.Changes["a.txt"]
	if a == nil || a.Staged != 'A' || a.Added != 2 || a.Deleted != 0 {
		t.Fatalf("a.txt = %+v", a)
	}
	b := st.Changes["dir/b.txt"]
	if b == nil || !b.Untracked || b.Added != 1 {
		t.Fatalf("dir/b.txt = %+v", b)
	}
}

func TestStatusModifiedRenamedDeleted(t *testing.T) {
	f := newFixture(t)
	f.write("keep.txt", "1\n2\n3\n")
	f.write("old name.txt", "same content here\nand more\n")
	f.write("gone.txt", "bye\n")
	f.git("add", ".")
	f.git("commit", "-qm", "init")

	f.write("keep.txt", "1\nTWO\n3\n4\n")
	f.git("mv", "old name.txt", "new name.txt")
	if err := os.Remove(filepath.Join(f.root, "gone.txt")); err != nil {
		t.Fatal(err)
	}

	st, err := f.repo().Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch.Initial {
		t.Fatal("expected a commit")
	}
	keep := st.Changes["keep.txt"]
	if keep == nil || keep.Unstaged != 'M' || keep.IsStaged() || keep.Added != 2 || keep.Deleted != 1 {
		t.Fatalf("keep.txt = %+v", keep)
	}
	moved := st.Changes["new name.txt"]
	if moved == nil || moved.Staged != 'R' || moved.From != "old name.txt" || moved.Added != 0 {
		t.Fatalf("new name.txt = %+v", moved)
	}
	gone := st.Changes["gone.txt"]
	if gone == nil || gone.Kind() != 'D' || gone.Deleted != 1 {
		t.Fatalf("gone.txt = %+v", gone)
	}
}

func TestStatusBinaryAndClean(t *testing.T) {
	f := newFixture(t)
	f.write("img.bin", "\x00\x01\x02")
	f.write("clean.txt", "ok\n")
	f.git("add", "clean.txt")
	f.git("commit", "-qm", "init")

	st, err := f.repo().Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Changes["clean.txt"]; ok {
		t.Fatal("clean file reported as changed")
	}
	if img := st.Changes["img.bin"]; img == nil || !img.Binary {
		t.Fatalf("img.bin = %+v", img)
	}
}

func TestFilesAndOpenFromSubdir(t *testing.T) {
	f := newFixture(t)
	f.write("src/main.go", "package main\n")
	f.git("add", ".")

	r, err := Open(context.Background(), filepath.Join(f.root, "src"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(f.root)
	if got, _ := filepath.EvalSymlinks(r.Root); got != want {
		t.Fatalf("root = %q, want %q", got, want)
	}
	files, err := r.Files(context.Background())
	if err != nil || len(files) != 1 || files[0] != "src/main.go" {
		t.Fatalf("files = %v, %v", files, err)
	}
}

func TestOpenOutsideRepo(t *testing.T) {
	if _, err := Open(context.Background(), t.TempDir()); err != ErrNotRepo {
		t.Fatalf("err = %v", err)
	}
}

func TestParseBranchAheadBehind(t *testing.T) {
	st := parseStatus([]byte("# branch.oid abc\x00# branch.head feat\x00# branch.upstream origin/feat\x00# branch.ab +3 -1\x00"))
	b := st.Branch
	if b.Head != "feat" || b.Upstream != "origin/feat" || b.Ahead != 3 || b.Behind != 1 {
		t.Fatalf("branch = %+v", b)
	}
}

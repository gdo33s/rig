package git

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestBranchDiffFeatureBranchAgainstLocalMain(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "1\n2\n3\n")
	f.write("gone.txt", "x\n")
	f.git("add", ".")
	f.git("commit", "-qm", "base")
	f.git("checkout", "-q", "-b", "feature")
	f.write("a.txt", "1\nTWO\n3\n4\n") // +2 -1
	f.write("b.txt", "new\nfile\n")    // +2, new
	f.git("add", ".")
	f.git("commit", "-qm", "work")
	f.git("rm", "-q", "gone.txt")   // -1, uncommitted
	f.write("untracked.txt", "u\n") // +1, new, untracked

	d, err := f.repo().BranchDiff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := BranchDiff{Branch: "feature", Base: "main", Commits: 1, Added: 5, Deleted: 2, Files: 4}
	if d != want {
		t.Fatalf("diff = %+v\n want %+v", d, want)
	}
}

func TestBranchDiffMainAgainstUpstream(t *testing.T) {
	remote := newFixture(t)
	remote.write("a.txt", "1\n")
	remote.git("add", ".")
	remote.git("commit", "-qm", "base")

	root := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", remote.root, root).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	f := &fixture{t: t, root: root}
	f.write("b.txt", "1\n2\n")
	f.git("add", ".")
	f.git("commit", "-qm", "unpushed")
	f.write("a.txt", "one\n")

	d, err := f.repo().BranchDiff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// On the default branch itself, the branch is what has not been pushed.
	want := BranchDiff{Branch: "main", Base: "origin/main", Commits: 1, Added: 3, Deleted: 1, Files: 2}
	if d != want {
		t.Fatalf("diff = %+v\n want %+v", d, want)
	}
}

func TestBranchDiffFeatureAgainstRemoteDefault(t *testing.T) {
	remote := newFixture(t)
	remote.write("a.txt", "1\n")
	remote.git("add", ".")
	remote.git("commit", "-qm", "base")
	root := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", remote.root, root).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	f := &fixture{t: t, root: root}
	f.git("checkout", "-q", "-b", "topic")
	f.write(filepath.Join("dir", "c.txt"), "c\n")
	f.git("add", ".")
	f.git("commit", "-qm", "topic work")

	d, err := f.repo().BranchDiff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Base != "origin/main" || d.Commits != 1 || d.Files != 1 || d.Added != 1 {
		t.Fatalf("diff = %+v", d)
	}
}

func TestBranchDiffNoBaseAndInitial(t *testing.T) {
	f := newFixture(t)
	f.write("a.txt", "1\n2\n")
	d, err := f.repo().BranchDiff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Base != "" || d.Files != 1 || d.Added != 2 {
		t.Fatalf("initial = %+v", d)
	}

	f.git("add", ".")
	f.git("commit", "-qm", "base")
	f.write("a.txt", "1\n")
	d, err = f.repo().BranchDiff(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if d.Base != "" || d.Commits != 0 || d.Files != 1 || d.Deleted != 1 {
		t.Fatalf("main with no remote = %+v", d)
	}
}

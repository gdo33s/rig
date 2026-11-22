package git

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
)

// BranchDiff is everything the current branch changes: its commits since
// it left its base, plus uncommitted and untracked work.
type BranchDiff struct {
	Branch  string // empty when detached
	Base    string // what it is compared with, such as origin/main; empty for HEAD
	Commits int    // commits since the base
	Added   int
	Deleted int
	Files   int // files touched
}

// BranchDiff compares the work tree with the point where the branch left
// its base. The base is the remote's default branch when on another
// branch, else the branch's upstream (so main shows what is unpushed),
// else a local main or master. With none of those it shows only
// uncommitted work.
func (r Repo) BranchDiff(ctx context.Context) (BranchDiff, error) {
	var d BranchDiff
	if out, err := r.run(ctx, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		d.Branch = strings.TrimSpace(string(out))
	}
	from := "HEAD"
	if !r.hasHead(ctx) {
		out, err := r.run(ctx, "hash-object", "-t", "tree", "/dev/null")
		if err != nil {
			return d, err
		}
		from = strings.TrimSpace(string(out))
	} else if d.Base = r.branchBase(ctx, d.Branch); d.Base != "" {
		out, err := r.run(ctx, "merge-base", "HEAD", d.Base)
		if err != nil {
			d.Base = "" // unrelated histories: fall back to uncommitted work
		} else {
			from = strings.TrimSpace(string(out))
			count, err := r.run(ctx, "rev-list", "--count", from+"..HEAD")
			if err != nil {
				return d, err
			}
			d.Commits, _ = strconv.Atoi(strings.TrimSpace(string(count)))
		}
	}

	names, err := r.run(ctx, "diff", "--name-status", "-z", "--no-ext-diff", from)
	if err != nil {
		return d, err
	}
	recs := splitNUL(names)
	for i := 0; i < len(recs); i++ {
		status := recs[i]
		if status == "" {
			continue
		}
		d.Files++
		if status[0] == 'R' || status[0] == 'C' {
			i += 2
		} else {
			i++
		}
	}
	counts, err := r.run(ctx, "diff", "--numstat", "-z", "--no-ext-diff", "--no-textconv", from)
	if err != nil {
		return d, err
	}
	eachNumstat(counts, func(_ string, added, deleted int, _ bool) {
		d.Added += added
		d.Deleted += deleted
	})

	untracked, err := r.run(ctx, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return d, err
	}
	for _, path := range splitNUL(untracked) {
		d.Files++
		if n, _ := countLines(filepath.Join(r.Root, path)); n > 0 {
			d.Added += n
		}
	}
	return d, nil
}

// branchBase picks the ref the branch is compared with, or "" for none.
func (r Repo) branchBase(ctx context.Context, branch string) string {
	ref := func(args ...string) string {
		out, err := r.run(ctx, args...)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if def := ref("symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); def != "" {
		if _, name, _ := strings.Cut(def, "/"); name != branch {
			return def
		}
	}
	if up := ref("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); up != "" {
		return up
	}
	for _, name := range []string{"main", "master"} {
		if name != branch && ref("rev-parse", "-q", "--verify", "refs/heads/"+name) != "" {
			return name
		}
	}
	return ""
}

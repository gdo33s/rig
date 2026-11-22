package git

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// None marks an empty side of a status pair, as porcelain v2 prints it.
const None = '.'

type Change struct {
	Staged    byte // M A D R C T U, or None
	Unstaged  byte
	Untracked bool
	Conflict  bool
	From      string // rename or copy source

	Added, Deleted int // lines against HEAD; -1 while unknown
	Binary         bool
}

// Kind is the single letter that best describes the change.
func (c *Change) Kind() byte {
	switch {
	case c.Conflict:
		return 'U'
	case c.Untracked:
		return '?'
	case c.Staged != None:
		return c.Staged
	default:
		return c.Unstaged
	}
}

func (c *Change) IsStaged() bool { return c.Staged != None && !c.Untracked }

type Branch struct {
	Head     string // branch name; empty when detached
	Initial  bool   // no commits yet
	Upstream string
	Ahead    int
	Behind   int
}

type Status struct {
	Branch  Branch
	Changes map[string]*Change
}

// Status reads the branch and every changed path, including untracked
// files, then fills in line counts against HEAD.
func (r Repo) Status(ctx context.Context) (*Status, error) {
	out, err := r.run(ctx, "status", "--porcelain=v2", "-z", "--branch", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	st := parseStatus(out)
	if err := r.fillCounts(ctx, st); err != nil {
		return nil, err
	}
	return st, nil
}

// Files lists every tracked path.
func (r Repo) Files(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "ls-files", "-z")
	if err != nil {
		return nil, err
	}
	return splitNUL(out), nil
}

// Project lists every file that belongs to the project: tracked ones and
// new ones git does not ignore.
func (r Repo) Project(ctx context.Context) ([]string, error) {
	out, err := r.run(ctx, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	return splitNUL(out), nil
}

func parseStatus(out []byte) *Status {
	st := &Status{Changes: map[string]*Change{}}
	records := splitNUL(out)
	for i := 0; i < len(records); i++ {
		rec := records[i]
		if len(rec) < 2 {
			continue
		}
		switch rec[0] {
		case '#':
			parseBranch(&st.Branch, rec[2:])
		case '1':
			if f, path, ok := fields(rec, 8); ok {
				st.Changes[path] = newChange(f[1])
			}
		case '2':
			if f, path, ok := fields(rec, 9); ok {
				c := newChange(f[1])
				if i+1 < len(records) {
					i++
					c.From = records[i]
				}
				st.Changes[path] = c
			}
		case 'u':
			if _, path, ok := fields(rec, 10); ok {
				st.Changes[path] = &Change{Staged: 'U', Unstaged: 'U', Conflict: true, Added: -1, Deleted: -1}
			}
		case '?':
			st.Changes[rec[2:]] = &Change{Staged: None, Unstaged: None, Untracked: true, Added: -1, Deleted: -1}
		}
	}
	return st
}

// fields splits a status record into n space-separated fields followed by
// a path, which may itself contain spaces.
func fields(rec string, n int) ([]string, string, bool) {
	parts := strings.SplitN(rec, " ", n+1)
	if len(parts) != n+1 {
		return nil, "", false
	}
	return parts[:n], parts[n], true
}

func newChange(xy string) *Change {
	c := &Change{Staged: None, Unstaged: None, Added: -1, Deleted: -1}
	if len(xy) == 2 {
		c.Staged, c.Unstaged = xy[0], xy[1]
	}
	return c
}

func parseBranch(b *Branch, line string) {
	key, val, _ := strings.Cut(line, " ")
	switch key {
	case "branch.oid":
		b.Initial = val == "(initial)"
	case "branch.head":
		if val != "(detached)" {
			b.Head = val
		}
	case "branch.upstream":
		b.Upstream = val
	case "branch.ab":
		a, bh, _ := strings.Cut(val, " ")
		b.Ahead, _ = strconv.Atoi(strings.TrimPrefix(a, "+"))
		b.Behind, _ = strconv.Atoi(strings.TrimPrefix(bh, "-"))
	}
}

// fillCounts sets line counts for tracked changes from one diff against
// HEAD, and counts untracked files directly.
func (r Repo) fillCounts(ctx context.Context, st *Status) error {
	if len(st.Changes) == 0 {
		return nil
	}
	base := "HEAD"
	if st.Branch.Initial {
		out, err := r.run(ctx, "hash-object", "-t", "tree", "/dev/null")
		if err != nil {
			return err
		}
		base = strings.TrimSpace(string(out))
	}
	out, err := r.run(ctx, "diff", base, "--numstat", "-z", "--no-ext-diff", "--no-textconv")
	if err != nil {
		return err
	}
	applyNumstat(st, out)
	for path, c := range st.Changes {
		if c.Untracked {
			c.Added, c.Binary = countLines(filepath.Join(r.Root, path))
			c.Deleted = 0
		}
	}
	return nil
}

func applyNumstat(st *Status, out []byte) {
	eachNumstat(out, func(path string, added, deleted int, binary bool) {
		if c, ok := st.Changes[path]; ok {
			c.Added, c.Deleted, c.Binary = added, deleted, binary
		}
	})
	// Changes that the diff did not mention (a mode-only change, say) have
	// nothing to count.
	for _, c := range st.Changes {
		if !c.Untracked && c.Added < 0 {
			c.Added, c.Deleted = 0, 0
		}
	}
}

// eachNumstat reads `--numstat -z` output. A plain entry is
// "added\tdeleted\tpath\0"; a rename leaves the path empty and follows it
// with "from\0to\0". Binary files count as zero lines.
func eachNumstat(out []byte, fn func(path string, added, deleted int, binary bool)) {
	records := splitNUL(out)
	for i := 0; i < len(records); i++ {
		parts := strings.SplitN(records[i], "\t", 3)
		if len(parts) != 3 {
			continue
		}
		path := parts[2]
		if path == "" && i+2 < len(records) {
			path = records[i+2]
			i += 2
		}
		if parts[0] == "-" {
			fn(path, 0, 0, true)
			continue
		}
		added, _ := strconv.Atoi(parts[0])
		deleted, _ := strconv.Atoi(parts[1])
		fn(path, added, deleted, false)
	}
}

const maxCountBytes = 4 << 20

// countLines counts newlines in an untracked file the way git would count
// added lines. Files that look binary, or are too big to be worth reading,
// report -1 lines.
func countLines(path string) (int, bool) {
	f, err := os.Open(path)
	if err != nil {
		return -1, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxCountBytes {
		return -1, false
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return -1, false
	}
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	if bytes.IndexByte(head, 0) >= 0 {
		return 0, true
	}
	n := bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		n++
	}
	return n, false
}

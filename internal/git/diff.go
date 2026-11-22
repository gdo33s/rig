package git

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DiffLine is one line of a unified diff: a hunk start ('@'), or a line
// kept (' '), added ('+') or removed ('-'), with its numbers on the old and
// new sides (0 where it has none).
type DiffLine struct {
	Kind     byte
	Old, New int
	Text     string
}

type FileDiff struct {
	Lines  []DiffLine
	Binary bool
}

// FileDiff is path's change: in commit against its first parent, or with
// commit empty, the work tree against HEAD. An untracked file is all
// added.
func (r Repo) FileDiff(ctx context.Context, path, commit string) (FileDiff, error) {
	var args []string
	if commit != "" {
		args = []string{"show", "--format=", "--diff-merges=first-parent", "--no-ext-diff", "--no-textconv", "-U3", commit, "--", path}
	} else {
		base, err := r.base(ctx)
		if err != nil {
			return FileDiff{}, err
		}
		if !r.tracked(ctx, path) {
			return r.untrackedDiff(path)
		}
		args = []string{"diff", "--no-ext-diff", "--no-textconv", "-U3", base, "--", path}
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return FileDiff{}, err
	}
	return ParseDiff(string(out)), nil
}

// base is HEAD, or the empty tree before the first commit.
func (r Repo) base(ctx context.Context) (string, error) {
	if r.hasHead(ctx) {
		return "HEAD", nil
	}
	out, err := r.run(ctx, "hash-object", "-t", "tree", "/dev/null")
	return strings.TrimSpace(string(out)), err
}

func (r Repo) tracked(ctx context.Context, path string) bool {
	_, err := r.run(ctx, "ls-files", "--error-unmatch", "--", path)
	return err == nil
}

func (r Repo) untrackedDiff(path string) (FileDiff, error) {
	data, err := os.ReadFile(filepath.Join(r.Root, path))
	if err != nil {
		return FileDiff{}, err
	}
	if isBinary(data) {
		return FileDiff{Binary: true}, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	d := FileDiff{Lines: []DiffLine{{Kind: '@', New: 1}}}
	for i, l := range lines {
		d.Lines = append(d.Lines, DiffLine{Kind: '+', New: i + 1, Text: l})
	}
	return d, nil
}

// ParseDiff reads unified diff text, skipping its file headers.
func ParseDiff(text string) FileDiff {
	var d FileDiff
	var old, nw int
	inHunk := false
	for line := range strings.SplitSeq(text, "\n") {
		switch {
		case strings.HasPrefix(line, "Binary files "):
			d.Binary = true
		case strings.HasPrefix(line, "@@"):
			a, _, c, _, heading, ok := hunkHeader(line)
			if !ok {
				continue
			}
			old, nw, inHunk = a, c, true
			d.Lines = append(d.Lines, DiffLine{Kind: '@', Old: a, New: c, Text: heading})
		case !inHunk:
		case strings.HasPrefix(line, "+"):
			d.Lines = append(d.Lines, DiffLine{Kind: '+', New: nw, Text: line[1:]})
			nw++
		case strings.HasPrefix(line, "-"):
			d.Lines = append(d.Lines, DiffLine{Kind: '-', Old: old, Text: line[1:]})
			old++
		case strings.HasPrefix(line, " "):
			d.Lines = append(d.Lines, DiffLine{Kind: ' ', Old: old, New: nw, Text: line[1:]})
			old++
			nw++
		}
	}
	return d
}

// hunkHeader reads "@@ -a,b +c,d @@ heading". A count left out is 1.
func hunkHeader(line string) (a, b, c, d int, heading string, ok bool) {
	rest, ok := strings.CutPrefix(line, "@@ -")
	if !ok {
		return
	}
	ranges, heading, found := strings.Cut(rest, " @@")
	if !found {
		return 0, 0, 0, 0, "", false
	}
	oldR, newR, found := strings.Cut(ranges, " +")
	if !found {
		return 0, 0, 0, 0, "", false
	}
	a, b = span(oldR)
	c, d = span(newR)
	return a, b, c, d, strings.TrimSpace(heading), true
}

func span(s string) (start, count int) {
	first, second, has := strings.Cut(s, ",")
	start, _ = strconv.Atoi(first)
	count = 1
	if has {
		count, _ = strconv.Atoi(second)
	}
	return start, count
}

// Line changes in the work tree against HEAD.
const (
	LineAdded    = 'A'
	LineModified = 'M'
	LineDeleted  = 'D' // lines were removed just after this one
)

// LineChanges marks the lines of path that differ from HEAD. An untracked
// file has no marks: all of it is new.
func (r Repo) LineChanges(ctx context.Context, path string) (map[int]byte, error) {
	base, err := r.base(ctx)
	if err != nil {
		return nil, err
	}
	out, err := r.run(ctx, "diff", "--no-ext-diff", "--no-textconv", "-U0", base, "--", path)
	if err != nil {
		return nil, err
	}
	marks := map[int]byte{}
	for line := range strings.SplitSeq(string(out), "\n") {
		_, removed, start, added, _, ok := hunkHeader(line)
		if !ok {
			continue
		}
		switch {
		case added == 0:
			marks[max(start, 1)] = LineDeleted
		case removed == 0:
			for n := start; n < start+added; n++ {
				marks[n] = LineAdded
			}
		default:
			for n := start; n < start+added; n++ {
				marks[n] = LineModified
			}
		}
	}
	return marks, nil
}

func isBinary(data []byte) bool {
	head := data
	if len(head) > 8000 {
		head = head[:8000]
	}
	return strings.IndexByte(string(head), 0) >= 0
}

// Contents reads path as it was in rev, or "" where rev does not have it.
func (r Repo) Contents(ctx context.Context, rev, path string) string {
	out, err := r.run(ctx, "show", rev+":"+path)
	if err != nil {
		return ""
	}
	return string(out)
}

// Sides are the two versions a FileDiff compares, for highlighting each in
// full: the old one, and the new one (the work tree when commit is empty).
func (r Repo) Sides(ctx context.Context, path, commit string) (old, new string) {
	if commit != "" {
		return r.Contents(ctx, commit+"^", path), r.Contents(ctx, commit, path)
	}
	if r.hasHead(ctx) {
		old = r.Contents(ctx, "HEAD", path)
	}
	data, _ := os.ReadFile(filepath.Join(r.Root, path))
	return old, string(data)
}

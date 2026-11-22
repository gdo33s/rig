package git

import (
	"bufio"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Commit struct {
	Hash    string
	Short   string
	Parents []string
	Author  string
	When    time.Time
	Subject string
}

const maxSubject = 4096

// Log lists up to n commits reachable from HEAD, newest first. A repository
// with no commits has an empty log.
func (r Repo) Log(ctx context.Context, n int) ([]Commit, error) {
	if !r.hasHead(ctx) {
		return nil, nil
	}
	out, err := r.run(ctx, "log", "-n", strconv.Itoa(n), "-z",
		"--format=%H%x1f%h%x1f%P%x1f%an%x1f%at%x1f%s")
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for _, rec := range splitNUL(out) {
		f := strings.SplitN(strings.TrimPrefix(rec, "\n"), "\x1f", 6)
		if len(f) != 6 {
			continue
		}
		secs, _ := strconv.ParseInt(f[4], 10, 64)
		subject := f[5]
		if len(subject) > maxSubject {
			subject = subject[:maxSubject] + "…"
		}
		commits = append(commits, Commit{
			Hash: f[0], Short: f[1], Parents: strings.Fields(f[2]),
			Author: f[3], When: time.Unix(secs, 0), Subject: subject,
		})
	}
	return commits, nil
}

func (r Repo) hasHead(ctx context.Context) bool {
	_, err := r.run(ctx, "rev-parse", "-q", "--verify", "HEAD^{commit}")
	return err == nil
}

// Tracking describes where the current branch stands against its remote.
type Tracking struct {
	Branch   string // empty when detached
	Remote   bool   // any remote is configured
	Base     string // the ref compared against, empty if none
	Upstream bool   // Base is the branch's own upstream, not the remote default
	Ahead    int
	Behind   int
}

// Tracking compares HEAD with the branch's upstream or, when it has none,
// with the remote's default branch.
func (r Repo) Tracking(ctx context.Context) (Tracking, error) {
	var t Tracking
	if out, err := r.run(ctx, "symbolic-ref", "-q", "--short", "HEAD"); err == nil {
		t.Branch = strings.TrimSpace(string(out))
	}
	remotes, err := r.run(ctx, "remote")
	if err != nil {
		return t, err
	}
	t.Remote = strings.TrimSpace(string(remotes)) != ""
	if !t.Remote || !r.hasHead(ctx) {
		return t, nil
	}
	if out, err := r.run(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); err == nil {
		t.Base, t.Upstream = strings.TrimSpace(string(out)), true
	} else if out, err := r.run(ctx, "symbolic-ref", "-q", "--short", "refs/remotes/origin/HEAD"); err == nil {
		t.Base = strings.TrimSpace(string(out))
	}
	if t.Base == "" {
		return t, nil
	}
	out, err := r.run(ctx, "rev-list", "--left-right", "--count", "HEAD..."+t.Base)
	if err != nil {
		return t, err
	}
	ahead, behind, _ := strings.Cut(strings.TrimSpace(string(out)), "\t")
	t.Ahead, _ = strconv.Atoi(ahead)
	t.Behind, _ = strconv.Atoi(behind)
	return t, nil
}

// Unpushed lists up to n commits in HEAD that base does not have.
func (r Repo) Unpushed(ctx context.Context, base string, n int) (map[string]bool, error) {
	if base == "" {
		return nil, nil
	}
	out, err := r.run(ctx, "rev-list", "-n", strconv.Itoa(n), "HEAD", "--not", base)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, h := range strings.Fields(string(out)) {
		set[h] = true
	}
	return set, nil
}

// FileChange is one file a commit touched, compared with its first parent.
type FileChange struct {
	Path           string
	From           string // rename or copy source
	Kind           byte   // A M D R C T
	Added, Deleted int
	Binary         bool
}

// ErrShallow means the commit's parent was cut off by a shallow clone, so
// its changes cannot be read.
var ErrShallow = errors.New("shallow history: parent unavailable")

// Changed lists the files c changed against its first parent.
func (r Repo) Changed(ctx context.Context, c Commit) ([]FileChange, error) {
	if r.shallowBoundary(c.Hash) {
		return nil, ErrShallow
	}
	base := []string{"--root", c.Hash}
	if len(c.Parents) > 0 {
		base = []string{c.Parents[0], c.Hash}
	}
	args := []string{"diff-tree", "-r", "-z", "-M", "--no-commit-id", "--no-ext-diff", "--no-textconv"}
	names, err := r.run(ctx, append(append(args, "--name-status"), base...)...)
	if err != nil {
		return nil, err
	}
	counts, err := r.run(ctx, append(append(args, "--numstat"), base...)...)
	if err != nil {
		return nil, err
	}

	var files []FileChange
	index := map[string]int{}
	recs := splitNUL(names)
	for i := 0; i < len(recs); i++ {
		status := recs[i]
		if status == "" || i+1 >= len(recs) {
			continue
		}
		fc := FileChange{Kind: status[0]}
		if fc.Kind == 'R' || fc.Kind == 'C' {
			if i+2 >= len(recs) {
				break
			}
			fc.From, fc.Path = recs[i+1], recs[i+2]
			i += 2
		} else {
			fc.Path = recs[i+1]
			i++
		}
		index[fc.Path] = len(files)
		files = append(files, fc)
	}
	eachNumstat(counts, func(path string, added, deleted int, binary bool) {
		if i, ok := index[path]; ok {
			files[i].Added, files[i].Deleted, files[i].Binary = added, deleted, binary
		}
	})
	return files, nil
}

// shallowBoundary reports whether hash is listed in .git/shallow, where a
// shallow clone records the commits whose parents it does not have.
func (r Repo) shallowBoundary(hash string) bool {
	gitDir, err := r.gitDir()
	if err != nil {
		return false
	}
	f, err := os.Open(filepath.Join(gitDir, "shallow"))
	if err != nil {
		return false
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	for scan.Scan() {
		if strings.TrimSpace(scan.Text()) == hash {
			return true
		}
	}
	return false
}

// Message is a commit's author, date and the body under its subject.
type Message struct {
	Author string
	Email  string
	When   time.Time
	Body   string
}

// Message reads the full message of the commit hash.
func (r Repo) Message(ctx context.Context, hash string) (Message, error) {
	out, err := r.run(ctx, "show", "-s", "--format=%an%x00%ae%x00%at%x00%b", hash)
	if err != nil {
		return Message{}, err
	}
	f := strings.SplitN(string(out), "\x00", 4)
	if len(f) != 4 {
		return Message{}, errors.New("git show: unexpected output")
	}
	secs, _ := strconv.ParseInt(f[2], 10, 64)
	return Message{Author: f[0], Email: f[1], When: time.Unix(secs, 0), Body: strings.TrimRight(f[3], "\n")}, nil
}

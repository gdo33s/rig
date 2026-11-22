// Package git reads repository state by running the git binary.
//
// Every command runs read-only in spirit: optional locks are disabled so a
// status never takes index.lock, and fsmonitor, external diff drivers and
// text converters are turned off so a repository cannot run code through us.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Repo struct {
	Root string
}

var ErrNotRepo = errors.New("not a git repository")

// Open finds the work tree that contains dir.
func Open(ctx context.Context, dir string) (Repo, error) {
	out, err := run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return Repo{}, ErrNotRepo
	}
	return Repo{Root: strings.TrimRight(string(out), "\n")}, nil
}

// Locked reports whether another git process holds the index lock.
func (r Repo) Locked() bool {
	gitDir, err := r.gitDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(gitDir, "index.lock"))
	return err == nil
}

// IndexStamp changes whenever the index is rewritten, which is when the
// tracked file list can change.
func (r Repo) IndexStamp() string {
	gitDir, err := r.gitDir()
	if err != nil {
		return ""
	}
	info, err := os.Stat(filepath.Join(gitDir, "index"))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
}

func (r Repo) gitDir() (string, error) {
	dot := filepath.Join(r.Root, ".git")
	info, err := os.Stat(dot)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return dot, nil
	}
	// A linked worktree or submodule: .git is a file pointing elsewhere.
	data, err := os.ReadFile(dot)
	if err != nil {
		return "", err
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok {
		return "", ErrNotRepo
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(r.Root, target)
	}
	return target, nil
}

func (r Repo) run(ctx context.Context, args ...string) ([]byte, error) {
	return run(ctx, r.Root, args...)
}

func run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	full := append([]string{
		"-c", "core.fsmonitor=false",
		"-c", "core.quotepath=off",
		"-c", "color.ui=never",
		"-c", "diff.external=",
	}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_NO_LAZY_FETCH=1",
		"LC_ALL=C",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return nil, fmt.Errorf("git %s: %w", args[0], err)
		}
		return nil, fmt.Errorf("git %s: %s", args[0], firstLine(msg))
	}
	return stdout.Bytes(), nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// splitNUL splits -z output, dropping the empty trailing record.
func splitNUL(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	parts := strings.Split(string(b), "\x00")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

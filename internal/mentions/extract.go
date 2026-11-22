// Package mentions lists the files a terminal has mentioned, such as the
// paths Claude Code prints as it reads, edits and explains, newest first,
// and opens the chosen one in rig's viewer.
package mentions

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Mention is a file named in a terminal, with the line it was named at,
// if any. Path is relative to Root.
type Mention struct {
	Root string
	Path string
	Line int
}

// Index answers which files a piece of text can mean: Files are the
// project's files relative to Root, for names given without their folder.
type Index struct {
	Root  string // the project, where rig view looks things up
	Cwd   string // where relative paths in the text start
	Files []string

	byName map[string][]string
}

// candidate is a word that could be a path, with an optional :line, :line:col
// or #Lline after it.
var candidate = regexp.MustCompile(`([\w~./@+-]+)(?::(\d+)(?::\d+)?|#L(\d+))?`)

// Extract finds the files text mentions, the most recently mentioned first,
// each once. A mention with a line number wins over a later one without.
func (ix *Index) Extract(text string) []Mention {
	type seen struct {
		m    Mention
		last int
	}
	found := map[string]*seen{}
	cache := map[string]*Mention{}
	order := 0
	for _, loc := range candidate.FindAllStringSubmatchIndex(text, -1) {
		word := strings.TrimRight(text[loc[2]:loc[3]], ".-")
		line := 0
		for g := 2; g <= 3; g++ {
			if loc[2*g] >= 0 {
				line, _ = strconv.Atoi(text[loc[2*g]:loc[2*g+1]])
			}
		}
		if !plausible(word) {
			continue
		}
		m, ok := cache[word]
		if !ok {
			m = ix.resolve(word)
			cache[word] = m
		}
		if m == nil {
			continue
		}
		order++
		key := filepath.Join(m.Root, m.Path)
		s := found[key]
		if s == nil {
			s = &seen{m: *m}
			found[key] = s
		}
		s.last = order
		if line > 0 {
			s.m.Line = line
		}
	}
	out := make([]*seen, 0, len(found))
	for _, s := range found {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].last > out[j].last })
	ms := make([]Mention, len(out))
	for i, s := range out {
		ms[i] = s.m
	}
	return ms
}

// plausible rules out words that cannot be a file name: those with no
// folder or extension, and the hosts and paths of URLs.
func plausible(w string) bool {
	if len(w) < 3 || strings.HasPrefix(w, "//") || strings.Contains(w, "...") {
		return false
	}
	return strings.ContainsAny(w, "/.") && strings.Trim(w, "./~") != ""
}

// resolve finds the file word names: a path from the folder the text was
// written in, from the project, or from home; else a file in the project
// that is the only one ending with word.
func (ix *Index) resolve(word string) *Mention {
	p := word
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil
		}
		p = filepath.Join(home, p[2:])
	}
	if filepath.IsAbs(p) {
		if m := ix.file(p); m != nil {
			return m
		}
		// A path shortened at the front, as "…/view/split.go", reads as
		// absolute once the ellipsis is dropped.
		if strings.Count(p, "/") < 2 {
			return nil
		}
		return ix.bySuffix(strings.TrimPrefix(p, "/"))
	}
	for _, base := range []string{ix.Cwd, ix.Root} {
		if base == "" {
			continue
		}
		if m := ix.file(filepath.Join(base, p)); m != nil {
			return m
		}
	}
	if strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
		return nil
	}
	return ix.bySuffix(path.Clean(p))
}

// file is the regular file at abs, relative to the project when inside it.
func (ix *Index) file(abs string) *Mention {
	info, err := os.Stat(abs)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	abs = filepath.Clean(abs)
	if ix.Root != "" {
		if rel, err := filepath.Rel(ix.Root, abs); err == nil && !strings.HasPrefix(rel, "..") {
			return &Mention{Root: ix.Root, Path: rel}
		}
	}
	return &Mention{Root: filepath.Dir(abs), Path: filepath.Base(abs)}
}

// bySuffix is the one project file whose path ends with p, as "split.go"
// or "view/split.go" name internal/view/split.go. A name shared by several
// files means none of them.
func (ix *Index) bySuffix(p string) *Mention {
	if ix.byName == nil {
		ix.byName = map[string][]string{}
		for _, f := range ix.Files {
			name := path.Base(f)
			ix.byName[name] = append(ix.byName[name], f)
		}
	}
	var match string
	for _, f := range ix.byName[path.Base(p)] {
		if f == p || strings.HasSuffix(f, "/"+p) {
			if match != "" {
				return nil
			}
			match = f
		}
	}
	if match == "" {
		return nil
	}
	return ix.file(filepath.Join(ix.Root, filepath.FromSlash(match)))
}

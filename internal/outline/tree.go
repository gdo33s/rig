package outline

import (
	"sort"
	"strings"

	"github.com/gdo33s/rig/internal/git"
)

type node struct {
	name     string
	path     string
	dir      bool
	children []*node
	change   *git.Change // files only

	// Folder rollups.
	changed        int
	staged         bool
	added, deleted int
	pending        bool // some line count is not known yet
}

// build turns flat paths into a tree, folders before files, each sorted by
// name. Changed paths that are not tracked (untracked, or staged deletions)
// are merged in so every change is visible.
func build(files []string, changes map[string]*git.Change) *node {
	root := &node{dir: true}
	dirs := map[string]*node{"": root}

	var add func(path string) *node
	add = func(path string) *node {
		if n, ok := dirs[path]; ok {
			return n
		}
		parentPath, name := splitPath(path)
		parent := add(parentPath)
		n := &node{name: name, path: path, dir: true}
		parent.children = append(parent.children, n)
		dirs[path] = n
		return n
	}

	seen := make(map[string]bool, len(files))
	insert := func(path string) {
		if seen[path] || path == "" {
			return
		}
		seen[path] = true
		parentPath, name := splitPath(path)
		add(parentPath).children = append(add(parentPath).children,
			&node{name: name, path: path, change: changes[path]})
	}
	for _, f := range files {
		insert(f)
	}
	for p := range changes {
		insert(p)
	}
	finish(root)
	return root
}

func splitPath(path string) (dir, name string) {
	i := strings.LastIndexByte(path, '/')
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}

// finish sorts children and rolls change totals up into folders.
func finish(n *node) {
	sort.Slice(n.children, func(i, j int) bool {
		a, b := n.children[i], n.children[j]
		if a.dir != b.dir {
			return a.dir
		}
		return strings.ToLower(a.name) < strings.ToLower(b.name)
	})
	for _, c := range n.children {
		if c.dir {
			finish(c)
			n.changed += c.changed
			n.staged = n.staged || c.staged
			n.added += c.added
			n.deleted += c.deleted
			n.pending = n.pending || c.pending
			continue
		}
		if ch := c.change; ch != nil {
			n.changed++
			n.staged = n.staged || ch.IsStaged()
			if ch.Added < 0 {
				n.pending = true
			} else {
				n.added += ch.Added
				n.deleted += ch.Deleted
			}
		}
	}
}

type row struct {
	node  *node
	depth int
	open  bool
}

// flatten lists the visible rows. Folders start closed; toggled holds the
// ones the user opened.
func flatten(root *node, toggled map[string]bool) []row {
	var rows []row
	var walk func(n *node, depth int)
	walk = func(n *node, depth int) {
		for _, c := range n.children {
			open := c.dir && toggled[c.path]
			rows = append(rows, row{node: c, depth: depth, open: open})
			if open {
				walk(c, depth+1)
			}
		}
	}
	walk(root, 0)
	return rows
}

// search lists files whose path contains every space-separated term,
// ignoring case. Changed files come first.
func search(root *node, query string) []row {
	terms := strings.Fields(strings.ToLower(query))
	var hits []row
	var walk func(n *node)
	walk = func(n *node) {
		for _, c := range n.children {
			if c.dir {
				walk(c)
				continue
			}
			lower := strings.ToLower(c.path)
			match := true
			for _, t := range terms {
				if !strings.Contains(lower, t) {
					match = false
					break
				}
			}
			if match {
				hits = append(hits, row{node: c})
			}
		}
	}
	walk(root)
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].node.change != nil && hits[j].node.change == nil
	})
	return hits
}

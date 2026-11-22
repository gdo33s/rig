// Package fff searches a project with fff (github.com/dmtrKovalenko/fff)
// through its MCP server, fff-mcp, run as a child process and spoken to as
// JSON-RPC over its stdin and stdout. Nothing goes over the network: rig
// turns off fff-mcp's update check.
package fff

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// ErrNotInstalled means fff-mcp is not on PATH or in ~/.local/bin.
var ErrNotInstalled = errors.New("fff-mcp is not installed")

// Client is one running fff-mcp, indexing one directory. Calls are made one
// at a time.
type Client struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	in   io.WriteCloser
	out  *bufio.Reader
	next int
}

// Binary finds fff-mcp.
func Binary() (string, error) {
	if p, err := exec.LookPath("fff-mcp"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	p := filepath.Join(home, ".local", "bin", "fff-mcp")
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p, nil
	}
	return "", ErrNotInstalled
}

// stateDir holds fff's ranking history and logs for rig.
func stateDir() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(dir) {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "rig", "fff")
}

// keepLogs is how many of fff-mcp's per-start log files are kept.
const keepLogs = 5

// Start runs fff-mcp on root and completes the MCP handshake.
func Start(root string) (*Client, error) {
	bin, err := Binary()
	if err != nil {
		return nil, err
	}
	state := stateDir()
	if err := os.MkdirAll(state, 0o700); err != nil {
		return nil, err
	}
	pruneLogs(state)
	cmd := exec.Command(bin,
		"--no-update-check",
		"--frecency-db", filepath.Join(state, "frecency.db"),
		"--history-db", filepath.Join(state, "history.db"),
		"--log-file", filepath.Join(state, "fff_mcp.log"),
		"--log-level", "warn",
		root)
	cmd.Dir = root
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	c := &Client{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<20)}
	_, err = c.call("initialize", map[string]any{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "rig", "version": "0"},
	})
	if err == nil {
		err = c.send(map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	}
	if err != nil {
		c.Close()
		return nil, fmt.Errorf("fff-mcp: %w", err)
	}
	return c, nil
}

// pruneLogs deletes all but the newest of fff-mcp's log files, since it
// writes a new one every time it starts.
func pruneLogs(dir string) {
	logs, _ := filepath.Glob(filepath.Join(dir, "fff_mcp+*.log"))
	if len(logs) <= keepLogs {
		return
	}
	sort.Slice(logs, func(i, j int) bool {
		a, _ := os.Stat(logs[i])
		b, _ := os.Stat(logs[j])
		return a != nil && b != nil && a.ModTime().After(b.ModTime())
	})
	for _, old := range logs[keepLogs:] {
		os.Remove(old)
	}
}

// Close stops fff-mcp.
func (c *Client) Close() {
	c.in.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill()
	}
	c.cmd.Wait()
}

func (c *Client) send(msg any) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	_, err = c.in.Write(append(data, '\n'))
	return err
}

type response struct {
	ID     *int            `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// call sends one request and waits for its answer, skipping anything else
// the server says meanwhile.
func (c *Client) call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.next++
	id := c.next
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	for {
		line, err := c.out.ReadBytes('\n')
		if err != nil {
			return nil, err
		}
		var r response
		if json.Unmarshal(line, &r) != nil || r.ID == nil || *r.ID != id {
			continue
		}
		if r.Error != nil {
			return nil, errors.New(r.Error.Message)
		}
		return r.Result, nil
	}
}

// tool calls an fff tool and returns its text answer.
func (c *Client) tool(name string, args map[string]any) (string, error) {
	raw, err := c.call("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", err
	}
	var res struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, c := range res.Content {
		text.WriteString(c.Text)
	}
	if res.IsError {
		return "", errors.New(strings.TrimSpace(text.String()))
	}
	return text.String(), nil
}

// Files finds up to max files by fuzzy name, best first. An empty query
// lists the files fff ranks highest: recently used and changed ones.
func (c *Client) Files(query string, max int) ([]string, error) {
	text, err := c.tool("find_files", map[string]any{"query": query, "maxResults": max})
	if err != nil {
		return nil, err
	}
	return parseFiles(text), nil
}

// Match is one line that matched a text search.
type Match struct {
	Path string
	Line int
	Text string
}

// Grep finds up to max lines containing query, best first.
func (c *Client) Grep(query string, max int) ([]Match, error) {
	text, err := c.tool("grep", map[string]any{"query": query, "maxResults": max, "context": 0})
	if err != nil {
		return nil, err
	}
	return parseGrep(text), nil
}

// stripTags removes the notes fff adds after a path for agents, as in
// "src/a.go - frequent git:untracked" or "src/a.go git:staged_modified".
func stripTags(line string) string {
	for {
		i := strings.LastIndexByte(line, ' ')
		if i < 0 || !isTag(line[i+1:]) {
			break
		}
		line = line[:i]
	}
	return strings.TrimSuffix(line, " -")
}

// isTag reports one of fff's notes: frequent, recent, git:modified and the
// like. A path's own words are not tags unless fff wrote them that way.
func isTag(word string) bool {
	if word == "frequent" || word == "recent" {
		return true
	}
	rest, ok := strings.CutPrefix(word, "git:")
	if !ok || rest == "" {
		return false
	}
	for _, r := range rest {
		if !(r >= 'a' && r <= 'z' || r == '_') {
			return false
		}
	}
	return true
}

// isHint reports the advice fff gives agents, such as "→ Read x (best
// match)".
func isHint(line string) bool { return strings.HasPrefix(line, "→") }

// parseFiles reads find_files' answer: a count line, one path per line,
// then a cursor line, with perhaps a hint first.
func parseFiles(text string) []string {
	var files []string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || isSummary(line) || isHint(line) {
			continue
		}
		files = append(files, stripTags(line))
	}
	return files
}

// parseGrep reads grep's answer: a file path, then its matches as
// "  12: text", with context lines as "  13| text" and hints that start
// with an arrow.
func parseGrep(text string) []Match {
	var matches []Match
	path := ""
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || isSummary(line) || isHint(line) {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			path = line
			continue
		}
		num, rest, ok := strings.Cut(strings.TrimLeft(line, " "), ": ")
		if !ok {
			// "12: " with an empty line keeps no space after the colon.
			num, rest, ok = strings.Cut(strings.TrimLeft(line, " "), ":")
		}
		n, err := strconv.Atoi(num)
		if !ok || err != nil || path == "" {
			continue // a context line, "13| text"
		}
		matches = append(matches, Match{Path: path, Line: n, Text: rest})
	}
	return matches
}

// isSummary reports fff's bookkeeping lines: "5/29 matches", "4/7 matches
// shown", "cursor: 2", "No matches".
func isSummary(line string) bool {
	if strings.HasPrefix(line, "cursor:") || strings.HasPrefix(strings.ToLower(line), "no ") {
		return true
	}
	first, rest, _ := strings.Cut(line, " ")
	if a, b, ok := strings.Cut(first, "/"); ok {
		_, errA := strconv.Atoi(a)
		_, errB := strconv.Atoi(b)
		return errA == nil && errB == nil && strings.HasPrefix(rest, "match")
	}
	return false
}

// Package rex talks to the Rex terminal through its command-line tool.
package rex

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// appBinary is where the Rex app keeps its CLI. Outside Rex terminals the
// CLI is usually not on PATH.
const appBinary = "/Applications/Rex Beta.app/Contents/Helpers/Rex Server.rex/Contents/MacOS/rex"

// Target is the Rex session and block this process runs in.
type Target struct {
	Session string
	Block   string
}

// Current reports the block rig is running in, if it runs inside Rex.
func Current() (Target, bool) {
	t := Target{Session: os.Getenv("REX_SESSION"), Block: os.Getenv("REX_BLOCK")}
	return t, t.Session != "" && t.Block != ""
}

// Bin finds the rex CLI.
func Bin() (string, error) {
	if dir := os.Getenv("REX_BIN_DIR"); dir != "" {
		if p := filepath.Join(dir, "rex"); executable(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath("rex"); err == nil {
		return p, nil
	}
	if executable(appBinary) {
		return appBinary, nil
	}
	return "", errors.New("rex CLI not found")
}

func executable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && !info.IsDir() && info.Mode()&0o111 != 0
}

// Do runs a Lua script against t with args available as rex.args, and
// returns what the script printed.
func Do(ctx context.Context, t Target, script string, args any) (string, error) {
	bin, err := Bin()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--autostart=false", "--timeout", "3s",
		"do", "-", "-s", t.Session, "-b", t.Block, "--args", string(payload))
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("rex: %s", msg)
		}
		return "", fmt.Errorf("rex: %w", err)
	}
	return strings.TrimSpace(stdout.String()), nil
}

// Capture is the text of a terminal's screen and scrollback, with the
// lines it wrapped joined again.
func Capture(ctx context.Context, session, block string) (string, error) {
	bin, err := Bin()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "--autostart=false", "capture", "-s", session, "-b", block, "--unwrap", "--trim")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", fmt.Errorf("rex: %s", msg)
		}
		return "", fmt.Errorf("rex: %w", err)
	}
	return stdout.String(), nil
}

//go:embed popup.lua
var popupScript string

// Popup is a program to show over the current tab. Its label is the
// popup's title and identifies it: opening a label that is already shown
// brings that popup forward instead. Popups whose labels start with
// Replaces are closed once p is open.
type Popup struct {
	Label    string   `json:"label"`
	Command  []string `json:"command"`
	Cwd      string   `json:"cwd"`
	Replaces string   `json:"replaces,omitempty"`
	Fresh    bool     `json:"fresh,omitempty"` // replace a popup with the same label
}

// SetLabel names the block t, as its pane header shows it.
func SetLabel(ctx context.Context, t Target, label string) error {
	_, err := Do(ctx, t, `local _, err = rex.session.set_block_label{ session_id = rex.session_id,
  block_id = rex.block_id, label = rex.args.label }
if err then error(err, 0) end`, map[string]string{"label": label})
	return err
}

// OpenPopup shows p in a floating layer over the tab t is in. If Rex cannot
// float it, p opens in a new panel beside the tab's other panes instead.
// It reports what it did: "float", "split" or "focus".
func OpenPopup(ctx context.Context, t Target, p Popup) (string, error) {
	out, err := Do(ctx, t, popupScript, p)
	if err != nil {
		return "", err
	}
	how, _, _ := strings.Cut(out, " ")
	if how != "float" && how != "split" && how != "focus" {
		return "", fmt.Errorf("rex: unexpected reply %q", out)
	}
	return how, nil
}

//go:embed sidebar.lua
var sidebarScript string

// SidebarPane is one rig pane in the sidebar, with either its share of the
// height that panes sized in rows leave, or an exact number of rows.
type SidebarPane struct {
	Pane   string  `json:"pane"`
	Height float64 `json:"height"`
	Rows   int     `json:"rows,omitempty"`
}

// Sidebar is rig's sidebar: which side, its share of the tab's width, and
// its panes from the top. Each pane runs Rig with its name as the command.
type Sidebar struct {
	Side  string        `json:"side"`
	Width float64       `json:"width"`
	Panes []SidebarPane `json:"panes"`
	Rig   string        `json:"rig"`
	Cwd   string        `json:"cwd"`
}

// OpenSidebar builds s beside the block t, replacing any sidebar already in
// its tab, and leaves t focused.
func OpenSidebar(ctx context.Context, t Target, s Sidebar) error {
	out, err := Do(ctx, t, sidebarScript, s)
	if err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("rex: unexpected reply %q", out)
	}
	return nil
}

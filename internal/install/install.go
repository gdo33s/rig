// Package install connects rig to Rex: it writes rig's Lua, which adds
// actions and shortcuts, and loads it from Rex's init.lua with one line.
package install

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gdo33s/rig/internal/rex"
)

//go:embed rex.lua
var luaTemplate string

// marker ends the line rig adds to init.lua, so it can be found again.
const marker = "-- added by rig install"

// Paths are the files install touches.
type Paths struct {
	Lua  string // rig's own Lua
	Init string // Rex's init.lua
}

// DefaultPaths follows Rex's rules for init.lua and puts rig's Lua beside
// rig's other configuration.
func DefaultPaths() Paths {
	config := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(config) {
		home, _ := os.UserHomeDir()
		config = filepath.Join(home, ".config")
	}
	init := os.Getenv("REX_CONFIG")
	if init == "" {
		init = filepath.Join(config, "rex", "init.lua")
	}
	return Paths{Lua: filepath.Join(config, "rig", "rex.lua"), Init: init}
}

// loadLine loads rig's Lua from path. It ends with a stamp of the Lua's
// content because Rex fingerprints init.lua alone: the Rex app refreshes
// its actions and shortcuts only when that fingerprint changes, so an
// action added to rig's file would otherwise never reach it.
func loadLine(path, lua string) string {
	sum := sha256.Sum256([]byte(lua))
	return fmt.Sprintf("pcall(dofile, %s) %s %x", strconv.Quote(path), marker, sum[:4])
}

// Lua renders rig's Lua for the rig binary at rigPath.
func Lua(rigPath string) string {
	return strings.Replace(luaTemplate, "RIG_BINARY", strconv.Quote(rigPath), 1)
}

// Install writes rig's Lua, checks it, adds the line that loads it to
// init.lua, checks the whole configuration and reloads it. If Rex
// complains, init.lua is put back as it was.
func Install(p Paths, rigPath string) error {
	lua := Lua(rigPath)
	staged, err := os.CreateTemp("", "rig-rex-*.lua")
	if err != nil {
		return err
	}
	defer os.Remove(staged.Name())
	staged.WriteString(lua)
	staged.Close()
	if err := check(staged.Name()); err != nil {
		return fmt.Errorf("rig's Rex Lua: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(p.Lua), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p.Lua, []byte(lua), 0o644); err != nil {
		return err
	}

	before, err := os.ReadFile(p.Init)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	after := withLine(string(before), loadLine(p.Lua, lua))
	if after != string(before) {
		if err := os.MkdirAll(filepath.Dir(p.Init), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p.Init, []byte(after), 0o644); err != nil {
			return err
		}
		if err := check(p.Init); err != nil {
			restore(p.Init, before)
			return fmt.Errorf("Rex's config with rig added (left as it was): %w", err)
		}
	}
	return reload()
}

// Uninstall removes rig's line from init.lua and rig's Lua, then reloads.
func Uninstall(p Paths) error {
	before, err := os.ReadFile(p.Init)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if after := withoutLine(string(before)); after != string(before) {
		if err := os.WriteFile(p.Init, []byte(after), 0o644); err != nil {
			return err
		}
	}
	if err := os.Remove(p.Lua); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return reload()
}

// withLine adds line at the end of init, replacing an earlier rig line.
func withLine(init, line string) string {
	init = withoutLine(init)
	if init != "" && !strings.HasSuffix(init, "\n") {
		init += "\n"
	}
	return init + line + "\n"
}

func withoutLine(init string) string {
	var kept []string
	for _, l := range strings.SplitAfter(init, "\n") {
		if !strings.Contains(l, marker) {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "")
}

func restore(path string, before []byte) {
	if before == nil {
		os.Remove(path)
		return
	}
	os.WriteFile(path, before, 0o644)
}

// check loads a config file the way Rex will, without starting anything.
func check(path string) error {
	out, err := rexCommand("config", "check", path)
	if err != nil {
		return fmt.Errorf("%s", firstLines(out, err))
	}
	return nil
}

func reload() error {
	out, err := rexCommand("config", "reload")
	if err != nil {
		return fmt.Errorf("rex config reload: %s", firstLines(out, err))
	}
	return nil
}

func rexCommand(args ...string) ([]byte, error) {
	bin, err := rex.Bin()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, append([]string{"--autostart=false"}, args...)...)
	return cmd.CombinedOutput()
}

func firstLines(out []byte, err error) string {
	text := strings.TrimSpace(string(bytes.TrimSpace(out)))
	if text == "" {
		return err.Error()
	}
	lines := strings.Split(text, "\n")
	return strings.Join(lines[:min(len(lines), 5)], "; ")
}

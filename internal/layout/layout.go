// Package layout reads ~/.rig/layout.json, which says how `rig` arranges a
// Rex tab: a sidebar of rig panes beside a main pane.
package layout

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type Config struct {
	Sidebar Sidebar `json:"sidebar"`
	Main    Main    `json:"main"`
}

type Sidebar struct {
	Side  string  `json:"side"`  // "left" or "right"
	Width float64 `json:"width"` // percent of the tab
	Panes []Pane  `json:"panes"` // top to bottom
}

type Pane struct {
	Pane   string  `json:"pane"`             // outline, history or usage
	Height float64 `json:"height,omitempty"` // percent of the sidebar
	Rows   int     `json:"rows,omitempty"`   // or an exact height in rows
}

// Main is the pane beside the sidebar: the terminal `rig` was run in.
type Main struct {
	// Command replaces rig in that terminal once the sidebar is up. Empty
	// leaves the shell.
	Command string `json:"command"`
}

// Default is the layout written when there is none yet.
var Default = Config{
	Sidebar: Sidebar{Side: "left", Width: 15, Panes: []Pane{
		{Pane: "outline", Height: 50},
		{Pane: "history", Height: 25},
		{Pane: "usage", Rows: 8},
	}},
	Main: Main{Command: "claude"},
}

var panes = map[string]bool{"outline": true, "history": true, "usage": true}

// Path is where the layout lives.
func Path() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".rig", "layout.json")
}

// Load reads the layout at path, writing Default there first if there is
// none. It reports whether it wrote one.
func Load(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if err := write(path, Default); err != nil {
			return Config{}, false, err
		}
		return Default, true, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return Config{}, false, fmt.Errorf("%s: %w", path, err)
	}
	if err := c.check(); err != nil {
		return Config{}, false, fmt.Errorf("%s: %w", path, err)
	}
	return c, false, nil
}

func (c *Config) check() error {
	s := &c.Sidebar
	if s.Side == "" {
		s.Side = "left"
	}
	if s.Side != "left" && s.Side != "right" {
		return fmt.Errorf(`sidebar.side must be "left" or "right", not %q`, s.Side)
	}
	if s.Width <= 0 || s.Width >= 100 {
		return fmt.Errorf("sidebar.width must be a percentage between 0 and 100, not %v", s.Width)
	}
	if len(s.Panes) == 0 {
		return errors.New("sidebar.panes is empty")
	}
	for _, p := range s.Panes {
		if !panes[p.Pane] {
			return fmt.Errorf(`unknown pane %q: use "outline", "history" or "usage"`, p.Pane)
		}
		if p.Height < 0 || p.Rows < 0 {
			return fmt.Errorf("pane %q has a negative height", p.Pane)
		}
		if p.Height > 0 && p.Rows > 0 {
			return fmt.Errorf("pane %q has both height and rows: give one", p.Pane)
		}
	}
	return nil
}

// Heights are the shares of the sidebar for the panes sized by height,
// summing to 1 over those panes; panes sized in rows get 0. Panes with
// neither split what the others leave.
func (s Sidebar) Heights() []float64 {
	total, unset := 0.0, 0
	for _, p := range s.Panes {
		total += p.Height
		if p.Height == 0 && p.Rows == 0 {
			unset++
		}
	}
	rest := 0.0
	if unset > 0 {
		rest = max(100-total, 0) / float64(unset)
		if rest == 0 {
			rest = 100 / float64(len(s.Panes))
		}
		total += rest * float64(unset)
	}
	out := make([]float64, len(s.Panes))
	for i, p := range s.Panes {
		h := p.Height
		if p.Rows > 0 {
			continue
		}
		if h == 0 {
			h = rest
		}
		out[i] = h / total
	}
	return out
}

func write(path string, c Config) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Package usage shows Claude Code's rate limits and context.
//
// Claude Code reports rate limits and each session's context window only
// to its status-line command, so `rig statusline` records each report to a
// small state file that the Usage pane reads.
package usage

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Window is one rate-limit window as Claude Code reports it, plus when a
// status line last reported it.
type Window struct {
	UsedPercentage float64  `json:"used_percentage"`
	ResetsAt       int64    `json:"resets_at"`
	SeenAt         int64    `json:"seen_at"`
	UsedUSD        *float64 `json:"used_usd,omitempty"`
	LimitUSD       *float64 `json:"limit_usd,omitempty"`
	Period         string   `json:"period,omitempty"`
}

// How long each window the pane shows lasts.
var windowLength = map[string]time.Duration{"five_hour": 5 * time.Hour, "seven_day": 7 * 24 * time.Hour}

// Session is one Claude Code session's context window, as of its latest
// status line.
type Session struct {
	Name           string  `json:"name,omitempty"`
	Dir            string  `json:"dir"`
	Model          string  `json:"model,omitempty"`
	UsedPercentage float64 `json:"used_percentage"`
	WindowSize     int64   `json:"window_size,omitempty"`
	InputTokens    int64   `json:"input_tokens,omitempty"`
	SeenAt         int64   `json:"seen_at"`
}

type State struct {
	Windows  map[string]Window  `json:"windows"`
	Sessions map[string]Session `json:"sessions,omitempty"`
}

// Report is what one status line said.
type Report struct {
	Windows   map[string]Window
	SessionID string
	Session   *Session
}

// StatePath is where reports are recorded.
func StatePath() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(dir) {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "rig", "claude.json")
}

// Load reads the recorded state. A missing file is nothing recorded yet.
func Load(path string) (State, error) {
	s := State{Windows: map[string]Window{}, Sessions: map[string]Session{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return State{}, err
	}
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, err
	}
	if s.Windows == nil {
		s.Windows = map[string]Window{}
	}
	if s.Sessions == nil {
		s.Sessions = map[string]Session{}
	}
	return s, nil
}

const (
	// confirmEvery is how often an unchanged reading is written again, just
	// to say it is still current.
	confirmEvery = time.Minute
	// Sessions not heard from in this long are forgotten.
	forgetAfter = 24 * time.Hour
)

// Record merges a report into the file at path, writing only when
// something changed or a reading is due to be confirmed.
//
// Several Claude Code sessions report the same account, and a session can
// report a reading older than one already saved, so a window only moves
// forward: a later reset replaces it, and within one reset its usage never
// goes down.
func Record(path string, r Report, now time.Time) error {
	if len(r.Windows) == 0 && r.Session == nil {
		return nil
	}
	saved, err := Load(path)
	if err != nil {
		// A damaged file is replaced rather than blocking every update.
		saved = State{Windows: map[string]Window{}, Sessions: map[string]Session{}}
	}
	changed := false
	for name, w := range r.Windows {
		w.SeenAt = now.Unix()
		old, had := saved.Windows[name]
		merged, moved := merge(old, w, had)
		if moved || due(old.SeenAt, now) {
			saved.Windows[name] = merged
			changed = true
		}
	}
	if s := r.Session; s != nil && r.SessionID != "" {
		old := saved.Sessions[r.SessionID]
		s.SeenAt = old.SeenAt
		if *s != old || due(old.SeenAt, now) {
			s.SeenAt = now.Unix()
			saved.Sessions[r.SessionID] = *s
			changed = true
		}
	}
	for id, s := range saved.Sessions {
		if now.Sub(time.Unix(s.SeenAt, 0)) > forgetAfter {
			delete(saved.Sessions, id)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return writeAtomic(path, saved)
}

func due(seenAt int64, now time.Time) bool {
	return now.Sub(time.Unix(seenAt, 0)) >= confirmEvery
}

// merge combines a saved window with a new report, saying whether the
// reading itself changed (not just when it was seen).
func merge(old, w Window, had bool) (Window, bool) {
	switch {
	case !had || w.ResetsAt > old.ResetsAt:
		return w, true
	case w.ResetsAt < old.ResetsAt:
		old.SeenAt = max(old.SeenAt, w.SeenAt)
		return old, false
	case w.UsedPercentage < old.UsedPercentage:
		old.SeenAt = w.SeenAt
		return old, false
	}
	return w, w.UsedPercentage != old.UsedPercentage ||
		!sameUSD(w.UsedUSD, old.UsedUSD) || !sameUSD(w.LimitUSD, old.LimitUSD) || w.Period != old.Period
}

func sameUSD(a, b *float64) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func writeAtomic(path string, s State) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".claude-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

package usage

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRecordMovesForwardOnly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "limits.json")
	now := time.Unix(1_800_000_000, 0)
	record := func(used float64, resets int64, at time.Time) {
		t.Helper()
		if err := Record(path, Report{Windows: map[string]Window{"five_hour": {UsedPercentage: used, ResetsAt: resets}}}, at); err != nil {
			t.Fatal(err)
		}
	}
	get := func() Window {
		t.Helper()
		l, err := Load(path)
		if err != nil {
			t.Fatal(err)
		}
		return l.Windows["five_hour"]
	}

	record(40, 2000, now)
	if w := get(); w.UsedPercentage != 40 || w.SeenAt != now.Unix() {
		t.Fatalf("first = %+v", w)
	}
	// A stale session reports less of the same window: keep the higher one.
	record(30, 2000, now.Add(10*time.Second))
	if w := get(); w.UsedPercentage != 40 {
		t.Fatalf("lower reading replaced: %+v", w)
	}
	// An older window never replaces a newer one.
	record(90, 1000, now.Add(20*time.Second))
	if w := get(); w.UsedPercentage != 40 || w.ResetsAt != 2000 {
		t.Fatalf("older window replaced: %+v", w)
	}
	record(55, 2000, now.Add(30*time.Second))
	if w := get(); w.UsedPercentage != 55 {
		t.Fatalf("higher reading ignored: %+v", w)
	}
	// After a reset, usage starts again from wherever it is.
	record(3, 3000, now.Add(40*time.Second))
	if w := get(); w.UsedPercentage != 3 || w.ResetsAt != 3000 {
		t.Fatalf("new window ignored: %+v", w)
	}
}

func TestRecordConfirmsUnchangedAtMostOncePerMinute(t *testing.T) {
	path := filepath.Join(t.TempDir(), "limits.json")
	now := time.Unix(1_800_000_000, 0)
	w := Report{Windows: map[string]Window{"seven_day": {UsedPercentage: 10, ResetsAt: 5000}}}
	_ = Record(path, w, now)
	_ = Record(path, w, now.Add(20*time.Second))
	l, _ := Load(path)
	if l.Windows["seven_day"].SeenAt != now.Unix() {
		t.Fatalf("rewrote an unchanged reading within a minute: %+v", l.Windows["seven_day"])
	}
	_ = Record(path, w, now.Add(61*time.Second))
	l, _ = Load(path)
	if l.Windows["seven_day"].SeenAt != now.Add(61*time.Second).Unix() {
		t.Fatalf("did not confirm after a minute: %+v", l.Windows["seven_day"])
	}
}

func TestStatusLineRecordsAndPrints(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	input := `{"session_id":"s1","model":{"display_name":"Opus 5.5"},
		"workspace":{"current_dir":"/x/rig/sub","project_dir":"/x/rig"},
		"context_window":{"used_percentage":34.2,"context_window_size":200000,"total_input_tokens":68400},
		"rate_limits":{"five_hour":{"used_percentage":12.4,"resets_at":4102444800}}}`
	var out bytes.Buffer
	if err := StatusLine(strings.NewReader(input), &out, ""); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out.String()); got != "Opus 5.5 · sub · ctx 34% · 5h 12%" {
		t.Fatalf("line = %q", got)
	}
	l, _ := Load(StatePath())
	if l.Windows["five_hour"].UsedPercentage != 12.4 {
		t.Fatalf("limits not recorded: %+v", l)
	}
	s := l.Sessions["s1"]
	if s.Dir != "/x/rig" || s.UsedPercentage != 34.2 || s.WindowSize != 200000 || s.InputTokens != 68400 || s.Model != "Opus 5.5" {
		t.Fatalf("session not recorded: %+v", s)
	}
}

func TestStatusLineChainsToPreviousCommand(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var out bytes.Buffer
	if err := StatusLine(strings.NewReader(`{"model":{"display_name":"X"}}`), &out, "tr a-z A-Z"); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); !strings.Contains(got, `"DISPLAY_NAME":"X"`) {
		t.Fatalf("chained output = %q", got)
	}
}

func TestStatusLineSurvivesBadInput(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var out bytes.Buffer
	if err := StatusLine(strings.NewReader("not json"), &out, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRecordSessionsAndForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Unix(1_800_000_000, 0)
	session := func(id string, used float64, at time.Time) {
		t.Helper()
		if err := Record(path, Report{SessionID: id, Session: &Session{Dir: "/p", UsedPercentage: used}}, at); err != nil {
			t.Fatal(err)
		}
	}
	session("a", 10, now)
	session("a", 25, now.Add(5*time.Second))
	session("b", 50, now.Add(25*time.Hour))
	l, _ := Load(path)
	if _, ok := l.Sessions["a"]; ok {
		t.Fatalf("stale session kept: %+v", l.Sessions)
	}
	if b := l.Sessions["b"]; b.UsedPercentage != 50 || b.SeenAt != now.Add(25*time.Hour).Unix() {
		t.Fatalf("b = %+v", b)
	}
}

func TestSessionContextAfterCompactionGoesDown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	now := time.Unix(1_800_000_000, 0)
	_ = Record(path, Report{SessionID: "a", Session: &Session{Dir: "/p", UsedPercentage: 80}}, now)
	_ = Record(path, Report{SessionID: "a", Session: &Session{Dir: "/p", UsedPercentage: 12}}, now.Add(time.Second))
	l, _ := Load(path)
	if got := l.Sessions["a"].UsedPercentage; got != 12 {
		t.Fatalf("context = %v, want 12 after compaction", got)
	}
}

package usage

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/gdo33s/rig/internal/git"
)

func view(m Model, width, height int) []string {
	m.width, m.height, m.ready = width, height, true
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	return lines
}

func sample(now time.Time) Model {
	return Model{dir: "/code/rig/internal", now: now, icons: fontIcons, state: State{
		Windows: map[string]Window{
			"five_hour": {UsedPercentage: 37.4, ResetsAt: now.Add(2*time.Hour + 14*time.Minute).Unix(), SeenAt: now.Unix()},
			"seven_day": {UsedPercentage: 71, ResetsAt: now.Add(3 * 24 * time.Hour).Unix(), SeenAt: now.Unix()},
		},
		Sessions: map[string]Session{
			"a": {Dir: "/code/other", UsedPercentage: 70, SeenAt: now.Unix()},
			"b": {Dir: "/code/rig", UsedPercentage: 41, SeenAt: now.Add(-time.Minute).Unix()},
			"c": {Dir: "/code/rig", UsedPercentage: 99, SeenAt: now.Add(-2 * time.Hour).Unix()},
		},
	}}
}

func withBranch(m Model) Model {
	m.branch = &git.BranchDiff{Branch: "feature/wind", Base: "origin/main", Commits: 3, Added: 412, Deleted: 87, Files: 12}
	return m
}

var noon = time.Date(2026, 11, 22, 12, 0, 0, 0, time.Local)

func TestWideStripsWithDetails(t *testing.T) {
	lines := view(withBranch(sample(noon)), 80, 5)
	want := []string{
		" 󰔟 37%     󰨳 71%         󰧑 41%",
		"   2h14m     Wed 12:00     rig",
		"",
		" 󰘬 feature/wind     󰦓 +412 -87    󰈢 12 files",
		"   vs origin/main     3 commits",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d:\n got %q\nwant %q\n%s", i, lines[i], want[i], strings.Join(lines, "\n"))
		}
	}
}

func TestNarrowPaneTable(t *testing.T) {
	lines := view(withBranch(sample(noon)), 36, 12)
	want := []string{
		"Usage",
		"󰔟 5-hour                  2h14m  37%",
		"󰨳 Weekly              Wed 12:00  71%",
		"󰧑 Context                   rig  41%",
		"",
		"Branch feature/wind   vs origin/main",
		"󰦓 Lines          3 commits  +412 -87",
		"󰈢 Files                           12",
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d:\n got %q\nwant %q\n%s", i, lines[i], want[i], strings.Join(lines, "\n"))
		}
	}
}

func TestTableSaysWhenReadingsAreOld(t *testing.T) {
	m := sample(noon)
	for k, w := range m.state.Windows {
		w.SeenAt = noon.Add(-12 * time.Minute).Unix()
		m.state.Windows[k] = w
	}
	for k, s := range m.state.Sessions {
		s.SeenAt = noon.Add(-12 * time.Minute).Unix()
		m.state.Sessions[k] = s
	}
	if lines := view(m, 36, 6); !strings.HasSuffix(lines[0], "12m ago") || !strings.HasPrefix(lines[0], "Usage") {
		t.Fatalf("title = %q", lines[0])
	}
}

func TestShortPaneDropsDetailsThenBranch(t *testing.T) {
	m := withBranch(sample(noon))
	if lines := view(m, 80, 3); lines[0] != " 󰔟 37%   󰨳 71%   󰧑 41%" || lines[1] != "" || !strings.Contains(lines[2], "feature/wind") {
		t.Fatalf("3 rows:\n%s", strings.Join(lines, "\n"))
	}
	if lines := view(m, 80, 1); len(lines) != 1 || !strings.Contains(lines[0], "37%") {
		t.Fatalf("1 row: %q", lines)
	}
	// Too narrow for the whole strip on one row: show what fits.
	if lines := view(m, 20, 1); !strings.Contains(lines[0], "37%") {
		t.Fatalf("narrow 1 row: %q", lines)
	}
}

func TestContextPrefersThisDirectory(t *testing.T) {
	lines := view(sample(noon), 80, 2)
	if !strings.Contains(lines[0], "󰧑 41%") || !strings.HasSuffix(lines[1], "rig") {
		t.Fatalf("context:\n%s", strings.Join(lines, "\n"))
	}
}

func TestTextLabels(t *testing.T) {
	m := withBranch(sample(noon))
	m.icons = textIcons
	m.branch.Base, m.branch.Added, m.branch.Deleted, m.branch.Files = "", 0, 0, 0
	lines := view(m, 80, 5)
	if lines[0] != " 5h 37%     7d 71%         ctx 41%" {
		t.Fatalf("usage = %q", lines[0])
	}
	if lines[3] != " feature/wind   no changes   0 files" || lines[4] != " uncommitted" {
		t.Fatalf("branch:\n%s", strings.Join(lines, "\n"))
	}
}

func TestUnknownAndElapsed(t *testing.T) {
	now := time.Now()
	m := Model{now: now, icons: textIcons, state: State{Windows: map[string]Window{
		"five_hour": {UsedPercentage: 99, ResetsAt: now.Add(-time.Minute).Unix(), SeenAt: now.Unix()},
	}}}
	lines := view(m, 60, 2)
	if lines[0] != " 5h –       7d –   ctx –" || lines[1] != "    reset" {
		t.Fatalf("lines = %q", lines)
	}
}

func TestImageIcons(t *testing.T) {
	m := withBranch(sample(noon))
	m.icons = imageIcons
	m.width, m.ready = 80, true
	images := func() int { return strings.Count(m.View(), "\U0010EEEE") / (iconCols * iconRows) }
	for height, want := range map[int]int{1: 0, 2: 3, 4: 0, 5: 6, 7: 6} {
		m.height = height
		if got := images(); got != want {
			t.Fatalf("height %d drew %d images, want %d\n%s", height, got, want, ansi.Strip(m.View()))
		}
	}
	// Narrow and tall, as in a sidebar: a table, with one-cell icons.
	m.width, m.height = 36, 14
	if got := images(); got != 0 || !strings.Contains(m.View(), "󰔟 5-hour") {
		t.Fatalf("sidebar drew %d images\n%s", got, ansi.Strip(m.View()))
	}
	for _, l := range strings.Split(m.View(), "\n") {
		if ansi.StringWidth(l) > 36 {
			t.Fatalf("line too wide: %d", ansi.StringWidth(l))
		}
	}
}

func TestPaceStyle(t *testing.T) {
	calm := paceStyle(0.30, 0.50).GetForeground()
	if paceStyle(0.70, 0.40).GetForeground() == calm {
		t.Error("over pace should stand out")
	}
	if paceStyle(0.95, 0.99).GetForeground() == paceStyle(0.70, 0.40).GetForeground() {
		t.Error("nearly gone should differ from over pace")
	}
}

func TestUntil(t *testing.T) {
	if got := until(noon.Add(41*time.Minute+10*time.Second), noon); got != "42m" {
		t.Errorf("until = %q", got)
	}
	if got := until(noon.Add(50*time.Hour), noon); got != "Tue 14:00" {
		t.Errorf("until = %q", got)
	}
}

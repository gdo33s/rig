package install

import (
	"strings"
	"testing"
)

func TestWithLineAddsOnceAndReplaces(t *testing.T) {
	mine := "-- my own config\nrex.bind(\"cmd+e\", \"pane.zoom\")"
	first := withLine(mine, loadLine("/a/rex.lua", "one"))
	want := mine + "\n" + `pcall(dofile, "/a/rex.lua") -- added by rig install 7692c3ad` + "\n"
	if first != want {
		t.Fatalf("first = %q", first)
	}
	// New Lua changes the line, so Rex sees a new configuration.
	if withLine(first, loadLine("/a/rex.lua", "two")) == first {
		t.Fatal("line unchanged for new Lua")
	}
	again := withLine(first, loadLine("/b/rex.lua", "one"))
	if strings.Count(again, marker) != 1 || !strings.Contains(again, `"/b/rex.lua"`) || !strings.HasPrefix(again, mine) {
		t.Fatalf("again = %q", again)
	}
	if got := withoutLine(again); got != mine+"\n" {
		t.Fatalf("removed = %q", got)
	}
}

func TestLuaQuotesThePath(t *testing.T) {
	lua := Lua(`/Users/me/my "tools"/rig`)
	if !strings.Contains(lua, `local rig = "/Users/me/my \"tools\"/rig"`) || strings.Contains(lua, "RIG_BINARY") {
		t.Fatalf("lua = %q", lua[:200])
	}
	for _, bind := range []string{`rex.bind("shift+cmd+f", "rig_find")`, `rex.bind("shift+cmd+o", "rig_mentions")`, `rex.bind("shift+cmd+d", "rig_toggle_diff")`} {
		if !strings.Contains(lua, bind) {
			t.Fatalf("no %s", bind)
		}
	}
}

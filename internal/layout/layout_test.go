package layout

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadWritesDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".rig", "layout.json")
	c, wrote, err := Load(path)
	if err != nil || !wrote {
		t.Fatalf("wrote = %v, err = %v", wrote, err)
	}
	if c.Main.Command != "claude" || c.Sidebar.Width != 15 || len(c.Sidebar.Panes) != 3 {
		t.Fatalf("config = %+v", c)
	}
	again, wrote, err := Load(path)
	if err != nil || wrote || again.Sidebar.Panes[2].Pane != "usage" || again.Sidebar.Panes[2].Rows != 8 {
		t.Fatalf("reload = %+v, wrote %v, %v", again, wrote, err)
	}
}

func TestLoadRejectsMistakes(t *testing.T) {
	for _, bad := range []string{
		`{"sidebar":{"width":0,"panes":[{"pane":"outline"}]}}`,
		`{"sidebar":{"width":20,"side":"top","panes":[{"pane":"outline"}]}}`,
		`{"sidebar":{"width":20,"panes":[]}}`,
		`{"sidebar":{"width":20,"panes":[{"pane":"files"}]}}`,
		`{"sidebar":`,
		`{"sidebar":{"width":20,"panes":[{"pane":"usage","height":20,"rows":8}]}}`,
	} {
		path := filepath.Join(t.TempDir(), "layout.json")
		if err := os.WriteFile(path, []byte(bad), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), path) {
			t.Errorf("%s: err = %v", bad, err)
		}
	}
}

func TestHeights(t *testing.T) {
	cases := []struct {
		heights []float64
		want    []float64
	}{
		{[]float64{50, 25, 25}, []float64{0.5, 0.25, 0.25}},
		{[]float64{2, 1, 1}, []float64{0.5, 0.25, 0.25}},
		{[]float64{60, 0, 0}, []float64{0.6, 0.2, 0.2}},
		{[]float64{0, 0}, []float64{0.5, 0.5}},
	}
	for _, c := range cases {
		var s Sidebar
		for _, h := range c.heights {
			s.Panes = append(s.Panes, Pane{Pane: "outline", Height: h})
		}
		got := s.Heights()
		for i := range got {
			if math.Abs(got[i]-c.want[i]) > 1e-9 {
				t.Errorf("Heights(%v) = %v, want %v", c.heights, got, c.want)
				break
			}
		}
	}
}

func TestHeightsLeaveRowsPanesOut(t *testing.T) {
	s := Sidebar{Panes: []Pane{{Pane: "outline", Height: 50}, {Pane: "history", Height: 25}, {Pane: "usage", Rows: 8}}}
	got := s.Heights()
	want := []float64{2.0 / 3, 1.0 / 3, 0}
	for i := range got {
		if math.Abs(got[i]-want[i]) > 1e-9 {
			t.Fatalf("Heights = %v, want %v", got, want)
		}
	}
}

package ui

import (
	"strings"
	"testing"
)

func TestParseBeads(t *testing.T) {
	raw := `[
		{"id":"qfg-2t2d","title":"Native Apple-platform SDK","status":"open","priority":1},
		{"id":"qfg-dcz6","title":"sdk-ruby: zero-ms bootstrap","status":"open","priority":1}
	]`
	beads := parseBeads([]byte(raw))
	if len(beads) != 2 {
		t.Fatalf("parsed %d beads, want 2", len(beads))
	}
	if beads[0].ID != "qfg-2t2d" || beads[0].Title != "Native Apple-platform SDK" {
		t.Errorf("first bead = %+v, want id qfg-2t2d / SDK title", beads[0])
	}
}

func TestParseBeadsBadInput(t *testing.T) {
	// Any failure mode — empty list, stderr noise, no bd at all — must come
	// back as nil so the section simply hides.
	for _, raw := range []string{"[]", "", "Error: database not initialized", "{"} {
		if got := parseBeads([]byte(raw)); len(got) != 0 {
			t.Errorf("parseBeads(%q) = %v, want empty", raw, got)
		}
	}
}

func TestBeadRowsToShow(t *testing.T) {
	tall := ComputeLayout(32, 60) // avail(1 session) = 60-2-2-3-3 = 50
	short := ComputeLayout(32, 18)
	cases := []struct {
		name     string
		l        Layout
		sessions int
		beads    int
		want     int
	}{
		{"no beads, no section", tall, 1, 0, 0},
		{"few beads all fit", tall, 1, 3, 3},
		{"caps at ten", tall, 1, 14, 10},
		// short pane: contentH 16, avail(2) = 16-2-3-6 = 5 → 3 bead rows
		{"short pane truncates harder", short, 2, 10, 3},
		// no room for even one row → hide entirely (header alone is useless)
		{"crowded pane hides section", short, 4, 10, 0},
	}
	for _, c := range cases {
		if got := c.l.BeadRowsToShow(c.sessions, c.beads); got != c.want {
			t.Errorf("%s: BeadRowsToShow(%d,%d) = %d, want %d", c.name, c.sessions, c.beads, got, c.want)
		}
	}
}

func TestShowLegendYieldsToBeads(t *testing.T) {
	// avail(1) = 30-2-2-3-3 = 20: legend fits alone, and with a small beads
	// section, but not once beads take 10+2 rows leaving 8... (8 ≥ 5 still
	// shows). Use a tighter pane: avail(1)=12 → beads 10+2 leaves 0 → no legend.
	l := ComputeLayout(32, 22) // contentH 20, avail(1) = 20-2-3-3 = 12
	if !l.showLegend(1, 0) {
		t.Error("legend should fit with no beads section")
	}
	if l.showLegend(1, 10) {
		t.Error("legend should yield when the beads section fills the space")
	}
}

func TestRenderBeadRowTruncates(t *testing.T) {
	b := Bead{ID: "qfg-2t2d", Title: strings.Repeat("x", 50)}
	row := renderBeadRow(b, 30)
	if w := visibleWidth(row); w > 30 {
		t.Errorf("bead row renders %d columns, want <= 30", w)
	}
	if !strings.Contains(row, "qfg-2t2d") {
		t.Errorf("bead row %q lost the id", row)
	}
}

// visibleWidth strips ANSI sequences the lipgloss styles add.
func visibleWidth(s string) int {
	w, in := 0, false
	for _, r := range s {
		switch {
		case in:
			if r == 'm' {
				in = false
			}
		case r == '\x1b':
			in = true
		default:
			w++
		}
	}
	return w
}

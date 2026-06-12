package ui

import (
	"strings"
	"testing"
)

func TestParseBeads(t *testing.T) {
	raw := `[
		{"id":"qfg-2t2d","title":"Native Apple-platform SDK","status":"open","priority":1},
		{"id":"qfg-dcz6","title":"sdk-ruby: zero-ms bootstrap","status":"open","priority":3}
	]`
	beads := parseBeads([]byte(raw))
	if len(beads) != 2 {
		t.Fatalf("parsed %d beads, want 2", len(beads))
	}
	if beads[0].ID != "qfg-2t2d" || beads[0].Title != "Native Apple-platform SDK" {
		t.Errorf("first bead = %+v, want id qfg-2t2d / SDK title", beads[0])
	}
	if beads[1].Status != "open" || beads[1].Priority != 3 {
		t.Errorf("second bead = %+v, want status open / priority 3", beads[1])
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

func TestBeadsPollStopsWithoutBD(t *testing.T) {
	// No bd on PATH means no amount of 60s retries will help — the poll chain
	// must end (no follow-up cmd), not spin forever execing a missing binary.
	m := testModel()
	if _, cmd := m.Update(beadsMsg{noBD: true}); cmd != nil {
		t.Error("beadsMsg{noBD} rescheduled a poll; want the chain to stop")
	}
	// With bd present, the chain continues even when there's no ready work.
	if _, cmd := m.Update(beadsMsg{}); cmd == nil {
		t.Error("empty beadsMsg ended the poll chain; want a reschedule")
	}
}

func TestBeadsToShow(t *testing.T) {
	tall := ComputeLayout(32, 60)  // avail(1 session) = 60-2-2-4-3 = 49
	mid := ComputeLayout(32, 20)   // avail(2) = 20-2-2-8-3 = 5
	short := ComputeLayout(32, 18) // avail(2) = 3
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
		// mid pane: 5 rows minus header 2 = 3 → one 2-line bead
		{"mid pane truncates harder", mid, 2, 10, 1},
		// 3 rows minus header 2 = 1 → not even one bead → hide entirely
		{"short pane hides section", short, 2, 10, 0},
		{"crowded pane hides section", short, 4, 10, 0},
	}
	for _, c := range cases {
		if got := c.l.BeadsToShow(c.sessions, c.beads); got != c.want {
			t.Errorf("%s: BeadsToShow(%d,%d) = %d, want %d", c.name, c.sessions, c.beads, got, c.want)
		}
	}
}

func TestShowLegendYieldsToBeads(t *testing.T) {
	// avail(1) = 22-2-2-4-3 = 11: legend (5 rows) fits alone, but not once
	// four 2-line beads plus the 2-row header take 10 of the 11 rows.
	l := ComputeLayout(32, 22)
	if !l.showLegend(1, 0) {
		t.Error("legend should fit with no beads section")
	}
	if shown := l.BeadsToShow(1, 10); shown != 4 {
		t.Fatalf("BeadsToShow(1,10) = %d, want 4 — legend case below assumes it", shown)
	}
	if l.showLegend(1, 4) {
		t.Error("legend should yield when the beads section fills the space")
	}
}

func TestRenderBeadRowTwoLines(t *testing.T) {
	// Each bead draws like a bd list row split over two lines:
	//   ○ cmdr-keen-gu3 ● P4
	//     Placeholder: second demo bead
	b := Bead{ID: "cmdr-keen-gu3", Title: strings.Repeat("x", 50), Status: "open", Priority: 4}
	line1, line2 := renderBeadRow(b, 30)
	for _, ln := range []string{line1, line2} {
		if w := visibleWidth(ln); w > 30 {
			t.Errorf("bead line %q renders %d columns, want <= 30", ln, w)
		}
	}
	if !strings.Contains(line1, "○") || !strings.Contains(line1, "cmdr-keen-gu3") || !strings.Contains(line1, "P4") {
		t.Errorf("line1 %q missing status circle, id, or priority badge", line1)
	}
	if !strings.Contains(line2, "xxx") {
		t.Errorf("line2 %q lost the title", line2)
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

func TestFetchBeadsDisabled(t *testing.T) {
	msg, ok := fetchBeads(".", nil).(beadsMsg)
	if !ok || !msg.noBD {
		t.Errorf("fetchBeads with empty argv = %+v, want noBD (section off, polling stops)", msg)
	}
}

func TestFetchBeadsCustomCommand(t *testing.T) {
	argv := []string{"sh", "-c", `printf '[{"id":"x-1","title":"a task","status":"open","priority":2}]'`}
	msg, ok := fetchBeads(t.TempDir(), argv).(beadsMsg)
	if !ok || msg.noBD || len(msg.beads) != 1 || msg.beads[0].ID != "x-1" {
		t.Errorf("fetchBeads custom argv = %+v, want one bead x-1", msg)
	}
}

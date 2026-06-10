package ui

import "testing"

func TestComputeLayout(t *testing.T) {
	l := ComputeLayout(32, 40)
	if l.SidebarW != 30 {
		t.Errorf("SidebarW = %d, want 30 (pane width minus border)", l.SidebarW)
	}
	// Tiny panes clamp rather than going non-positive.
	l = ComputeLayout(3, 5)
	if l.SidebarW < 1 {
		t.Errorf("SidebarW = %d on tiny pane, want >= 1", l.SidebarW)
	}
}

func TestSessionIndexAt(t *testing.T) {
	l := ComputeLayout(32, 40)
	row0 := l.sessionRow0()

	cases := []struct {
		name  string
		x, y  int
		count int
		want  int
	}{
		{"first row of first session", 5, row0, 3, 0},
		{"last row of first session", 5, row0 + linesPerSession - 1, 3, 0},
		{"first row of second session", 5, row0 + linesPerSession, 3, 1},
		{"third session", 5, row0 + 2*linesPerSession, 3, 2},
		{"below the list", 5, row0 + 3*linesPerSession, 3, -1},
		{"header row misses", 5, 0, 3, -1},
		{"x out of pane", l.W + 1, row0, 3, -1},
	}
	for _, c := range cases {
		if got := l.SessionIndexAt(c.x, c.y, c.count); got != c.want {
			t.Errorf("%s: SessionIndexAt(%d,%d,%d) = %d, want %d", c.name, c.x, c.y, c.count, got, c.want)
		}
	}
}

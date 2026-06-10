package ui

import (
	"strings"
	"testing"

	"github.com/quonfig/cmdr-keen/internal/reg"
)

func twoSessions() []*reg.Session {
	return []*reg.Session{
		{Topic: "alpha work", Task: "doing a thing", Status: reg.StatusCrunching},
		{Topic: "beta work", Task: "other thing", Status: reg.StatusDone},
	}
}

// stripANSI removes escape sequences so tests can assert on the plain glyphs.
func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case in:
			if r == 'm' {
				in = false
			}
		case r == '\x1b':
			in = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Every rendered line must be exactly the pane width in both focus states —
// the hand-drawn frame has no lipgloss safety net.
func TestFrameLinesExactWidth(t *testing.T) {
	l := ComputeLayout(32, 40)
	for _, focused := range []bool{true, false} {
		out := RenderSidebar(l, twoSessions(), nil, 0, focused, false)
		for i, line := range strings.Split(out, "\n") {
			if w := visibleWidth(line); w != l.W {
				t.Errorf("focused=%v line %d: width %d, want %d (%q)", focused, i, w, l.W, line)
			}
		}
	}
}

// Blurred, the frame opens beside the active session — a notebook-tab cutout
// aimed at the session pane: ─╯ above, ─╮ below, open right edge between.
func TestFrameTabCutout(t *testing.T) {
	l := ComputeLayout(32, 40)
	lines := strings.Split(stripANSI(RenderSidebar(l, twoSessions(), nil, 0, false, false)), "\n")

	row0 := l.sessionRow0()
	top, bot := row0-1, row0+linesPerSession
	if !strings.HasSuffix(lines[top], "╯") || !strings.Contains(lines[top], "──") {
		t.Errorf("tab top edge = %q, want ─…╯", lines[top])
	}
	if !strings.HasSuffix(lines[bot], "╮") {
		t.Errorf("tab bottom edge = %q, want ─…╮", lines[bot])
	}
	for r := row0; r < row0+linesPerSession; r++ {
		if strings.HasSuffix(strings.TrimRight(lines[r], " "), "│") {
			t.Errorf("active row %d = %q closed on the right; want open toward the pane", r, lines[r])
		}
		if !strings.HasPrefix(lines[r], "│") {
			t.Errorf("active row %d = %q missing the left tab edge", r, lines[r])
		}
	}
	// The inactive session keeps the right rail — the fence stays closed there.
	if inactive := row0 + sessionStride; !strings.HasSuffix(lines[inactive], "│") {
		t.Errorf("inactive row = %q lost the right rail", lines[inactive])
	}
}

// Focused, the whole frame is one solid thick box: keys are here.
func TestFrameFocusedBox(t *testing.T) {
	l := ComputeLayout(32, 40)
	lines := strings.Split(stripANSI(RenderSidebar(l, twoSessions(), nil, 0, true, false)), "\n")
	if !strings.HasPrefix(lines[0], "┏") || !strings.HasSuffix(lines[0], "┓") {
		t.Errorf("top frame row = %q, want ┏━…┓", lines[0])
	}
	if !strings.HasPrefix(lines[2], "┃") || !strings.HasSuffix(lines[2], "┃") {
		t.Errorf("content row = %q, want ┃…┃", lines[2])
	}
	if last := lines[len(lines)-1]; !strings.HasPrefix(last, "┗") || !strings.HasSuffix(last, "┛") {
		t.Errorf("bottom frame row = %q, want ┗━…┛", last)
	}
}

package ui

// Layout is the sidebar's on-screen geometry. The sidebar is a whole tmux pane
// now — keen no longer lays out a session pane next to it (tmux does that) —
// so layout is just the pane size plus the row math that render and mouse
// hit-testing share, so they can never disagree. Coordinates are 0-based and
// pane-local (tmux translates mouse events into the pane).
//
// Vertical: row 0 = top border, content rows 1..H-2, row H-1 = bottom border.
type Layout struct {
	W, H     int
	SidebarW int // content width inside the border box
}

const (
	sidebarHeader = 2 // "sessions" title + one blank line
	boxBorder     = 2 // a lipgloss border adds 1 cell on each side
	hintRows      = 3 // footer hint lines RenderSidebar pins to the bottom
)

// ComputeLayout derives geometry from the sidebar pane size.
func ComputeLayout(w, h int) Layout {
	sw := w - boxBorder
	if sw < 4 {
		sw = 4
	}
	return Layout{W: w, H: h, SidebarW: sw}
}

// showLegend reports whether the color key fits below the session list without
// pushing any session or the bottom hint out of the box.
func (l Layout) showLegend(count int) bool {
	contentH := l.H - boxBorder
	need := sidebarHeader + legendHeight() + count*linesPerSession + hintRows
	return contentH >= need
}

// sessionRow0 is the 0-based row of the first session entry (past the top
// border and header lines).
func (l Layout) sessionRow0() int {
	return 1 + sidebarHeader
}

// linesPerSession is how many rows one session occupies. RenderSidebar draws
// three — topic, current task, phase+context bar — so hit-testing steps by
// three to stay aligned.
const linesPerSession = 3

// SessionIndexAt returns the session index a click landed on, or -1 if the
// click wasn't on a session entry.
func (l Layout) SessionIndexAt(x, y, count int) int {
	if x < 0 || x >= l.W {
		return -1
	}
	rel := y - l.sessionRow0()
	if rel < 0 {
		return -1
	}
	idx := rel / linesPerSession
	if idx >= count {
		return -1
	}
	return idx
}

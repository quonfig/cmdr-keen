package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/quonfig/cmdr-keen/internal/reg"
)

// Focus must be unmistakable at a glance: typing j/k into a Claude composer
// because the sidebar looked alive is the worst keen failure mode. tmux
// renders no visible chrome (its mandatory divider column is painted to the
// background); keen hand-draws every edge instead — see frame. Focused: a
// solid thick blue box around the sidebar. Blurred: a dim rail along the
// right edge, broken open beside the active session, whose blue outline wraps
// the entry like a notebook tab opening toward its pane.
var (
	hintStyle   = lipgloss.NewStyle().Faint(true)
	activeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))

	frameStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("33"))  // focused box + tab outline
	railStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("238")) // blurred right rail

	focusTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("33"))
	blurKeyStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("33"))

	confirmStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")) // red — armed close prompt
)

const (
	// defaultContextWindow is the context budget assumed when nothing overrides
	// it. The transcript doesn't expose the model's real window, so we default to
	// 1M (the common case on extended-context models like Opus); users on a
	// smaller-window model set KEEN_CONTEXT_WINDOW to match.
	defaultContextWindow = 1_000_000
	miniBarW             = 8 // cells in the sidebar's mini context bar
)

// contextWindowMax is the denominator for the sidebar usage bar.
func contextWindowMax() int {
	if v := os.Getenv("KEEN_CONTEXT_WINDOW"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultContextWindow
}

// sidebarIndent aligns rows 2+ under a session's row-1 label (marker 2 + glyph
// 1 + space 1).
const sidebarIndent = "    "

// phaseW pads the phase word so the context bars line up across sessions.
const phaseW = 8

// phaseBadge renders the work phase as a short colored word, padded to phaseW
// so what follows it stays column-aligned. An unknown phase shows a faint dot.
func phaseBadge(phase string) string {
	if phase == "" {
		return hintStyle.Render(padRight("·", phaseW))
	}
	var c lipgloss.Color
	switch phase {
	case "planning":
		c = lipgloss.Color("4") // blue
	case "building":
		c = lipgloss.Color("6") // cyan
	case "testing":
		c = lipgloss.Color("3") // yellow
	case "shipping":
		c = lipgloss.Color("5") // magenta
	case "done":
		c = lipgloss.Color("2") // green
	default:
		c = lipgloss.Color("7")
	}
	return lipgloss.NewStyle().Foreground(c).Render(padRight(phase, phaseW))
}

// padRight pads s with spaces to exactly w columns (truncating if longer).
func padRight(s string, w int) string {
	r := []rune(s)
	if len(r) >= w {
		return string(r[:w])
	}
	return s + strings.Repeat(" ", w-len(r))
}

// contextBar is a compact gauge of how much of the context window is in use,
// colored like the canonical bottom status line (green → yellow → red → bold
// red as it fills). A session we haven't heard token counts from yet shows a
// faint empty bar.
func contextBar(tokens int) string {
	if tokens <= 0 {
		return hintStyle.Render("[" + strings.Repeat("·", miniBarW) + "]")
	}

	pct := tokens * 100 / contextWindowMax()
	if pct > 100 {
		pct = 100
	}
	filled := pct * miniBarW / 100
	if filled > miniBarW {
		filled = miniBarW
	}
	bar := strings.Repeat("▓", filled) + strings.Repeat("░", miniBarW-filled)

	style := lipgloss.NewStyle()
	switch {
	case pct >= 90:
		style = style.Bold(true).Foreground(lipgloss.Color("1")) // bold red
	case pct >= 75:
		style = style.Foreground(lipgloss.Color("1")) // red
	case pct >= 50:
		style = style.Foreground(lipgloss.Color("3")) // yellow
	default:
		style = style.Foreground(lipgloss.Color("2")) // green
	}
	return style.Render(fmt.Sprintf("[%s] %d%%", bar, pct))
}

// legend is the sidebar color key — glyph → what it means — shown below the
// session list when the terminal is tall enough (see Layout.showLegend).
// Ordered by how much each state wants your attention, so "needs you" reads
// first.
var legend = []struct {
	st    reg.Status
	label string
}{
	{reg.StatusWaiting, "needs you · permission"},
	{reg.StatusIdle, "needs you · idle"},
	{reg.StatusCrunching, "working"},
	{reg.StatusDone, "done"},
}

// legendHeight is how many sidebar rows the legend occupies: one per entry plus
// a leading blank separating it from the list. Derived from legend so the
// layout math can't drift from what RenderSidebar actually draws.
func legendHeight() int { return len(legend) + 1 }

// statusGlyph returns a single colored cell for a session's status.
func statusGlyph(st reg.Status) string {
	switch st {
	case reg.StatusCrunching:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("●") // yellow
	case reg.StatusWaiting:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("◐") // red — permission
	case reg.StatusIdle:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render("◐") // magenta — idle ping
	case reg.StatusDone:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("✓") // green
	case reg.StatusExited:
		return hintStyle.Render("✕")
	default:
		return hintStyle.Render("·")
	}
}

// beadIDStyle colors the issue id in a beads row so it reads apart from the
// faint title beneath it.
var beadIDStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")) // cyan

// beadGlyph mirrors bd's own status circles (○ open, ◐ in_progress, ● blocked,
// ✓ closed, ❄ deferred). bd list emits open and in_progress by default; the
// rest are mapped defensively so a future bd can't render garbage.
func beadGlyph(status string) string {
	switch status {
	case "in_progress":
		return "◐"
	case "blocked":
		return "●"
	case "closed":
		return "✓"
	case "deferred":
		return "❄"
	default:
		return "○"
	}
}

// priorityBadge renders bd's "● Pn" marker, hotter colors for higher priority
// (P0 bold red → P4 faint).
func priorityBadge(p int) string {
	var style lipgloss.Style
	switch {
	case p <= 0:
		style = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")) // bold red
	case p == 1:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	case p == 2:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	case p == 3:
		style = lipgloss.NewStyle().Foreground(lipgloss.Color("4")) // blue
	default:
		style = hintStyle
	}
	return style.Render(fmt.Sprintf("● P%d", p))
}

// beadBadgeW is the columns the priority badge needs beside the id: a space
// plus "● P4".
const beadBadgeW = 5

// renderBeadRow draws one ready issue as two lines echoing bd list's row
// style — status circle, id, priority badge, then the faint title beneath:
//
//	○ cmdr-keen-gu3 ● P4
//	  Placeholder: second demo bead
//
// The id wins when space is tight: it truncates to leave the badge room, and
// the badge drops entirely before the id loses its tail to it.
func renderBeadRow(b Bead, w int) (string, string) {
	id := truncate(b.ID, w-2-beadBadgeW)
	line1 := hintStyle.Render(beadGlyph(b.Status)) + " " + beadIDStyle.Render(id)
	if len([]rune(b.ID)) <= w-2-beadBadgeW { // id intact — badge fits beside it
		line1 += " " + priorityBadge(b.Priority)
	}
	line2 := "  " + hintStyle.Render(truncate(b.Title, w-2))
	return line1, line2
}

// RenderSidebar draws the fixed-order session list, full pane height. When
// confirming is set, the footer turns into a close-confirmation prompt.
func RenderSidebar(l Layout, sessions []*reg.Session, beads []Bead, active int, focused, confirming bool) string {
	contentH := l.H - boxBorder
	lines := make([]string, 0, contentH)

	title := hintStyle.Render("sessions")
	if focused {
		title = focusTitle.Render(" sessions ")
	}
	lines = append(lines, title, "")

	// tabTop/tabBot bracket the active session's rows when the sidebar is
	// blurred; frame draws the tab outline's edges on those lines.
	tabTop, tabBot := -1, -1
	for i, s := range sessions {
		if i == active && !focused {
			tabTop = len(lines) - 1 // header blank or the previous separator
		}
		marker := "  "
		if i == active {
			marker = "› "
		}
		// Three rows per session (kept in lockstep with layout.linesPerSession),
		// all derived from a Haiku summary of the recent transcript:
		//   Row 1: status glyph + the overall topic (what this session is about).
		//   Row 2: the current task (what it's doing right now).
		//   Row 3: a phase badge (planning→done) + the context-window usage bar.
		// Until the first summary lands we show a "freshie" placeholder.
		topic := firstNonEmpty(s.Topic, s.Task, "freshie")
		topic = truncate(topic, l.SidebarW-4) // marker(2)+glyph(1)+space(1)
		if i == active {
			topic = activeStyle.Render(topic)
		}
		lines = append(lines, marker+statusGlyph(s.Status)+" "+topic)

		task := "…"
		if s.Task != "" {
			task = truncate(s.Task, l.SidebarW-len(sidebarIndent))
		}
		lines = append(lines, sidebarIndent+hintStyle.Render(task))

		row3 := sidebarIndent + phaseBadge(s.Phase) + contextBar(s.ContextTokens)
		// Right-align the elapsed timer into whatever space row 3 has left. On a
		// narrow sidebar the badge + bar can already fill the width, so drop the
		// timer rather than wrap (a wrapped row would desync click hit-testing).
		if label := s.ElapsedLabel(); label != "" {
			if gap := l.SidebarW - lipgloss.Width(row3) - len(label); gap >= 1 {
				row3 += strings.Repeat(" ", gap) + hintStyle.Render(label)
			}
		}
		lines = append(lines, row3)

		// Trailing separator (sessionStride row 4): blank breathing room, and
		// the canvas for the tab outline's bottom edge when i is active.
		lines = append(lines, "")
		if i == active && !focused {
			tabBot = len(lines) - 1
		}
	}

	// Live work from the bd tracker, just below the list — open and claimed
	// issues alike, glyphed like bd's own output. Truncated to fit (top
	// maxBeads at most); the header carries the true total.
	beadsShown := l.BeadsToShow(len(sessions), len(beads))
	if beadsShown > 0 {
		header := fmt.Sprintf("bd list · %d", len(beads))
		lines = append(lines, "", hintStyle.Render(truncate(header, l.SidebarW)))
		for _, b := range beads[:beadsShown] {
			line1, line2 := renderBeadRow(b, l.SidebarW)
			lines = append(lines, line1, line2)
		}
	}

	// Color key for the status glyphs, below the work sections — handy when
	// getting started. Yields to sessions and beads on short terminals.
	if l.showLegend(len(sessions), beadsShown) {
		lines = append(lines, "")
		for _, e := range legend {
			lines = append(lines, statusGlyph(e.st)+" "+hintStyle.Render(e.label))
		}
	}

	// Pin the hints to the bottom of the box. The copy line earns its row:
	// tmux owns the mouse, so the highlight vanishes the moment you release —
	// which reads as "selection broken" unless you know release IS the copy
	// (it lands on the system clipboard; Cmd-C never applies).
	hints := []string{
		hintStyle.Render("⏎ to claude · n new · x close"),
		hintStyle.Render("drag selects · release copies"),
		hintStyle.Render("q detach (sessions live on)"),
	}
	if !focused { // keys go to claude — say so instead of showing dead bindings
		hints = []string{
			hintStyle.Render("drag selects · release copies"),
			"",
			blurKeyStyle.Render("keys → claude · ^k = here"),
		}
	}
	if confirming { // an 'x' is armed — replace the footer with a clear prompt
		hints = []string{
			"",
			confirmStyle.Render("close session?"),
			confirmStyle.Render("x confirm · any cancel"),
		}
	}
	used := len(lines) + hintRows
	for used < contentH {
		lines = append(lines, "")
		used++
	}
	lines = append(lines, hints...)

	return frame(l, lines, focused, tabTop, tabBot)
}

// frame hand-draws the sidebar border — lipgloss boxes can't do the one thing
// the design needs, a gap, so keen owns every edge cell. Focused: a solid
// thick blue box (keys are here). Blurred: a dim rail down the right edge —
// the fence line toward the session pane — broken open beside the active
// session, whose blue outline wraps the entry like a notebook tab. tabTop and
// tabBot are the content-line indexes of the outline's edge rows (-1 = none;
// both separator rows, so overwriting them with ─ fill loses nothing).
func frame(l Layout, content []string, focused bool, tabTop, tabBot int) string {
	fill := strings.Repeat("─", l.SidebarW)
	blank := strings.Repeat(" ", l.SidebarW+1)
	rows := make([]string, 0, len(content)+boxBorder)

	edge := func() string { // blurred top/bottom rows carry only the rail
		return blank + railStyle.Render("│")
	}
	if focused {
		rows = append(rows, frameStyle.Render("┏"+strings.Repeat("━", l.SidebarW)+"┓"))
	} else {
		rows = append(rows, edge())
	}
	for i, line := range content {
		if pad := l.SidebarW - lipgloss.Width(line); pad > 0 {
			line += strings.Repeat(" ", pad)
		}
		switch {
		case focused:
			rows = append(rows, frameStyle.Render("┃")+line+frameStyle.Render("┃"))
		case i == tabTop:
			rows = append(rows, frameStyle.Render("╭"+fill+"╯"))
		case i == tabBot:
			rows = append(rows, frameStyle.Render("╰"+fill+"╮"))
		case tabTop >= 0 && i > tabTop && i < tabBot:
			rows = append(rows, frameStyle.Render("│")+line+" ") // open toward the pane
		default:
			rows = append(rows, " "+line+railStyle.Render("│"))
		}
	}
	if focused {
		rows = append(rows, frameStyle.Render("┗"+strings.Repeat("━", l.SidebarW)+"┛"))
	} else {
		rows = append(rows, edge())
	}
	return strings.Join(rows, "\n")
}

// firstNonEmpty returns the first non-empty string, or "" if all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// truncate shortens s to at most w visible columns, adding an ellipsis.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

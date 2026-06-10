package ui

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/quonfig/cmdr-keen/internal/reg"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	hintStyle   = lipgloss.NewStyle().Faint(true)
	activeStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))

	focusBorder   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("12"))
	unfocusBorder = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("240"))

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

// RenderSidebar draws the fixed-order session list, full pane height. When
// confirming is set, the footer turns into a close-confirmation prompt.
func RenderSidebar(l Layout, sessions []*reg.Session, active int, focused, confirming bool) string {
	contentH := l.H - boxBorder
	lines := make([]string, 0, contentH)

	lines = append(lines, titleStyle.Render("sessions"), "")

	for i, s := range sessions {
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
	}

	// Color key for the status glyphs, just below the list — handy when getting
	// started. Yields to the session list on short terminals (showLegend).
	if l.showLegend(len(sessions)) {
		lines = append(lines, "")
		for _, e := range legend {
			lines = append(lines, statusGlyph(e.st)+" "+hintStyle.Render(e.label))
		}
	}

	// Pin the hint to the bottom of the box. Copy needs no hint anymore — tmux
	// owns the mouse, so plain click-drag selects within a pane and copies on
	// release. The second line sells the new superpower instead.
	hint := hintStyle.Render("⏎ to claude · n new · x close")
	hint2 := hintStyle.Render("q detach (sessions live on)")
	if confirming { // an 'x' is armed — replace the footer with a clear prompt
		hint = confirmStyle.Render("close session?")
		hint2 = confirmStyle.Render("x confirm · any cancel")
	}
	used := len(lines) + 2
	for used < contentH {
		lines = append(lines, "")
		used++
	}
	lines = append(lines, hint, hint2)

	box := unfocusBorder
	if focused {
		box = focusBorder
	}
	return box.Width(l.SidebarW).Height(contentH).Render(strings.Join(lines, "\n"))
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

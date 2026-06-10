package ui

import (
	"strconv"
	"strings"

	"github.com/quonfig/cmdr-keen/internal/reg"
)

// fleetTitle summarizes the whole fleet as a terminal-title string, e.g.
// "keen · 2◐ 3● 4✓" — needs-you first (waiting and idle pool together; a
// title can't carry the red/magenta split), then working, then done. Counts
// of zero drop out, and an empty fleet is just "keen". This is keen's only
// out-of-sidebar signal, and it stays visual-only: no bell, no notification.
func fleetTitle(sessions []*reg.Session) string {
	var needsYou, working, done int
	for _, s := range sessions {
		switch s.Status {
		case reg.StatusWaiting, reg.StatusIdle:
			needsYou++
		case reg.StatusCrunching:
			working++
		case reg.StatusDone:
			done++
		}
	}
	var parts []string
	if needsYou > 0 {
		parts = append(parts, strconv.Itoa(needsYou)+"◐")
	}
	if working > 0 {
		parts = append(parts, strconv.Itoa(working)+"●")
	}
	if done > 0 {
		parts = append(parts, strconv.Itoa(done)+"✓")
	}
	if len(parts) == 0 {
		return "keen"
	}
	return "keen · " + strings.Join(parts, " ")
}

// syncTitle pushes the fleet summary to the outer terminal tab, skipping the
// tmux round-trip when nothing changed. Called from the 1s tick and on every
// status event, so the title tracks the sidebar within a second.
func (m *Model) syncTitle() {
	t := fleetTitle(m.mgr.Sessions())
	if t == m.lastTitle {
		return
	}
	m.lastTitle = t
	m.mgr.SetTerminalTitle(t)
}

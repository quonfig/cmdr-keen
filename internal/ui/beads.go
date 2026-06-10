package ui

import (
	"encoding/json"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// keen shows ready work from the bd (beads) issue tracker below the session
// list: a slow poll of `bd ready --json` in the directory keen was launched
// from. Display-only — keen never writes to the tracker — and entirely
// optional: when bd is missing, the database isn't initialized, or the pane
// is too short, the section silently disappears.

const (
	// beadsPollInterval is deliberately slow — ready work changes on the
	// timescale of finished sessions, not keystrokes, and each poll execs bd.
	beadsPollInterval = time.Minute

	// maxBeadRows caps the section however tall the pane is; past the top ten
	// you'd reach for `bd ready` itself.
	maxBeadRows = 10
)

// Bead is one ready issue, as `bd ready --json` reports it.
type Bead struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// beadsMsg delivers a poll result to the update loop. nil means "nothing to
// show" regardless of why (no bd, no db, no ready work).
type beadsMsg struct{ beads []Bead }

// fetchBeadsNow polls immediately (startup); fetchBeadsLater after the
// interval (steady state). Both run off the UI thread as tea.Cmds.
func fetchBeadsNow(cwd string) tea.Cmd {
	return func() tea.Msg { return beadsMsg{fetchBeads(cwd)} }
}

func fetchBeadsLater(cwd string) tea.Cmd {
	return tea.Tick(beadsPollInterval, func(time.Time) tea.Msg { return beadsMsg{fetchBeads(cwd)} })
}

// fetchBeads runs `bd ready --json` in cwd. Output() captures stdout only, so
// bd's stderr warnings (auto-export notes, permission nags) never reach the
// parser.
func fetchBeads(cwd string) []Bead {
	cmd := exec.Command("bd", "ready", "--json")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseBeads(out)
}

// parseBeads decodes the bd ready list, mapping every failure to nil — a
// malformed or empty result hides the section rather than erroring anywhere.
func parseBeads(raw []byte) []Bead {
	var beads []Bead
	if err := json.Unmarshal(raw, &beads); err != nil {
		return nil
	}
	return beads
}

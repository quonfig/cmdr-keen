package ui

import (
	"encoding/json"
	"os/exec"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// keen shows live work from the bd (beads) issue tracker below the session
// list: a slow poll of `bd list --json` (open and in_progress issues; closed
// stay out by bd's default filter) in the directory keen was launched from.
// Display-only — keen never writes to the tracker — and entirely optional:
// when bd is missing, the database isn't initialized, or the pane is too
// short, the section silently disappears.

const (
	// beadsPollInterval is deliberately slow — the issue list changes on the
	// timescale of finished sessions, not keystrokes, and each poll execs bd.
	beadsPollInterval = time.Minute

	// maxBeads caps the section at ten beads however tall the pane is; past
	// the top ten you'd reach for `bd list` itself.
	maxBeads = 10
)

// Bead is one issue, as `bd list --json` reports it.
type Bead struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
}

// beadsMsg delivers a poll result to the update loop. An empty beads list
// means "nothing to show" (no db, no live issues, bd errored) and polling
// continues; noBD means bd isn't on PATH at all, which ends the poll chain —
// a 60s retry loop can't install it. (Installing bd mid-flight needs a
// sidebar restart to pick up.)
type beadsMsg struct {
	beads []Bead
	noBD  bool
}

// fetchBeadsNow polls immediately (startup); fetchBeadsLater after the
// interval (steady state). Both run off the UI thread as tea.Cmds.
func fetchBeadsNow(cwd string) tea.Cmd {
	return func() tea.Msg { return fetchBeads(cwd) }
}

func fetchBeadsLater(cwd string) tea.Cmd {
	return tea.Tick(beadsPollInterval, func(time.Time) tea.Msg { return fetchBeads(cwd) })
}

// fetchBeads runs `bd list --json` in cwd (open + in_progress; bd's default
// filter excludes closed). Output() captures stdout only, so bd's stderr
// warnings (auto-export notes, permission nags) never reach the parser.
func fetchBeads(cwd string) tea.Msg {
	if _, err := exec.LookPath("bd"); err != nil {
		return beadsMsg{noBD: true}
	}
	cmd := exec.Command("bd", "list", "--json")
	cmd.Dir = cwd
	out, err := cmd.Output()
	if err != nil {
		return beadsMsg{}
	}
	return beadsMsg{beads: parseBeads(out)}
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

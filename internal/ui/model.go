package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quonfig/cmdr-keen/internal/debug"
	"github.com/quonfig/cmdr-keen/internal/hooks"
	"github.com/quonfig/cmdr-keen/internal/reg"
	"github.com/quonfig/cmdr-keen/internal/titler"
)

// summaryMsg carries a freshly generated set of session labels back to the
// update loop.
type summaryMsg struct {
	ID    string
	Topic string
	Task  string
	Phase string
}

// resummarizeEvery re-labels a session every N user prompts, so the sidebar
// keeps up when the work changes tracks mid-session. At 1 we re-label on every
// prompt (the in-flight guard in maybeSummarize still coalesces bursts).
const resummarizeEvery = 1

// tickInterval is how often the model wakes to repaint elapsed timers and
// prune sessions whose pane has died. The timer is minute-coarse and the prune
// is one tmux list-panes, so a 1s cadence is cheap.
const tickInterval = time.Second

// tickMsg is the periodic nudge that keeps timers live and the list pruned.
type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// transcriptTailChars is how much of the recent transcript we feed the titler.
const transcriptTailChars = 4000

// Model is the sidebar's Bubble Tea model. It runs inside the left tmux pane
// and never touches a session's bytes: tmux routes keystrokes, renders panes,
// and owns selection/copy. keen only lists, labels, and switches sessions.
//
// Focus is tmux pane focus. When the sidebar pane is focused, keys navigate;
// Enter (or a row click) hands focus to the session pane; the C-k binding in
// keen's tmux config toggles back.
type Model struct {
	mgr    *reg.Manager
	layout Layout
	ready  bool

	// focused mirrors tmux pane focus (focus-events on + WithReportFocus), so
	// the border shows where keystrokes go.
	focused bool

	// confirmClose is armed when 'x' is pressed; the next key either confirms
	// the close ('x'/'y') or cancels it.
	confirmClose bool

	// beads is the latest `bd list` poll result (see beads.go); lastTitle is
	// the fleet summary most recently pushed to the terminal title, kept to
	// skip redundant tmux calls.
	beads     []Bead
	lastTitle string

	initialCwd  string
	initialArgs []string
}

func NewModel(mgr *reg.Manager, initialCwd string, initialArgs []string) *Model {
	return &Model{mgr: mgr, focused: true, initialCwd: initialCwd, initialArgs: initialArgs}
}

func (m *Model) Init() tea.Cmd { return tea.Batch(tick(), fetchBeadsNow(m.initialCwd)) }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.layout = ComputeLayout(msg.Width, msg.Height)
		m.ready = true
		if m.mgr.Count() == 0 { // fresh server — spawn the first session
			if err := m.mgr.Spawn(m.initialCwd, m.initialArgs); err != nil {
				debug.Logf("initial spawn: %v", err)
			}
		}
		return m, nil

	case tea.FocusMsg:
		m.focused = true
		return m, nil

	case tea.BlurMsg:
		m.focused = false
		return m, nil

	case tea.KeyMsg:
		return m, m.handleKey(msg)

	case tea.MouseMsg:
		m.handleMouse(msg)
		return m, nil

	case tickMsg:
		m.mgr.Prune() // drop sessions whose pane died; repaint refreshes timers
		m.syncTitle()
		return m, tick()

	case beadsMsg:
		m.beads = msg.beads
		if msg.noBD { // no bd on PATH — stay hidden and stop polling for good
			return m, nil
		}
		return m, fetchBeadsLater(m.initialCwd)

	case hooks.StatusEventMsg:
		if st, ok := statusForEvent(msg.Event); ok {
			m.mgr.MarkStatus(msg.Session, st)
		}
		m.mgr.SetContextTokens(msg.Session, msg.Tokens)
		m.mgr.SetTranscript(msg.Session, msg.Transcript)
		m.syncTitle()
		return m, m.maybeSummarize(msg)

	case summaryMsg:
		m.mgr.SetSummary(msg.ID, msg.Topic, msg.Task, msg.Phase)
		return m, nil
	}
	return m, nil
}

// maybeSummarize kicks off a Haiku re-label of a session from its transcript
// tail. It fires on the first prompt (so labels appear quickly) and then every
// resummarizeEvery prompts, skipping if a call is already in flight. Runs off
// the UI thread as a tea.Cmd; falls back to a heuristic task label on error.
func (m *Model) maybeSummarize(msg hooks.StatusEventMsg) tea.Cmd {
	if msg.Prompt == "" { // only user-prompt events advance the clock
		return nil
	}
	s := m.mgr.Find(msg.Session)
	if s == nil {
		return nil
	}
	s.Prompts++
	due := s.Topic == "" || s.Prompts%resummarizeEvery == 0
	if s.Titling || !due {
		return nil
	}
	s.Titling = true
	id, path, prompt, prevTopic := msg.Session, s.TranscriptPath, msg.Prompt, s.Topic
	return func() tea.Msg {
		text := titler.TranscriptTail(path, transcriptTailChars)
		if text == "" { // transcript not readable yet — fall back to the prompt
			text = prompt
		}
		sum, err := titler.Summarize(text, prevTopic)
		if err != nil || sum.Task == "" {
			sum.Task = titler.Heuristic(prompt)
		}
		return summaryMsg{ID: id, Topic: sum.Topic, Task: sum.Task, Phase: sum.Phase}
	}
}

// statusForEvent maps a hook event name to a sidebar status. "start" is
// intentionally unmapped — a freshly launched session stays neutral until the
// first prompt makes it crunch.
func statusForEvent(event string) (reg.Status, bool) {
	switch event {
	case "crunching":
		return reg.StatusCrunching, true
	case "waiting":
		return reg.StatusWaiting, true
	case "idle":
		return reg.StatusIdle, true
	case "done":
		return reg.StatusDone, true
	case "exit":
		return reg.StatusExited, true
	}
	return 0, false
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	if m.confirmClose { // a close is armed — this key confirms or cancels it
		return m.handleConfirmClose(k)
	}
	rune1 := ""
	if k.Type == tea.KeyRunes && len(k.Runes) == 1 {
		rune1 = string(k.Runes[0])
	}
	switch {
	case k.Type == tea.KeyEnter, k.Type == tea.KeyTab:
		m.mgr.FocusActive() // hand keyboard focus to the session pane
	case k.Type == tea.KeyUp, rune1 == "k":
		m.mgr.Activate(m.mgr.ActiveIndex() - 1)
	case k.Type == tea.KeyDown, rune1 == "j":
		m.mgr.Activate(m.mgr.ActiveIndex() + 1)
	case rune1 == "n":
		if err := m.mgr.Spawn(m.initialCwd, m.initialArgs); err != nil {
			debug.Logf("spawn: %v", err)
		} else {
			m.mgr.FocusActive()
		}
	case rune1 == "x":
		if m.mgr.Count() > 0 { // arm confirmation; the next key decides
			m.confirmClose = true
		}
	case rune1 == "q", k.Type == tea.KeyCtrlC:
		// Detach the client; the tmux server — and every Claude — lives on.
		// The sidebar keeps running inside its pane, ready for the reattach.
		m.mgr.Detach()
	case rune1 >= "1" && rune1 <= "9":
		m.mgr.Activate(int(k.Runes[0] - '1'))
	}
	return nil
}

// handleConfirmClose resolves an armed close: 'x' or 'y' confirms and closes
// the active session, any other key cancels. Either way the prompt clears.
func (m *Model) handleConfirmClose(k tea.KeyMsg) tea.Cmd {
	m.confirmClose = false
	if k.Type == tea.KeyRunes && len(k.Runes) == 1 {
		if r := k.Runes[0]; r == 'x' || r == 'y' {
			m.mgr.Close(m.mgr.ActiveIndex())
		}
	}
	return nil
}

// handleMouse: a click on a session row switches to it and hands focus to the
// session pane. Coordinates are pane-local — tmux only delivers events inside
// the sidebar pane, so there is nothing else to hit.
func (m *Model) handleMouse(ms tea.MouseMsg) {
	if ms.Action != tea.MouseActionPress || ms.Button != tea.MouseButtonLeft {
		return
	}
	if idx := m.layout.SessionIndexAt(ms.X, ms.Y, m.mgr.Count()); idx >= 0 {
		debug.Logf("mouse press at (%d,%d) hit row %d", ms.X, ms.Y, idx)
		m.mgr.Activate(idx)
		m.mgr.FocusActive()
	}
}

func (m *Model) View() string {
	if !m.ready {
		return "starting keen…"
	}
	return RenderSidebar(m.layout, m.mgr.Sessions(), m.beads, m.mgr.ActiveIndex(), m.focused, m.confirmClose)
}

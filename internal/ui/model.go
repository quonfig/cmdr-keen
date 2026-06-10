package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/quonfig/cmdr-keen/internal/debug"
	"github.com/quonfig/cmdr-keen/internal/hooks"
	"github.com/quonfig/cmdr-keen/internal/session"
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

// tickInterval is how often the model wakes to repaint so the sidebar's elapsed
// timers advance even while every session is quiet. The timer is minute-coarse,
// so a 1s cadence is plenty without being a busy-loop.
const tickInterval = time.Second

// tickMsg is the periodic repaint nudge that keeps the elapsed timers live.
type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(tickInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// transcriptTailChars is how much of the recent transcript we feed the titler.
const transcriptTailChars = 4000

// Model is the top-level Bubble Tea model: a fixed-order list of sessions and
// an embedded pane for the active one. Focus is either the pane (keystrokes go
// to Claude — the default) or the sidebar (keystrokes navigate). Ctrl-K toggles.
type Model struct {
	mgr          *session.Manager
	layout       Layout
	sidebarFocus bool
	ready        bool

	// confirmClose is armed when 'x' is pressed in the sidebar; the next key
	// either confirms the close ('x'/'y') or cancels it. Guards a running
	// session against an accidental single keystroke.
	confirmClose bool

	initialCwd  string
	initialArgs []string
}

func NewModel(initialCwd string, initialArgs []string) *Model {
	return &Model{initialCwd: initialCwd, initialArgs: initialArgs}
}

// SetManager wires the session manager (created with the program's send func)
// after tea.NewProgram but before Run.
func (m *Model) SetManager(mgr *session.Manager) { m.mgr = mgr }

func (m *Model) Init() tea.Cmd { return tick() }

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.layout = ComputeLayout(msg.Width, msg.Height)
		m.ready = true
		m.mgr.Resize(m.layout.PaneW, m.layout.PaneH)
		if m.mgr.Count() == 0 { // spawn the first session at the real pane size
			_ = m.mgr.Spawn(m.initialCwd, m.initialArgs)
		}
		return m, nil

	case tea.KeyMsg:
		return m, m.handleKey(msg)

	case tea.MouseMsg:
		m.handleMouse(msg)
		return m, nil

	case tickMsg:
		return m, tick() // repaint so elapsed timers advance; reschedule

	case session.OutputMsg:
		return m, nil // a session painted; Bubble Tea will re-render the view

	case session.ExitMsg:
		m.mgr.MarkStatus(msg.ID, session.StatusExited)
		return m, nil

	case hooks.StatusEventMsg:
		if st, ok := statusForEvent(msg.Event); ok {
			m.mgr.MarkStatus(msg.Session, st)
		}
		m.mgr.SetContextTokens(msg.Session, msg.Tokens)
		m.mgr.SetTranscript(msg.Session, msg.Transcript)
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
func statusForEvent(event string) (session.Status, bool) {
	switch event {
	case "crunching":
		return session.StatusCrunching, true
	case "waiting":
		return session.StatusWaiting, true
	case "idle":
		return session.StatusIdle, true
	case "done":
		return session.StatusDone, true
	case "exit":
		return session.StatusExited, true
	}
	return 0, false
}

func (m *Model) handleKey(k tea.KeyMsg) tea.Cmd {
	if m.confirmClose { // a close is armed — this key confirms or cancels it
		return m.handleConfirmClose(k)
	}
	if k.Type == tea.KeyCtrlK { // the keen prefix — never reaches Claude
		m.sidebarFocus = !m.sidebarFocus
		return nil
	}
	if m.sidebarFocus {
		return m.handleSidebarKey(k)
	}
	if s := m.mgr.Active(); s != nil {
		b := KeyToBytes(k)
		debug.Logf("key -> %s (idx %d): %q", s.ID, m.mgr.ActiveIndex(), b)
		s.ResetScroll() // typing jumps back to the live bottom
		s.Write(b)
	}
	return nil
}

func (m *Model) handleSidebarKey(k tea.KeyMsg) tea.Cmd {
	rune1 := ""
	if k.Type == tea.KeyRunes && len(k.Runes) == 1 {
		rune1 = string(k.Runes[0])
	}
	switch {
	case k.Type == tea.KeyEnter:
		m.sidebarFocus = false // hand control back to the session
	case k.Type == tea.KeyUp, rune1 == "k":
		m.mgr.SetActive(m.mgr.ActiveIndex()-1, "key:up")
	case k.Type == tea.KeyDown, rune1 == "j":
		m.mgr.SetActive(m.mgr.ActiveIndex()+1, "key:down")
	case rune1 == "n":
		_ = m.mgr.Spawn(m.initialCwd, m.initialArgs)
		m.sidebarFocus = false
	case rune1 == "x":
		if m.mgr.Count() > 0 { // arm confirmation; the next key decides
			m.confirmClose = true
		}
	case rune1 == "q", k.Type == tea.KeyCtrlC:
		return tea.Quit
	case rune1 >= "1" && rune1 <= "9":
		m.mgr.SetActive(int(k.Runes[0]-'1'), "key:number")
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

func (m *Model) handleMouse(ms tea.MouseMsg) {
	// Click on a sidebar row → switch to it and hand control to the session.
	if idx := m.layout.SessionIndexAt(ms.X, ms.Y, m.mgr.Count()); idx >= 0 {
		if ms.Action == tea.MouseActionPress {
			debug.Logf("mouse press at (%d,%d) hit sidebar row idx %d (button %v, action %v)", ms.X, ms.Y, idx, ms.Button, ms.Action)
			m.mgr.SetActive(idx, "mouse")
			m.sidebarFocus = false
		}
		return
	}
	// Otherwise the event belongs to the active session's pane.
	if !m.layout.InPane(ms.X, ms.Y) {
		return
	}
	s := m.mgr.Active()
	if s == nil {
		return
	}
	// A wheel notch scrolls keen's own scrollback unless Claude is tracking the
	// mouse, in which case the wheel is Claude's to handle. Clicks/drags/motion
	// always go into the vt (it drops them when Claude isn't tracking).
	switch {
	case ms.Button == tea.MouseButtonWheelUp && !s.MouseEnabled():
		s.ScrollBy(1)
	case ms.Button == tea.MouseButtonWheelDown && !s.MouseEnabled():
		s.ScrollBy(-1)
	default:
		if ev, ok := MouseToVT(ms, m.layout); ok {
			s.SendMouse(ev)
		}
	}
}

func (m *Model) View() string {
	if !m.ready {
		return "starting keen…"
	}
	sidebar := RenderSidebar(m.layout, m.mgr.Sessions(), m.mgr.ActiveIndex(), m.sidebarFocus, m.confirmClose)
	pane := RenderPane(m.layout, m.mgr.Active(), !m.sidebarFocus)
	return lipgloss.JoinHorizontal(lipgloss.Top, sidebar, pane)
}

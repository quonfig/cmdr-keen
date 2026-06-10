package session

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quonfig/cmdr-keen/internal/debug"
	"github.com/quonfig/cmdr-keen/internal/hooks"
)

// Manager holds the fixed-order list of sessions and which one is active.
// It is only touched from the Bubble Tea update loop (single-threaded), except
// that sessions' own goroutines call the shared send func — so no locking here.
type Manager struct {
	sessions     []*Session
	active       int
	send         func(tea.Msg)
	hookCfg      *hooks.Config
	paneW, paneH int
}

func NewManager(send func(tea.Msg), hookCfg *hooks.Config) *Manager {
	return &Manager{send: send, hookCfg: hookCfg, active: -1, paneW: 80, paneH: 24}
}

// Spawn starts a new session at cwd and makes it active.
func (m *Manager) Spawn(cwd string, args []string) error {
	s, err := New(cwd, args, m.paneW, m.paneH, m.hookCfg, m.send)
	if err != nil {
		return err
	}
	m.sessions = append(m.sessions, s)
	m.setActive(len(m.sessions)-1, "spawn")
	return nil
}

func (m *Manager) Sessions() []*Session { return m.sessions }
func (m *Manager) Count() int           { return len(m.sessions) }
func (m *Manager) ActiveIndex() int     { return m.active }

func (m *Manager) Active() *Session {
	if m.active < 0 || m.active >= len(m.sessions) {
		return nil
	}
	return m.sessions[m.active]
}

// SetActive switches the focused session. reason is a short tag (e.g. "mouse",
// "key:j", "number") recorded in the debug log so a stray switch can be traced
// to what triggered it.
func (m *Manager) SetActive(i int, reason string) { m.setActive(i, reason) }

func (m *Manager) setActive(i int, reason string) {
	if i < 0 || i >= len(m.sessions) {
		debug.Logf("setActive(%d, %q) ignored: out of range (count %d, active %d)", i, reason, len(m.sessions), m.active)
		return
	}
	if i != m.active && debug.Enabled() {
		from, to := "-", m.sessions[i].ID
		if m.active >= 0 && m.active < len(m.sessions) {
			from = m.sessions[m.active].ID
		}
		debug.Logf("setActive: %s (idx %d) -> %s (idx %d) reason=%s", from, m.active, to, i, reason)
	}
	for j, s := range m.sessions {
		if j == i {
			s.Focus()
		} else {
			s.Blur()
		}
	}
	m.active = i
}

func (m *Manager) Find(id string) *Session {
	for _, s := range m.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// Resize stores the pane size and applies it to every session.
func (m *Manager) Resize(paneW, paneH int) {
	m.paneW, m.paneH = paneW, paneH
	for _, s := range m.sessions {
		s.Resize(paneW, paneH)
	}
}

// Close kills and removes session i, keeping the active index sane.
func (m *Manager) Close(i int) {
	if i < 0 || i >= len(m.sessions) {
		return
	}
	m.sessions[i].Close()
	m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
	if m.active >= len(m.sessions) {
		m.active = len(m.sessions) - 1
	}
	if m.active >= 0 {
		m.setActive(m.active, "close")
	}
}

func (m *Manager) MarkStatus(id string, st Status) {
	if s := m.Find(id); s != nil {
		// Don't let a stray permission "waiting" repaint a finished session red.
		// A real permission prompt fires mid-turn (while Crunching), never after
		// Stop, so a StatusWaiting arriving while Done is necessarily the ~60s
		// idle Notification whose wording drifted past isIdleNotification. Don't
		// swallow it — that leaves the green ✓ stuck exactly when the idle ping
		// means to say "this finished session is waiting on you". Reclassify it
		// as the idle ping it really is, so it surfaces as magenta "needs you"
		// instead of looking complete. (A correctly-tagged StatusIdle already
		// flows through unchanged and overrides Done.)
		if st == StatusWaiting && s.Status == StatusDone {
			st = StatusIdle
		}
		if s.Status != st { // reset the elapsed timer only on a real transition
			s.StatusSince = time.Now()
		}
		s.Status = st
	}
}

// SetSummary applies a fresh set of LLM labels and clears the in-flight guard.
// Empty fields are left untouched so a partial or flaky re-summarize never wipes
// a good topic/task/phase we already had.
func (m *Manager) SetSummary(id, topic, task, phase string) {
	s := m.Find(id)
	if s == nil {
		return
	}
	s.Titling = false
	if topic != "" {
		s.Topic = topic
	}
	if task != "" {
		s.Task = task
	}
	if phase != "" {
		s.Phase = phase
	}
}

// SetTranscript records the latest transcript path reported for a session, used
// as the source for re-summarizing.
func (m *Manager) SetTranscript(id, path string) {
	if s := m.Find(id); s != nil && path != "" {
		s.TranscriptPath = path
	}
}

// SetContextTokens records a session's reported context-window usage. Zero is
// treated as "no fresh reading" and left as-is, so an event without a token
// count never wipes a good value.
func (m *Manager) SetContextTokens(id string, tokens int) {
	if s := m.Find(id); s != nil && tokens > 0 {
		s.ContextTokens = tokens
	}
}

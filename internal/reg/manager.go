package reg

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/quonfig/cmdr-keen/internal/debug"
	"github.com/quonfig/cmdr-keen/internal/hooks"
	"github.com/quonfig/cmdr-keen/internal/tmuxctl"
)

// SessionName is the tmux session keen lives in.
const SessionName = "keen"

// fieldSep separates fields in scan format strings. Unit separator: it can't
// appear in pane ids, paths, or the labels keen writes.
const fieldSep = "\x1f"

// scanFormat is the per-pane format used to rebuild the registry. Field order
// is mirrored by parseScanLine.
const scanFormat = "#{pane_id}" + fieldSep +
	"#{window_id}" + fieldSep +
	"#{@keen-session}" + fieldSep +
	"#{@keen-seq}" + fieldSep +
	"#{@keen-name}" + fieldSep +
	"#{@keen-branch}" + fieldSep +
	"#{@keen-cwd}" + fieldSep +
	"#{@keen-status}" + fieldSep +
	"#{@keen-topic}" + fieldSep +
	"#{@keen-task}" + fieldSep +
	"#{@keen-phase}" + fieldSep +
	"#{@keen-tokens}" + fieldSep +
	"#{@keen-transcript}"

// Manager owns the ordered session list and every tmux operation that touches
// it. It is only called from the Bubble Tea update loop (single-threaded), so
// it needs no locking.
type Manager struct {
	T            *tmuxctl.Server
	SidebarPane  string // the sidebar's own pane id ($TMUX_PANE)
	ViewWindow   string // window id the sidebar lives in — sessions appear here
	SidebarWidth int    // columns the sidebar reclaims after splicing a pane in
	HookSocket   string // unix socket sessions report lifecycle events to
	HookBin      string // command prefix Claude invokes at each lifecycle event

	sessions []*Session
	active   int
}

func NewManager(t *tmuxctl.Server, sidebarPane, viewWindow, hookSocket, hookBin string, sidebarWidth int) *Manager {
	return &Manager{
		T: t, SidebarPane: sidebarPane, ViewWindow: viewWindow,
		SidebarWidth: sidebarWidth, HookSocket: hookSocket, HookBin: hookBin,
		active: -1,
	}
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

func (m *Manager) Find(id string) *Session {
	for _, s := range m.sessions {
		if s.ID == id {
			return s
		}
	}
	return nil
}

// Scan rebuilds the registry from pane options — the startup path, and what
// makes keen survive its own death: sessions, labels, and statuses all come
// back from the panes themselves.
func (m *Manager) Scan() error {
	lines, err := m.T.ListPanes(SessionName, scanFormat)
	if err != nil {
		return err
	}
	m.sessions = m.sessions[:0]
	visible := ""
	for _, ln := range lines {
		s, windowID, ok := parseScanLine(ln)
		if !ok {
			continue
		}
		s.settingsPath = settingsPath(m.T.Socket, s.ID)
		m.sessions = append(m.sessions, s)
		if windowID == m.ViewWindow && s.PaneID != m.SidebarPane {
			visible = s.ID
		}
	}
	sortBySeq(m.sessions)
	m.active = -1
	for i, s := range m.sessions {
		if s.ID == visible {
			m.active = i
		}
	}
	if m.active < 0 && len(m.sessions) > 0 {
		m.active = 0
	}
	debug.Logf("scan: %d sessions, active idx %d", len(m.sessions), m.active)
	return nil
}

// parseScanLine decodes one scanFormat line. Panes without a @keen-session
// option (the sidebar itself, anything the user conjured by hand) report ok =
// false.
func parseScanLine(line string) (s *Session, windowID string, ok bool) {
	f := strings.Split(line, fieldSep)
	if len(f) != 13 || f[2] == "" {
		return nil, "", false
	}
	seq, _ := strconv.Atoi(f[3])
	tokens, _ := strconv.Atoi(f[11])
	return &Session{
		PaneID: f[0], ID: f[2], Seq: seq,
		Name: f[4], Branch: f[5], Cwd: f[6],
		Status: ParseStatus(f[7]),
		Topic:  f[8], Task: f[9], Phase: f[10],
		ContextTokens: tokens, TranscriptPath: f[12],
	}, f[1], true
}

func sortBySeq(ss []*Session) {
	for i := 1; i < len(ss); i++ { // insertion sort; n is ~10
		for j := i; j > 0 && ss[j-1].Seq > ss[j].Seq; j-- {
			ss[j-1], ss[j] = ss[j], ss[j-1]
		}
	}
}

// Prune drops sessions whose pane has died (Claude exited, `exit` typed, pane
// killed by hand). Returns true if anything changed. Called on the UI tick.
func (m *Manager) Prune() bool {
	lines, err := m.T.ListPanes(SessionName, "#{pane_id}")
	if err != nil {
		return false
	}
	alive := make(map[string]bool, len(lines))
	for _, p := range lines {
		alive[p] = true
	}
	changed := false
	kept := m.sessions[:0]
	activeID := ""
	if a := m.Active(); a != nil {
		activeID = a.ID
	}
	for _, s := range m.sessions {
		if alive[s.PaneID] {
			kept = append(kept, s)
			continue
		}
		debug.Logf("prune: session %s pane %s died", s.ID, s.PaneID)
		if s.settingsPath != "" {
			_ = os.Remove(s.settingsPath)
		}
		changed = true
	}
	m.sessions = kept
	if changed {
		m.active = -1
		for i, s := range m.sessions {
			if s.ID == activeID {
				m.active = i
			}
		}
		if m.active < 0 && len(m.sessions) > 0 {
			m.active = 0
		}
	}
	return changed
}

// Spawn starts a new session at cwd in a background window, persists its
// metadata onto the pane, and brings it into view as the active session.
func (m *Manager) Spawn(cwd string, args []string) error {
	if len(args) == 0 {
		args = []string{"claude"}
	}
	id := newID()
	seq := m.nextSeq()

	env := map[string]string{}
	cmd := args
	settings := ""
	// Wire hooks only for real claude sessions (don't break `keen -- bash`).
	if isClaude(args[0]) {
		sp := settingsPath(m.T.Socket, id)
		if err := hooks.WriteSettings(sp, m.HookBin); err == nil {
			settings = sp
			cmd = append([]string{args[0], "--settings", sp}, args[1:]...)
			env["KEEN_SOCKET"] = m.HookSocket
			env["KEEN_SESSION"] = id
		}
	}

	pane, err := m.T.NewWindow(SessionName, id, cwd, env, cmd)
	if err != nil {
		return err
	}

	s := &Session{
		ID: id, PaneID: pane, Seq: seq,
		Name: filepath.Base(cwd), Branch: gitBranch(cwd), Cwd: cwd,
		Status: StatusStarting, StatusSince: time.Now(),
		settingsPath: settings,
	}
	m.persistIdentity(s)
	m.persistStatus(s)
	m.sessions = append(m.sessions, s)
	m.bringIntoView(s)
	m.active = len(m.sessions) - 1
	debug.Logf("spawn: %s pane %s seq %d cwd %s", id, pane, seq, cwd)
	return nil
}

// Activate brings session i into view (without stealing keyboard focus from
// the sidebar — Enter or a click hands focus over via FocusActive).
func (m *Manager) Activate(i int) {
	if i < 0 || i >= len(m.sessions) {
		return
	}
	m.bringIntoView(m.sessions[i])
	m.active = i
}

// FocusActive moves tmux's pane focus to the active session, so keystrokes go
// to Claude.
func (m *Manager) FocusActive() {
	if s := m.Active(); s != nil {
		_ = m.T.SelectPane(s.PaneID)
	}
}

// bringIntoView puts s's pane in the view window next to the sidebar. The
// invariant: every session pane is either the view window's right pane or the
// sole pane of a background window. swap preserves it; join consumes the
// background window of the pane being brought in.
func (m *Manager) bringIntoView(s *Session) {
	vis := m.visiblePane()
	switch vis {
	case s.PaneID: // already showing
	case "":
		// Nothing beside the sidebar yet — splice the pane in and reclaim the
		// sidebar's fixed width.
		if err := m.T.JoinPane(s.PaneID, m.SidebarPane); err != nil {
			debug.Logf("join-pane %s: %v", s.PaneID, err)
			return
		}
		_ = m.T.ResizePaneWidth(m.SidebarPane, m.SidebarWidth)
	default:
		if err := m.T.SwapPanes(s.PaneID, vis); err != nil {
			debug.Logf("swap-pane %s <-> %s: %v", s.PaneID, vis, err)
		}
	}
}

// visiblePane returns the pane id currently sharing the view window with the
// sidebar, or "" when the sidebar is alone.
func (m *Manager) visiblePane() string {
	panes, err := m.T.ListWindowPanes(m.ViewWindow, "#{pane_id}")
	if err != nil {
		return ""
	}
	for _, p := range panes {
		if p != m.SidebarPane {
			return p
		}
	}
	return ""
}

// Close kills session i's pane. If it was the visible one and others remain,
// the next session is swapped into view first so the view window never
// collapses.
func (m *Manager) Close(i int) {
	if i < 0 || i >= len(m.sessions) {
		return
	}
	s := m.sessions[i]
	if m.visiblePane() == s.PaneID && len(m.sessions) > 1 {
		next := i + 1
		if next >= len(m.sessions) {
			next = i - 1
		}
		m.bringIntoView(m.sessions[next]) // swap parks s in a background window
	}
	if err := m.T.KillPane(s.PaneID); err != nil {
		debug.Logf("kill-pane %s: %v", s.PaneID, err)
	}
	if s.settingsPath != "" {
		_ = os.Remove(s.settingsPath)
	}
	m.sessions = append(m.sessions[:i], m.sessions[i+1:]...)
	if m.active >= len(m.sessions) {
		m.active = len(m.sessions) - 1
	}
}

// Detach disconnects every attached client. Sessions keep running inside the
// tmux server; `keen` reattaches to exactly this state.
func (m *Manager) Detach() { _ = m.T.DetachAll(SessionName) }

// MarkStatus applies a hook-reported status, with the Done-precedence backstop
// (a "waiting" that arrives while Done is really the idle ping — see
// internal/hook). Persists so a restarted sidebar shows the right colors.
func (m *Manager) MarkStatus(id string, st Status) {
	s := m.Find(id)
	if s == nil {
		return
	}
	if st == StatusWaiting && s.Status == StatusDone {
		st = StatusIdle
	}
	if s.Status != st { // reset the elapsed timer only on a real transition
		s.StatusSince = time.Now()
	}
	s.Status = st
	m.persistStatus(s)
}

// SetSummary applies fresh LLM labels and clears the in-flight guard. Empty
// fields are left untouched so a flaky re-summarize never wipes good labels.
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
	m.persistLabels(s)
}

// SetTranscript records the latest transcript path reported for a session.
func (m *Manager) SetTranscript(id, path string) {
	if s := m.Find(id); s != nil && path != "" && s.TranscriptPath != path {
		s.TranscriptPath = path
		_ = m.T.SetPaneOption(s.PaneID, "@keen-transcript", path)
	}
}

// SetContextTokens records a session's reported context-window usage. Zero is
// "no fresh reading" and never wipes a good value.
func (m *Manager) SetContextTokens(id string, tokens int) {
	if s := m.Find(id); s != nil && tokens > 0 {
		s.ContextTokens = tokens
		_ = m.T.SetPaneOption(s.PaneID, "@keen-tokens", strconv.Itoa(tokens))
	}
}

// persistIdentity writes the immutable per-session options at spawn.
func (m *Manager) persistIdentity(s *Session) {
	set := func(k, v string) { _ = m.T.SetPaneOption(s.PaneID, k, v) }
	set("@keen-session", s.ID)
	set("@keen-seq", strconv.Itoa(s.Seq))
	set("@keen-name", s.Name)
	set("@keen-branch", s.Branch)
	set("@keen-cwd", s.Cwd)
}

func (m *Manager) persistStatus(s *Session) {
	_ = m.T.SetPaneOption(s.PaneID, "@keen-status", s.Status.String())
}

func (m *Manager) persistLabels(s *Session) {
	set := func(k, v string) { _ = m.T.SetPaneOption(s.PaneID, k, v) }
	set("@keen-topic", s.Topic)
	set("@keen-task", s.Task)
	set("@keen-phase", s.Phase)
}

func (m *Manager) nextSeq() int {
	n := 0
	for _, s := range m.sessions {
		if s.Seq >= n {
			n = s.Seq + 1
		}
	}
	return n
}

// newID returns a session id unique across sidebar restarts (ids live in pane
// options, so a plain counter would collide after a rescan).
func newID() string {
	return fmt.Sprintf("k%x", time.Now().UnixNano()&0xffffffffff)
}

// settingsPath is where a session's generated hooks settings file lives. Keyed
// by server socket + session id (not pid — the sidebar that cleans up may not
// be the one that spawned).
func settingsPath(socket, id string) string {
	return filepath.Join("/tmp", fmt.Sprintf("keen-%s-%s-settings.json", socket, id))
}

// HookSocketPath is the unix socket sessions report lifecycle events to. One
// per tmux server (not per pid), so hooks keep working across sidebar
// restarts.
func HookSocketPath(socket string) string {
	return filepath.Join("/tmp", fmt.Sprintf("keen-%s.sock", socket))
}

func isClaude(cmd string) bool { return filepath.Base(cmd) == "claude" }

func gitBranch(cwd string) string {
	c := exec.Command("git", "-C", cwd, "rev-parse", "--abbrev-ref", "HEAD")
	out, err := c.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

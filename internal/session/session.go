// Package session owns a single Claude Code process: its PTY, its terminal
// emulator, and its lifecycle. The wrapper stays thin — output is fed into the
// emulator for rendering and the emulator's query replies are pumped back so
// Claude arms its input (the M0 lesson); keystrokes are written straight to the
// PTY by the UI layer.
package session

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/quonfig/cmdr-keen/internal/debug"
	"github.com/quonfig/cmdr-keen/internal/hooks"
)

// wheelStep is how many lines one mouse-wheel notch moves the scrollback view.
const wheelStep = 3

// Status is the high-level state shown in the sidebar. In M1 only Starting and
// Exited are driven (by lifecycle); Crunching/Waiting/Done get wired to Claude
// Code hooks in M2.
type Status int

const (
	StatusStarting Status = iota
	StatusCrunching
	StatusWaiting // blocked on you (permission) — fires mid-turn, shows red
	StatusIdle    // pinged you after going idle (~60s post-Stop) — shows magenta
	StatusDone
	StatusExited
)

// ElapsedLabel is a coarse "how long the session has been in its current state"
// label for the sidebar — minutes then hours (1m, 5m, 59m, 1h, …), with no
// seconds. It's only meaningful while there's something to wait on, so it
// returns "" for every state except Crunching and the two waiting-on-you states.
func (s *Session) ElapsedLabel() string {
	switch s.Status {
	case StatusCrunching, StatusWaiting, StatusIdle:
	default:
		return ""
	}
	if s.StatusSince.IsZero() {
		return ""
	}
	return formatElapsed(time.Since(s.StatusSince))
}

// formatElapsed renders a duration as a coarse, second-free label: whole
// minutes under an hour (sub-minute rounds up to "1m" so an active row is never
// blank), whole hours beyond that.
func formatElapsed(d time.Duration) string {
	if d < time.Hour {
		m := int(d / time.Minute)
		if m < 1 {
			m = 1
		}
		return strconv.Itoa(m) + "m"
	}
	return strconv.Itoa(int(d/time.Hour)) + "h"
}

// OutputMsg is sent (via the program) when a session produced output and the
// screen may need a repaint. ExitMsg is sent when the child process ends.
type OutputMsg struct{ ID string }
type ExitMsg struct{ ID string }

var idCounter int64

func nextID() string { return fmt.Sprintf("s%d", atomic.AddInt64(&idCounter, 1)) }

// Session is one Claude Code process and everything needed to render/drive it.
type Session struct {
	ID     string
	Name   string // directory basename
	Branch string // git branch, if any
	Cwd    string
	Status Status
	// StatusSince is when the session last entered its current Status, stamped by
	// Manager.MarkStatus on transitions. Drives the sidebar's "how long in this
	// state" timer (see ElapsedLabel).
	StatusSince time.Time

	// LLM-generated labels, refreshed periodically from the transcript tail.
	Topic string // overall project or goal — fairly stable
	Task  string // current activity — changes as the work moves
	Phase string // planning|building|testing|shipping|done ("" = unknown yet)

	ContextTokens  int    // input-side tokens in use, reported by hooks (0 = unknown)
	TranscriptPath string // latest transcript path reported by hooks, for re-summarizing
	Prompts        int    // user prompts seen — drives periodic re-summarizing
	Titling        bool   // a summarize call is in flight; guards against overlap

	emu          *vt.SafeEmulator
	ptmx         *os.File
	cmd          *exec.Cmd
	settingsPath string // generated hooks settings file to clean up on Close

	// scrollOffset is how many lines the pane view is scrolled back from the
	// live bottom (0 = following Claude's output). Touched only by the UI
	// goroutine (Bubble Tea Update/View run serially), so it needs no lock.
	scrollOffset int

	// mouseModes tracks which mouse-tracking DEC modes Claude has enabled, fed
	// by the emulator's mode callbacks (which fire on the PTY-reader goroutine).
	// Guarded by mu since MouseEnabled is read from the UI goroutine.
	mu         sync.Mutex
	mouseModes map[ansi.Mode]bool
}

// New spawns `args` (default: claude) in cwd, wired to a paneW×paneH emulator.
// When hookCfg is non-nil and the command is claude, it injects per-session
// hooks via `--settings` so status events flow back to keen. send is the
// program's message pump, used by the output/exit goroutines.
func New(cwd string, args []string, paneW, paneH int, hookCfg *hooks.Config, send func(tea.Msg)) (*Session, error) {
	if len(args) == 0 {
		args = []string{"claude"}
	}
	if paneW < 1 {
		paneW = 80
	}
	if paneH < 1 {
		paneH = 24
	}

	id := nextID()
	env := append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	finalArgs := args
	settingsPath := ""

	// Wire hooks only for real claude sessions (don't break `keen -- bash`).
	if hookCfg != nil && isClaude(args[0]) {
		sp := hookCfg.SettingsPath(id)
		if err := hooks.WriteSettings(sp, hookCfg.HookBin); err == nil {
			settingsPath = sp
			finalArgs = append([]string{args[0], "--settings", sp}, args[1:]...)
			env = append(env, "KEEN_SOCKET="+hookCfg.Socket, "KEEN_SESSION="+id)
		}
	}

	c := exec.Command(finalArgs[0], finalArgs[1:]...)
	c.Dir = cwd
	c.Env = env

	ptmx, err := pty.Start(c)
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(paneH), Cols: uint16(paneW)})

	emu := vt.NewSafeEmulator(paneW, paneH)

	s := &Session{
		ID:           id,
		Name:         filepath.Base(cwd),
		Branch:       gitBranch(cwd),
		Cwd:          cwd,
		Status:       StatusStarting,
		StatusSince:  time.Now(),
		emu:          emu,
		ptmx:         ptmx,
		cmd:          c,
		settingsPath: settingsPath,
		mouseModes:   map[ansi.Mode]bool{},
	}

	// Track Claude's mouse-tracking mode so keen knows whether a wheel event
	// should be forwarded to Claude or used to scroll keen's own scrollback.
	// Set before the reader goroutine starts, so it's safe without the lock.
	emu.SetCallbacks(vt.Callbacks{
		EnableMode:  s.onMode(true),
		DisableMode: s.onMode(false),
	})

	// PTY output -> emulator -> repaint nudge.
	go func() {
		buf := make([]byte, 8192)
		for {
			n, rerr := ptmx.Read(buf)
			if n > 0 {
				_, _ = emu.Write(buf[:n])
				send(OutputMsg{ID: s.ID})
			}
			if rerr != nil {
				send(ExitMsg{ID: s.ID})
				return
			}
		}
	}()

	// Emulator query replies (cursor reports, device attributes, …) -> PTY, so
	// Claude's startup handshake completes and input arms. See spike/README.md.
	// This is the only non-keystroke channel into Claude's stdin, so under
	// KEEN_DEBUG we log every chunk it sends — that's where phantom input would
	// surface if it ever carried prose (see cmdr-keen-j4k).
	go func() {
		if !debug.Enabled() {
			_, _ = io.Copy(ptmx, emu)
			return
		}
		buf := make([]byte, 8192)
		for {
			n, rerr := emu.Read(buf)
			if n > 0 {
				debug.Logf("emu -> %s ptmx: %q", s.ID, buf[:n])
				_, _ = ptmx.Write(buf[:n])
			}
			if rerr != nil {
				return
			}
		}
	}()

	return s, nil
}

// Write forwards input bytes to the child.
func (s *Session) Write(b []byte) {
	if len(b) > 0 {
		_, _ = s.ptmx.Write(b)
	}
}

// SendMouse hands a mouse event to the emulator, which forwards it to Claude
// using Claude's negotiated mouse mode/encoding (or drops it if Claude isn't
// tracking the mouse). Callers gate wheel events on MouseEnabled first.
func (s *Session) SendMouse(m vt.Mouse) { s.emu.SendMouse(m) }

// MouseEnabled reports whether Claude has turned on any mouse-tracking mode.
// When it has, wheel events belong to Claude; when it hasn't, keen treats the
// wheel as scrollback navigation.
func (s *Session) MouseEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.mouseModes) > 0
}

// onMode returns an EnableMode/DisableMode callback (set=true for enable) that
// records mouse-tracking mode changes as Claude requests them.
func (s *Session) onMode(set bool) func(ansi.Mode) {
	return func(m ansi.Mode) {
		if !isMouseMode(m) {
			return
		}
		s.mu.Lock()
		if set {
			s.mouseModes[m] = true
		} else {
			delete(s.mouseModes, m)
		}
		s.mu.Unlock()
	}
}

// isMouseMode reports whether m is one of the DEC mouse-tracking modes.
func isMouseMode(m ansi.Mode) bool {
	switch m {
	case ansi.ModeMouseX10, ansi.ModeMouseNormal, ansi.ModeMouseHighlight,
		ansi.ModeMouseButtonEvent, ansi.ModeMouseAnyEvent:
		return true
	}
	return false
}

// ScrollBy moves the pane view by notches (positive = up/back into history,
// negative = down toward live output), clamped to the scrollback extent.
func (s *Session) ScrollBy(notches int) {
	s.scrollOffset += notches * wheelStep
	if s.scrollOffset < 0 {
		s.scrollOffset = 0
	}
	if max := s.emu.ScrollbackLen(); s.scrollOffset > max {
		s.scrollOffset = max
	}
}

// ResetScroll snaps the pane view back to the live bottom.
func (s *Session) ResetScroll() { s.scrollOffset = 0 }

// ScrollOffset is how many lines the view is scrolled back (0 = live).
func (s *Session) ScrollOffset() int { return s.scrollOffset }

// Render returns the pane view (paneW×paneH) as a styled string: the live
// emulated screen, or — when scrolled back — a window into the scrollback.
func (s *Session) Render() string {
	if s.scrollOffset <= 0 {
		return s.emu.Render()
	}
	return s.renderScrollback()
}

// renderScrollback composes a paneW×paneH window over the scrollback buffer
// followed by the live screen, ending scrollOffset lines above the bottom.
func (s *Session) renderScrollback() string {
	w, h := s.emu.Width(), s.emu.Height()
	sbLen := s.emu.ScrollbackLen()
	off := s.scrollOffset
	if off > sbLen {
		off = sbLen
	}
	// Combined rows are [0,sbLen) scrollback then [sbLen,sbLen+h) live screen.
	top := sbLen - off
	buf := uv.NewRenderBuffer(w, h)
	for row := 0; row < h; row++ {
		gi := top + row
		for x := 0; x < w; x++ {
			var c *uv.Cell
			if gi < sbLen {
				c = s.emu.ScrollbackCellAt(x, gi)
			} else {
				c = s.emu.CellAt(x, gi-sbLen)
			}
			if c != nil && c.Content != "" {
				buf.SetCell(x, row, c)
			}
		}
	}
	return buf.Render()
}

// Resize matches the emulator and PTY to a new pane size.
func (s *Session) Resize(w, h int) {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	s.scrollOffset = 0 // scrollback geometry shifts under a resize
	s.emu.Resize(w, h)
	_ = pty.Setsize(s.ptmx, &pty.Winsize{Rows: uint16(h), Cols: uint16(w)})
}

func (s *Session) Focus() { s.emu.Focus() }
func (s *Session) Blur()  { s.emu.Blur() }

// Close kills the child and releases the PTY.
func (s *Session) Close() {
	if s.cmd != nil && s.cmd.Process != nil {
		_ = s.cmd.Process.Kill()
	}
	_ = s.ptmx.Close()
	if s.settingsPath != "" {
		_ = os.Remove(s.settingsPath)
	}
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

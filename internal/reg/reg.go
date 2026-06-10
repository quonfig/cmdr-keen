// Package reg is keen's session registry on top of tmux. One Claude session =
// one tmux pane, living either beside the sidebar (visible) or parked as the
// sole pane of a background window. Everything keen knows about a session —
// id, spawn order, labels, status — is persisted as @keen-* user options on
// the pane itself, so a restarted sidebar rebuilds the registry by scanning
// panes. tmux owns the processes and their terminals; reg owns the metadata.
package reg

import (
	"strconv"
	"time"
)

// Status is the high-level state shown in the sidebar, driven by Claude Code
// lifecycle hooks (see internal/hooks).
type Status int

const (
	StatusStarting Status = iota
	StatusCrunching
	StatusWaiting // blocked on you (permission) — fires mid-turn, shows red
	StatusIdle    // pinged you after going idle (~60s post-Stop) — shows magenta
	StatusDone
	StatusExited
)

// statusNames is the wire form used in pane options — stable strings, not
// ints, so a keen upgrade that reorders the enum can't corrupt live sessions.
var statusNames = map[Status]string{
	StatusStarting:  "starting",
	StatusCrunching: "crunching",
	StatusWaiting:   "waiting",
	StatusIdle:      "idle",
	StatusDone:      "done",
	StatusExited:    "exited",
}

func (s Status) String() string { return statusNames[s] }

// ParseStatus is the inverse of String; unknown input maps to StatusStarting.
func ParseStatus(s string) Status {
	for st, name := range statusNames {
		if name == s {
			return st
		}
	}
	return StatusStarting
}

// Session is one Claude Code process as keen sees it. The process itself, its
// PTY, and its rendering all belong to tmux; this is pure bookkeeping.
type Session struct {
	ID     string // keen's own id, stable across sidebar restarts
	PaneID string // tmux pane id (%N) — stable as the pane moves between windows
	Seq    int    // spawn order; the sidebar lists sessions by this, fixed forever

	Name   string // directory basename
	Branch string // git branch at spawn, if any
	Cwd    string

	Status Status
	// StatusSince is when the session last entered its current Status. Drives
	// the sidebar's elapsed timer. Zero after a sidebar restart (we don't
	// persist it) — ElapsedLabel treats zero as "no timer".
	StatusSince time.Time

	// LLM-generated labels, refreshed from the transcript tail (see titler).
	Topic string // overall project or goal — fairly stable
	Task  string // current activity — changes as the work moves
	Phase string // planning|building|testing|shipping|done ("" = unknown yet)

	ContextTokens  int    // input-side tokens in use, reported by hooks (0 = unknown)
	TranscriptPath string // latest transcript path reported by hooks
	Prompts        int    // user prompts seen — drives periodic re-summarizing
	Titling        bool   // a summarize call is in flight; guards against overlap

	settingsPath string // generated hooks settings file to clean up on Close
}

// ElapsedLabel is a coarse "how long in this state" label for the sidebar.
// Only meaningful while there's something to wait on, so it returns "" for
// every state except Crunching and the two waiting-on-you states.
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
// minutes under an hour (sub-minute rounds up to "1m" so an active row is
// never blank), whole hours beyond that.
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

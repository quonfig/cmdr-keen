package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quonfig/cmdr-keen/internal/session"
)

// runeKey builds a single-rune key message for driving the key handlers.
func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestConfirmCloseArmingGuard(t *testing.T) {
	// With no sessions, 'x' must not arm a close — there's nothing to close and
	// a dangling prompt would be confusing.
	m := &Model{mgr: session.NewManager(func(tea.Msg) {}, nil), sidebarFocus: true}
	m.handleSidebarKey(runeKey('x'))
	if m.confirmClose {
		t.Fatal("x armed confirmation with no sessions; want no-op")
	}
}

func TestConfirmCloseCancelsOnOtherKey(t *testing.T) {
	// Any key other than x/y cancels an armed close and leaves the flag clear.
	for _, k := range []tea.KeyMsg{runeKey('n'), runeKey('j'), {Type: tea.KeyEnter}, {Type: tea.KeyCtrlK}} {
		m := &Model{mgr: session.NewManager(func(tea.Msg) {}, nil), sidebarFocus: true, confirmClose: true}
		m.handleKey(k)
		if m.confirmClose {
			t.Errorf("key %v left confirmation armed; want cancelled", k)
		}
	}
}

func TestConfirmCloseConfirmKeys(t *testing.T) {
	// x and y both resolve the prompt (and trigger the close); the flag clears.
	for _, r := range []rune{'x', 'y'} {
		m := &Model{mgr: session.NewManager(func(tea.Msg) {}, nil), sidebarFocus: true, confirmClose: true}
		m.handleKey(runeKey(r))
		if m.confirmClose {
			t.Errorf("%q did not resolve the confirmation", r)
		}
	}
}

func TestStatusForEvent(t *testing.T) {
	tests := []struct {
		event  string
		want   session.Status
		wantOK bool
	}{
		{"crunching", session.StatusCrunching, true},
		{"waiting", session.StatusWaiting, true},
		{"idle", session.StatusIdle, true},
		{"done", session.StatusDone, true},
		{"exit", session.StatusExited, true},
		// "start" is intentionally unmapped: a freshly launched session stays
		// neutral until the first prompt makes it crunch.
		{"start", 0, false},
		{"bogus", 0, false},
		{"", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.event, func(t *testing.T) {
			got, ok := statusForEvent(tt.event)
			if ok != tt.wantOK {
				t.Fatalf("statusForEvent(%q) ok = %v, want %v", tt.event, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("statusForEvent(%q) = %v, want %v", tt.event, got, tt.want)
			}
		})
	}
}

package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quonfig/cmdr-keen/internal/reg"
	"github.com/quonfig/cmdr-keen/internal/tmuxctl"
)

// testModel builds a Model over an empty manager. The manager points at a
// tmux server that doesn't exist — fine for these tests, which exercise pure
// key-handling state with zero sessions (no tmux command ever fires).
func testModel() *Model {
	t := &tmuxctl.Server{Socket: "keen-test-none"}
	mgr := reg.NewManager(t, "%0", "@0", "/tmp/keen-test-none.sock", "true", SidebarPaneWidth)
	return NewModel(mgr, ".", []string{"true"})
}

// runeKey builds a single-rune key message for driving the key handlers.
func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestConfirmCloseArmingGuard(t *testing.T) {
	// With no sessions, 'x' must not arm a close — there's nothing to close and
	// a dangling prompt would be confusing.
	m := testModel()
	m.handleKey(runeKey('x'))
	if m.confirmClose {
		t.Fatal("x armed confirmation with no sessions; want no-op")
	}
}

func TestConfirmCloseCancelsOnOtherKey(t *testing.T) {
	// Any key other than x/y cancels an armed close and leaves the flag clear.
	for _, k := range []tea.KeyMsg{runeKey('j'), {Type: tea.KeyEnter}} {
		m := testModel()
		m.confirmClose = true
		m.handleKey(k)
		if m.confirmClose {
			t.Errorf("key %v left confirmation armed; want cancelled", k)
		}
	}
}

func TestConfirmCloseConsumesTheKey(t *testing.T) {
	// 'n' resolving an armed close must be consumed, not also spawn a session.
	m := testModel()
	m.confirmClose = true
	m.handleKey(runeKey('n'))
	if m.confirmClose {
		t.Fatal("n left confirmation armed; want cancelled")
	}
	if m.mgr.Count() != 0 {
		t.Fatal("n spawned a session while resolving a close; want consumed")
	}
}

func TestConfirmCloseConfirmKeys(t *testing.T) {
	// x and y both resolve the prompt; the flag clears either way.
	for _, r := range []rune{'x', 'y'} {
		m := testModel()
		m.confirmClose = true
		m.handleKey(runeKey(r))
		if m.confirmClose {
			t.Errorf("%c left confirmation armed; want resolved", r)
		}
	}
}

func TestStatusForEvent(t *testing.T) {
	cases := []struct {
		event string
		want  reg.Status
		ok    bool
	}{
		{"crunching", reg.StatusCrunching, true},
		{"waiting", reg.StatusWaiting, true},
		{"idle", reg.StatusIdle, true},
		{"done", reg.StatusDone, true},
		{"exit", reg.StatusExited, true},
		{"start", 0, false}, // intentionally unmapped — stays neutral
		{"bogus", 0, false},
	}
	for _, c := range cases {
		got, ok := statusForEvent(c.event)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("statusForEvent(%q) = (%v,%v), want (%v,%v)", c.event, got, ok, c.want, c.ok)
		}
	}
}

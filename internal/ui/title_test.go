package ui

import (
	"testing"

	"github.com/quonfig/cmdr-keen/internal/reg"
)

func sessionWith(st reg.Status) *reg.Session { return &reg.Session{Status: st} }

func TestFleetTitle(t *testing.T) {
	cases := []struct {
		name string
		ss   []*reg.Session
		want string
	}{
		{"no sessions", nil, "keen"},
		{"only starting", []*reg.Session{sessionWith(reg.StatusStarting)}, "keen"},
		{"exited ignored", []*reg.Session{sessionWith(reg.StatusExited)}, "keen"},
		{"zero counts drop out", []*reg.Session{
			sessionWith(reg.StatusDone), sessionWith(reg.StatusDone),
		}, "keen · 2✓"},
		// Waiting and idle pool into one "needs you" count, listed first.
		{"full mix", []*reg.Session{
			sessionWith(reg.StatusWaiting), sessionWith(reg.StatusIdle),
			sessionWith(reg.StatusCrunching), sessionWith(reg.StatusDone),
		}, "keen · 2◐ 1● 1✓"},
	}
	for _, c := range cases {
		if got := fleetTitle(c.ss); got != c.want {
			t.Errorf("%s: fleetTitle = %q, want %q", c.name, got, c.want)
		}
	}
}

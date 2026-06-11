package main

import (
	"strings"
	"testing"
)

func TestDefaultSocket(t *testing.T) {
	t.Run("stable for same directory", func(t *testing.T) {
		a := defaultSocket("/Users/jeff/code/quonfig/cmdr-keen")
		b := defaultSocket("/Users/jeff/code/quonfig/cmdr-keen")
		if a != b {
			t.Fatalf("same dir produced different sockets: %q vs %q", a, b)
		}
	})

	t.Run("distinct for different directories", func(t *testing.T) {
		a := defaultSocket("/Users/jeff/code/quonfig/cmdr-keen")
		b := defaultSocket("/Users/jeff/code/quonfig")
		if a == b {
			t.Fatalf("different dirs produced the same socket: %q", a)
		}
	})

	t.Run("distinct for same basename in different parents", func(t *testing.T) {
		a := defaultSocket("/Users/jeff/code/app")
		b := defaultSocket("/Users/jeff/other/app")
		if a == b {
			t.Fatalf("same basename in different parents collided: %q", a)
		}
	})

	t.Run("readable prefix includes directory basename", func(t *testing.T) {
		s := defaultSocket("/Users/jeff/code/quonfig/cmdr-keen")
		if !strings.HasPrefix(s, "keen-cmdr-keen-") {
			t.Fatalf("socket %q does not start with keen-cmdr-keen-", s)
		}
	})

	t.Run("sanitizes characters tmux/socket paths dislike", func(t *testing.T) {
		s := defaultSocket("/Users/jeff/My Projects/weird dir.name!")
		for _, r := range s {
			ok := r == '-' || r == '_' ||
				(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
			if !ok {
				t.Fatalf("socket %q contains unsafe char %q", s, r)
			}
		}
	})

	t.Run("caps length for very long basenames", func(t *testing.T) {
		s := defaultSocket("/tmp/" + strings.Repeat("a", 200))
		// /tmp/keen-<socket>.sock must stay under the ~104-char unix
		// socket path limit on macOS, with headroom.
		if len(s) > 40 {
			t.Fatalf("socket %q is %d chars, want <= 40", s, len(s))
		}
	})
}

package tmuxctl

import "testing"

func TestShellJoin(t *testing.T) {
	cases := []struct {
		argv []string
		want string
	}{
		{[]string{"claude"}, "'claude'"},
		{[]string{"claude", "--settings", "/tmp/a b.json"}, "'claude' '--settings' '/tmp/a b.json'"},
		// Single quotes survive via the POSIX close-escape-reopen dance.
		{[]string{"echo", "it's"}, `'echo' 'it'\''s'`},
	}
	for _, c := range cases {
		if got := ShellJoin(c.argv); got != c.want {
			t.Errorf("ShellJoin(%v) = %s, want %s", c.argv, got, c.want)
		}
	}
}

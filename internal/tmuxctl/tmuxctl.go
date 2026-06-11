// Package tmuxctl drives keen's private tmux server. tmux is an implementation
// detail of keen: each server runs on its own socket (-L keen-<dir>-<hash>,
// derived from the startup directory) with its own generated config, so it
// never touches — and is never touched by — any tmux the user runs themselves.
// Every tmux invocation funnels through Server so that isolation lives in
// exactly one place.
package tmuxctl

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Server identifies keen's private tmux server.
type Server struct {
	Socket string // tmux -L socket name
	Config string // path to keen's generated config ("" = tmux defaults; tests)
}

// args returns the common argument prefix for invoking this server.
func (s *Server) args(rest ...string) []string {
	a := []string{"-L", s.Socket}
	if s.Config != "" {
		a = append(a, "-f", s.Config)
	}
	return append(a, rest...)
}

// Run executes one tmux command against the server and returns trimmed stdout.
func (s *Server) Run(rest ...string) (string, error) {
	out, err := exec.Command("tmux", s.args(rest...)...).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("tmux %s: %s", strings.Join(rest, " "), strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("tmux %s: %w", strings.Join(rest, " "), err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// AttachArgv is the argv for exec'ing into an attach (the boot path replaces
// the keen process with the tmux client, so keen adds zero overhead once up).
func (s *Server) AttachArgv(session string) []string {
	return append([]string{"tmux"}, s.args("attach-session", "-t", "="+session)...)
}

func (s *Server) HasSession(name string) bool {
	_, err := s.Run("has-session", "-t", "="+name)
	return err == nil
}

// NewSession creates a detached session whose first window runs cmd.
func (s *Server) NewSession(name, windowName, cwd string, w, h int, env map[string]string, cmd []string) error {
	a := []string{"new-session", "-d", "-s", name, "-n", windowName,
		"-x", fmt.Sprint(w), "-y", fmt.Sprint(h)}
	if cwd != "" {
		a = append(a, "-c", cwd)
	}
	a = append(a, envFlags(env)...)
	a = append(a, ShellJoin(cmd))
	_, err := s.Run(a...)
	return err
}

// NewWindow spawns cmd in a new background window of session and returns the
// new pane's id. The window exists only to park the pane; bringing the session
// into view moves the pane, not the window.
func (s *Server) NewWindow(session, name, cwd string, env map[string]string, cmd []string) (string, error) {
	a := []string{"new-window", "-d", "-P", "-F", "#{pane_id}", "-t", session + ":", "-n", name}
	if cwd != "" {
		a = append(a, "-c", cwd)
	}
	a = append(a, envFlags(env)...)
	a = append(a, ShellJoin(cmd))
	return s.Run(a...)
}

// SplitWindow splits target horizontally and runs cmd in the new pane,
// returning its id. before=true puts the new pane on the left.
func (s *Server) SplitWindow(target string, before bool, width int, cwd string, env map[string]string, cmd []string) (string, error) {
	a := []string{"split-window", "-h", "-d", "-P", "-F", "#{pane_id}", "-t", target}
	if before {
		a = append(a, "-b")
	}
	if width > 0 {
		a = append(a, "-l", fmt.Sprint(width))
	}
	if cwd != "" {
		a = append(a, "-c", cwd)
	}
	a = append(a, envFlags(env)...)
	a = append(a, ShellJoin(cmd))
	return s.Run(a...)
}

// JoinPane moves pane src out of its window and splices it in to the right of
// dst (horizontal split, after). src's old window dies if src was its last
// pane. Focus is left where it was.
func (s *Server) JoinPane(src, dst string) error {
	_, err := s.Run("join-pane", "-h", "-d", "-s", src, "-t", dst)
	return err
}

// SwapPanes exchanges the positions of two panes (the processes travel with
// their panes; pane ids are stable). Focus is left where it was.
func (s *Server) SwapPanes(a, b string) error {
	_, err := s.Run("swap-pane", "-d", "-s", a, "-t", b)
	return err
}

func (s *Server) KillPane(id string) error {
	_, err := s.Run("kill-pane", "-t", id)
	return err
}

func (s *Server) SelectPane(id string) error {
	_, err := s.Run("select-pane", "-t", id)
	return err
}

// ResizePaneWidth sets a pane's width in columns.
func (s *Server) ResizePaneWidth(id string, w int) error {
	_, err := s.Run("resize-pane", "-t", id, "-x", fmt.Sprint(w))
	return err
}

// SetPaneOption sets a per-pane user option (@name). Pane options are keen's
// persistence layer: they live exactly as long as the pane does and survive a
// sidebar restart or client detach.
func (s *Server) SetPaneOption(pane, name, value string) error {
	_, err := s.Run("set-option", "-p", "-t", pane, name, value)
	return err
}

// Display expands a format string in the context of a pane.
func (s *Server) Display(pane, format string) (string, error) {
	return s.Run("display-message", "-p", "-t", pane, format)
}

// ListPanes expands format once per pane across the whole session, returning
// one line per pane.
func (s *Server) ListPanes(session, format string) ([]string, error) {
	out, err := s.Run("list-panes", "-s", "-t", session+":", "-F", format)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// ListWindowPanes is ListPanes scoped to a single window.
func (s *Server) ListWindowPanes(window, format string) ([]string, error) {
	out, err := s.Run("list-panes", "-t", window, "-F", format)
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// DetachAll detaches every client attached to session. The server — and every
// Claude in it — keeps running; `keen` reattaches.
func (s *Server) DetachAll(session string) error {
	_, err := s.Run("detach-client", "-s", session)
	return err
}

func (s *Server) KillServer() error {
	_, err := s.Run("kill-server")
	return err
}

// envFlags renders env as repeated `-e K=V` flags (new-session/new-window/
// split-window all accept them), sorted for deterministic argv.
func envFlags(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	a := make([]string, 0, 2*len(keys))
	for _, k := range keys {
		a = append(a, "-e", k+"="+env[k])
	}
	return a
}

// ShellJoin renders argv as a single shell command string. tmux's
// shell-command arguments are parsed by /bin/sh, so each arg is single-quoted
// with embedded quotes escaped the POSIX way.
func ShellJoin(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
	}
	return strings.Join(parts, " ")
}

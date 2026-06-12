package ui

import (
	"encoding/json"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/quonfig/cmdr-keen/internal/config"
	"github.com/quonfig/cmdr-keen/internal/hooks"
	"github.com/quonfig/cmdr-keen/internal/reg"
	"github.com/quonfig/cmdr-keen/internal/tmuxctl"
)

// SidebarPaneWidth is the fixed width (columns) of the sidebar's tmux pane:
// 30 columns of content inside the lipgloss border.
const SidebarPaneWidth = 32

// RunSidebar is the `keen __sidebar` entry point: the Bubble Tea program that
// lives in the left tmux pane for the life of the server. It owns the session
// registry, the hooks socket, and the titler — and nothing else; tmux owns the
// terminals.
//
// Configuration arrives via environment (set on the pane by the boot path):
// KEEN_TMUX_SOCKET, KEEN_TMUX_CONF, KEEN_CWD, KEEN_CMD (JSON argv for what to
// run per session; default claude).
func RunSidebar() error {
	pane := os.Getenv("TMUX_PANE")
	if pane == "" {
		return fmt.Errorf("__sidebar must run inside keen's tmux session (TMUX_PANE unset)")
	}
	t := &tmuxctl.Server{Socket: socketName(), Config: os.Getenv("KEEN_TMUX_CONF")}

	viewWindow, err := t.Display(pane, "#{window_id}")
	if err != nil {
		return fmt.Errorf("resolve view window: %w", err)
	}
	// Tag our pane so the boot path can tell a live sidebar from a dead one
	// (and so Scan skips it).
	_ = t.SetPaneOption(pane, "@keen-sidebar", "1")

	cwd := os.Getenv("KEEN_CWD")
	if cwd == "" {
		cwd, _ = os.Getwd()
	}
	cfg, err := config.Load(cwd)
	if err != nil {
		// Boot already validated the files loudly; a sidebar-side failure
		// (file edited mid-flight) degrades to defaults rather than dying.
		cfg = config.Default()
	}
	args := sessionArgs(cfg)

	hookSocket := reg.HookSocketPath(t.Socket)
	mgr := reg.NewManager(t, pane, viewWindow, hookSocket, hooks.ResolveHookBin(), SidebarPaneWidth)
	if err := mgr.Scan(); err != nil {
		return fmt.Errorf("scan panes: %w", err)
	}

	model := NewModel(mgr, cwd, args, cfg)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion(), tea.WithReportFocus())

	srv, err := hooks.NewServer(hookSocket, p.Send)
	if err != nil {
		return fmt.Errorf("status socket: %w", err)
	}
	defer srv.Close()
	go srv.Serve()

	_, err = p.Run()
	return err
}

// socketName is the tmux -L socket keen's private server runs on.
func socketName() string {
	if s := os.Getenv("KEEN_TMUX_SOCKET"); s != "" {
		return s
	}
	return "keen"
}

// sessionArgs is the argv each new session runs: KEEN_CMD (a JSON array, set
// by `keen -- <cmd>` at boot) wins, else the configured session_command
// (default: the claude invocation).
func sessionArgs(cfg config.Config) []string {
	if raw := os.Getenv("KEEN_CMD"); raw != "" {
		var args []string
		if json.Unmarshal([]byte(raw), &args) == nil && len(args) > 0 {
			return args
		}
	}
	return cfg.SessionCommand
}

// Command keen is a thin multiplexer for Claude Code: a fixed-order sidebar of
// sessions with live status indicators, with each session running raw in a
// tmux pane beside it. tmux is keen's engine, not its interface — keen boots a
// private tmux server (own socket, own config) and execs into the attach;
// the user never drives tmux directly.
//
// What tmux buys over keen's old built-in terminal emulator:
//
//   - Persistence: keen (or the whole terminal) can die; sessions live on.
//     Running `keen` again reattaches to exactly where you left off.
//
//   - Native copy: plain click-drag selects within a pane — bounded to that
//     pane, no Option key — and copies to the system clipboard on release.
//
//   - Fidelity: bytes flow terminal ↔ tmux ↔ claude with no keen-authored
//     emulation or key reconstruction in the path.
//
// Instances are per-directory: the server socket is derived from the cwd keen
// was started in, so `keen` in project A and `keen` in project B are fully
// independent — each silently boots or reattaches its own instance.
//
//	keen              # boot (or reattach to) this directory's keen, one claude per session
//	keen -- bash      # wrap an arbitrary command instead (handy for testing)
//	keen kill         # tear down this directory's server and every session in it
package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"

	"github.com/quonfig/cmdr-keen/internal/config"
	"github.com/quonfig/cmdr-keen/internal/hook"
	"github.com/quonfig/cmdr-keen/internal/reg"
	"github.com/quonfig/cmdr-keen/internal/tmuxctl"
	"github.com/quonfig/cmdr-keen/internal/ui"
)

// version is the keen build version. It defaults to "dev" and can be set at
// build time via -ldflags "-X main.version=v1.2.3". When unset, we fall back to
// the module version embedded by `go install module@version`.
var version = "dev"

// bootCols/bootRows size the detached session until a client attaches and
// window-size latest takes over.
const (
	bootCols = 200
	bootRows = 50
)

func main() {
	// keen is a multi-call binary: when Claude runs it as a lifecycle hook
	// (`keen __hook <event>`), act as the hook helper and exit. When tmux runs
	// it as the sidebar pane (`keen __sidebar`), run the sidebar program. This
	// is what lets a single `go install` of keen be entirely self-contained.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "__hook":
			hook.Run(os.Args[2:])
			return
		case "__sidebar":
			if err := ui.RunSidebar(); err != nil {
				fmt.Fprintln(os.Stderr, "keen sidebar:", err)
				os.Exit(1)
			}
			return
		case "--version", "-v":
			fmt.Println("keen", resolveVersion())
			return
		case "kill":
			srv := server("")
			if err := srv.KillServer(); err != nil {
				fmt.Fprintln(os.Stderr, "keen kill:", err)
				os.Exit(1)
			}
			fmt.Println("keen: server and all sessions killed")
			return
		}
	}

	if err := boot(); err != nil {
		fmt.Fprintln(os.Stderr, "keen:", err)
		os.Exit(1)
	}
}

// boot ensures the private tmux server and keen session exist, then replaces
// this process with the tmux client. Idempotent: a running session is simply
// reattached (that's the crash/quit recovery path), with a dead sidebar
// respawned if needed.
func boot() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("tmux not found on PATH — keen runs sessions inside tmux (brew install tmux)")
	}
	if os.Getenv("TMUX") != "" {
		return fmt.Errorf("already inside a tmux session — run keen from a plain terminal")
	}

	// Validate the config files here, where stderr is still the user's
	// terminal — the sidebar quietly falls back to defaults if they go bad
	// later, but a typo at boot should be loud.
	if cwd, err := os.Getwd(); err == nil {
		if _, err := config.Load(cwd); err != nil {
			return fmt.Errorf("config: %w", err)
		}
	}

	conf, err := writeConfig()
	if err != nil {
		return fmt.Errorf("write tmux config: %w", err)
	}
	srv := server(conf)

	if !srv.HasSession(reg.SessionName) {
		if err := freshSession(srv, conf); err != nil {
			return err
		}
	} else if err := repairSidebar(srv, conf); err != nil {
		return err
	}

	if os.Getenv("KEEN_NO_ATTACH") != "" { // headless boot, for tests
		fmt.Println("keen: booted (not attaching)")
		return nil
	}

	tmuxPath, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	argv := srv.AttachArgv(reg.SessionName)
	return syscall.Exec(tmuxPath, argv, os.Environ())
}

// freshSession creates the keen session: one window whose only pane runs the
// sidebar. The sidebar spawns the first claude beside itself once it measures
// its pane.
func freshSession(srv *tmuxctl.Server, conf string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	return srv.NewSession(reg.SessionName, "view", cwd, bootCols, bootRows,
		sidebarEnv(srv, conf, cwd), []string{exe, "__sidebar"})
}

// repairSidebar respawns the sidebar pane if the session is running but the
// sidebar program died (its pane dies with it). Sessions in background windows
// are re-adopted by the new sidebar's Scan.
func repairSidebar(srv *tmuxctl.Server, conf string) error {
	panes, err := srv.ListPanes(reg.SessionName, "#{@keen-sidebar}")
	if err != nil {
		return err
	}
	for _, p := range panes {
		if p == "1" {
			return nil // sidebar alive
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "."
	}
	_, err = srv.SplitWindow(reg.SessionName+":", true, ui.SidebarPaneWidth, cwd,
		sidebarEnv(srv, conf, cwd), []string{exe, "__sidebar"})
	return err
}

// sidebarEnv is the environment the sidebar pane needs: which server it
// belongs to and what each session should run.
func sidebarEnv(srv *tmuxctl.Server, conf, cwd string) map[string]string {
	env := map[string]string{
		"KEEN_TMUX_SOCKET": srv.Socket,
		"KEEN_TMUX_CONF":   conf,
		"KEEN_CWD":         cwd,
	}
	// Per-session command: everything after `--`, else the sidebar's default.
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			if b, err := json.Marshal(os.Args[i+1:]); err == nil {
				env["KEEN_CMD"] = string(b)
			}
			break
		}
	}
	if d := os.Getenv("KEEN_DEBUG"); d != "" {
		env["KEEN_DEBUG"] = d
	}
	return env
}

func server(conf string) *tmuxctl.Server {
	sock := os.Getenv("KEEN_TMUX_SOCKET")
	if sock == "" {
		sock = socketForCwd()
	}
	return &tmuxctl.Server{Socket: sock, Config: conf}
}

// socketForCwd names the tmux server for the directory keen was started in.
// One server per directory is what makes `keen` in project A and `keen` in
// project B independent instances, each reattaching to its own sessions.
func socketForCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "keen"
	}
	if resolved, err := filepath.EvalSymlinks(cwd); err == nil {
		cwd = resolved
	}
	if abs, err := filepath.Abs(cwd); err == nil {
		cwd = abs
	}
	return defaultSocket(cwd)
}

// defaultSocket is the per-directory socket name: "keen-<base>-<hash>", where
// base is the sanitized directory basename (readable in `tmux -L` and /tmp
// paths) and hash is a short digest of the full path (so equal basenames under
// different parents don't collide). Kept short: the hook socket lives at
// /tmp/keen-<socket>.sock and unix socket paths cap out near 104 chars on
// macOS.
func defaultSocket(dir string) string {
	base := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, filepath.Base(dir))
	if len(base) > 20 {
		base = base[:20]
	}
	sum := sha256.Sum256([]byte(dir))
	return fmt.Sprintf("keen-%s-%x", base, sum[:4])
}

// tmuxConfig is keen's entire tmux configuration. Regenerated at every boot so
// upgrades apply; never reads the user's ~/.tmux.conf (the -f flag replaces
// it). Design goals: tmux invisible (no prefix, no status bar), mouse-native
// copy (drag selects within a pane, release copies to the system clipboard),
// and full fidelity for whatever runs in the panes.
const tmuxConfig = `# Generated by keen — do not edit (rewritten at every keen boot).
set -g prefix None
set -g status off
set -g mouse on
set -g set-clipboard on
set -g default-terminal "tmux-256color"
set -as terminal-features ",*:RGB"
set -g allow-passthrough on
set -s escape-time 0
set -g focus-events on
set -g history-limit 50000
set -g window-size latest
set -g renumber-windows off
# Fleet summary in the outer terminal's tab title (e.g. "keen · 2◐ 3● 4✓"):
# the sidebar rewrites set-titles-string as statuses change. Visual only —
# the no-bell / no-notification decision stands.
set -g set-titles on
set -g set-titles-string "keen"
# keen draws all chrome itself — the sidebar hand-draws its frame and the
# active-session tab cutout (see ui.frame) — so tmux renders nothing visible.
# The divider column between panes is mandatory; painting it near-background
# turns it into the invisible gutter the tab cutout opens across.
set -g pane-border-style "fg=colour234"
set -g pane-active-border-style "fg=colour234"
# The keen prefix: C-k hops between the sidebar and the session pane. Bound at
# the tmux layer so it never reaches Claude — same guarantee as old keen.
bind -n C-k select-pane -t :.+
`

// writeConfig writes keen's tmux config under the state dir and returns its
// path.
func writeConfig() (string, error) {
	dir := stateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "tmux.conf")
	return path, os.WriteFile(path, []byte(tmuxConfig), 0o644)
}

func stateDir() string {
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "keen")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "keen-state")
	}
	return filepath.Join(home, ".local", "state", "keen")
}

// resolveVersion returns the build version, preferring an ldflags-injected
// value and otherwise the module version recorded by `go install`. It falls
// back to "dev" when no version information is available (e.g. `go run`).
func resolveVersion() string {
	if version != "dev" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return version
}

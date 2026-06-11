# Changelog

All notable changes to keen. Format loosely follows
[Keep a Changelog](https://keepachangelog.com); versions follow semver
(pre-1.0: minor bumps may break things).

## [0.3.0] — 2026-06-11

### Changed
- **One keen per directory.** The private tmux server's socket is now derived
  from the directory keen is started in (`keen-<basename>-<hash>`), so `keen`
  in project A and `keen` in project B are fully independent instances — each
  silently boots or reattaches its own, no questions asked. Previously a
  single global server meant running keen in a second project reattached the
  first project's sessions. `keen kill` now tears down only the current
  directory's instance. `KEEN_TMUX_SOCKET` still overrides the derived name.
  Upgrade note: an instance started by an older build lives on the old global
  `keen` socket; clear it once with `tmux -L keen kill-server` when you're
  done with those sessions.

## [0.2.0] — 2026-06-10

The engine swap: keen's embedded VT emulator is gone, replaced by a private
tmux server (own socket, own generated config — it never touches your tmux,
and you never drive tmux directly).

### Changed
- **tmux is the engine.** Sessions live in panes of a private tmux server;
  keystrokes, rendering, mouse, and copy-mode are tmux's, with no keen-authored
  emulation in the byte path. keen's sidebar runs in the leftmost pane and only
  lists, labels, and switches sessions.
- **Sessions persist.** Quit keen, crash it, or lose the terminal — sessions
  keep running. `keen` reattaches; `q` detaches on purpose; `keen kill` tears
  everything down.
- **The sidebar draws its own chrome.** tmux renders nothing visible (no pane
  borders; the mandatory divider column is painted to the background). Where
  your keys go is unmistakable: a solid thick blue frame around the sidebar
  when it has them, and a notebook-tab cutout — the right rail breaks open and
  a blue outline wraps the active session's entry, aimed at its pane — when
  the session has them. The footer states it outright (`keys → claude · ^k =
  here`).
- Sessions render on a 4-row stride (a breathing row between entries) so the
  tab outline has edges; click targets are identical in both focus states.

### Added
- **Native copy**: click-drag selects within a pane (bounded — never bleeds
  across panes) and lands on the system clipboard on release via OSC 52. The
  sidebar footer documents it: `drag selects · release copies`.
- **Image paste works**: Ctrl-V in a session pastes a clipboard image into
  Claude Code (it reads the clipboard directly; the terminal and tmux are not
  in the image's path).
- `Tab` joins `Enter` for hopping sidebar → session.
- **Live work (beads)**: when the launch directory is a bd workspace, the
  sidebar lists the top live issues below the sessions, two lines each in bd's
  row style (status circle, id, priority badge, title). Refreshes once a
  minute; hides when bd or the database is absent.
- **Fleet rollup in the tab title**: the outer terminal's title shows
  `keen · 2◐ 3● 4✓` (needs-you / working / done), so a backgrounded keen tab
  still shows when sessions want you.

### Removed
- The embedded VT emulator (`embed-vt`) and all key-reconstruction code.

## [0.1.3] — 2026-06

### Changed
- Module rehomed to `github.com/quonfig/cmdr-keen`.
- The waiting status split in two: `◐` red = waiting on permission, `◐`
  magenta = idle ping (Claude finished and is waiting on you); a stray
  permission-while-Done now reads as the idle ping.

## [0.1.2] — 2026-06

### Changed
- Sessions re-label on every prompt (Haiku summarizes the transcript tail);
  topics stay stable while the task line tracks the work.

### Added
- `keen --version`.

## [0.1.1] — 2026-06

### Changed
- keen is a self-contained multi-call binary: the Claude Code lifecycle hooks
  re-invoke `keen __hook <event>`, so `go install` is the whole setup (the
  standalone `cc-deck-hook` build remains optional).

### Added
- Closing a session asks for confirmation (`x`, then `x`/`y`).
- Git policy for agents: commit on completion, push only with approval.

## [0.1.0] — 2026-05

Initial release: a fixed-order sidebar of Claude Code sessions with live
status colors (crunching / waiting / done) driven by lifecycle hooks, an
embedded VT emulator rendering the active session beside it, and Haiku-powered
topic/task labels from the transcript.

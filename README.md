# keen

A thin multiplexer for Claude Code. Run many `claude` sessions behind one
screen: a fixed-order sidebar showing each session's name and a live status
color (crunching / waiting on you / all done), with the active session running
raw in a pane beside it. Keystrokes pass straight through to Claude — keen only
adds the list, the names, and the statuses.

Under the hood each session lives in a **private tmux server** (own socket, own
config — it never touches your tmux, and you never drive tmux directly). That
buys three things the old built-in terminal emulator couldn't:

- **Persistence** — quit keen, crash it, or lose the whole terminal: sessions
  keep running. `keen` reattaches to exactly where you left off.
- **Native copy** — plain click-drag selects text within a pane (no Option key,
  never bleeding into the sidebar) and copies to the system clipboard on release.
- **Fidelity** — bytes flow terminal ↔ tmux ↔ claude with no keen-authored
  emulation or key reconstruction in the path.

![keen running several Claude Code sessions behind one screen](docs/images/cmdr-keen.png)

See [`docs/spec.md`](docs/spec.md) for the full design and milestones, and
[`CHANGELOG.md`](CHANGELOG.md) for what each release shipped.

## Install

With a Go toolchain (1.26+), install the `keen` binary straight from the repo:

```sh
go install github.com/quonfig/cmdr-keen/cmd/keen@latest
```

This drops `keen` into your `$GOBIN` (defaults to `~/go/bin`). Make sure that
directory is on your `PATH`:

```sh
export PATH="$PATH:$(go env GOPATH)/bin"
```

To **upgrade** later, re-run the same command, or pin a specific release:

```sh
go install github.com/quonfig/cmdr-keen/cmd/keen@latest   # newest tag
go install github.com/quonfig/cmdr-keen/cmd/keen@v0.2.0   # a fixed version
```

`go install` is per-user — every user runs it for themselves. To install once
for **all** users on a machine, install it as yourself and copy the binary into
a shared directory on the system `PATH`:

```sh
go install github.com/quonfig/cmdr-keen/cmd/keen@latest
sudo cp "$(go env GOPATH)/bin/keen" /usr/local/bin/keen
```

### Build from source

```sh
go build -o bin/keen ./cmd/keen
```

That's the whole install — `keen` is a single self-contained binary. It serves
the Claude Code lifecycle hooks by re-invoking *itself* (`keen __hook <event>`),
so there's no second helper to build or keep on your `PATH`. (A standalone
`cc-deck-hook` build still exists under `cmd/cc-deck-hook` for anyone wiring
hooks by hand, but it's optional.)

keen needs `tmux` on your `PATH` (`brew install tmux`).

## Run

```sh
keen                # boot (or reattach): `claude --permission-mode auto` per session
keen -- bash        # wrap an arbitrary command instead (handy for testing)
keen kill           # tear down this directory's server and every session in it
```

(If you built from source instead of installing, run `./bin/keen`.)

Instances are **per-directory**: each directory you start `keen` in gets its
own private tmux server, so `keen` in project A and `keen` in project B are
fully independent. Running `keen` where an instance is already up simply
**reattaches** — your sessions, names, and statuses come back exactly as you
left them. (`keen kill` only tears down the instance for the directory you run
it in.)

Each session is spawned with hooks injected via `claude --settings <tempfile>`,
so your global `~/.claude` is never modified.

## Controls

| Action | Key |
|---|---|
| Toggle focus: sidebar ⇄ session | **Ctrl-K** (or **Cmd-K**, see setup below) |
| Move selection (sidebar focused) | `j`/`k` or ↑/↓ |
| Jump into the session | `Enter` or `Tab` |
| New session | `n` |
| Close session | `x`, then `x`/`y` to confirm |
| Jump to session N | `1`–`9` |
| Detach — sessions keep running | `q` |
| Switch to a session | click its row |

When the session is focused, everything (typing, paste, mouse scroll/click) goes
straight to Claude. Only the prefix key is intercepted.

### Where your keys go

The sidebar's frame always tells you. A **solid thick blue box** around the
sidebar means keys drive the session list. When the session has them instead,
the frame opens like a **notebook tab**: the sidebar's right rail breaks and a
blue outline wraps the active session's entry, aimed at its pane — and the
footer says it outright (`keys → claude · ^k = here`).

### Copying and pasting

Click and drag to select text — the selection is bounded to the pane you're in,
and it lands on the system clipboard **when you release** (tmux `set-clipboard`
via OSC 52). The highlight vanishing on release is the copy happening — there's
no Cmd-C step. No modifier keys needed, and it works the same inside
Cursor/VS Code terminals. The mouse wheel scrolls a pane's history.

**Pasting text** is plain Cmd-V. **Pasting an image** is **Ctrl-V** — terminals
only transmit text, so Claude Code binds Ctrl-V and reads the image straight
off the system clipboard; tmux is not in the way. You can also **drag files
into** a session to insert their paths.

## Status colors

| Glyph | Meaning | Hook event |
|---|---|---|
| `·` grey | starting | `SessionStart` |
| `●` yellow | crunching | `UserPromptSubmit`, `PreToolUse` |
| `◐` red | waiting on you (permission) | `Notification` (permission prompt) |
| `◐` magenta | waiting on you (idle) | `Notification` (idle ping) |
| `✓` green | all done (your move) | `Stop` |
| `✕` faint | exited | `SessionEnd` |

The same counts roll up into the outer terminal's tab title — `keen · 2◐ 3● 4✓`
(needs-you / working / done) — so even a backgrounded keen tab shows at a
glance when sessions are waiting on you. Visual only, like the sidebar: no
bell, no notifications.

## Live work (beads)

If the directory you launch keen from is tracked with the
[bd (beads)](https://github.com/steveyegge/beads) issue tracker, the sidebar
lists the top 10 live issues (`bd list` — open and in-progress) below the
sessions, two lines each in bd's own row style: status circle, id, priority
badge, then the title. It refreshes once a minute and is purely
informational; the section hides when bd isn't installed, the database isn't
initialized, or there are no live issues.

## Setup: make Cmd-K work too (macOS / Cursor / VS Code)

keen's prefix is **Ctrl-K**. To also trigger it with **Cmd-K**, the terminal
must be told to send the Ctrl-K byte on Cmd-K — macOS terminals swallow the Cmd
modifier, so a terminal program can't see Cmd-K on its own.

Add this to your Cursor/VS Code `keybindings.json`
(`Cmd-Shift-P` → *Preferences: Open Keyboard Shortcuts (JSON)*):

```jsonc
{
  "key": "cmd+k",
  "command": "workbench.action.terminal.sendSequence",
  "args": { "text": "\u000b" },   // \u000b = Ctrl-K
  "when": "terminalFocus"
}
```

Trade-offs while the terminal is focused: this overrides Cmd-K's default
"clear terminal" and any Cmd-K chord shortcuts, and in a non-keen shell Cmd-K
will send Ctrl-K (kill-to-end-of-line). It only applies when the terminal has
focus.

> On macOS this file lives at
> `~/Library/Application Support/Cursor/User/keybindings.json` (Cursor) or
> `.../Code/User/keybindings.json` (VS Code).

## Layout

```
cmd/keen/            entry point: boots/attaches the tmux server; also serves
                     `keen __sidebar` (the sidebar pane) and `keen __hook`
internal/tmuxctl/    the one place that talks to keen's private tmux server
internal/reg/        session registry: metadata persisted as tmux pane options
internal/ui/         Bubble Tea sidebar: model, render, hit-testing
internal/hook/       Claude Code hook helper: reports status to keen's socket
cmd/cc-deck-hook/    optional standalone build of internal/hook
internal/hooks/      unix-socket status server + per-session settings generation
internal/titler/     turns a session's transcript into Haiku topic/task labels
spike/               M0 de-risk spikes (passthrough vs embedded-vt)
```

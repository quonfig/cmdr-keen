# cmdr-keen — a thin, self-naming multiplexer for Claude Code

> Working title: **keen**. A single-screen Bubble Tea app that hosts ~10 Claude Code
> sessions side-by-side, auto-names each tab, and shows at a glance which ones are
> **crunching**, **waiting on you**, or **all done** — so you stop clicking blindly
> between identical terminal tabs.

---

## 1. Problem

You run ~10 `claude` sessions plus a few utility terminals out of Cursor's terminal
panel. The tabs are all named the same generic thing, so you burn time clicking
through them to answer "is this one done? is it blocked on me? is it still working?"
Renaming by hand is a chore you don't keep up with.

**Goal:** one screen. A live list of every Claude session with a meaningful name and a
one-word status, plus the actual Claude Code UI right next to it. Thin wrapper —
keystrokes go straight to Claude; the wrapper only adds the list, the names, and the
statuses.

## 2. Decisions (locked)

| Decision | Choice | Consequence |
|---|---|---|
| **Layout** | Always-on sidebar + live embedded session pane (preferred), **overlay-switcher as equal fallback** | Decided by the M0 spike — whichever preserves **full Claude fidelity** wins (see §2.1, §9). |
| **Persistence** | ~~Wrapper owns the PTYs (no tmux)~~ **Superseded by M5:** sessions live in a private tmux server | Sessions survive keen quitting/crashing; `keen` reattaches. Costs a tmux dependency (the user never drives tmux directly). |
| **Naming** | Hooks for status, cheap LLM (Haiku) for the title | Deterministic status; nice human titles. Falls back to heuristic title with no API key. |
| **Prefix key** | **`Ctrl-k`** | Wrapper steals `Ctrl-k` before forwarding; everything else flows to Claude. |
| **Alerts** | **No bell / no OS notification** — colored indicator on the tab only | Status conveyed purely visually in the sidebar. |
| **Sidebar order** | **Fixed** (spawn order) | No float-to-top, no reordering. Predictable muscle memory. |

### 2.1 Hard requirement: full Claude Code fidelity

The wrapper is **thin**. Anything that works in a bare terminal must work here,
specifically and non-negotiably:

- **Image paste** — pasting an image into the prompt (Claude reads the OS clipboard).
- **Voice-to-text** — macOS dictation typing into the focused prompt.
- Permission prompts, file edits, history scroll, truecolor, resize, bracketed paste.

This requirement **outranks the sidebar aesthetic.** The M0 spike (§9) exists to prove
image paste + dictation survive the chosen rendering path. If the always-on embedded
pane can't carry them, we fall back to the overlay-switcher (full-screen direct
passthrough) which is maximally faithful.

## 3. The shape (UX)

```
┌ sessions ───────────────┐┌ keen/parser · "Add VT escape parsing" ──── ● crunching ┐
│ › keen/parser    ● 0:12  ││ ⏺ I'll add the CSI dispatch table now…                  │
│   api/auth       ◐       ││   Tool: Edit  vt/parser.go                              │
│   docs           ✓       ││ > _                                                     │
│   web/ui         ● 1:48  ││                                                         │
│   infra/tf       ◐       ││                                                         │
│   …                      ││                                                         │
├──────────────────────────┤│                                                         │
│ n new   x close   ⌃k list││                                                         │
└──────────────────────────┘└─────────────────────────────────────────────────────────┘
```

- **Left sidebar (Lipgloss):** one row per session = colored status glyph + name +
  subtitle. Fixed spawn order. Active row highlighted.
- **Right pane:** the genuine Claude Code TUI for the focused session.
- **Mouse:** click a sidebar row → switch focus. Click in the pane → focus the pane.
- **Keyboard:** `Ctrl-k` (intercepted) toggles focus to the sidebar; `j/k`/arrows +
  `Enter` switch; `n` new; `x` close; `1–9` jump. Everything else → Claude untouched.

### Status vocabulary — conveyed by **color only** (no bell)

| Glyph + color | Label | Meaning | Source event |
|---|---|---|---|
| `●` yellow | **crunching** | actively working / a tool is running | `UserPromptSubmit`, `PreToolUse` |
| `◐` red | **waiting you (permission)** | blocked on an approval | `Notification` / `permission_prompt` |
| `◐` magenta | **waiting you (idle)** | pinged you after idle | `Notification` / `idle_prompt` |
| `✓` green | **all done** | finished its turn, your move | `Stop` |
| `·` grey | starting / exited | spawned, no events yet / ended | `SessionStart`, `SessionEnd` |

## 4. Architecture

```
                         ┌──────────────────────────────────────────────┐
                         │  keen (one Bubble Tea program)                 │
                         │                                                │
   mouse/keys  ───────►  │  Update(): route by focus + region            │
                         │     ├─ Ctrl-k          → toggle sidebar focus  │
                         │     ├─ sidebar region  → nav commands          │
                         │     └─ pane region     → forward bytes to PTY  │
                         │                                                │
                         │  View(): lipgloss.JoinHorizontal(sidebar,pane) │
                         │                                                │
                         │  ┌────────────── SessionManager ─────────────┐ │
                         │  │ Session[i]:                                │ │
                         │  │   • PTY (creack/pty) running `claude`      │ │
                         │  │   • *vt.Terminal  ← fed by reader goroutine│ │
                         │  │   • status, name, title, cwd, transcript   │ │
                         │  └────────────────────────────────────────────┘ │
                         │                                                │
                         │  StatusServer: unix socket @ $XDG_RUNTIME/keen │
                         └───────▲──────────────────────────────┬────────┘
                                 │ hook events (JSON)            │ spawn with
                                 │                               │ --settings {hooks…}
              ┌──────────────────┴───────┐                       ▼
              │ keen __hook (self re-exec)│◄──── stdin JSON ───  claude (session i)
              │ reads hook JSON + env,    │      from Claude Code hooks
              │ POSTs to the unix socket  │
              └───────────────────────────┘
```

### 4.1 SessionManager / PTY host
- Spawns `claude` in the chosen cwd via **`creack/pty`** (or `charmbracelet/x/xpty`).
- Per session, a goroutine reads PTY output → feeds the session's **`*vt.Terminal`** →
  emits a coalesced `paneDirtyMsg{sessionID}` (throttled; repaints only if focused).
- Env injected per session: `KEEN_SOCKET`, `KEEN_SESSION=<uuid>`, `TERM=xterm-256color`,
  `COLORTERM=truecolor`.
- All PTYs sized to the **pane** dimensions; on `WindowSizeMsg`, relayout + `pty.Setsize`
  every session.

### 4.2 Embedded terminal pane (the hard part — §9)
- **`charmbracelet/x/vt`** parses the PTY byte stream (CSI/SGR/alt-screen/mouse/
  bracketed-paste) into a cell grid; `View()` renders cells via Lipgloss + draws cursor.
- Pane focused → forward raw input bytes (keys, mouse, **bracketed-paste payloads incl.
  image-paste sequences**) straight to the PTY; wrapper steals only `Ctrl-k`.
- **Fidelity note:** image paste and dictation work because Claude reads the OS clipboard
  itself and dictation arrives as ordinary input bytes — *provided we forward input
  byte-for-byte and enable bracketed paste passthrough.* This is precisely what M0 tests.
- **Alternative:** `bubbleterm` (higher-level pty+vt embed) — evaluate vs hand-rolling.

### 4.3 Sidebar
- Pure Lipgloss render of `[]Session`, fixed order, color-coded status glyph, active-row
  highlight, per-row spinner while crunching.

### 4.4 Status engine (hooks → socket)
- Wrapper runs a **unix-socket server**. On spawn it writes a per-session settings JSON
  and launches `claude --settings <file>` so hooks inject **without touching your global
  `~/.claude/settings.json`** (confirmed flag: `--settings <file-or-json>`).
- Each hook = keen re-invoking itself as `keen __hook <event>` (logic in `internal/hook`,
  also built standalone as `cc-deck-hook`): reads hook JSON from stdin + `KEEN_SESSION`/
  `KEEN_SOCKET` from env, forwards `{session,event,payload}` to the socket. Pointing hooks
  at the running keen binary keeps a single `go install` self-contained. (MVP fallback:
  a `jq`+`nc` one-liner.)
- Server maps events to the §3 state machine. Hooks exit 0 fast, never block Claude.

Per-session settings we generate:
```jsonc
{
  "hooks": {
    "SessionStart":     [{ "hooks": [{ "type": "command", "command": "\"<keen>\" __hook start" }] }],
    "UserPromptSubmit": [{ "hooks": [{ "type": "command", "command": "\"<keen>\" __hook crunching" }] }],
    "PreToolUse":       [{ "matcher": "*", "hooks": [{ "type": "command", "command": "\"<keen>\" __hook crunching" }] }],
    "Notification":     [{ "hooks": [{ "type": "command", "command": "\"<keen>\" __hook waiting" }] }],
    "Stop":             [{ "hooks": [{ "type": "command", "command": "\"<keen>\" __hook done" }] }],
    "SessionEnd":       [{ "hooks": [{ "type": "command", "command": "\"<keen>\" __hook exit" }] }]
  }
}
```

### 4.5 Auto-naming
- **Seed (instant):** `basename(cwd)` + git branch.
- **Title (cheap LLM):** on first `UserPromptSubmit`, send the prompt to
  **`claude-haiku-4-5`** (Messages API) → 2–5 word title; cache per session.
- **Refresh:** optionally re-title on `Stop` if drift (debounced). MVP: title once.
- **Fallback (no `ANTHROPIC_API_KEY`):** first line of opening prompt, truncated.
- Manual rename always available.

## 5. Focus & input routing (precise)

```
Update(msg):
  KeyMsg:
    if key == Ctrl-k:                     toggle focus(sidebar ⇄ pane); consume
    else if focus == sidebar:             handle nav (j/k/enter/n/x/1-9)
    else (focus == pane):                 forward raw bytes → active PTY
  MouseMsg:
    if in sidebar region:                 row hit-test → switch active session
    else (in pane region):                forward → active PTY
  WindowSizeMsg:                           relayout; Setsize all PTYs
  paneDirtyMsg{id}:                        if id == active: request repaint
  statusMsg{id,status}:                    update sidebar model (color only)
```

## 6. Tech stack

- **Go** + **`charmbracelet/bubbletea`** + **`charmbracelet/lipgloss`**.
- **`charmbracelet/x/vt`** — terminal emulator. **`creack/pty`** / **`charmbracelet/x/xpty`** — PTYs.
- **`charmbracelet/bubbles`** — filepicker / list / spinner.
- Stdlib `net` (unix socket), `os/exec`, `encoding/json`. Optional `bubbleterm`.

## 7. New-session flow

1. `n` → directory picker (filepicker bubble, recent/favorite dirs, fuzzy).
2. Optional initial prompt.
3. Manager spawns PTY, writes per-session settings, registers + focuses. Seed name shows
   instantly; LLM title fills after first prompt.

## 8. Milestones

- **M0 — De-risk spike (✅ done).** Embedded `vt`+`pty` pane is interactive once query
  replies are pumped back to the PTY; image input works via drag-and-drop; bracketed
  paste preserved. See `spike/README.md`. Embedded path chosen.
- **M1 — Multiplex (✅ done).** Real app at `cmd/keen` + `internal/{session,ui}`: N
  sessions, fixed-order Lipgloss sidebar, focus toggle on `Ctrl-k`, click-to-switch,
  full key table + SGR mouse forwarding, relayout & resize-all. Static `dir (branch)`
  names. Statuses: only Starting/Exited driven (live states arrive in M2).
- **M2 — Status engine (✅ done).** `internal/hooks` + `cmd/cc-deck-hook` + per-session
  `--settings` injection (global `~/.claude` untouched). Unix socket in keen receives
  events → live color-coded glyphs: `UserPromptSubmit`/`PreToolUse` → ● crunching
  (yellow), `Notification` → ◐ waiting (red), `Stop` → ✓ done (green), `SessionEnd` →
  ✕ exited. keen is a single self-contained binary — it serves the hooks by
  re-invoking itself (`keen __hook <event>`, logic in `internal/hook`), so
  `go build -o bin/keen ./cmd/keen` (or `go install …/cmd/keen`) is the whole
  install; `cmd/cc-deck-hook` remains as an optional standalone build.
  Permission-vs-idle split and `start`→ready left for later.
- **M3 — Naming (✅ done).** the hook helper extracts the first prompt from the
  `UserPromptSubmit` payload → keen titles the tab once via `internal/titler`
  (`claude -p --model haiku`, reusing existing auth — no API key), heuristic fallback
  on error. Sidebar is now two lines per session: `dir` + faint Haiku subtitle (git
  branch shown until the title lands). Manual rename still TODO.
- **M4 — Polish.** Manual rename, spinners, elapsed timers, new-session dir picker,
  close/confirm, permission-vs-idle status split, config file.
- **M5 — tmux rototill (the `tmux-spike` branch).** The embedded VT emulator and
  the entire input-routing layer (`internal/session`, `KeyToBytes`, `MouseToVT`,
  sidebar hit-testing against a shared screen) are replaced by a **private tmux
  server**: `keen` boots `tmux -L keen` with a generated config and execs into
  the attach; the sidebar is a Bubble Tea program in the left pane; each Claude
  runs raw in its own pane, parked in a background window and brought into view
  by `swap-pane`/`join-pane`. Session metadata (id, spawn order, labels, status)
  persists as `@keen-*` pane user options, so a restarted sidebar — or a fresh
  `keen` after the client died — rebuilds the registry by scanning panes.
  Motivations and what we bought: (1) **bounded native copy** — tmux mouse mode
  selects within a pane and copies via `set-clipboard`/OSC 52, no Option key, no
  sidebar bleed, works in Cursor; (2) **persistence** — quitting or crashing keen
  no longer kills the herd (`q` detaches); (3) the phantom-input bug class
  (mis-routed keystrokes, see `.beads/keen-phantom-input.md`) is structurally
  impossible — keys go to the focused pane, and background panes can't receive
  input; (4) ~900 lines of emulator/key-reconstruction code deleted along with
  the experimental `x/vt` dependency risk. Hooks, statuses, and Haiku titling
  carry over unchanged.

## 9. Risks & the M0 spike

Always-on sidebar means **embedding a full alt-screen TUI inside another TUI**. Hazards:
alt-screen + raw mode + mouse + bracketed paste fidelity; truecolor/SGR re-render;
resize reflow to pane size; repaint cost across 10 streams; cursor/focus passthrough.

**M0 acceptance test (fidelity-first):** in the embedded pane, run a real task with a
permission prompt, edit a file, scroll history, resize the outer terminal, **paste an
image into the prompt, and dictate text via macOS voice-to-text** — all behave exactly
as in a bare terminal. If `vt` can't carry image paste / dictation, switch to the
**overlay-switcher** (full-screen direct passthrough + popup list on `Ctrl-k`), which is
maximally faithful by construction.

## 10. Open questions

1. **Title refresh policy** — once vs re-summarize on drift (cost vs freshness).
2. **`--continue` hook bug** — resumed sessions report a *stale* `session_id`/
   `transcript_path`. We key on our own `KEEN_SESSION`, sidestepping it — but verify.
3. **Crash recovery** — out of scope (wrapper owns PTYs); the one thing tmux would buy.

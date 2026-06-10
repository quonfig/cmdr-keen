# Phantom text in a session's input box (not typed by the user)

## Symptom
When the user returns to a keen session that is idle / done / waiting for input,
there is sometimes text already sitting in Claude Code's input box that they did
not type. Observed example: the box held `Improve indicator latency and add
status legend` — which matched that session's own topic/task.

Key detail from the user: when they start typing, the existing text is **not**
cleared — their new keystrokes **insert into / overtype** it. So it is a real,
editable draft living in Claude Code's input buffer, NOT ghost/placeholder text
and NOT a keen render glitch (it persists, doesn't repaint away).

## What was already ruled out (code-traced)
keen writes only TWO things to a session's PTY:
  1. user keystrokes — `internal/ui/model.go` `handleKey` -> `s.Write(KeyToBytes(k))`
  2. emulator protocol replies (cursor/DA/DSR) — `internal/session/session.go:167`
     `io.Copy(ptmx, emu)` — these are control sequences, never prose.
The other two channels are read-only w.r.t. Claude:
  - the hook (`cmd/cc-deck-hook/main.go`) is outbound-only (Claude -> keen socket);
    `internal/hooks/server.go` never writes back to Claude.
  - the titler (`internal/titler/titler.go:45`) shells out to a SEPARATE
    `claude -p --model haiku` process, captures stdout via `.Output()`, stdin is
    /dev/null, and it does NOT inherit KEEN_SOCKET/KEEN_SESSION. It cannot touch
    the visible session's PTY.
Conclusion: the task/topic label keen computes never flows into any input box.
keen is structurally incapable of typing that text. The bytes came from a
keyboard.

## Two remaining hypotheses
1. **Mis-routed keystrokes (keen bug).** keen routes every keystroke to
   `m.mgr.Active()`. The active session can change WITHOUT Ctrl-K via a mouse
   press on a sidebar row (`model.go` `handleMouse` -> `SetActive(idx,"mouse")`,
   also clears `sidebarFocus`). If a stray/mis-hit click silently re-targets the
   active session mid-type, subsequent keystrokes pile into a session the user
   isn't watching. Note: last commit before this was `61311b5 "fix sidebar click
   hit-testing"` — hit-testing here has been fragile. (`SessionIndexAt` in
   `internal/ui/layout.go`.)
2. **Claude Code's own draft preservation (keen behaving correctly).** Claude
   keeps unsubmitted input per session. If the user typed that line here earlier,
   left it unsubmitted, and navigated away, Claude holds it; it's still there on
   return. Matches the topic precisely because they typed it here.

## Instrumentation already wired (this is what the repro session will use)
A `KEEN_DEBUG`-gated file logger was added: `internal/debug/debug.go`.
  - Unset => every `debug.Logf` is a no-op.
  - `KEEN_DEBUG=1` (or `true`/`on`) => logs to `$TMPDIR/keen-debug.log`.
  - `KEEN_DEBUG=/path` => logs to that path.
  - Startup banner: `--- keen debug log opened (pid N) ---` (grep by pid to
    separate runs; the file appends across runs).
Log points:
  - `key -> <sessionID> (idx N): "<bytes>"`  — every keystroke + where it routed
    (`model.go` handleKey)
  - `setActive: <from> (idx) -> <to> (idx) reason=<tag>` — every focus switch;
    reasons: mouse, key:up, key:down, key:number, spawn, close
    (`internal/session/manager.go` setActive)
  - `mouse press at (x,y) hit sidebar row idx N (button .., action ..)` — clicks
    landing on a sidebar row (`model.go` handleMouse)

## How to diagnose when the bug reproduces
Run keen with `KEEN_DEBUG=1` (the user is doing this now). When the phantom text
appears, inspect `$TMPDIR/keen-debug.log`:
  - If you see a `setActive ... reason=mouse` (or a `mouse press ... hit sidebar
    row`) the user did NOT intend, followed by `key ->` lines landing on a
    session they weren't watching => **mis-routing bug**. Fix in hit-testing:
    require the press to be inside sidebar bounds, and don't switch on
    release/drag — only on a genuine press. See `SessionIndexAt`
    (`internal/ui/layout.go`) and `handleMouse` (`internal/ui/model.go`).
  - If `key ->` lines always land on the expected session with no surprise
    `setActive` => it's Claude's draft preservation; keen is correct, close as
    not-a-bug (or document the behavior).

## Third hypothesis (added 2026-06-10, untested)
The observed text reads exactly like a keen haiku label, and the sidebar
renders those directly left of the pane. Old keen held the terminal in
mouse-tracking mode, so copying required Option-drag — whose selection was NOT
bounded to the pane and could sweep in a sidebar row. Chain: Option-drag copy
→ clipboard gains a haiku-ish line → stray Cmd-V into an input box → Claude
preserves it as a draft → "phantom" text matching the session's own topic.
This fits the topic-match detail better than mis-routing (mis-routed typing
would be your words for a *different* session).

## Impact of the tmux rototill (branch `tmux-spike`)
The M5 architecture deletes the input-routing layer entirely: tmux delivers
keys to the focused pane, background panes can't receive input, so
hypothesis 1 (mis-routing) becomes structurally impossible — and tmux's
bounded per-pane selection removes hypothesis 3's trigger. If phantom text
still appears under the tmux build, it is hypothesis 2 (Claude's own draft
preservation) and this closes as not-a-bug. The KEEN_DEBUG key logging no
longer exists there (no keystrokes pass through keen to log).

## Next steps
- [ ] Reproduce with KEEN_DEBUG on; capture the relevant log window.
- [ ] Decide mis-routing vs. draft-preservation from the log.
- [ ] If mis-routing: harden sidebar click hit-testing; add a regression test in
      `internal/ui` (there are already layout/hit-test tests there).

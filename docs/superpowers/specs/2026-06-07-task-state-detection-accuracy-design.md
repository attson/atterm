# Task state detection accuracy — alt-screen silence heuristic (design)

Date: 2026-06-07
Status: Draft (design phase); pending implementation plan
Roadmap item: P-personal — second iteration on session task state. The
session-attention work (2026-06-06 spec) made `task_state` user-visible
via the desktop sidebar; this spec closes the next pain point reported
in practice: AI / TUI sessions get stuck at `running` for the entire
duration of the tool, so the inbox model can't tell "Claude is thinking"
from "Claude finished its reply, waiting on me".

## 1. Goal

Today the `task_state` transitions in `internal/session/session.go` rely
on two signals:

1. **OSC 133** events emitted by the shell integration (high confidence).
   The shell prints `\x1b]133;C\x07` before each command and `\x1b]133;D;<exit>\x07`
   after, so we get clean `running` → `completed/failed` transitions.
2. **`looksLikeWaitingInput`** keyword regex (`[y/n]`, `password:`,
   `confirm`, …) — a tiny fixed list, applied to every output chunk.

The gap: when a user runs `claude`, `codex`, `aider`, or any other
full-screen TUI inside the shell, the shell emits `OSC 133;C` once on
launch and `OSC 133;D` once on exit. **The whole interactive session in
between is reported as `running`** — even when the AI has finished its
reply and is silently waiting for the next prompt. The session-attention
inbox depends on `→ waiting_input` to surface unread badges, so this
gap means the most attention-worthy moment (AI is ready for you) never
fires.

Goal: add a third signal — **alt-screen + output silence > N seconds** —
so the relay correctly transitions `running` → `waiting_input` when an
interactive TUI goes idle, and back to `running` when activity resumes.

After this lands:

- Open `claude` → shell emits `OSC 133;C` → `running` (unchanged).
- Claude streams a response → output flowing → `running` (unchanged).
- Claude finishes, sits at its input box → 5 s of silence → relay flips
  to `waiting_input` and bumps `attention_at` → desktop sidebar shows
  the orange `◐` and the session lights up unread.
- User attaches the tab → relay auto-marks seen (existing logic).
- User types, Claude responds → output resumes → relay flips back to
  `running`. `attention_at` is **not** rolled back (so the unread badge
  persists until attach — which is the right "you have something to
  read" semantics).
- Claude finishes again → another 5 s of silence → fresh
  `waiting_input` with a newer `attention_at`.

Out of scope:

- Foreground process introspection (`tcgetpgrp` + `/proc`, `ps`) — a v2
  signal if the silence heuristic alone proves insufficient. Adds
  Linux/macOS-only code paths.
- Multi-signal scoring (BEL, OSC 9 notifications, cursor position).
- Cross-session correlation / per-host throttling.
- Web / mobile / relay code changes — protocol fields don't change, the
  improved derivation is transparent downstream.

## 2. Architecture

```
internal/session/session.go (modified)
  Session struct (additive fields)
    silenceTimer       *time.Timer  // per-session, may be nil
    waitingFromSilence bool         // set only when our heuristic flipped state

  updateTerminalState(data) (modified)
    - existing OSC 133 + looksLikeWaitingInput path stays first
    - if waitingFromSilence:                       // output arrived
        TaskState ← running                        // restore
        waitingFromSilence ← false
        broadcast META
    - rescheduleSilenceTimerLocked()               // tail of every call

  rescheduleSilenceTimerLocked() (new)
    - stop existing timer (if any)
    - if !altScreen || TaskState != running || closed: return  (don't rearm)
    - timer ← time.AfterFunc(silenceThreshold, s.onSilenceFired)

  onSilenceFired() (new, runs in timer goroutine)
    - s.mu.Lock(); defer s.mu.Unlock()
    - re-check guards (closed / state / altScreen) — could have raced
    - if (now - LastOutputAt) < silenceThreshold → reschedule, return
    - TaskState ← waiting_input
    - AttentionAt ← now.Unix()
    - waitingFromSilence ← true
    - broadcast META

  applyOSC133Locked('D') (modified)
    - existing logic
    - additionally: silenceTimer.Stop(); waitingFromSilence ← false

  Close() (modified)
    - existing logic + silenceTimer.Stop()
```

No protocol changes (`waiting_input` is already a `task_state` value).
No new clients to update — the desktop sidebar, web list, mobile inbox,
and Web Push paths all react to `task_state` changes uniformly.

## 3. Configuration

Read once at session creation (or registry init) from env, with sane
defaults:

| Variable | Default | Effect |
| --- | --- | --- |
| `ATTERM_TASK_SILENCE_DETECT` | `1` | `0` disables the heuristic entirely; only OSC 133 + keyword path remains. |
| `ATTERM_TASK_SILENCE_THRESHOLD_MS` | `5000` | Idle time in alt-screen + running before we flip to `waiting_input`. |

For tests, the threshold is read into a struct field
(`Session.silenceThresholdMS`) at construction, so the test can
construct sessions with `100ms` and run the table in under a second.

## 4. State transition table

(All transitions happen under `s.mu.Lock()`.)

| Trigger | Pre-state | Conditions | Post-state | Side effects |
| --- | --- | --- | --- | --- |
| `OSC 133;C` | any | — | `running` | reschedule silence timer |
| `OSC 133;D;<exit>` | any (except `idle`) | — | `completed` / `failed` | stop timer; `waitingFromSilence ← false`; bump `AttentionAt` for non-shell types (existing rule) |
| `looksLikeWaitingInput` match | `idle` or `running` | not in `waiting_input` already | `waiting_input` | bump `AttentionAt` (existing); `waitingFromSilence` left false |
| Silence timer fires | `running` | `altScreen && now-LastOutputAt ≥ silenceThreshold` | `waiting_input` | bump `AttentionAt`; `waitingFromSilence ← true` |
| Output arrives | `waiting_input` (silence) | `waitingFromSilence == true` | `running` | reschedule silence timer; do **not** touch `AttentionAt` |
| Output arrives | `waiting_input` (keyword) | `waitingFromSilence == false` | unchanged | next OSC 133 / external mergeTaskMeta will move it |
| `Close()` | any | — | `closed` | stop timer; clear `waitingFromSilence` |
| External `mergeTaskMeta` (disconnected) | any | — | `disconnected` | stop timer; clear `waitingFromSilence` |

## 5. The `waitingFromSilence` flag — why it must exist

The existing keyword heuristic (`looksLikeWaitingInput`) is a
high-confidence signal: if the output literally says `password:`, the
process is almost certainly waiting on a read. We do **not** want the
new "output arrived → flip back to running" rule to undo that decision
(echoing the user's character of input is "output" too).

`waitingFromSilence` records the provenance of the current
`waiting_input`. The "output arrived → restore" rule only fires when
that flag is true. Result:

- Silence heuristic: state oscillates with activity. Bumps
  `AttentionAt` each silence cycle.
- Keyword heuristic: state sticks until the next OSC 133 frame (or an
  external state push, e.g. relay-side adoption).

## 6. AttentionAt semantics on silence cycles

When a silence event flips to `waiting_input`, `AttentionAt` is bumped
to `now`. The session attention spec defines `unread = AttentionAt >
SeenAt && SubscriberCount == 0`, so the desktop sidebar lights up.

When output resumes and we restore to `running`, we **do not** rewind
`AttentionAt`. Rationale: the user might not have looked yet. The
"there's something for you" signal should persist until they actually
attach (which auto-marks seen — existing behavior).

If the AI then completes its next response and goes silent again, the
timer fires again with a **newer** `AttentionAt`. The session stays
unread, but the timestamp tracks the most recent "ready for you"
moment, which is what consumers like notification deduplication should
key on.

Known minor noise: a session that briefly thinks-silently for ≥
threshold seconds before resuming output will briefly flip to
`waiting_input` and bump `AttentionAt`. The UI shows orange `◐` for a
beat and the unread badge appears. This is acceptable for the MVP. If
it proves annoying, a v2 settling window (state must stay
`waiting_input` for K seconds before AttentionAt actually bumps) is the
mitigation, but it adds complexity for a tradeoff that may not matter
in practice.

## 7. Testing

New file: `internal/session/silence_test.go`. All cases use an injected
short threshold (≈ 100 ms) so the suite stays sub-second.

| Case | Setup | Assertion |
| --- | --- | --- |
| OSC 133 C + idle in alt-screen | `C;claude`, then 200 ms wait | TaskState ends at `waiting_input`; `waitingFromSilence == true`; AttentionAt > 0 |
| Output cancels silence | C + 50 ms wait + 1 byte output | Never enters `waiting_input` |
| Output restores running | C + 200 ms wait + 1 byte output | `waiting_input` → `running`; `waitingFromSilence` cleared; AttentionAt unchanged from the bump |
| OSC 133 D after silence | C + 200 ms wait + `D;0` | Ends at `completed` (D wins); `waitingFromSilence == false`; timer stopped |
| Non-alt-screen silence | C in normal mode + 200 ms wait | Stays `running` (no auto-flip) |
| Keyword waiting_input + output | Output containing `password:` + then a newline | Remains `waiting_input`; `waitingFromSilence == false` |
| Threshold via env | `ATTERM_TASK_SILENCE_THRESHOLD_MS=50` + 80 ms wait | Fires at 50 ms |
| Disable via env | `ATTERM_TASK_SILENCE_DETECT=0` + 500 ms wait | Never fires |
| Close cancels timer | C + Close() immediately | No-op when timer would have fired; no race; no panic |
| Disconnected stops timer | C + external mergeTaskMeta(disconnected) + wait | No silence transition after; timer cleared |
| Repeat silence is monotone | C + 200 ms wait + output + 200 ms wait | Second AttentionAt > first; both fire `waiting_input` |
| Concurrency | High-frequency outputs from multiple goroutines | `go test -race ./internal/session/...` clean |

## 8. Migration & compatibility

Backward compatible. No protocol fields change. Older clients render
`task_state` exactly as before; the only visible difference is that
those values now reflect actual activity in alt-screen TUIs. Older
agents / older shells without OSC 133 integration still see no silence
detection (because they never reach `running` in alt-screen).

No database migrations. No new bindings. No frontend changes required
for this spec — the desktop sidebar (2026-06-06) already renders
`waiting_input` correctly; the silence heuristic just makes it fire.

## 9. Why not v2 signals now

A note on the rejected approaches, for future readers:

- **`tcgetpgrp` + `/proc/PID/status` / `ps`**: most accurate possible
  signal (process is sleeping on `read`), but adds Linux- and
  macOS-specific code paths, needs to plumb the PTY `fd` into the
  session layer, and gives almost nothing on Windows. The silence
  heuristic catches the same cases at the cost of a 5 s lag, with zero
  OS surface area.
- **Multi-signal scoring**: BEL counts, OSC 9 notifications, DSR
  requests, recent-output magnitude. Each could improve the heuristic
  by some marginal percentage, but with a quadratic test matrix and a
  pile of tuning knobs. Ship the simple thing; revisit if it's wrong
  in measurable ways.

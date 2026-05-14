# Shell Integration (OSC 133) — Command Completion Notifications Design

**Date:** 2026-05-14
**Status:** Approved

## Goal

Detect command boundaries in atterm-spawned shells by injecting OSC 133 emitter hooks at PTY spawn time, parse them in the desktop frontend's xterm.js, and fire a system notification when a command finishes while the AT Term window is unfocused and the command ran long enough to be worth interrupting the user.

This is the first stone of the Phase 3 roadmap item "离开电脑后继续看任务" in `docs/spec/architecture.md`. It lays an event-layer foundation that later iterations (Web Push, cross-session command history, mobile prompt-state detection) can build on without revisiting shell integration plumbing.

## Motivation

Today's BEL-based notification (see `2026-05-14-bel-notifications-design.md`) only fires when a CLI tool happens to emit `\x07`. Most build / test / deploy commands do not. We want a generic "command finished" signal that works for any command, with the user's exit code and elapsed time, and only interrupts the user when it actually matters (long-running command, window unfocused).

OSC 133 is the established way shells signal command boundaries to the terminal. iTerm2, WezTerm, Kitty, VS Code embedded terminal, and Warp all consume it. We follow the same convention.

## Non-Goals

- Cross-session command history / search. The events are consumed live in the frontend and not persisted. A future iteration may add a structured event frame to the wire protocol and persist events for search.
- Web Push to a browser tab that is not open. Only the desktop frontend (Wails) sees notifications in this iteration. Browsers / mobile WebView consume OUT bytes but do not invoke notification APIs.
- Prompt-state detection (`OSC 133;A`/`B`) used to drive a mobile "common reply" toolbar. The parser tracks A/B for completeness, but no consumer wires them in this iteration.
- Block-style UI à la Warp. atterm stays a scrolling terminal; OSC 133 is metadata only.
- Shell other than zsh / bash / fish / PowerShell. cmd.exe, nu, xonsh, elvish, etc. fall back to "no integration"; the PTY still works.
- Manual / opt-in user installation. Integration is auto-injected by default. A Settings toggle exists for users who want it off.
- Persisting OSC 133 events. Tracker state lives only inside a `TerminalView` instance.

## Behavior

### Trigger

For each `TerminalView` instance attached to a local session:

1. The xterm.js parser invokes our OSC 133 handler whenever an `OSC 133 ; … BEL` (or `ST`) sequence reaches the screen. xterm passes the content between the leading `OSC 133;` and the terminator as the handler's payload (so the handler sees `"A"`, `"B"`, `"C"`, `"D"`, or `"D;<exit>"`).
2. The handler updates a per-pane `CommandTracker`:
   - `A` (prompt start) and `B` (prompt end) are recorded but not consumed by this iteration.
   - `C` (command exec start) sets `state = { phase: "running", startedAt: nowMs }`.
   - `D` or `D;<exit>` (command finished) emits a `CommandEvent{ exitCode, elapsedMs }` if a prior `C` was recorded; otherwise the orphan `D` is ignored. A bare `D` (no exit code) is treated as `exitCode = 0`.
3. On each `CommandEvent`, the view checks three gates:
   - **Local-session gate** — if `props.endpoint` resolves to a remote relay (i.e., the pane is attached to someone else's session via cast), no notification.
   - **Focus gate** — if `document.hasFocus() === true`, no notification.
   - **Threshold gate** — if `elapsedMs < thresholdSec * 1000`, no notification.
4. If all gates pass, call the existing `showNotification(title, body)` wrapper (the same one BEL notifications use).

### Notification content

- **Title:** `"AT Term"`.
- **Body:** `"Command finished · exit <code> · <elapsed> · <sessionLabel>"`. `<elapsed>` is `Ns` if `<60`, else `MmSs`. `<sessionLabel>` reuses the prop already plumbed for BEL.

### Injection

When `desktop/relay_host.go::NewSession` spawns a PTY, it consults `appConfig.ShellIntegrationEnabledOrDefault()`. If `true`, it calls `shellintegration.Prepare(shellPath, true)` which returns a `Plan` containing extra env vars, extra command args, a cleanup function, and the detected shell name (for logs).

The Plan is merged into the spawn parameters. The cleanup function is invoked when the session is removed from the registry (PTY exit or user closes the tab).

If `enabled=false` or the shell is unrecognized, `Prepare` returns a zero-value Plan and the PTY spawns identically to today.

### Per-shell strategy

| Shell | Mechanism | Notes |
|-------|-----------|-------|
| zsh | Set `ZDOTDIR=<UserCacheDir>/atterm/shell-integration/zsh-<sessionId>/`; write a `.zshrc` there that sources the user's original rc (from `$ATTERM_ORIG_ZDOTDIR` if set, else `$HOME/.zshrc`) and then sources the embedded `atterm.zsh` snippet. | Original `$ZDOTDIR` exported as `$ATTERM_ORIG_ZDOTDIR` so the wrapper can find user config. |
| bash | Append `--rcfile <path> -i` args; rcfile sources `~/.bashrc` (guarded) then the snippet. | bash flags must be passed as command-line args; `BASH_ENV` is fallback for non-interactive shells we do not target. |
| fish | Write `$XDG_CONFIG_HOME/fish/conf.d/atterm-integration.fish` (or `$HOME/.config/fish/conf.d/`). fish auto-loads `conf.d/*.fish`; this file is shared across sessions and is overwritten on each Prepare (idempotent). | No cleanup — the file persists. Documented as a known limitation. |
| PowerShell | Append `-NoExit -Command "& '<path>'"` args; the script body is the embedded `atterm.ps1` snippet. | We do not modify `$PROFILE`; integration is per-spawn only. |

All snippets share:

- A guard at the top (`[[ -n "$ATTERM_SHELL_INTEGRATION" ]] && return` or shell-equivalent) so re-sourcing is a no-op.
- Hooks registered via the shell's *additive* mechanism (`precmd_functions+=`, `preexec_functions+=`, `PROMPT_COMMAND` chaining, `--on-event` listeners, PowerShell `prompt` function wrapping with original-prompt preservation) so frameworks like oh-my-zsh / starship / powerlevel10k / oh-my-posh keep working.
- `set +e` / `try` style guards so a snippet bug never breaks shell startup.

### Configuration

Two new fields in `appConfig` follow the existing pointer-or-default pattern:

- `ShellIntegrationEnabled *bool` — `nil` (= default `true`). Persisted at `~/.config/atterm/config.json`.
- `CommandNotifyThresholdSeconds *int` — `nil` (= default `10`). Clamped to `[1, 600]` by `CommandNotifyThresholdSecondsOrDefault()`.

Setting `ShellIntegrationEnabled=false` does not affect already-running sessions. Setting it `true` does not retroactively inject into running sessions. Injection only happens at spawn time.

Changing `CommandNotifyThresholdSeconds` takes effect immediately for all panes via the reactive prop.

## Components

### Backend

- **`desktop/shellintegration/` (new package)** — Embeds four snippets via `//go:embed`. Public surface:

  ```go
  type Plan struct {
      ExtraEnv  []string  // appended to cmd.Env
      ExtraArgs []string  // appended to cmd.Args after the shell binary
      Cleanup   func()    // may be nil; callers must nil-check before invoking
      Shell     string    // "zsh"/"bash"/"fish"/"pwsh"/"" — for logs
  }

  // Prepare never returns an error. Internal failures (mkdir, write,
  // unknown shell) yield a zero-value Plan plus a one-time warn log.
  func Prepare(shellPath string, enabled bool) Plan
  ```

  Fish returns `Cleanup = nil` because its snippet lives in shared `conf.d/` and is overwritten idempotently rather than scoped per session. Zsh / bash / pwsh return non-nil `Cleanup` that removes the per-session temp directory.

  Files: `detect.go`, `zsh.go`, `bash.go`, `fish.go`, `pwsh.go`, `prepare.go`, `snippets.go` (the `//go:embed` directives), plus `snippets/atterm.{zsh,bash,fish,ps1}`. Tests as listed under Testing.

- **`desktop/relay_host.go`** — `NewSession` calls `Prepare`, merges Plan into `ptyhost.OpenOptions`, and registers `Plan.Cleanup` to run when the session is removed from the registry. ~20–30 lines of wiring.

- **`desktop/config.go`** — Two new fields with `*bool` / `*int` types and `OrDefault` accessor methods.

- **`desktop/app.go`** — Four new Wails bindings:
  - `GetShellIntegrationEnabled() bool`
  - `SetShellIntegrationEnabled(enabled bool) error`
  - `GetCommandNotifyThresholdSeconds() int`
  - `SetCommandNotifyThresholdSeconds(seconds int) error`

  `Set*` writes via the existing `cfgStore.Update` flow.

### Frontend

- **`desktop/frontend/src/lib/commandFinish.ts` (new)** — Pure helpers:

  ```ts
  export interface CommandEvent {
      kind: "finished"
      exitCode: number
      elapsedMs: number
  }

  export class CommandTracker {
      onOsc133(payload: string, nowMs: number): CommandEvent | null
  }

  export function shouldNotifyCommand(
      ev: CommandEvent,
      opts: { focused: boolean; thresholdSec: number; isLocal: boolean },
  ): boolean

  export function formatElapsed(ms: number): string
  ```

  `payload` is the string xterm.js passes to a handler registered via `term.parser.registerOscHandler(133, cb)`. xterm strips the leading `OSC 133;` and the terminator (`BEL` or `ST`), so the handler sees `"A"`, `"B"`, `"C"`, `"D"`, or `"D;<exit>"` (`<exit>` is the decimal exit code, possibly empty). `CommandTracker` parses on that grammar.

- **`desktop/frontend/src/components/TerminalView.vue`** — In `ensureTerm()` after the existing setup, register a `term.parser.registerOscHandler(133, payload => …)`. The handler instantiates one `CommandTracker` per `TerminalView` and wires through `shouldNotifyCommand` + the existing `showNotification` wrapper. Add a `commandNotifyThresholdSec: number` prop. The handler returns `false` so xterm continues normal processing of the (otherwise invisible) OSC sequence. Plumb a local-session predicate from existing endpoint inspection.

- **`desktop/frontend/src/App.vue`** — Read `GetCommandNotifyThresholdSeconds()` once at startup, store reactively, pass down through `PaneGrid` → `TerminalView` like the existing theme prop.

- **`desktop/frontend/src/components/PaneGrid.vue`** — Forward the new prop.

- **`desktop/frontend/src/components/SettingsGeneral.vue`** — Add two controls below the existing notifications toggle:
  - A toggle "Enable shell integration" bound to `ShellIntegrationEnabled`.
  - A number input "Command-finished notification threshold (seconds)" bound to `CommandNotifyThresholdSeconds`, `min=1 max=600`.

- **`desktop/frontend/src/lib/api.ts`** — Four typed wrappers mirroring the new bindings; updates to the `AppBindings` interface block.

### Documentation

- **`docs/shell-integration.md` (new)** — Plain-language doc covering: what gets injected, where files live, how to disable, fallback snippets for users on shells we do not auto-inject (cmd.exe, nu, xonsh). Linked from README under "Shell integration".

## Data flow

### Spawn-time injection

```
NewSession(req)
  → cfgStore.Get().ShellIntegrationEnabledOrDefault()
  → shellintegration.Prepare(shellPath, enabled)
      ├─ enabled=false → zero Plan
      ├─ unknown shell → zero Plan (one-time info log)
      └─ recognized shell:
          ├─ mkdir + write wrapper rc + write snippet (best-effort)
          └─ return Plan{ExtraEnv, ExtraArgs, Cleanup, Shell}
  → ptyhost.Open(shell, args..., env...)
  → relay.Server.AdoptSession(...)
  → on session removal: if Plan.Cleanup != nil, call it
```

### Run-time event flow

```
Shell hook prints OSC 133;C  (preexec) → PTY stdout
  → ptyhost reader → session.PushOut(seq, bytes)
  → fan-out to subscribers → xterm.js term.write(bytes)
  → xterm parser invokes OSC 133 handler
      → CommandTracker.onOsc133("C", now): state = {running, startedAt}
... command runs ...
Shell hook prints OSC 133;D;0 (precmd) → ... → OSC handler
  → CommandTracker.onOsc133("D;0", now): returns CommandEvent
  → shouldNotifyCommand(ev, {focused, thresholdSec, isLocal})
      ├─ false: nothing
      └─ true:  showNotification("AT Term",
                  "Command finished · exit 0 · 12s · <label>")
```

### Settings update

```
User toggles "Enable shell integration"
  → SettingsGeneral.vue onChange
  → api.SetShellIntegrationEnabled(false)
  → desktop/app.go → cfgStore.Update → atomic config write
  → no effect on running sessions
  → next NewSession reads the new value

User changes threshold from 10 to 60
  → api.SetCommandNotifyThresholdSeconds(60)
  → App.vue's reactive ref updates → prop flows to all TerminalView panes
  → next CommandEvent uses 60s threshold
```

## Error handling

All failures degrade to current behavior; nothing in this design must block PTY startup.

### Injection failures (spawn-time)

| Failure | Handling | User-visible |
|---------|----------|--------------|
| Empty `shellPath` or unrecognized basename | Zero Plan; `log.Info("shell integration: unsupported shell")` once per shell name | None |
| `os.UserCacheDir` fails | Zero Plan; one-time warn log | None |
| Write to temp dir / fish conf.d fails | Zero Plan; warn log with `error` | None |
| Cleanup fails (file in use, perms) | Warn log; not propagated | None |

`shellintegration.Prepare` returns no error to keep the call site free of branching.

### Snippet runtime conflicts

| Scenario | Handling |
|----------|----------|
| User original rc errors out | Wrapper sources it with `|| true`; snippet still loads |
| Framework (oh-my-zsh / starship / powerlevel10k / oh-my-posh) sets PS1 | Snippets never touch PS1; use additive hook arrays / preserve original `prompt` function (PowerShell) |
| `set -e` in user bash rc | Snippet brackets its body with `set +e` … restore-old-state |
| Double-source via `exec zsh` | Guard variable prevents double-registration; `exec` clears env so a new shell process gets injected fresh (acceptable) |
| Fish conf.d collision with stale file | Idempotent overwrite; snippet header guards against re-loading inside one fish session |
| Pwsh `$PROFILE` missing | Not used; we go through `-NoExit -Command` |

### Parser-runtime edge cases

| Scenario | Handling |
|----------|----------|
| User process emits a fake `OSC 133;D;0` | Trusted as a real event. OSC 133 has no authentication; matches behavior of iTerm2 / VS Code. |
| `D` without prior `C` | Tracker returns `null`; no notification |
| `C` without subsequent `D` (process killed) | Tracker state lingers as "running"; next `C` overwrites; no leak |
| Non-numeric exit in `D;<x>` | `exitCode = -1`; still treated as command finished |
| Multiple `D` in quick succession (`make && make test`) | Each evaluated independently; threshold filters short ones |
| `term.parser.registerOscHandler` throws (e.g. `allowProposedApi` not set) | `try/catch` around registration; `console.warn`; pane stays usable |
| `showNotification` binding fails | Existing `notify.go` swallows + logs once; unchanged behavior |

### Config edge cases

| Scenario | Handling |
|----------|----------|
| Threshold persisted as 0 / negative / >600 | `OrDefault` clamps to `[1, 600]` |
| Config file corrupted | Existing `config.go` fallback path applies; new fields default |
| Toggle off while sessions running | Existing sessions keep emitting OSC; frontend keeps processing; new sessions skip injection |

### Remote-attach edges

| Scenario | Handling |
|----------|----------|
| Browser attached to desktop session, on a phone | Frontend running in browser sees OSC bytes (sequences are fan-out unchanged) but local-session gate suppresses notifications — out of scope for this iteration |
| Desktop A cast-attached to desktop B's session | Same as above: pane is "remote", no notification on A |
| Viewer (non-driver) on local desktop | Notifies if PTY unfocused; driver/viewer role does not affect notification gating |

### Platform notes

| Platform | Notes |
|----------|-------|
| macOS | Default shell zsh; users with bash/fish via `$SHELL` honored |
| Linux | bash / zsh / fish all common; fish path uses `$XDG_CONFIG_HOME` with `$HOME/.config` fallback |
| Windows | Default cmd.exe is not supported (zero Plan, no error). PowerShell (`pwsh.exe`) supported via `-NoExit -Command`. Windows Terminal vs Wails-spawned shells unaffected since we control spawn. |
| WSL | `shellPath` typically `/bin/bash`; treated as Linux |

## Testing

### Go unit tests

| File | Coverage |
|------|----------|
| `desktop/shellintegration/detect_test.go` | Basename / path / case mapping to shell enum; unknown shells; cmd.exe rejected |
| `desktop/shellintegration/zsh_test.go` | Prepare returns Plan with `ZDOTDIR=`, temp dir contains `.zshrc` + `atterm.zsh`, wrapper sources `$ATTERM_ORIG_ZDOTDIR or $HOME`, cleanup deletes the dir |
| `desktop/shellintegration/bash_test.go` | ExtraArgs contains `--rcfile <path> -i`; rcfile guards `~/.bashrc` source; cleanup runs |
| `desktop/shellintegration/fish_test.go` | Path is `$XDG_CONFIG_HOME/fish/conf.d/atterm-integration.fish` or `$HOME/.config/...` fallback; second Prepare overwrites cleanly; Cleanup is no-op |
| `desktop/shellintegration/pwsh_test.go` | ExtraArgs contains `-NoExit -Command "& '<path>'"`; ps1 contents include guard + snippet body |
| `desktop/shellintegration/prepare_test.go` | `enabled=false` → zero Plan; unknown shell → zero Plan; UserCacheDir failure (injected) → zero Plan; no panic on any error path |
| `desktop/shellintegration/snippets_test.go` | All four embedded snippets non-empty; each contains its expected guard variable |
| `desktop/config_shell_integration_test.go` | Default `ShellIntegrationEnabledOrDefault()=true`; round-trip false; threshold default 10; threshold clamps `[1, 600]` |
| `desktop/app_shell_integration_test.go` | Four bindings round-trip through cfgStore |
| `desktop/relay_host_shell_integration_test.go` | Spawn path invokes `Prepare` when enabled; Plan content merged into PTY args/env; cleanup invoked on session close |

### TypeScript unit / source-level tests

| File | Coverage |
|------|----------|
| `desktop/frontend/src/lib/commandFinish.test.ts` | Tracker C/D happy path; orphan D ignored; two consecutive C overwrite; non-numeric exit → -1; `shouldNotifyCommand` four-branch matrix (focused / non-local / below-threshold / pass); `formatElapsed` table |
| `desktop/frontend/src/components/TerminalView.test.ts` (extend) | Source-level: imports `CommandTracker` + `shouldNotifyCommand`; registers OSC 133 handler; invokes `showNotification` with "Command finished" body; `commandNotifyThresholdSec` prop declared; local-session guard present |
| `desktop/frontend/src/components/SettingsGeneral.test.ts` (extend) | Renders shell-integration toggle + threshold number input; wires onChange to the new api wrappers; `min=1 max=600` attributes present |
| `desktop/frontend/src/lib/api.ts` test extension | Four new wrappers exist with expected signatures |

### Manual smoke (pre-merge checklist)

1. macOS / zsh — `sleep 12; ls`, blur window, notification arrives (`exit 0 · 12s · <label>`).
2. macOS / bash — same flow.
3. Linux / fish — same flow.
4. Windows / pwsh — same flow.
5. Disable shell-integration toggle, restart session, no notification.
6. Threshold = 60, run 30s command → no notification; 70s → notification.
7. Window focused while command runs → no notification (focus gate).
8. zsh with oh-my-zsh + powerlevel10k — prompt renders normally and notifications fire.

### Not tested

- Shell version compatibility matrix (zsh 5.0 vs 5.9, bash 3.2 vs 5.2). Covered by manual review + smoke list.
- Notification rendering style or sound on the user's OS.
- Custom prompt frameworks beyond the ones listed in smoke item 8. Snippet design relies on additive hook arrays / function wrapping so the failure mode is "no integration" rather than "broken prompt"; we document this in `docs/shell-integration.md`.

## Limitations and known issues

- **Spoofable events.** Any process inside the PTY can write `OSC 133;D;0`. We treat all events as authentic. Matches industry behavior.
- **Fish conf.d file persists across uninstalls.** atterm does not include an uninstaller in MVP; the shared fish snippet remains on disk if the user removes atterm. Documented in `docs/shell-integration.md`.
- **PowerShell injection via `-NoExit -Command`** is per-spawn; we never touch `$PROFILE`. If the user wants integration in non-atterm-spawned PowerShells they must source the snippet manually (snippet content is reachable via Settings → "Show shell-integration snippet" → out of MVP scope, deferred).
- **No event persistence.** Tracker state lives only in memory per pane. Restarting the pane loses pending state. Cross-session history is a future iteration.
- **Remote attach unaware.** Browser / mobile attach sees the OSC bytes but does not notify, by design. Web Push is the future iteration that closes this gap.
- **cmd.exe unsupported.** Falls back to no integration silently.

## Future work (extension points designed in but not implemented now)

- `CommandEvent` is a structured object so a future iteration can route it into a wire-protocol frame (e.g. `TypeCommandEvent = 0x35`) without touching the parser.
- `CommandTracker` records A / B / C / D events; only D currently produces a `CommandEvent`. Adding "prompt-ready" / "running-since" outputs is a tracker-internal change with no parser rewiring.
- `shellintegration.Plan` carries a `Shell` field that is currently only used for logs. It is the natural seam to plug in shell-specific cwd reporting (OSC 7) when we move beyond OSC 133.

## Implementation pointers

- `desktop/notify.go` — existing notification plumbing; reuse `ShowNotification` binding wholesale.
- `desktop/frontend/src/lib/terminalBell.ts` — pattern for the new `commandFinish.ts` (pure helpers, structured event, gating).
- `desktop/frontend/src/components/TerminalView.vue` line ~268 (`term.onBell` hook) — adjacent place to add `term.parser.registerOscHandler(133, ...)`.
- `desktop/config.go` — `AutoCheckUpdates *bool` / `NotificationsEnabled *bool` are the templates for `ShellIntegrationEnabled` and `CommandNotifyThresholdSeconds`.
- `internal/ptyhost/` — confirm `OpenOptions` accepts `Env` and `Args` extensions; if not, extend (small, well-bounded) before doing the wiring.

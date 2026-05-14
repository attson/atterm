# Terminal Bell System Notifications Design

**Date:** 2026-05-14
**Status:** Approved

## Goal

Fire an OS-level system notification (macOS Notification Center / Linux libnotify / Windows balloon) when a terminal pane receives a BEL (`\x07`), while the AT Term window is not focused. Used to surface "task done" / "waiting for input" cues from CLI tools like Claude Code and codex without forcing the user to keep the window in view.

## Motivation

Claude Code, codex, and many other interactive CLI tools emit `\x07` (BEL) on task completion and when waiting for input. Users running these tools in atterm currently get no out-of-window feedback — they have to keep glancing at the terminal. Native terminals like iTerm2 and Warp surface BEL as system notifications by default; AT Term should match that convention.

## Non-Goals

- Pattern matching specific output strings (e.g., "Task completed", "Waiting for input"). Only BEL is interpreted.
- Per-session opt-out. Single global toggle.
- Notification sound customization or custom icons.
- Click-to-focus a specific pane or tab. macOS will raise the app naturally on click; selecting the right pane is left to the user.
- iOS / mobile notifications.

## Behavior

### Trigger

1. xterm.js fires `term.onBell` when the PTY output contains `\x07`.
2. The `TerminalView.vue` handler runs three gates in order:
   - **Focus gate** — if `document.hasFocus() === true`, ignore. The user is already looking at the window.
   - **Throttle gate** — if the same session bell'd within the last 3000 ms, ignore. Avoids notification storms from zsh autocomplete misfires or tools that bell repeatedly.
   - **Enabled gate** — handled in the backend (see below) so the frontend doesn't need to fetch the preference per bell.
3. If all gates pass, the handler records `lastBellAt[sessionId] = Date.now()` and calls the backend `ShowNotification(title, body)` binding.

### Backend

`ShowNotification(title, body string) error` checks the persisted `notifications_enabled` flag (default `true`). If disabled, returns `nil` silently. Otherwise shells out to the platform-appropriate command, mirroring the pattern in `paste_image.go` and `clipboard_read.go`:

- **darwin:** `osascript -e 'display notification "<body>" with title "<title>"'`. AppleScript strings are escaped via the existing `appleScriptString` helper.
- **linux:** `notify-send <title> <body>`. If `notify-send` is not on `$PATH`, log once per process lifetime and return `nil` — silent failure is preferable to per-bell toast spam.
- **windows:** PowerShell invocation that uses `System.Windows.Forms.NotifyIcon.ShowBalloonTip`. This works without any external module (BurntToast is not required). Args are passed through `powerShellSingleQuotedString` (existing helper) to prevent injection.

All platform handlers run with a 5-second context timeout so a hung notification daemon cannot pile up goroutines.

### Notification content

- **Title:** `"AT Term"`.
- **Body:** `"Bell in {sessionLabel}"`, where the frontend computes `sessionLabel = basename(cwd) || command || "session"` and passes it as a new optional prop `sessionLabel?: string` on `TerminalView`. If `sessionLabel` is empty, the body falls back to `"Bell"`.

## Components

### Backend

- **`desktop/notify.go` (new)** — `ShowNotification` binding, platform helpers, log-once-per-missing-tool registry. Exports `darwinNotifySpec` / `linuxNotifySpec` / `windowsNotifySpec` returning the `commandSpec` struct (already defined in `paste_image.go`) so tests can verify arg construction without shelling out.
- **`desktop/notify_test.go` (new)** — table tests for the three platform spec functions plus a test that `ShowNotification` returns silently when the enabled getter returns `false`.
- **`desktop/config.go`** — add `NotificationsEnabled *bool` field with `json:"notifications_enabled,omitempty"` tag and `NotificationsEnabledOrDefault() bool` method returning `true` when `nil`. Same pointer-or-default pattern as `AutoCheckUpdates`.
- **`desktop/config_notify_test.go` (new)** — asserts default is `true` and that round-tripping `false` returns `false`.
- **`desktop/app.go`** — three new Wails bindings:
  - `GetNotificationsEnabled() bool`
  - `SetNotificationsEnabled(enabled bool) error`
  - `ShowNotification(title, body string) error` — wires through to `notify.go` using `a.cfgStore.Get().NotificationsEnabledOrDefault()` as the enabled getter.

### Frontend

- **`desktop/frontend/src/lib/api.ts`** — three new wrapper functions and `AppBindings` entries.
- **`desktop/frontend/src/lib/terminalBell.ts` (new)** — pure helpers, easy to unit-test:
  - `shouldNotify(now: number, lastBellAt: number, focused: boolean, throttleMs?: number): boolean` — gates the focus + throttle decision. Default `throttleMs = 3000`.
  - `extractSessionLabel(info: SessionInfo | null): string` — returns `basename(cwd) || command basename || ""`.
- **`desktop/frontend/src/lib/terminalBell.test.ts` (new)** — table tests for `shouldNotify` (focused → false; unfocused + first bell → true; within throttle → false; after throttle → true) and `extractSessionLabel` (cwd present, only command, both empty).
- **`desktop/frontend/src/components/TerminalView.vue`** — add `sessionLabel?: string` prop. After `term = new Terminal(...)`, subscribe to `term.onBell` with a handler that:
  - Calls `shouldNotify(Date.now(), lastBellAt, document.hasFocus())`.
  - On true, updates `lastBellAt = Date.now()` and calls `showNotification("AT Term", \`Bell in ${props.sessionLabel || "session"}\`)`.
  - The `lastBellAt` is component-instance-scoped (one BEL stream per pane), not global.
- **`desktop/frontend/src/components/PaneGrid.vue`** — forward `sessionLabel` from `sessionInfoFor(pane)` through to `TerminalView`.
- **`desktop/frontend/src/App.vue`** — no new logic; the existing `paneSessionInfo(pane)` already returns the data the helper needs. `PaneGrid` does the extraction inline.
- **`desktop/frontend/src/components/SettingsGeneral.vue`** — add a checkbox below the theme select:
  - Label: `Show system notifications on terminal bell`
  - Hint: `Only fires when the AT Term window is not focused.`
  - Loads via `getNotificationsEnabled()` on mount; saves via `setNotificationsEnabled(value)` on change. Independent from theme save flow.
- **`desktop/frontend/src/components/SettingsGeneral.test.ts`** — extend with assertions for the new checkbox markup, `getNotificationsEnabled` / `setNotificationsEnabled` usage, and the hint text.
- **`desktop/frontend/src/components/TerminalView.test.ts`** — assert the new prop and source-level wiring (`term.onBell(`, `shouldNotify`, `showNotification`).

## Data Flow

```
PTY → conn.onOutput → term.write(\x07) → term.onBell fires
   → TerminalView handler:
        shouldNotify(now, lastBellAt, document.hasFocus())
        ↓ true
        showNotification("AT Term", "Bell in <label>")
   → Wails binding ShowNotification(title, body)
   → cfgStore.Get().NotificationsEnabledOrDefault()
        ↓ true
        runtime.GOOS switch → osascript / notify-send / powershell.exe
```

## Error Handling

- Backend platform shell-outs log warnings on failure but do NOT propagate errors to the caller. A failed notification is not worth interrupting the terminal experience.
- Missing-tool detection on Linux (`exec.LookPath("notify-send")`) logs once per process lifetime using a `sync.Once` so the user is informed if they need to install libnotify but isn't spammed on every bell.
- Frontend doesn't surface notification errors at all. Failures are invisible — the user can verify by sending `printf '\a'` in an unfocused window.

## Testing

**Go (`desktop/notify_test.go`, `desktop/config_notify_test.go`):**

- `TestDarwinNotifySpecEscapesQuotes` — title with double-quote, body with backslash; assert AppleScript escaping.
- `TestLinuxNotifySpec` — assert command name and arg order.
- `TestWindowsNotifySpec` — assert PowerShell script contains the title and body in single-quoted form.
- `TestShowNotificationSkipsWhenDisabled` — inject an enabled getter returning `false`, assert no shell-out is attempted (use a stub runner injected via interface).
- `TestNotificationsEnabledDefaultsToTrue` — fresh `appConfig{}` returns `true`.
- `TestNotificationsEnabledRoundtrip` — set `false`, then `OrDefault()` returns `false`.

**Frontend (`terminalBell.test.ts`, source-level extensions):**

- `shouldNotify` table — focused, first-bell, within-window, after-window.
- `extractSessionLabel` table — cwd, command-only, empty.
- `TerminalView.test.ts` — assert `sessionLabel?: string` prop, `term.onBell(`, `showNotification(` strings present.
- `SettingsGeneral.test.ts` — assert the new checkbox markup, `getNotificationsEnabled` import, `setNotificationsEnabled` call site, hint text.

**Manual verification:**

1. Open atterm on darwin. Settings → General → confirm checkbox is on by default.
2. Switch focus to another app. Type `printf '\a'` in atterm's terminal. A macOS notification appears. Click it; atterm focuses.
3. Focus atterm. Type `printf '\a'` again. No notification (focus gate).
4. Unfocus atterm. Run `for i in 1 2 3; do printf '\a'; sleep 1; done`. Only one notification per 3s window.
5. Toggle the setting off. Bell unfocused → no notification.
6. Repeat 2-4 on linux with libnotify installed.

## Risks

- **macOS Notification Center grant flow.** First osascript-driven notification prompts the user to grant `Script Editor` permission in System Settings → Notifications. We accept this; iTerm2 and similar apps have the same first-run flow. We do not attempt to use a separate signed helper to avoid the prompt.
- **`notify-send` not installed.** Common on minimal/server Linux. Failure is silent (logged once); user must `apt install libnotify-bin` or distro equivalent. Surfacing this in the UI would require backend → frontend signalling we do not currently have for non-paste paths.
- **Windows balloon tips are old-style.** They show as transient pop-ups, not modern Action Center toasts. Acceptable for v1; if users complain we can layer on `BurntToast` later.
- **Tab-active vs window-focused.** A user with three tabs open and focused on tab 1 will see notifications from tabs 2 and 3 only if the *window* loses focus. Within-window switching does not notify. We considered per-pane focus suppression but decided window-level is the right default — a user inside the atterm window typically sees the visual bell flash already.

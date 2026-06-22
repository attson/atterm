# Desktop logging: structured format, levels, colorized viewer + PTY input debug

Date: 2026-06-22
Status: Design (pending implementation)

## Background & motivation

A user reported that AI sessions running **inside the atterm desktop app** occasionally
stop mid-action ("call" shown, turn ends after `Brewed for 6m 9s") — a symptom that does
not reproduce in other terminals. The leading hypothesis is that a stray/lone `ESC`
(`0x1b`) byte reaches the child TUI's PTY, which the TUI interprets as an "ESC keypress"
and uses to cancel its in-progress action. This can happen if an escape sequence (e.g. the
focus-report `\x1b[O`) is split such that the child reads the leading `ESC` in isolation
and its ESC-disambiguation timer fires.

To diagnose this (and future input issues) without ad-hoc env-var hacks, we want a
**user-toggleable PTY input debug log**. While adding it, the user also asked to improve
the desktop logging output overall: a **consistent line format with levels and a line
"title"/tag**, kept as **plain text in the log file**, and **colorized when rendered in the
settings log viewers**, with **log-level filtering**.

A temporary instrumentation was added to `internal/ptyhost/ptyhost.go` (env var
`ATTERM_PTY_DEBUG`) during investigation. This design **reverts that** and replaces it with
the proper, config-driven mechanism described below.

## Goals

1. A single, parseable, plain-text log line format for all desktop logging:
   `TIMESTAMP LEVEL [tag] message`.
2. A leveled logging API (DEBUG/INFO/WARN/ERROR) with a per-line tag, routed through the
   existing `loggingManager` (so file rotation, dev-stderr mirroring, and the on/off log
   setting all keep working unchanged).
3. Legacy `log.Printf` output auto-normalized to the standard format so the file is
   uniformly formatted (no big-bang migration of every call site).
4. A config-toggled **PTY input debug log** that records every byte slice written into the
   child PTY as hex, tagged `[pty-input]` at DEBUG level, with `LONE-ESC` / `ESC-LEAD`
   markers — written into the **same** desktop log file.
5. The existing settings log viewers render the plain-text log **colorized by level**, with
   a **level-threshold filter**.

## Non-goals (YAGNI)

- Logging PTY *output* (shell → screen) bytes. Input only.
- A separate debug log file. Reuse the existing `desktop.log`.
- Writing ANSI color codes into the log file.
- Migrating every existing `log.Printf` call site to precise levels/tags (legacy lines
  render as `INFO [app]`; precise tagging is an incremental follow-up).
- Log search, pagination, or full-text indexing in the viewer.

## Log line format

Plain text written to the file (one logical line per record):

```
2026/06/22 15:04:05.123 DEBUG [pty-input] write n=1 hex=1b LONE-ESC
2026/06/22 15:04:05.140 INFO  [app] desktop-shell-integration: enabled session=...
2026/06/22 15:04:05.200 WARN  [relay] client: inbound full, dropping frame
```

- **Timestamp**: `2006/01/02 15:04:05.000` (millisecond precision — needed to see the gap
  between a split `ESC` and its continuation bytes).
- **Level**: one of `DEBUG`, `INFO`, `WARN`, `ERROR`, left-padded to a fixed width (5) for
  column alignment.
- **Tag** (the "title"): a short component name in square brackets, e.g. `[pty-input]`,
  `[app]`, `[relay]`.
- **Message**: free text.

Parsing regex (frontend): `^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}\.\d{3}) (DEBUG|INFO|WARN|ERROR)\s+\[([^\]]+)\] (.*)$`.
Lines that do not match (e.g. multi-line stack-trace continuations) render verbatim at no
level.

## Architecture

### Component A — Go logging core (`desktop/logging.go`)

`loggingManager` already implements `io.Writer` and is installed via `log.SetOutput(m)`, so
**every `log.Printf` call funnels through it**. We make the manager own formatting:

1. In `newLoggingManager`, call `log.SetFlags(0)` (and set the manager's own
   `m.logger` flags to 0) so the stdlib no longer prepends its own timestamp — the manager
   adds the standard prefix itself.
2. Add `func (m *loggingManager) Emit(level, tag, msg string)` — formats
   `timestamp LEVEL [tag] msg\n` and writes to `currentWriter` under `mu`.
3. The existing `Write([]byte)` path (which now receives the raw message for legacy
   `log.Printf` callers) wraps each chunk as `timestamp INFO  [app] <raw>` so legacy lines
   are uniformly formatted. (One `log.Printf` call = one `Write` = one line.)
4. Timestamp source: `time.Now()` formatted with the layout above.

A package-level handle to the active manager is stored in `main` (set where
`newDesktopLoggingManager` is constructed in `desktop/main.go`) so the leveled helpers can
reach it, mirroring the stdlib global-logger model.

Leveled helpers (desktop package):

```go
func logDebug(tag, format string, args ...any) // -> Emit("DEBUG", tag, fmt.Sprintf(...))
func logInfo(tag, format string, args ...any)
func logWarn(tag, format string, args ...any)
func logError(tag, format string, args ...any)
```

If the manager handle is nil (e.g. early init / tests), helpers fall back to stdlib
`log.Printf` so nothing panics.

### Component B — PTY input debug (`desktop/`)

1. **Revert** the `ATTERM_PTY_DEBUG` instrumentation added to
   `internal/ptyhost/ptyhost.go`, restoring its clean, side-effect-free `Write`.
2. Config (in `desktop/config.go`, following the existing `*bool` + `...OrDefault()`
   pattern used by e.g. `NotificationsEnabled`):
   - field `PtyInputDebugEnabled *bool` (JSON `ptyInputDebugEnabled,omitempty`)
   - accessor `PtyInputDebugEnabledOrDefault() bool` → **default false**
3. Wails methods (in `desktop/app.go`, mirroring `Get/SetNotificationsEnabled`):
   - `GetPtyInputDebugEnabled() bool`
   - `SetPtyInputDebugEnabled(enabled bool) error` (snapshot `cfgStore.Get()`, set field,
     `cfgStore.Set`). Reads are live, so the toggle takes effect without restart.
4. `desktopPtyHost` (in `desktop/paste_image.go`, embeds `*ptyhost.Host`) gains a reference
   to the config store and a `Write([]byte)` override:
   - construction at `desktop/relay_host.go:453` passes `h.cfg` into the struct.
   - `Write(p)`: if `cfg.Get().PtyInputDebugEnabledOrDefault()`, call
     `logDebug("pty-input", "write n=%d hex=%s%s", len(p), hex.EncodeToString(p), tag)`
     where `tag` is ` LONE-ESC` when `p == [0x1b]`, ` ESC-LEAD` when `p[0] == 0x1b`, else
     empty. Then `return h.Host.Write(p)`.
   - Because `AdoptSession` receives the `*desktopPtyHost` and `internal/relay/adopt.go`
     calls `Write` on it, this intercepts all session input.

### Component C — Frontend colorized rendering + level filter

Existing log displays both render raw `preview.content` inside a `<pre>`:
- `desktop/frontend/src/components/SettingsLogging.vue` — live tail (3 s refresh), line 156.
- `desktop/frontend/src/components/LogViewerDialog.vue` — full-screen dialog, line 53.

Changes:
1. New util `desktop/frontend/src/lib/parseLogLine.ts`:
   - `parseLogLine(line: string): { ts, level, tag, msg } | { raw: string }` using the regex
     above.
   - `LEVEL_ORDER = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 }` for threshold filtering.
2. New shared presentational component
   `desktop/frontend/src/components/LogLines.vue`:
   - props: `content: string`, `minLevel: 'DEBUG' | 'INFO' | 'WARN' | 'ERROR'` (default
     `DEBUG`).
   - splits `content` into lines, parses each, filters by `minLevel` (unparseable lines
     always shown so context/stack traces aren't lost), renders each line with a
     level CSS class and a styled tag span.
   - colors via CSS classes keyed to theme vars: DEBUG dim (`--fg-dim`), INFO default
     (`--fg`), WARN (`--warn`/amber), ERROR (`--bad`); tag in accent (`--accent`).
     (Reuse existing theme vars; add `--warn` if none exists.)
3. `SettingsLogging.vue`: replace the tail `<pre>` with `<LogLines :content :minLevel>` and
   add a small level-threshold `<select>` in the tail header.
4. `LogViewerDialog.vue`: replace the `<pre>` with `<LogLines>` and add a level-threshold
   `<select>` in the dialog toolbar.
5. i18n: add `settings.logging.levelFilter` (+ level labels if needed) to
   `desktop/frontend/src/i18n/messages/en.ts` and `zh-CN.ts`. Add the PTY-debug toggle
   label under `settings.logging` too (see Component B UI).

### PTY input debug toggle UI

The toggle lives in `SettingsLogging.vue` (alongside "write logs to file"), since the debug
output goes to the same log file shown there:
- reactive `ptyInputDebug` ref, loaded via `getPtyInputDebugEnabled()` on mount.
- a checkbox bound to an `onTogglePtyInputDebug` handler calling
  `setPtyInputDebugEnabled(checked)` with error rollback (mirroring existing toggles).
- label (zh) "记录终端输入字节（排查输入丢失/卡死）", (en) "Log terminal input bytes
  (diagnose stuck/dropped input)".
- api.ts shims `getPtyInputDebugEnabled` / `setPtyInputDebugEnabled` + `AppBindings` entries.

## Data flow

```
keypress / focus / paste
  → xterm onData → IN frame → relay → desktopPtyHost.Write(p)
       ├─ if PtyInputDebugEnabled: logDebug("pty-input", hex(p)+tag)
       │      → loggingManager.Emit("DEBUG","pty-input",...) → desktop.log (plain)
       └─ ptyhost.Host.Write(p) → child PTY

Settings log viewer
  → GetLogPreview() (readLogPreview, last 256 KB) → content string
  → LogLines.vue: parseLogLine per line + minLevel filter + CSS colorize
```

## Error handling

- Logging helpers no-op-safely if the manager handle is nil (fall back to `log.Printf`).
- Config Set failures surface to the UI toggle which rolls back its optimistic state
  (existing pattern).
- `parseLogLine` never throws; non-matching lines fall through to verbatim rendering.
- PTY debug logging must never block or fail the actual `Host.Write` — logging happens
  before the write and its result is ignored.

## Testing

- Go: `loggingManager.Emit` format + legacy `Write` normalization (table test);
  `PtyInputDebugEnabledOrDefault` default; `desktopPtyHost.Write` emits a DEBUG line with
  `LONE-ESC`/`ESC-LEAD` only when enabled, and always forwards bytes.
- Frontend: `parseLogLine` unit tests (matching line, unparseable line, each level);
  `LogLines.vue` filters by `minLevel` and shows unparseable lines; `SettingsLogging` /
  `LogViewerDialog` render `LogLines` (extend existing source-assertion tests).

## Delivery slices

- **Slice A — logging core + viewer**: Component A (format/levels/Emit/helpers) + Component
  C (parseLogLine, LogLines, both viewers, level filter, i18n).
- **Slice B — PTY input debug**: revert env-var code + Component B (config, Wails, toggle,
  `desktopPtyHost.Write`). Depends on A's `logDebug` helper.

## Open defaults (chosen)

1. Level filter present in **both** the tail and the full-screen viewer (threshold dropdown:
   All/DEBUG+, INFO+, WARN+, ERROR only).
2. Millisecond timestamps.
3. Legacy lines tagged `[app]` at `INFO`.

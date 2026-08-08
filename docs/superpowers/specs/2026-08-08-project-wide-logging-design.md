# Project-wide logging: shared core, accurate levels, frontend logs on disk

Date: 2026-08-08
Status: Implemented

## Background

`docs/superpowers/specs/2026-06-22-desktop-logging-formatting-design.md` built the desktop
log format (`TS LEVEL [tag] message`), the rotating file sink, and the colorized viewer —
but explicitly deferred the rest:

> Non-goal: Migrating every existing `log.Printf` call site to precise levels/tags (legacy
> lines render as `INFO [app]`; precise tagging is an incremental follow-up).

This is that follow-up, widened to the whole repo. Current state as measured:

| Area | bare `log.Printf` | notes |
|---|---|---|
| `desktop/*.go` (package main) | 135 | all render as `INFO [app]` |
| `desktop/feishu` | 37 | separate package |
| `desktop/shellintegration` | 17 | separate package |
| `internal/relay` | 40 | |
| `internal/webpush` / `internal/feishu` | 6 / 6 | |
| `internal/session` / `internal/safekeyring` | 1 / 1 | |
| `cmd/atterm-relay` | 35 | |
| **total** | **278** | |

Plus `desktop/frontend/src` has 63 `console.*` calls (49 `warn`, 11 `error`, 3 `info`,
2 `log`) that never reach the log file at all — including the red-line-#19 `[boot] step
failed` diagnostics, which are precisely what a user needs when the app fails to start.

`logWarn` / `logError` were specified in the 2026-06-22 design but never implemented; only
`logDebug` / `logInfo` exist, with 3 business call sites.

Net effect: the log file is a single undifferentiated `INFO [app]` stream, and the viewer's
level/tag filters are decorative.

## Goals

1. One shared logging core usable by `desktop/`, `internal/*` and `cmd/atterm-relay`,
   respecting the dependency direction `desktop → internal` (red line #5).
2. Every log call site carries an accurate level and a subsystem tag.
3. A configurable write threshold so DEBUG-level per-frame/per-keystroke logging exists in
   the code but does not flood the rotating file in normal use.
4. Frontend (`desktop/frontend/src`) logs land in the same `desktop.log`, with graceful
   degradation to console-only on Capacitor/iOS and in the browser.
5. Regression guards so new code cannot silently reintroduce bare `log.Printf` /
   `console.*`.

## Non-goals (YAGNI)

- No logrus/zap/slog. `conventions.md` §111/§188 says standard library; `internal/logging`
  is ~150 lines, not a framework.
- No JSON/structured event log. Plain text stays; the viewer already parses it.
- No log upload / remote shipping / crash reporting.
- No `web/src` changes — it has zero `console.*` and no local file to write to.
- No change to the on-disk line format, rotation policy, or `parseLogLine.ts`.

## Architecture

### A. `internal/logging` — the single source of truth for format and level

```go
type Level int8 // LevelDebug < LevelInfo < LevelWarn < LevelError

func ParseLevel(s string) (Level, bool)
func (l Level) String() string

func SetSink(w io.Writer)  // receives fully-formatted lines; default os.Stderr
func SetLevel(l Level)     // atomic, hot-applied; default LevelInfo
func CurrentLevel() Level  // current threshold
func Enabled(l Level) bool

func Debug(tag, format string, args ...any)
func Info(tag, format string, args ...any)
func Warn(tag, format string, args ...any)
func Error(tag, format string, args ...any)

func EmitAt(t time.Time, l Level, tag, msg string) // frontend records keep their own ts
func EmitForced(l Level, tag, msg string)          // bypasses the threshold

func FormatLine(t time.Time, l Level, tag, msg string) string
```

The threshold check happens **first** in every helper, before `fmt.Sprintf`, so
below-threshold calls on hot paths (per-frame, per-keystroke) allocate nothing.

Sink and level live in package-level `atomic.Pointer` / `atomic.Int32` — no mutex on the
hot path, and safe for the relay's hot-reloadable debug flag (red line #26).

Format is byte-identical to today's `formatLogLine`, so `parseLogLine.ts` and
`LogLines.vue` need no change:

```
2026/08/08 15:04:05.123 DEBUG [pty-input] write n=1 hex=1b LONE-ESC
2026/08/08 15:04:05.140 INFO  [uplink] connected, sent ANNOUNCE (3 session(s))
2026/08/08 15:04:05.200 WARN  [uplink] out chan full; dropping command event session=…
```

### B. `desktop/logging.go` becomes a wiring layer

`loggingManager` keeps only what is genuinely its own: **rotation, dev stderr mirroring,
and the on/off + path config**.

- New `m.rawWriter() io.Writer` — writes pre-formatted lines straight to `currentWriter`
  without re-wrapping. `newLoggingManager` calls `logging.SetSink(m.rawWriter())`.
- The existing `m.Write([]byte)` (the `log.SetOutput(m)` leg) **stays** as a safety net: any
  stray `log.Printf` — including from third-party deps like Wails — still gets normalized
  to `INFO [app]`. It is no longer the main path.
- `logDebug/logInfo` stay as thin aliases; **`logWarn`/`logError` are added**.
- `desktop/feishu` and `desktop/shellintegration` are separate packages and call
  `internal/logging` directly.

### C. Write threshold

- `appConfig.LogLevel string` (`json:"log_level,omitempty"`) + `LogLevelOrDefault()`
  returning `"INFO"` when empty or unparseable.
- `LoggingConfig` binding gains `Level string`; `SetLoggingConfig` validates it and calls
  `logging.SetLevel` for hot effect; `newDesktopLoggingManager` applies it at startup.
  An **empty** `Level` means "leave the stored value alone", so the callers that only
  change the path or the on/off switch cannot silently reset a level the user picked.
- `SettingsLogging.vue` gains a **write-level** dropdown, distinct from the existing
  **view-filter** dropdown in the viewer. i18n copy must make the difference explicit:
  write-level = "记录到文件的最低级别", view-filter = "查看时过滤".
- **`PtyInputDebugEnabled` uses `EmitForced`.** Otherwise a user ticks "记录终端输入字节"
  and sees nothing because the threshold is INFO. The toggle *is* the gate for that one
  tag.

### D. `cmd/atterm-relay`

`cmd/atterm-relay/logging.go` calls `logging.SetSink(os.Stderr)` and resolves the level
from the new `--log-level` flag / `ATTERM_RELAY_LOG_LEVEL` (default INFO), with
`--debug` / `ATTERM_RELAY_DEBUG` forcing DEBUG since that is the switch operators
actually reach for. It also does `log.SetFlags(0)` +
`log.SetOutput(logging.StdlibWriter(...))` so stray stdlib lines match.

`internal/relay`'s `s.debugOn()` / `s.debugPayloadOn()` gating **stays** — it also controls
whether payload bytes are included, and it is hot-reloaded from `AdminConfig` (red line
#26). Its `log.Printf` bodies become `logging.EmitForced(LevelDebug, "relay-debug", …)`:
`debugOn()` is already the gate, so making the output *also* depend on the process level
would mean flipping "debug" on in the admin UI and seeing nothing. `Config.DebugLog` is
deleted — it existed only so tests could capture output, and they now swap the shared
sink instead.

`internal/session`'s `ATTERM_DEBUG_SILENCE=1` trace follows the same rule, for the same
reason. Those three (pty-input, relay debug, silence) are the only `EmitForced` callers.

Docker log lines change shape to the unified format. That is intended and is the only
user-visible relay change.

### E. Frontend `desktop/frontend/src/lib/log.ts`

```ts
logDebug(tag, msg, fields?) / logInfo / logWarn / logError
type LogFields = Record<string, string | number | boolean | null | undefined>
```

- **Buffering**: in-memory array; flush when 64 records accumulate, on a 1 s timer, and
  immediately on ERROR. Forced flush on `beforeunload` and `visibilitychange: hidden`.
  Hard cap 512 records — oldest dropped, and a `dropped N record(s)` line is appended so
  the loss is visible.
- **Sink**: Wails binding `AppendFrontendLogs(records)` → Go loops
  `logging.EmitAt(ts, level, "ui-"+tag, msg)`. The `ui-` prefix is added **on the Go side**
  so frontend tags can never collide with Go tags.
- **Degradation**: if `window.go?.main?.App?.AppendFrontendLogs` is missing (Capacitor/iOS,
  browser, unit tests) the module silently stays console-only. Never throws.
- **Dev**: console output is kept alongside file output when `import.meta.env.DEV`.
- **Redaction** (red line #21): `fields` accepts only primitives — no arbitrary object
  serialization — and any key matching `/pass|token|key|secret|cred/i` has its value
  replaced with `***`. `account_key`, passwords and wrap intermediates therefore cannot
  reach the file even by accident.

## Level semantics (the "accurate levels" rule)

Applied uniformly across Go and the frontend:

- **ERROR** — an operation the user asked for failed, the failure is user-visible, and
  nothing retries it. Startup fatals, keychain write failure, update signature
  verification failure, config save failure, E2EE seal failure that falls back to
  plaintext.
- **WARN** — failed but degraded/retried automatically, or an anomaly that does not break
  the main flow. Uplink reconnect, Feishu card patch retry, `notify-send` missing, dropped
  frames on a full channel, discarded malformed recovery snapshot, permission-denied
  inbound frames.
- **INFO** — lifecycle and state transitions, low-frequency, each line meaningful to a
  human reading the file. Uplink connected, session created, shell integration enabled,
  Feishu mode reconciled, recovery resume injected, updater found a new version.
- **DEBUG** — per-frame, per-byte, per-poll internals. `inbound_recv`/`inbound_forward_ok`,
  `stream_out_progress`, repaint nudge steps, `pty-input` hex, AI sid resolve intermediate
  steps, relay client/agent frame tracing.

**Tag vocabulary** (lowercase kebab, one per subsystem):
`app` · `config` · `boot` · `uplink` · `uplink-stream` · `relay` · `relay-client` ·
`relay-agent` · `relay-uplink` · `relay-admin` · `relay-adopt` · `relay-config` ·
`relay-feishu` · `relay-debug` · `relay-host` · `session` · `silence` · `pty` ·
`pty-input` · `repaint` · `recovery` · `ai-sid` · `feishu` · `feishu-anchor` ·
`feishu-form` · `feishu-hook` · `feishu-card` · `feishu-turn` · `shell-integration` ·
`paste` · `updater` · `notify` · `keychain` · `e2ee` · `prefs` · `plugin` · `webpush` ·
`opaque` · `hookinstall` · `remote-proxy` · `migrate` · `bootstrap`.
Frontend tags get a `ui-` prefix added by Go: `ui-boot`, `ui-term`, `ui-conn`, `ui-fs`,
`ui-paste`, `ui-recovery`, `ui-plugin`, `ui-settings`, `ui-capacitor`.

## Data flow

```
Go call site                     logging.Warn("uplink", …)
  → threshold check (atomic)     ↓ below threshold: return, zero alloc
  → FormatLine                   ↓
  → sink (atomic.Pointer)        → desktop: loggingManager.rawWriter()
                                       → rotatingFileWriter (+ stderr in dev)
                                 → relay:   os.Stderr → docker logs

Frontend call site               logWarn("term", …)
  → buffer (≤512, flush @64/1s/ERROR)
  → AppendFrontendLogs(records)  → logging.EmitAt(ts, lvl, "ui-"+tag, msg)
                                 → same sink as above
  → (no binding) console only

Viewer                           GetLogPreview() → last 256 KB
  → parseLogLine + LogLines.vue  → colorize + view-level filter
```

## Error handling

- `logging` never panics and never returns an error: nil sink → `io.Discard`; a failing
  sink write is dropped silently (logging must not become a failure source).
- `AppendFrontendLogs` validates each record (known level, non-empty tag, tag length ≤ 32,
  msg truncated at 4 KB, batch capped at 512) and skips bad ones rather than failing the
  call — a broken log record must never break the UI.
- Frontend `flush()` catches binding rejection, keeps the records in the buffer for one
  retry, then drops them and counts the drop.
- Invalid `log_level` in config falls back to INFO instead of refusing to start.

## Testing

**Go**
- `internal/logging`: format golden test per level; threshold early-return (a sink that
  records writes sees nothing below threshold); `EmitForced` bypasses; `EmitAt` uses the
  supplied timestamp; `ParseLevel` round-trip and garbage input; concurrent
  `SetLevel`/`Debug` under `-race`.
- `desktop`: `rawWriter` does not double-wrap a pre-formatted line while `Write` still
  wraps a legacy one; `LogLevelOrDefault` defaults and rejects garbage;
  `SetLoggingConfig` applies the level and rolls back on config-save failure;
  `AppendFrontendLogs` prefixes `ui-`, filters invalid records, truncates long messages.
- **Regression guard**: `desktop/logging_no_stdlib_test.go` walks the repo AST and fails if
  any non-test file outside the allowed set (`desktop/logging.go` fallback,
  `internal/logging` itself) calls `log.Printf`/`log.Println`/`log.Print`.

**Frontend**
- `lib/log.test.ts`: buffers until 64 / flushes on timer / flushes immediately on ERROR /
  degrades to console when the binding is absent / redacts secret-ish field keys / drops
  oldest past 512 and reports the drop count / never throws when the binding rejects.
- **Regression guard**: `lib/noConsole.test.ts` walks `src/` and fails on any `console.*`
  outside `lib/log.ts`. Implemented as a test rather than an ESLint rule because the
  project has no ESLint and adding it for one rule is not worth the dependency.

## Delivery slices

- **A** — `internal/logging` package + tests.
- **A2** — desktop wiring: `rawWriter`, `logWarn`/`logError`, `LogLevel` config,
  `LoggingConfig.Level` binding, `SettingsLogging.vue` write-level dropdown, i18n.
- **B** — migrate the 189 desktop Go call sites (`desktop/*.go`, `desktop/feishu`,
  `desktop/shellintegration`) + the AST regression guard.
- **C** — migrate `internal/*` (54) and `cmd/atterm-relay` (35).
- **D** — frontend `lib/log.ts` + `AppendFrontendLogs` binding + 63 `console.*` call sites
  + the `no-console` regression test.

Each slice compiles and passes tests on its own; B/C/D are independent of each other once
A/A2 land.

## Post-implementation notes

Counts as landed: 188 Go call sites (the 189th, `pty_host.go`, was already on the leveled
helpers) and 63 frontend call sites. The level chosen for each one is visible in the code;
the rules behind those choices live in the level-semantics section above and in
`docs/spec/conventions.md` §日志.

Deviations from the design above, all noted inline: relay debug uses `EmitForced` instead
of pushing the process level; `Config.DebugLog` was deleted rather than kept; the frontend
`no-console` guard is a test, not an ESLint rule.

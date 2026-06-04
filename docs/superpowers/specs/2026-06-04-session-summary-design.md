# Session task summary (design)

Date: 2026-06-04
Status: Draft (design phase); pending implementation plan
Roadmap item: P2.12

## 1. Goal

When an OSC 133 `D` event closes a command, capture a small, machine-
and human-friendly summary of what just ran — the ANSI-stripped tail
of the output plus the extracted error lines — and ride it through
the existing `SessionInfo` / `MetaPayload` channel so task cards on
mobile and the web admin can show "this command failed because:
<one-line error>" without the user having to attach a terminal.

Also: a leftover gap from P2.11 is fixed in passing — the `type`
field was added to `SessionInfo` but never to `MetaPayload`, so
real-time changes (e.g. shell → ai when the user starts `claude`)
don't reach already-connected subscribers until they refresh the
list. P2.12 adds both `Type` and `Summary` to `MetaPayload` together.

After this lands:

- Every `D` event triggers a single `computeSummary(...)` pass on the
  session's existing scrollback ring buffer.
- The summary is stored on the session and broadcast on the next
  META frame; older clients ignore the field.
- Mobile `MobileSessionList` and web `SessionList` show the first
  extracted error line under failed task cards, in monospace red.
- Successful and running sessions are unchanged visually — the card
  list stays calm.

Out of scope:

- Storing per-command history (only the most recent command's
  summary is kept).
- Surface in desktop tab bar or terminal view (the terminal itself
  already shows the output; the chip would be redundant).
- Click-to-open "session detail" view — that's a future enhancement;
  for now the summary lives inline on the card.
- AI-based error summarisation; the extraction is regex heuristics.
- Persistence across relay restarts; summaries live in process
  memory and reset along with sessions.

## 2. Architecture

```
┌── relay (Go) ──────────────────────────────────────────────────────────┐
│                                                                         │
│  internal/session/                                                      │
│    ansistrip.go (new)                                                   │
│      func StripANSI(b []byte) []byte                                    │
│                                                                         │
│    summary.go (new)                                                     │
│      const summaryTailBytes  = 8 * 1024                                 │
│      const summaryLineLimit  = 32                                       │
│      const summaryErrorLimit = 5                                        │
│      const summaryOutputBytes = 4 * 1024                                │
│      func computeSummary(buf *ringbuf.Buffer, now time.Time,            │
│                          isFailure bool) *proto.SessionSummary          │
│      func extractErrorLines(lines []string, limit int) []string         │
│                                                                         │
│    session.go (modified)                                                │
│      applyOSC133Locked 'D' branch:                                      │
│        s.meta.Summary = computeSummary(s.scroll, now,                   │
│                                        exitCode != 0)                   │
│                                                                         │
│  internal/ringbuf/                                                      │
│    ringbuf.go (modified)                                                │
│      func (b *Buffer) TailBytes(n int) []byte                           │
│                                                                         │
│  internal/proto/frame.go (modified)                                     │
│    type SessionSummary struct { ... }                                   │
│    SessionInfo.Summary *SessionSummary                                  │
│    MetaPayload.Summary *SessionSummary                                  │
│    MetaPayload.Type    string                       (P2.11 fix)         │
│                                                                         │
└────────────────────────────────────────────────────────────────────────┘
                            │
                            ▼
   SessionInfo / MetaPayload JSON → desktop / mobile / web frontends
                            │
                            ▼
┌── frontends ──────────────────────────────────────────────────────────┐
│  TypeScript: SessionSummary { recent_output?, error_lines?, captured_at? }
│  RemoteSession.summary?, SessionInfo.summary? added on all three      │
│                                                                       │
│  Renderers (only changes are conditional renders under failed cards): │
│    MobileSessionList.vue — .err-line below the .meta row              │
│    web/SessionList.vue   — .err-line mirroring mobile                 │
│                                                                       │
│  No new i18n keys (error lines render as raw text)                    │
└───────────────────────────────────────────────────────────────────────┘
```

## 3. Data model

### 3.1 Go types (`internal/proto/frame.go`)

```go
// SessionSummary is the post-D snapshot of a command's tail output
// and extracted error lines. RecentOutput is ANSI-stripped UTF-8 text;
// ErrorLines is non-empty only when the captured-at-time exit code
// was non-zero. Nil before the first D event, overwritten on each
// subsequent D.
type SessionSummary struct {
    RecentOutput string   `json:"recent_output,omitempty"`
    ErrorLines   []string `json:"error_lines,omitempty"`
    CapturedAt   int64    `json:"captured_at,omitempty"`
}

type SessionInfo struct {
    // ...existing fields including Type...
    Summary *SessionSummary `json:"summary,omitempty"`
}

type MetaPayload struct {
    // ...existing fields...
    Type    string          `json:"type,omitempty"`     // P2.11 carry-over
    Summary *SessionSummary `json:"summary,omitempty"`
}
```

Pointer (`*SessionSummary`) so JSON marshals to either `null`-absent
(`omitempty`) or a populated object; clients distinguish "never had a
summary" from "had a summary with empty fields".

### 3.2 TypeScript types

```ts
// desktop/frontend/src/lib/connection.ts and matching shapes in
// platform/types.ts (RemoteSession) and web/src/shared/api/types.ts
export interface SessionSummary {
  recent_output?: string
  error_lines?: string[]
  captured_at?: number
}

// On the existing SessionInfo / RemoteSession interfaces:
//   summary?: SessionSummary
```

## 4. Capture algorithm

When `applyOSC133Locked` handles a `D` event:

1. Compute `exitCode` (existing logic).
2. Set `s.meta.CommandEndedAt`, `s.meta.CommandDurationMS`,
   `s.meta.CommandExitCode`, `s.meta.TaskState` (existing).
3. Call `s.meta.Summary = computeSummary(s.scroll, now, exitCode != 0)`.
4. Mark `changed = true`.

`computeSummary`:

```go
func computeSummary(buf *ringbuf.Buffer, now time.Time, isFailure bool) *proto.SessionSummary {
    raw := buf.TailBytes(summaryTailBytes)            // last ≤8 KB of bytes
    clean := StripANSI(raw)                            // remove CSI/OSC/ESC X seqs
    lines := splitLastN(clean, summaryLineLimit)       // up to last 32 lines
    output := joinTruncated(lines, summaryOutputBytes) // <= 4 KB UTF-8

    s := &proto.SessionSummary{
        RecentOutput: output,
        CapturedAt:   now.Unix(),
    }
    if isFailure {
        s.ErrorLines = extractErrorLines(lines, summaryErrorLimit)
    }
    return s
}
```

`splitLastN(b []byte, n int) []string`:

- Splits on `\n`, drops trailing empty entry from the trailing newline,
  returns the last `n` non-empty lines (trimmed of trailing `\r`).

`joinTruncated(lines []string, maxBytes int) string`:

- Joins with `\n`. If the joined length exceeds `maxBytes`, drops
  lines from the FRONT (older) until it fits. Older lines being
  dropped is correct — recent output is what users want.

`extractErrorLines(lines []string, limit int) []string`:

- Match each line against (case-insensitive, single pass):
  - `\b(error|errored|failed|fatal|panic|traceback|exception)\b` — word boundary, anywhere in line.
  - `^(error|panic|fatal|warning|warn):\s` — start of trimmed line, followed by colon.
- First match wins; matched lines are appended in order. Stop after
  `limit` lines.
- Lines that are pure whitespace, only ANSI artefacts after strip
  (length 0), or longer than 512 bytes are skipped (long lines are
  binary noise more often than useful errors; the truncation cap
  keeps the JSON payload bounded).
- Returns `nil` if no matches — so the JSON field `omitempty`
  drops out entirely instead of an empty array.

## 5. `ringbuf.Buffer.TailBytes`

```go
// TailBytes returns the last n bytes of the buffer's content as a
// fresh slice. If the buffer has fewer than n bytes, returns
// everything. Returns nil for n <= 0 or empty buffer.
func (b *Buffer) TailBytes(n int) []byte
```

Implementation: walk `b.chunks` from the tail, accumulating chunk
lengths until ≥ n, then build a single fresh byte slice that
includes exactly the last n bytes (cropping the oldest accumulated
chunk if needed). O(chunks) walk, O(n) allocation. Holds the mutex
for the duration. Returned slice is a copy — callers can keep it
across mutations.

## 6. ANSI stripping (`StripANSI`)

Strip the three escape sequence shapes that commonly appear in PTY
streams:

| Shape | Pattern | Action |
|---|---|---|
| CSI | `ESC [ ... [@-~]` | drop entire sequence |
| OSC | `ESC ] ... (BEL\|ESC\\)` | drop entire sequence |
| Other ESC sequences | `ESC <single char>` | drop both bytes |

Everything else (printable, `\t`, `\n`, `\r`) is preserved verbatim.

Implementation is a single linear scan with a small state machine —
no regex (the input can be 8 KB; allocating regex matches per call
adds GC pressure). State enum: `stateText | stateAfterEsc | stateCSI
| stateOSC`. About 30 lines of Go. Handles a truncated sequence at
EOF by treating the remaining input as text (the start of a
sequence whose end was cut off ends up dropped, but no panic).

## 7. Broadcast

`applyOSC133Locked` returns `changed = true` after setting
`s.meta.Summary`, which causes `PushOut`'s existing
`broadcastCurrentMeta` flow to fire. The new `MetaPayload.Summary`
field rides that broadcast. Old clients that don't know the field
ignore it; new clients pick it up.

Same path for `MetaPayload.Type` — the P2.11 carry-over. Every
existing META trigger (driver change, cwd update, OSC 133 state
change) carries the current `Type` now.

## 8. Frontend renderers

Both `MobileSessionList.vue` and `web/src/main/components/SessionList.vue`
get one new conditional block under the existing meta row inside each
task card:

```vue
<span
  v-if="s.task_state === 'failed' && s.summary?.error_lines?.length"
  class="err-line"
  :data-testid="`task-err-${s.session_id}`"
>
  {{ s.summary.error_lines[0] }}
</span>
```

CSS (mobile, web mirrors):

```css
.err-line {
  display: block;
  color: #f87171;                  /* red-400 */
  font-size: 0.72rem;
  font-family: var(--font-mono);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
```

Only the first `error_lines[0]` is shown on the card. Showing more
would crowd the list; the others stay in the data for a future
session-detail view.

## 9. Errors and observability

- `computeSummary` returns a non-nil `*SessionSummary` even when the
  buffer is empty (RecentOutput will be `""`, ErrorLines `nil`,
  CapturedAt = now). The `omitempty` JSON tags drop the empty fields
  on the wire.
- `StripANSI` is total; never panics; preserves input length-or-less.
- No new log lines. No new metrics — summary computation is one pass
  over 8 KB at D-event frequency (rare), so the overhead is invisible.

## 10. Testing

### 10.1 `internal/session/ansistrip_test.go`

Table-driven, ~10 cases:

```go
cases := []struct{ name, in, want string }{
    {"plain", "hello world\n", "hello world\n"},
    {"csi color", "\x1b[31mred\x1b[0m", "red"},
    {"csi cursor", "\x1b[2Jclear", "clear"},
    {"osc bel", "\x1b]0;title\x07hello", "hello"},
    {"osc st", "\x1b]0;title\x1b\\hello", "hello"},
    {"esc x", "\x1b=raw", "raw"},
    {"mixed", "[\x1b[1mbold\x1b[0m] ok\n", "[bold] ok\n"},
    {"preserves crlf", "a\r\nb\r\n", "a\r\nb\r\n"},
    {"truncated csi at eof", "abc\x1b[3", "abc"},
    {"truncated osc at eof", "abc\x1b]title", "abc"},
}
```

### 10.2 `internal/session/summary_test.go`

```go
func TestComputeSummary_FailureExtractsErrorLines(t *testing.T) {
    // Push lines into a real ringbuf.Buffer with seq markers.
    // Expected: ErrorLines contains the FAIL line and the "error:" line.
}

func TestComputeSummary_SuccessNoErrorLines(t *testing.T) {
    // Same data, isFailure=false → ErrorLines is nil.
}

func TestComputeSummary_StripsAnsiInRecentOutput(t *testing.T) {
    // Push "\x1b[31mboom\x1b[0m\n" → RecentOutput contains "boom"
    // and does NOT contain "\x1b".
}

func TestComputeSummary_RespectsByteLimit(t *testing.T) {
    // Push 20 KB → RecentOutput is <= summaryOutputBytes (4 KB) AND
    // ends with the most recent line (oldest dropped).
}

func TestExtractErrorLines_DeduplicationAndOrder(t *testing.T) {
    // Mixed lines; verify match order is by input order, and
    // duplicates are NOT silently deduped (5 'error' lines yield 5
    // entries up to the limit — useful for stack traces).
}

func TestExtractErrorLines_LongLineSkipped(t *testing.T) {
    // A 600-char line containing "error" is skipped to keep payload
    // bounded.
}
```

### 10.3 `internal/session/session_test.go` integration

```go
func TestPushOut_DEventCapturesSummary(t *testing.T) {
    // 1. PushOut OSC 133 C
    // 2. PushOut output bytes including "error: bad thing\n"
    // 3. PushOut OSC 133 D;1
    // Assert s.Info().Summary != nil
    // Assert summary.ErrorLines == ["error: bad thing"]
    // Assert summary.CapturedAt > 0
    // Drain meta broadcasts and confirm one of them carries the
    // summary payload.
}
```

### 10.4 `internal/ringbuf/ringbuf_test.go`

```go
func TestTailBytes(t *testing.T) {
    cases := []struct{
        name string
        chunks [][]byte
        n int
        want []byte
    }{
        {"empty", nil, 10, nil},
        {"smaller than buffer", [][]byte{[]byte("hello world")}, 5, []byte("world")},
        {"larger than buffer", [][]byte{[]byte("abc")}, 10, []byte("abc")},
        {"spans multiple chunks", [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}, 4, []byte("cdef")},
        {"zero n", [][]byte{[]byte("abc")}, 0, nil},
    }
    // ...
}
```

### 10.5 Frontend renderer tests

- `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts`:
  - failed session WITH summary.error_lines → `.err-line` renders, text matches `error_lines[0]`.
  - failed session WITHOUT summary → no `.err-line`.
  - completed session WITH summary → no `.err-line`.
- Web equivalent in whatever vitest harness web uses.

## 11. Migration / rollout

- Single Go ALTER-free change — `Summary` is a pointer field on
  in-memory structs. SQLite isn't involved.
- Old publishers' MetaPayload JSON without `summary` decodes
  cleanly; field stays `nil` and renderers branch correctly.
- Old consumers ignore the new field (`omitempty` + unknown-field
  tolerance in `json.Unmarshal`).
- No feature flag.

## 12. Non-goals revisited

- **No SessionSummary history** — only the most recent command's
  summary is kept per session. A future "command history" feature
  is a separate roadmap line (P4 territory).
- **No structured exit-code → error-line correlation** beyond
  isFailure boolean. Sophisticated parsers (per-test-framework
  output schemas) are a future enhancement.
- **No client-side error extraction fallback** — relay computes,
  clients display. Single source of truth.
- **No re-computation on subscribe** — if a subscriber joins
  mid-command, they'll get the LATEST summary (from the previous
  command) in their snapshot META, then a fresh one on the next D.
  Acceptable for v0.5.

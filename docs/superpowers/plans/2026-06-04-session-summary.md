# Session task summary — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When an OSC 133 `D` event closes a command, capture an ANSI-stripped tail of the output + extracted error lines as a `SessionSummary` on `proto.SessionInfo` + `MetaPayload`, and render the first error line under failed task cards on mobile and web. Also fix the P2.11 carry-over by adding `Type` to `MetaPayload` so real-time type changes broadcast.

**Architecture:** New pure helpers in `internal/session/{ansistrip.go, summary.go}` plus a `TailBytes` accessor on `ringbuf.Buffer`. Capture fires inside `applyOSC133Locked`'s `D` branch and rides the existing `broadcastCurrentMeta` path. Frontends gain a `SessionSummary` TS type and one conditional `<span class="err-line">` per session card.

**Tech Stack:** Go stdlib (no new deps). TypeScript + Vue 3 (no new deps). `go test` + vitest.

**Reference spec:** `docs/superpowers/specs/2026-06-04-session-summary-design.md`

---

## File map

### Backend (Go)
- **Modify:** `internal/ringbuf/ringbuf.go` — add `TailBytes(n int) []byte`.
- **Modify:** `internal/ringbuf/ringbuf_test.go` — table-driven test for the new method.
- **Create:** `internal/session/ansistrip.go` — `StripANSI(b []byte) []byte`.
- **Create:** `internal/session/ansistrip_test.go` — 10 cases covering CSI / OSC / ESC X / truncation.
- **Create:** `internal/session/summary.go` — `computeSummary`, `extractErrorLines`, `splitLastN`, `joinTruncated`, plus the four `summary*` constants.
- **Create:** `internal/session/summary_test.go` — 6 cases for `computeSummary` and `extractErrorLines`.
- **Modify:** `internal/proto/frame.go` — add `SessionSummary` type; `Summary` field on `SessionInfo`; `Summary` and `Type` fields on `MetaPayload`.
- **Modify:** `internal/session/session.go` — wire `computeSummary` into the `D` branch; extend `encodeMetaPayload` with `Type` + `Summary`.
- **Modify:** `internal/session/session_test.go` — one integration test that drives C → output → D and asserts the summary lands on `s.Info()` and in a broadcast META frame.

### Frontend types
- **Modify:** `desktop/frontend/src/lib/connection.ts` — `SessionSummary` interface; `SessionInfo.summary?`.
- **Modify:** `desktop/frontend/src/platform/types.ts` — `RemoteSession.summary?` re-exports the same type.
- **Modify:** `desktop/frontend/src/platform/capacitor.ts` — `listRemoteSessions` map propagates `summary`.
- **Modify:** `web/src/shared/api/types.ts` — mirror `SessionSummary` + `summary?`.

### Renderers
- **Modify:** `desktop/frontend/src/mobile/MobileSessionList.vue` — `.err-line` conditional under failed cards.
- **Modify:** `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts` — three new cases (failed-with-summary / failed-no-summary / completed-with-summary).
- **Modify:** `web/src/main/components/SessionList.vue` — mirror the chip.

No i18n changes (error lines render as raw text). No new dependencies.

---

## Task 1: `ringbuf.Buffer.TailBytes` (test-first)

**Files:**
- Modify: `internal/ringbuf/ringbuf.go`
- Modify: `internal/ringbuf/ringbuf_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/ringbuf/ringbuf_test.go`:

```go
func TestTailBytes(t *testing.T) {
	cases := []struct {
		name   string
		chunks [][]byte
		n      int
		want   []byte
	}{
		{"empty buffer", nil, 10, nil},
		{"zero n", [][]byte{[]byte("abc")}, 0, nil},
		{"negative n", [][]byte{[]byte("abc")}, -1, nil},
		{"smaller than buffer", [][]byte{[]byte("hello world")}, 5, []byte("world")},
		{"larger than buffer", [][]byte{[]byte("abc")}, 10, []byte("abc")},
		{"spans multiple chunks", [][]byte{[]byte("ab"), []byte("cd"), []byte("ef")}, 4, []byte("cdef")},
		{"exact boundary", [][]byte{[]byte("aa"), []byte("bb")}, 2, []byte("bb")},
		{"crosses chunks unaligned", [][]byte{[]byte("foo"), []byte("bar"), []byte("baz")}, 5, []byte("rbaz")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := New(1024)
			for i, data := range tc.chunks {
				b.Push(Chunk{Seq: uint64(i + 1), Data: data})
			}
			got := b.TailBytes(tc.n)
			if string(got) != string(tc.want) {
				t.Fatalf("TailBytes(%d) = %q, want %q", tc.n, got, tc.want)
			}
		})
	}
}

func TestTailBytes_ReturnsCopy(t *testing.T) {
	b := New(1024)
	b.Push(Chunk{Seq: 1, Data: []byte("hello")})
	got := b.TailBytes(5)
	if string(got) != "hello" {
		t.Fatalf("got %q", got)
	}
	// Mutating the returned slice must not affect the buffer.
	got[0] = 'x'
	again := b.TailBytes(5)
	if string(again) != "hello" {
		t.Fatalf("buffer mutated: %q", again)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/ringbuf/ -run TestTailBytes -v`
Expected: FAIL with "undefined: TailBytes" or similar.

- [ ] **Step 3: Implement `TailBytes`**

Append to `internal/ringbuf/ringbuf.go` after `LatestSeq`:

```go
// TailBytes returns the last n bytes of the buffer's content as a fresh
// slice. Returns nil for n <= 0 or an empty buffer. If the buffer has
// fewer than n bytes total, returns everything. The returned slice is a
// copy, safe to retain across mutations.
//
// Walks chunks from the tail accumulating lengths until ≥ n, then
// copies the suffix into a fresh slice — O(chunks) time, O(n) memory.
func (b *Buffer) TailBytes(n int) []byte {
	if n <= 0 {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	if len(b.chunks) == 0 {
		return nil
	}
	// Find the first chunk we have to include (walking from the tail).
	startIdx := len(b.chunks)
	collected := 0
	for i := len(b.chunks) - 1; i >= 0; i-- {
		startIdx = i
		collected += len(b.chunks[i].Data)
		if collected >= n {
			break
		}
	}
	// Total bytes in chunks[startIdx:] is `collected`. Crop the front of
	// the first chunk if we have more than n.
	out := make([]byte, 0, min(collected, n))
	if collected > n {
		skip := collected - n
		first := b.chunks[startIdx].Data
		out = append(out, first[skip:]...)
		startIdx++
	}
	for i := startIdx; i < len(b.chunks); i++ {
		out = append(out, b.chunks[i].Data...)
	}
	return out
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
```

(If the file already imports `min` from somewhere — e.g. Go 1.21 builtin — the local helper isn't needed. The module's `go.mod` should already be on Go 1.21+; verify with `head -5 /Users/attson/code/github.com.attson/atterm/go.mod`. If `go 1.21` or higher is declared, drop the local `min`.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/ringbuf/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/ringbuf/ringbuf.go internal/ringbuf/ringbuf_test.go
git -c commit.gpgsign=false commit -m "ringbuf: TailBytes returns the last n bytes as a fresh copy"
```

---

## Task 2: `StripANSI` (test-first)

**Files:**
- Create: `internal/session/ansistrip.go`
- Create: `internal/session/ansistrip_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/session/ansistrip_test.go`:

```go
package session

import "testing"

func TestStripANSI(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain", "hello world\n", "hello world\n"},
		{"csi color", "\x1b[31mred\x1b[0m", "red"},
		{"csi cursor move", "\x1b[2Jclear", "clear"},
		{"osc bel", "\x1b]0;title\x07hello", "hello"},
		{"osc st", "\x1b]0;title\x1b\\hello", "hello"},
		{"esc single char", "\x1b=raw", "raw"},
		{"mixed inline", "[\x1b[1mbold\x1b[0m] ok\n", "[bold] ok\n"},
		{"preserves crlf and tabs", "a\r\nb\tc\n", "a\r\nb\tc\n"},
		{"truncated csi at eof", "abc\x1b[3", "abc"},
		{"truncated osc at eof", "abc\x1b]title", "abc"},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(StripANSI([]byte(tc.in)))
			if got != tc.want {
				t.Fatalf("StripANSI(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestStripANSI -v`
Expected: FAIL — `undefined: StripANSI`.

- [ ] **Step 3: Implement `StripANSI`**

Create `internal/session/ansistrip.go`:

```go
package session

// StripANSI removes ANSI / VT escape sequences from b, returning a fresh
// slice. Three sequence shapes are stripped:
//   - CSI:  ESC '[' ... <final byte in 0x40..0x7E>
//   - OSC:  ESC ']' ... <BEL (0x07) or ST (ESC '\')>
//   - Other single-byte ESC sequences (ESC X): the ESC and the following byte
//
// All other input — including newlines, tabs, ASCII, multi-byte UTF-8,
// and CR — is preserved verbatim. A truncated sequence at end-of-input
// is dropped silently (the partial sequence is consumed but not emitted).
//
// Single-pass, O(len(b)) time, no regex.
func StripANSI(b []byte) []byte {
	const (
		stateText = iota
		stateAfterEsc
		stateCSI
		stateOSC
	)
	out := make([]byte, 0, len(b))
	state := stateText
	for i := 0; i < len(b); i++ {
		c := b[i]
		switch state {
		case stateText:
			if c == 0x1b { // ESC
				state = stateAfterEsc
				continue
			}
			out = append(out, c)
		case stateAfterEsc:
			switch c {
			case '[':
				state = stateCSI
			case ']':
				state = stateOSC
			default:
				// ESC X — drop ESC and X.
				state = stateText
			}
		case stateCSI:
			// CSI parameter bytes 0x30-0x3F, intermediates 0x20-0x2F, final 0x40-0x7E.
			if c >= 0x40 && c <= 0x7E {
				state = stateText
			}
			// else stay in CSI consuming parameter/intermediate bytes.
		case stateOSC:
			if c == 0x07 { // BEL terminator
				state = stateText
				continue
			}
			if c == 0x1b { // ST is ESC + '\'. Consume ESC; look for '\' next.
				// Peek next byte.
				if i+1 < len(b) && b[i+1] == '\\' {
					i++
					state = stateText
					continue
				}
				// Lone ESC inside OSC — treat as text-state reset.
				state = stateText
				continue
			}
		}
	}
	return out
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestStripANSI -v`
Expected: PASS — 11 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/session/ansistrip.go internal/session/ansistrip_test.go
git -c commit.gpgsign=false commit -m "session/ansistrip: StripANSI removes CSI/OSC/ESC X sequences"
```

---

## Task 3: `computeSummary` + `extractErrorLines` (test-first)

**Files:**
- Create: `internal/session/summary.go`
- Create: `internal/session/summary_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/session/summary_test.go`:

```go
package session

import (
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/ringbuf"
)

func feed(buf *ringbuf.Buffer, text string) {
	buf.Push(ringbuf.Chunk{Seq: buf.LatestSeq() + 1, Data: []byte(text)})
}

func TestComputeSummary_FailureExtractsErrorLines(t *testing.T) {
	buf := ringbuf.New(64 * 1024)
	feed(buf, "go: building...\n")
	feed(buf, "FAIL\tpackage [build failed]\n")
	feed(buf, "error: something specific\n")
	feed(buf, "ok\n")
	got := computeSummary(buf, time.Unix(123, 0), true)
	if got == nil {
		t.Fatal("nil summary")
	}
	if got.CapturedAt != 123 {
		t.Errorf("CapturedAt = %d, want 123", got.CapturedAt)
	}
	if !strings.Contains(got.RecentOutput, "FAIL") || !strings.Contains(got.RecentOutput, "error: something specific") {
		t.Errorf("RecentOutput missing data: %q", got.RecentOutput)
	}
	if len(got.ErrorLines) < 2 {
		t.Fatalf("ErrorLines too short: %v", got.ErrorLines)
	}
	joined := strings.Join(got.ErrorLines, "\n")
	if !strings.Contains(joined, "FAIL") || !strings.Contains(joined, "error: something specific") {
		t.Errorf("ErrorLines missing entries: %v", got.ErrorLines)
	}
}

func TestComputeSummary_SuccessNoErrorLines(t *testing.T) {
	buf := ringbuf.New(64 * 1024)
	feed(buf, "FAIL would otherwise extract\n")
	feed(buf, "error: would otherwise extract\n")
	feed(buf, "ok\n")
	got := computeSummary(buf, time.Unix(1, 0), false)
	if got == nil {
		t.Fatal("nil summary")
	}
	if len(got.ErrorLines) != 0 {
		t.Fatalf("expected no error lines on success, got %v", got.ErrorLines)
	}
}

func TestComputeSummary_StripsAnsiInRecentOutput(t *testing.T) {
	buf := ringbuf.New(64 * 1024)
	feed(buf, "\x1b[31mboom\x1b[0m\n")
	got := computeSummary(buf, time.Unix(1, 0), false)
	if got == nil {
		t.Fatal("nil summary")
	}
	if !strings.Contains(got.RecentOutput, "boom") {
		t.Errorf("output missing payload: %q", got.RecentOutput)
	}
	if strings.Contains(got.RecentOutput, "\x1b") {
		t.Errorf("output still contains ESC: %q", got.RecentOutput)
	}
}

func TestComputeSummary_RespectsByteLimit(t *testing.T) {
	buf := ringbuf.New(64 * 1024)
	for i := 0; i < 600; i++ {
		feed(buf, "0123456789\n") // 11 bytes each → 6 600 bytes total
	}
	got := computeSummary(buf, time.Unix(1, 0), false)
	if got == nil {
		t.Fatal("nil summary")
	}
	if n := len(got.RecentOutput); n > summaryOutputBytes {
		t.Fatalf("RecentOutput len = %d, want <= %d", n, summaryOutputBytes)
	}
	if !strings.HasSuffix(got.RecentOutput, "0123456789") {
		t.Errorf("expected the newest line to be kept, got tail %q", got.RecentOutput[len(got.RecentOutput)-20:])
	}
}

func TestExtractErrorLines_OrderAndLimit(t *testing.T) {
	lines := []string{
		"info: starting",
		"error: a",
		"info: still running",
		"FAIL: b",
		"panic: c",
		"error: d",
		"fatal: e",
		"error: f",
	}
	got := extractErrorLines(lines, 5)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	wantFirst := []string{"error: a", "FAIL: b", "panic: c", "error: d", "fatal: e"}
	for i, w := range wantFirst {
		if got[i] != w {
			t.Errorf("got[%d] = %q, want %q", i, got[i], w)
		}
	}
}

func TestExtractErrorLines_SkipsLongLinesAndBlank(t *testing.T) {
	long := strings.Repeat("x", 600) + " error here"
	got := extractErrorLines([]string{"", "   ", long, "error: kept"}, 5)
	if len(got) != 1 || got[0] != "error: kept" {
		t.Fatalf("got %v", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run 'TestComputeSummary|TestExtractErrorLines' -v`
Expected: FAIL — `undefined: computeSummary` / `extractErrorLines`.

- [ ] **Step 3: Implement `summary.go`**

Create `internal/session/summary.go`:

```go
package session

import (
	"regexp"
	"strings"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/ringbuf"
)

const (
	summaryTailBytes   = 8 * 1024 // bytes pulled from the scroll buffer
	summaryLineLimit   = 32       // last N lines kept after stripping
	summaryErrorLimit  = 5        // max error lines extracted on failure
	summaryOutputBytes = 4 * 1024 // RecentOutput max length on the wire
	summaryLineCap     = 512      // skip individual lines longer than this
)

// errorWordRE matches "error" / "failed" / "fatal" / "panic" / "traceback" /
// "exception" / "errored" as a whole word, case-insensitive. The (?i) prefix
// is local to this regex; \b uses ASCII word boundaries.
var errorWordRE = regexp.MustCompile(`(?i)\b(errored?|failed|fatal|panic|traceback|exception)\b`)

// errorPrefixRE matches lines that start (after leading whitespace) with a
// known severity prefix followed by ':'.
var errorPrefixRE = regexp.MustCompile(`^\s*(?i:error|panic|fatal|warning|warn):\s`)

// computeSummary builds a SessionSummary from the tail of buf as of `now`.
// isFailure controls whether ErrorLines is populated. Returns non-nil even
// when the buffer is empty; the JSON omitempty tags drop empty fields on
// the wire.
func computeSummary(buf *ringbuf.Buffer, now time.Time, isFailure bool) *proto.SessionSummary {
	raw := buf.TailBytes(summaryTailBytes)
	clean := StripANSI(raw)
	lines := splitLastN(clean, summaryLineLimit)
	output := joinTruncated(lines, summaryOutputBytes)

	s := &proto.SessionSummary{
		RecentOutput: output,
		CapturedAt:   now.Unix(),
	}
	if isFailure {
		s.ErrorLines = extractErrorLines(lines, summaryErrorLimit)
	}
	return s
}

// splitLastN splits b on '\n' and returns up to the last n non-empty lines
// (after trimming trailing '\r').
func splitLastN(b []byte, n int) []string {
	if len(b) == 0 || n <= 0 {
		return nil
	}
	all := strings.Split(string(b), "\n")
	out := make([]string, 0, n)
	for i := len(all) - 1; i >= 0; i-- {
		line := strings.TrimRight(all[i], "\r")
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) >= n {
			break
		}
	}
	// Reverse to chronological order.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// joinTruncated joins lines with '\n'. If the joined length exceeds maxBytes,
// drops from the FRONT (oldest) until it fits.
func joinTruncated(lines []string, maxBytes int) string {
	if len(lines) == 0 {
		return ""
	}
	joined := strings.Join(lines, "\n")
	if len(joined) <= maxBytes {
		return joined
	}
	// Drop oldest lines until under the limit.
	i := 0
	for i < len(lines) {
		joined = strings.Join(lines[i+1:], "\n")
		if len(joined) <= maxBytes {
			return joined
		}
		i++
	}
	// Single remaining line still too long — return a hard tail crop.
	last := lines[len(lines)-1]
	if len(last) > maxBytes {
		return last[len(last)-maxBytes:]
	}
	return last
}

// extractErrorLines returns up to `limit` lines from `lines` that match
// either the word regex or the prefix regex. Lines longer than summaryLineCap
// or pure-whitespace are skipped. Order is preserved (input order).
// Duplicates are NOT deduplicated — repeated "error" lines from a stack
// trace are useful context.
func extractErrorLines(lines []string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var out []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(line) > summaryLineCap {
			continue
		}
		if errorPrefixRE.MatchString(line) || errorWordRE.MatchString(line) {
			out = append(out, line)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}
```

Note: `proto.SessionSummary` is added in Task 4. This file imports it ahead of time; the test runs Task 3 step 2 will fail with both `undefined: computeSummary` AND `undefined: proto.SessionSummary` — that's fine, the test only completes after Task 4 lands. To unblock the implementation and tests in this task, do Task 4 first and Task 3 second. Re-ordering: **swap Task 3 and Task 4**, or finish Task 4 step 1 alone before running Task 3 step 2.

The pragmatic ordering: do Task 4 in full first, then Task 3. The plan presents them in dependency-natural order (helpers before the wiring) but the proto type is a dependency.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run 'TestComputeSummary|TestExtractErrorLines' -v`
Expected: PASS — 6 cases.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/session/summary.go internal/session/summary_test.go
git -c commit.gpgsign=false commit -m "session/summary: computeSummary + extractErrorLines (tail + ANSI strip)"
```

---

## Task 4: Add `SessionSummary` to proto + extend `MetaPayload`

**Do this BEFORE Task 3 to unblock its imports.**

**Files:**
- Modify: `internal/proto/frame.go`

- [ ] **Step 1: Add the new type and fields**

In `internal/proto/frame.go`, add the `SessionSummary` type definition above `MetaPayload` (around line 76):

```go
// SessionSummary is the post-D snapshot of a command's tail output and
// (when the captured exit code was non-zero) the extracted error lines.
// Nil before the first D event on a session; overwritten on each
// subsequent D. RecentOutput is ANSI-stripped UTF-8 text.
type SessionSummary struct {
	RecentOutput string   `json:"recent_output,omitempty"`
	ErrorLines   []string `json:"error_lines,omitempty"`
	CapturedAt   int64    `json:"captured_at,omitempty"`
}
```

Then extend `MetaPayload` (the struct starting at line 78):

```go
type MetaPayload struct {
	// ...existing fields up to LastOutputAt...
	LastOutputAt int64 `json:"last_output_at,omitempty"`
	// Type is the session workload tag (carried-over from P2.11 which only
	// added Type to SessionInfo, never to MetaPayload — meant real-time
	// type changes didn't reach subscribers until they refreshed the list).
	Type string `json:"type,omitempty"`
	// Summary carries the most recent SessionSummary for the session.
	Summary *SessionSummary `json:"summary,omitempty"`
}
```

Then extend `SessionInfo` (the struct starting at line 182). After `Type`:

```go
type SessionInfo struct {
	// ...existing fields including Type...
	Type    string          `json:"type,omitempty"`
	Summary *SessionSummary `json:"summary,omitempty"`
}
```

- [ ] **Step 2: Verify compilation**

Run: `cd /Users/attson/code/github.com.attson/atterm && go build ./...`
Expected: clean.

Run the proto tests:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/proto/`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/proto/frame.go
git -c commit.gpgsign=false commit -m "proto: SessionSummary type + Type/Summary on MetaPayload"
```

---

## Task 5: Wire `computeSummary` into session OSC 133 'D' + extend `encodeMetaPayload` (test-first)

**Files:**
- Modify: `internal/session/session.go`
- Modify: `internal/session/session_test.go`

- [ ] **Step 1: Write the failing integration test**

Append to `internal/session/session_test.go`:

```go
func TestPushOut_DEventCapturesSummary(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})

	// 1. Start a command.
	if !s.PushOut(1, []byte("\x1b]133;C;go test ./...\x07")) {
		t.Fatal("first PushOut returned false")
	}
	// 2. Stream some output including a line that should match the error regex.
	if !s.PushOut(2, []byte("=== RUN   TestX\nerror: bad thing\nFAIL\n")) {
		t.Fatal("output PushOut returned false")
	}
	// 3. End with a non-zero exit code.
	if !s.PushOut(3, []byte("\x1b]133;D;1\x07")) {
		t.Fatal("D PushOut returned false")
	}

	info := s.Info()
	if info.Summary == nil {
		t.Fatal("expected non-nil Summary after D event")
	}
	if info.Summary.CapturedAt == 0 {
		t.Errorf("CapturedAt is zero")
	}
	if len(info.Summary.ErrorLines) == 0 {
		t.Fatalf("expected ErrorLines, got %#v", info.Summary)
	}
	joined := strings.Join(info.Summary.ErrorLines, "|")
	if !strings.Contains(joined, "error: bad thing") {
		t.Errorf("ErrorLines missing entry: %v", info.Summary.ErrorLines)
	}
	if !strings.Contains(joined, "FAIL") {
		t.Errorf("ErrorLines missing FAIL: %v", info.Summary.ErrorLines)
	}
	if !strings.Contains(info.Summary.RecentOutput, "error: bad thing") {
		t.Errorf("RecentOutput missing line: %q", info.Summary.RecentOutput)
	}
}

func TestPushOut_DEventOnSuccess_NoErrorLines(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})
	s.PushOut(1, []byte("\x1b]133;C;echo ok\x07"))
	s.PushOut(2, []byte("ok\nerror: this should NOT be extracted on success\n"))
	s.PushOut(3, []byte("\x1b]133;D;0\x07"))

	info := s.Info()
	if info.Summary == nil {
		t.Fatal("expected non-nil Summary even on success")
	}
	if len(info.Summary.ErrorLines) != 0 {
		t.Fatalf("ErrorLines should be empty on success, got %v", info.Summary.ErrorLines)
	}
}
```

The test uses `s.Info()` and `s.PushOut(seq, data)` — verify both signatures match what's already in the test file (the existing `TestPushOutTracksTaskLifecycleFromOSC133` test should show the call pattern).

Also ensure `strings` is in the test file's imports — most test files in this package already have it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run 'TestPushOut_DEvent' -v`
Expected: FAIL — `s.meta.Summary` is never set; the assertions don't find it.

- [ ] **Step 3: Wire `computeSummary` into the D branch**

Open `internal/session/session.go`. In `applyOSC133Locked`, locate the `case 'D':` block (line ~633). After the `s.meta.CommandExitCode = &v ; changed = true` block at the end, insert:

```go
			// Capture a structured summary of this command's tail output.
			// Always populate Summary on D so clients can show the most
			// recent context; ErrorLines is filled only when the command
			// failed (extractErrorLines on lines we already split).
			s.meta.Summary = computeSummary(s.scroll, now, exitCode != 0)
			changed = true
```

So the final shape of the `case 'D':` block is:

```go
		case 'D':
			if s.meta.TaskState != proto.TaskStateRunning && s.meta.CommandStartedAt == 0 {
				continue
			}
			exitCode := parseOSC133Exit(payload)
			state := proto.TaskStateCompleted
			if exitCode != 0 {
				state = proto.TaskStateFailed
			}
			// ...existing s.meta.{TaskState,CommandEndedAt,CommandDurationMS,CommandExitCode} updates...
			s.meta.Summary = computeSummary(s.scroll, now, exitCode != 0)
			changed = true
```

- [ ] **Step 4: Extend `encodeMetaPayload` with `Type` and `Summary`**

In `internal/session/session.go`, find `encodeMetaPayload` (line ~495). Update the `json.Marshal(proto.MetaPayload{...})` body to include the new fields:

```go
func encodeMetaPayload(meta proto.SessionInfo, driverClientID, driverClientName string) ([]byte, error) {
	return json.Marshal(proto.MetaPayload{
		Cwd:               meta.Cwd,
		Title:             meta.Title,
		DriverClientID:    driverClientID,
		DriverClientName:  driverClientName,
		Cols:              meta.Cols,
		Rows:              meta.Rows,
		TaskState:         meta.TaskState,
		CurrentCommand:    meta.CurrentCommand,
		CommandStartedAt:  meta.CommandStartedAt,
		CommandEndedAt:    meta.CommandEndedAt,
		CommandDurationMS: meta.CommandDurationMS,
		CommandExitCode:   meta.CommandExitCode,
		LastOutputAt:      meta.LastOutputAt,
		Type:              meta.Type,
		Summary:           meta.Summary,
	})
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestPushOut_DEvent -v`
Expected: PASS — both tests.

Run the full session suite as a regression gate:
Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add internal/session/session.go internal/session/session_test.go
git -c commit.gpgsign=false commit -m "session: capture summary on OSC 133 D + broadcast Type/Summary in META"
```

---

## Task 6: Frontend TS types

**Files:**
- Modify: `desktop/frontend/src/lib/connection.ts`
- Modify: `desktop/frontend/src/platform/types.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Modify: `web/src/shared/api/types.ts`

- [ ] **Step 1: Add `SessionSummary` + field on desktop's `SessionInfo`**

In `desktop/frontend/src/lib/connection.ts`, locate the `SessionInfo` interface. Add the new interface above it and the new field at its end:

```ts
export interface SessionSummary {
  recent_output?: string;
  error_lines?: string[];
  captured_at?: number;
}

export interface SessionInfo {
  // ...existing fields including type?: string...
  type?: string;
  summary?: SessionSummary;
}
```

- [ ] **Step 2: Mirror on mobile `RemoteSession`**

In `desktop/frontend/src/platform/types.ts`, locate the `RemoteSession` interface. Add at the end:

```ts
export interface RemoteSession {
  // ...existing fields...
  type?: string;
  summary?: SessionSummary;
}
```

If `SessionSummary` isn't already imported in this file, add the import near the top:

```ts
import type { SessionSummary } from '../lib/connection'
```

- [ ] **Step 3: Propagate in Capacitor's `listRemoteSessions`**

In `desktop/frontend/src/platform/capacitor.ts`, find the `listRemoteSessions` function and the `.map((s) => { ... })` block. Two changes:

(a) Extend the inline element type (the `Array<{...}>` declaration in the `raw` line) to include `summary`:

```ts
const raw = (await res.json()) as Array<{
  // ...existing fields including type?: string...
  type?: string;
  summary?: SessionSummary;
}>
```

(b) Inside the map function, after the existing `if (s.type !== undefined) out.type = s.type` line, append:

```ts
if (s.summary !== undefined) out.summary = s.summary
```

Add the import at the top of the file if not already present:

```ts
import type { SessionSummary } from '../lib/connection'
```

- [ ] **Step 4: Mirror in the web project**

In `web/src/shared/api/types.ts`, find the `SessionInfo` interface. Add:

```ts
export interface SessionSummary {
  recent_output?: string;
  error_lines?: string[];
  captured_at?: number;
}

export interface SessionInfo {
  // ...existing fields including type?: string...
  type?: string;
  summary?: SessionSummary;
}
```

- [ ] **Step 5: Verify type-check passes**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

Run: `cd /Users/attson/code/github.com.attson/atterm/web && npx vue-tsc --noEmit 2>&1 | tail -3`
Expected: clean. If web doesn't run vue-tsc in CI, run `npm run build` instead and expect a clean build.

- [ ] **Step 6: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/lib/connection.ts desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/capacitor.ts web/src/shared/api/types.ts
git -c commit.gpgsign=false commit -m "frontend/types: SessionSummary + .summary? on three SessionInfo shapes"
```

---

## Task 7: Mobile `MobileSessionList.vue` — render `.err-line` (test-first)

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileSessionList.vue`
- Modify: `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts`

- [ ] **Step 1: Write the failing tests**

Append to `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts` (use the same `createFakePlatform` / `__setPlatformForTests` pattern the existing tests use):

```ts
it('renders the first error line under failed cards that carry a summary', async () => {
  const fakePlatform = createFakePlatform()
  fakePlatform.sessions.listRemoteSessions = vi.fn().mockResolvedValue([
    {
      session_id: 'a', host_id: 'h', host: 'box', user: 'me',
      title: 'go test', cwd: '/', cols: 80, rows: 24,
      task_state: 'failed',
      summary: { recent_output: 'FAIL\nerror: boom\n', error_lines: ['FAIL', 'error: boom'], captured_at: 1 },
    },
  ])
  __setPlatformForTests(fakePlatform)

  const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
  await flushPromises()

  const errLine = w.find('[data-testid="task-err-a"]')
  expect(errLine.exists()).toBe(true)
  expect(errLine.text()).toBe('FAIL')
})

it('does not render the error line when a failed session has no summary', async () => {
  const fakePlatform = createFakePlatform()
  fakePlatform.sessions.listRemoteSessions = vi.fn().mockResolvedValue([
    { session_id: 'b', host_id: 'h', host: 'box', user: 'me', title: 'go test', cwd: '/', cols: 80, rows: 24, task_state: 'failed' },
  ])
  __setPlatformForTests(fakePlatform)

  const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
  await flushPromises()

  expect(w.find('[data-testid="task-err-b"]').exists()).toBe(false)
})

it('does not render the error line when a session is not in failed state', async () => {
  const fakePlatform = createFakePlatform()
  fakePlatform.sessions.listRemoteSessions = vi.fn().mockResolvedValue([
    {
      session_id: 'c', host_id: 'h', host: 'box', user: 'me',
      title: 'go test', cwd: '/', cols: 80, rows: 24,
      task_state: 'completed',
      summary: { error_lines: ['error: should be ignored on completed'], captured_at: 1 },
    },
  ])
  __setPlatformForTests(fakePlatform)

  const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
  await flushPromises()

  expect(w.find('[data-testid="task-err-c"]').exists()).toBe(false)
})
```

If the existing test file's pattern uses local fetch mocks rather than `createFakePlatform`, mirror that. The previous P2.11 task added a similar test ('shows the localised type chip for non-shell sessions') — copy the surrounding scaffold from there.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/MobileSessionList.test.ts -t "error line"`
Expected: FAIL — `[data-testid="task-err-a"]` not found.

- [ ] **Step 3: Update `MobileSessionList.vue`**

Open `desktop/frontend/src/mobile/MobileSessionList.vue`. Locate the task card template — specifically the inner `<span class="col2">` block. After the existing `<span class="meta">{{ taskMeta(s) }}</span>` line, add:

```vue
<span
  v-if="s.task_state === 'failed' && s.summary?.error_lines?.length"
  class="err-line"
  :data-testid="`task-err-${s.session_id}`"
>{{ s.summary.error_lines[0] }}</span>
```

In the `<style scoped>` block, add (near the existing `.meta` / `.cwd` styles):

```css
.err-line {
  display: block;
  color: #f87171;
  font-size: 0.72rem;
  font-family: var(--font-mono);
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/mobile/__tests__/MobileSessionList.test.ts`
Expected: PASS — three new tests + all existing tests.

- [ ] **Step 5: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add desktop/frontend/src/mobile/MobileSessionList.vue desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts
git -c commit.gpgsign=false commit -m "frontend/mobile: render first error line under failed task cards"
```

---

## Task 8: Web `SessionList.vue` — mirror `.err-line`

**Files:**
- Modify: `web/src/main/components/SessionList.vue`

- [ ] **Step 1: Add the conditional block + CSS**

Open `web/src/main/components/SessionList.vue`. Locate the per-session card template (P2.11 added the type chip there). After the existing meta row (or wherever the chip + title were placed), add:

```vue
<span
  v-if="s.task_state === 'failed' && s.summary?.error_lines?.length"
  class="err-line"
  :data-testid="`task-err-${s.session_id}`"
>{{ s.summary.error_lines[0] }}</span>
```

(Replace `s.session_id` with whichever property is the session id in this file — `s.id`, `s.session_id`, etc. — verify against existing template references.)

Add CSS mirroring mobile:

```css
.err-line {
  display: block;
  color: #f87171;
  font-size: 0.72rem;
  font-family: ui-monospace, Menlo, Consolas, monospace;
  margin-top: 2px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
```

(Web doesn't have the `--font-mono` CSS variable that desktop uses; the inline mono stack matches what other monospace bits in the web file already use — verify by skim.)

- [ ] **Step 2: Verify the web build still compiles**

Run: `cd /Users/attson/code/github.com.attson/atterm/web && npm run build 2>&1 | tail -5`
Expected: succeeds.

If web has unit tests that already cover this file, add one analogous to the mobile tests. If web doesn't have vitest coverage on SessionList.vue, lean on manual smoke (Task 9 step 6).

- [ ] **Step 3: Commit**

```bash
cd /Users/attson/code/github.com.attson/atterm
git add web/src/main/components/SessionList.vue
git -c commit.gpgsign=false commit -m "web/SessionList: render first error line under failed cards"
```

---

## Task 9: Final smoke

**Files:**
- (none — verification only)

- [ ] **Step 1: Full Go suite**

Run: `cd /Users/attson/code/github.com.attson/atterm && go test ./...`
Expected: PASS.

- [ ] **Step 2: `go vet`**

Run: `cd /Users/attson/code/github.com.attson/atterm && go vet ./...`
Expected: clean.

- [ ] **Step 3: Desktop frontend tests + type-check**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test`
Expected: PASS — every existing test plus the new ones in Tasks 7.

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit`
Expected: clean.

- [ ] **Step 4: Frontend builds**

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:wails`
Expected: succeeds.

Run: `cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm run build:capacitor`
Expected: succeeds.

- [ ] **Step 5: Web build (and tests if present)**

Run: `cd /Users/attson/code/github.com.attson/atterm/web && npm run build 2>&1 | tail -3`
Expected: succeeds.

If web has tests:
Run: `cd /Users/attson/code/github.com.attson/atterm/web && npm test 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 6: Manual smoke (documented, not gating)**

For local verification after merging:
1. `go run ./cmd/atterm-relay --dev-insecure --addr :8080`
2. From the desktop app: open a tab, set up OSC 133 shell integration if not already, run `go test ./somepackage` against a package that fails. The tab itself stays as-is; on the mobile app or web admin, the failed task card now shows a red error line like `FAIL: pkg/foo (build failed)`.
3. Run a successful command — the error line is gone on the card; only the existing meta row shows.

No commit needed.

---

## Self-review notes

- **Spec coverage:**
  - §3 Data model — Task 4 (`SessionSummary` type, fields on `SessionInfo` and `MetaPayload`)
  - §4 Capture algorithm — Task 5 step 3
  - §5 `TailBytes` — Task 1
  - §6 ANSI stripping — Task 2
  - §6 `extractErrorLines` rules — Task 3
  - §7 Broadcast — Task 5 step 4 (`encodeMetaPayload` extension)
  - §8 Renderers — Task 7 (mobile), Task 8 (web)
  - §9 Error handling — total/no-panic implementations; `omitempty` covers absent fields; no new log lines
  - §10 Testing — Tasks 1, 2, 3, 5 (each has its own test file or appends to existing)
  - §11 Migration — single Go in-memory change; `omitempty` for old publishers/consumers — falls out of design, no separate task

- **Placeholder scan:** No TBDs. Concrete code in every step. The "swap Task 3/4 ordering" note is a runtime instruction, not a placeholder — Task 4 must complete before Task 3 step 4 runs. Implementing agents should follow Task 4 first when batching.

- **Type consistency:**
  - `proto.SessionSummary` (Task 4) is used by `computeSummary` (Task 3), `session.go` (Task 5), and the TS `SessionSummary` interface (Task 6) — fields match: `recent_output` / `error_lines` / `captured_at`.
  - `*proto.SessionSummary` pointer in Go aligns with TypeScript optional `summary?: SessionSummary` (absent vs null are treated the same by Vue templates).
  - The `summary*` constants in `summary.go` are referenced once in `computeSummary` body; no caller depends on them externally.

- **Ordering note (re-emphasised):** When executing, do **Task 4 BEFORE Task 3** — Task 3's `summary.go` imports `proto.SessionSummary` and won't compile without Task 4. The plan presents them in spec-natural order (helpers before wiring); the implementer should reorder when scheduling work.

---

Plan complete and saved to `docs/superpowers/plans/2026-06-04-session-summary.md`. Execution choice:

**1. Subagent-Driven (recommended)** — fresh subagent per task + two-stage review.

**2. Inline Execution** — batch tasks with checkpoints.

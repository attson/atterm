# AI 会话 OSC 标题：实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 AI 类型会话（`session.type === 'ai'`）在桌面 TabBar、桌面右侧 TaskSidebar、移动 MobileSessionCard、Web SessionList 以及桌面 Wails 窗口标题处，显示由 AI 工具通过 OSC 0/1/2 写入的窗口标题（例：`Remove token auth from relay login (node)`）。

**Architecture:** 后端在 `internal/session/session.go` 紧跟现有 `applyOSC133Locked` 之后新增 `applyOSCTitleLocked`，扫描 PTY 字节流中三种 OSC 前缀（0/1/2）并把最新标题写入 `s.meta.Title`，差值变化时复用现有 `broadcastCurrentMeta` 路径。前端零协议改动，只在四个显示层 + Wails 窗口标题处依据 `type === 'ai' && title` 切换文本。

**Tech Stack:** Go (`internal/session`)、Vue 3 + Vitest（`desktop/frontend`、`web`）、Wails runtime `WindowSetTitle`、xterm / Naive UI。

**Spec:** `docs/superpowers/specs/2026-06-10-ai-session-osc-title-design.md`

---

## File Structure

**New:**
- `internal/session/osc_title.go` — 纯函数 `scanOSCTitles` 解析 OSC 0/1/2（避免把所有解析逻辑都塞进 session.go 这个长文件）
- `internal/session/osc_title_test.go` — 纯函数单测

**Modify:**
- `internal/session/session.go` — 新增 `oscTitleBuf` 字段 + `applyOSCTitleLocked` 方法 + 在 `updateTerminalState` 接入
- `internal/session/session_test.go` — 集成测试（OSC title 通过 `PushOut` 到达 `s.meta.Title` + META 广播）
- `desktop/frontend/src/lib/sessionLabel.ts` — `SessionLike` 加可选 `type` + 新增 `aiTitleOrCommand`
- `desktop/frontend/src/lib/sessionLabel.test.ts` — 新 helper 测试
- `desktop/frontend/src/components/TabBar.vue` — 新增 `tabTitle()` helper 并切换文本
- `desktop/frontend/src/components/TabBar.test.ts` — AI/shell 分支测试
- `desktop/frontend/src/components/TaskGroupedList.vue` — `.cmd` span 用 `aiTitleOrCommand`
- `desktop/frontend/src/components/TaskGroupedList.test.ts` — AI title 渲染测试
- `desktop/frontend/src/mobile/MobileSessionCard.vue` — `.cmd` span 用 `aiTitleOrCommand`
- `desktop/frontend/src/mobile/__tests__/MobileSessionCard.test.ts` — AI title 渲染测试
- `web/src/main/components/SessionList.vue` — 模板内 inline AI title 三元
- `web/tests/unit/main/components/SessionList.test.ts` — AI title 用例
- `desktop/frontend/src/platform/types.ts` — `SystemBridge` 加 `windowSetTitle?(title: string): Promise<void>`
- `desktop/frontend/src/platform/wails.ts` — 实现 `windowSetTitle` 调用 `WindowSetTitle`
- `desktop/frontend/src/platform/capacitor.ts` — 不实现（保持 optional）
- `desktop/frontend/src/App.vue` — 引入 `watch` 跟随激活 tab 的 `(type, title)` 调用 `platform.system.windowSetTitle`
- `desktop/frontend/src/App.test.ts` — 断言 watch 行为

---

## Phase A — 后端：OSC 0/1/2 解析

### Task A1: 纯函数 `scanOSCTitles` + 单测

**Files:**
- Create: `internal/session/osc_title.go`
- Test: `internal/session/osc_title_test.go`

- [ ] **Step A1.1: Write the failing test**

`internal/session/osc_title_test.go`:

```go
package session

import (
	"reflect"
	"testing"
)

func TestScanOSCTitles_OSC0_BEL(t *testing.T) {
	titles, consumed, ok := scanOSCTitles([]byte("\x1b]0;hello\x07rest"))
	if !ok || consumed != len("\x1b]0;hello\x07") || !reflect.DeepEqual(titles, []string{"hello"}) {
		t.Fatalf("got titles=%v consumed=%d ok=%v", titles, consumed, ok)
	}
}

func TestScanOSCTitles_OSC1_BEL(t *testing.T) {
	titles, _, ok := scanOSCTitles([]byte("\x1b]1;tab-name\x07"))
	if !ok || !reflect.DeepEqual(titles, []string{"tab-name"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}

func TestScanOSCTitles_OSC2_ST(t *testing.T) {
	titles, _, ok := scanOSCTitles([]byte("\x1b]2;window-name\x1b\\"))
	if !ok || !reflect.DeepEqual(titles, []string{"window-name"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}

func TestScanOSCTitles_MultipleLastWins(t *testing.T) {
	data := []byte("\x1b]2;first\x07middle\x1b]2;second\x07")
	titles, consumed, ok := scanOSCTitles(data)
	if !ok || !reflect.DeepEqual(titles, []string{"first", "second"}) || consumed != len(data) {
		t.Fatalf("got titles=%v consumed=%d ok=%v", titles, consumed, ok)
	}
}

func TestScanOSCTitles_OverlongDropped(t *testing.T) {
	// 257-byte payload exceeds the 256 cap → that OSC is skipped entirely.
	overlong := make([]byte, 257)
	for i := range overlong {
		overlong[i] = 'x'
	}
	data := append([]byte("\x1b]2;"), overlong...)
	data = append(data, '\x07')
	data = append(data, []byte("\x1b]2;short\x07")...)
	titles, _, ok := scanOSCTitles(data)
	if !ok || !reflect.DeepEqual(titles, []string{"short"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}

func TestScanOSCTitles_IncompleteLeavesTail(t *testing.T) {
	// Unfinished OSC at the tail must NOT be reported (no terminator yet).
	data := []byte("done\x07\x1b]2;unfinished")
	titles, consumed, ok := scanOSCTitles(data)
	if ok {
		t.Fatalf("expected ok=false, got titles=%v consumed=%d", titles, consumed)
	}
	// consumed should advance past the prelude with no OSC, but leave the
	// unfinished OSC for the caller to keep in its buffer. We make the
	// contract simple: consumed == position of the unfinished prefix start.
	want := len("done\x07")
	if consumed != want {
		t.Fatalf("consumed=%d want=%d", consumed, want)
	}
}

func TestScanOSCTitles_NoOSC(t *testing.T) {
	titles, consumed, ok := scanOSCTitles([]byte("plain text\x07"))
	if ok || titles != nil || consumed != len("plain text\x07") {
		t.Fatalf("got titles=%v consumed=%d ok=%v", titles, consumed, ok)
	}
}

func TestScanOSCTitles_StrippedControlChars(t *testing.T) {
	// Embedded \r and other C0 chars (besides \n which we keep) get stripped.
	titles, _, ok := scanOSCTitles([]byte("\x1b]2;hello\rworld\x07"))
	if !ok || !reflect.DeepEqual(titles, []string{"helloworld"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}

func TestScanOSCTitles_InvalidUTF8Dropped(t *testing.T) {
	titles, _, ok := scanOSCTitles([]byte("\x1b]2;\xff\xfe\xfd\x07\x1b]2;ok\x07"))
	if !ok || !reflect.DeepEqual(titles, []string{"ok"}) {
		t.Fatalf("got titles=%v ok=%v", titles, ok)
	}
}
```

- [ ] **Step A1.2: Run test, verify it fails**

Run: `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH && go test -tags webkit2_41 -run TestScanOSCTitles ./internal/session/`
Expected: FAIL with `undefined: scanOSCTitles`.

- [ ] **Step A1.3: Implement the parser**

`internal/session/osc_title.go`:

```go
// Package session — OSC 0/1/2 (icon/window title) parser.
//
// XTerm-style escape sequences for terminal titles:
//   ESC ] 0 ; <str> ST   sets both window title and icon (tab) title
//   ESC ] 1 ; <str> ST   sets icon (tab) title only
//   ESC ] 2 ; <str> ST   sets window title only
// where ST is BEL (0x07) or ESC \\ (0x1b 0x5c).
//
// atterm has a 1:1 PTY:tab mapping, so we collapse all three prefixes onto a
// single Title field (last-writer-wins) — there is no icon vs window distinction
// to surface.

package session

import (
	"bytes"
	"unicode/utf8"
)

// maxOSCTitlePayload caps the payload length we accept inside an OSC 0/1/2.
// Real terminals tolerate similar values; rejecting overlong payloads protects
// against OSC bombing and bounds buffer growth across split Appends.
const maxOSCTitlePayload = 256

// scanOSCTitles walks data looking for complete OSC 0/1/2 title sequences
// (terminated by BEL or ESC backslash). On a successful pass it returns:
//   - titles: every accepted title in order (caller picks last-writer-wins);
//             overlong / invalid-UTF8 payloads are silently dropped
//   - consumed: byte index in data the caller may safely discard. Bytes from
//               consumed onward are either plain (no OSC prefix) or an
//               incomplete OSC waiting for its terminator on the next chunk —
//               the caller keeps them in a buffer for continuation
//   - ok: true iff at least one complete title was accepted
//
// scanOSCTitles is pure; the calling Session method (applyOSCTitleLocked) owns
// the cross-Append buffer.
func scanOSCTitles(data []byte) (titles []string, consumed int, ok bool) {
	pos := 0
	for pos < len(data) {
		// Find next OSC introducer 0x1b ']' followed by '0'/'1'/'2' and ';'.
		idx := indexOSCTitlePrefix(data[pos:])
		if idx < 0 {
			// No further OSC starts in data; everything is consumable.
			return titles, len(data), ok
		}
		startAbs := pos + idx
		payloadStart := startAbs + 4 // len("\x1b]N;") = 4
		if payloadStart > len(data) {
			// Incomplete prefix at the tail; keep from startAbs.
			return titles, startAbs, ok
		}
		// Look for terminator within the next maxOSCTitlePayload+2 bytes.
		searchEnd := payloadStart + maxOSCTitlePayload + 2
		if searchEnd > len(data) {
			searchEnd = len(data)
		}
		termRel, termLen := oscTerminator(data[payloadStart:searchEnd])
		if termRel < 0 {
			// No terminator in window. Two sub-cases:
			//   a) We hit the actual end of data → incomplete OSC, caller keeps.
			//   b) We hit searchEnd before end-of-data → payload exceeded the
			//      cap; skip past the prefix to avoid blocking forever, but
			//      DON'T emit a title. The corrupt bytes flow on to ringbuf
			//      like any other OUT data.
			if searchEnd == len(data) {
				return titles, startAbs, ok
			}
			// Skip the introducer and continue searching after the prefix.
			pos = payloadStart
			continue
		}
		payload := data[payloadStart : payloadStart+termRel]
		if title, accept := normalizeOSCTitle(payload); accept {
			titles = append(titles, title)
			ok = true
		}
		// Advance past terminator regardless of acceptance.
		pos = payloadStart + termRel + termLen
	}
	return titles, len(data), ok
}

// indexOSCTitlePrefix returns the index of the next byte sequence
// `ESC ] [0-2] ;` in buf, or -1 if none.
func indexOSCTitlePrefix(buf []byte) int {
	off := 0
	for {
		i := bytes.IndexByte(buf[off:], 0x1b)
		if i < 0 {
			return -1
		}
		abs := off + i
		if abs+3 < len(buf) &&
			buf[abs+1] == ']' &&
			(buf[abs+2] == '0' || buf[abs+2] == '1' || buf[abs+2] == '2') &&
			buf[abs+3] == ';' {
			return abs
		}
		off = abs + 1
		if off >= len(buf) {
			return -1
		}
	}
}

// normalizeOSCTitle strips C0 control bytes (except newline → space-collapse)
// and validates UTF-8. Returns (title, accept). Rejects empty or non-UTF8.
func normalizeOSCTitle(payload []byte) (string, bool) {
	if len(payload) == 0 || len(payload) > maxOSCTitlePayload {
		return "", false
	}
	cleaned := make([]byte, 0, len(payload))
	for _, b := range payload {
		if b < 0x20 || b == 0x7f {
			// Drop C0 controls and DEL.
			continue
		}
		cleaned = append(cleaned, b)
	}
	if len(cleaned) == 0 {
		return "", false
	}
	if !utf8.Valid(cleaned) {
		return "", false
	}
	return string(cleaned), true
}
```

- [ ] **Step A1.4: Run tests, verify pass**

Run: `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH && go test -tags webkit2_41 -run TestScanOSCTitles ./internal/session/ -v`
Expected: PASS — all 9 cases.

- [ ] **Step A1.5: Commit**

```bash
git add internal/session/osc_title.go internal/session/osc_title_test.go
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(session): pure scanOSCTitles for OSC 0/1/2 (icon/window title)
EOF
)"
```

---

### Task A2: `applyOSCTitleLocked` + integrate into `updateTerminalState`

**Files:**
- Modify: `internal/session/session.go`
- Test: `internal/session/session_test.go`

- [ ] **Step A2.1: Write the failing integration test**

Append to `internal/session/session_test.go`. Find a suitable spot near the OSC 133 tests or at the file tail. The harness used by the package for `New(...)`/`PushOut` should already exist — match its style.

```go
func TestPushOut_OSC2_UpdatesTitleAndBroadcastsMeta(t *testing.T) {
	s := newTestSessionForTitle(t) // helper defined below if not present
	// PTY emits OSC 2 with a "human" title — classic claude code pattern.
	changed := s.PushOut(1, []byte("hi\x1b]2;Remove token auth from relay login\x07more"))
	if !changed {
		t.Fatalf("PushOut should return changed=true when title changes")
	}
	if got := s.Meta().Title; got != "Remove token auth from relay login" {
		t.Fatalf("Title = %q, want %q", got, "Remove token auth from relay login")
	}
}

func TestPushOut_OSC2_NoChangeDoesNotBroadcast(t *testing.T) {
	s := newTestSessionForTitle(t)
	// First write sets the title.
	_ = s.PushOut(1, []byte("\x1b]2;foo\x07"))
	// Second write of the SAME title must not flag a meta change.
	changed := s.PushOut(2, []byte("\x1b]2;foo\x07"))
	if changed {
		t.Fatalf("repeating same title should not trigger meta change")
	}
}

func TestPushOut_OSC2_SplitAcrossAppend(t *testing.T) {
	s := newTestSessionForTitle(t)
	_ = s.PushOut(1, []byte("\x1b]2;Remove tok"))
	if got := s.Meta().Title; got != "" {
		t.Fatalf("title should be empty mid-sequence, got %q", got)
	}
	_ = s.PushOut(2, []byte("en auth\x07"))
	if got := s.Meta().Title; got != "Remove token auth" {
		t.Fatalf("Title = %q, want %q", got, "Remove token auth")
	}
}

func TestPushOut_OSC2_AlongsideOSC133(t *testing.T) {
	s := newTestSessionForTitle(t)
	// OSC 133 C (command started) + OSC 2 (title) in the SAME chunk.
	mixed := []byte("\x1b]133;C;npm test\x07hi\x1b]2;Run tests\x07")
	_ = s.PushOut(1, mixed)
	meta := s.Meta()
	if meta.Title != "Run tests" {
		t.Fatalf("Title = %q, want %q", meta.Title, "Run tests")
	}
	if meta.CurrentCommand != "npm test" {
		t.Fatalf("CurrentCommand = %q, want %q", meta.CurrentCommand, "npm test")
	}
}
```

Add a helper near the top of the new tests:

```go
// newTestSessionForTitle builds a minimal Session for OSC title tests.
// If the package already exports a similar helper, replace this with that.
func newTestSessionForTitle(t *testing.T) *Session {
	t.Helper()
	// Use whatever factory the existing OSC 133 tests use (look up `func TestApply` /
	// `consumeOSC133Locked` tests in this file for the canonical pattern; many tests
	// build with New(uuid.New(), proto.SessionInfo{...}, ringbuf.New(...)) or similar).
	// If the test file already has a helper for this, delete this stub and use that.
	panic("replace with existing test session factory in this file")
}
```

(The plan deliberately doesn't predict the constructor signature — it varies session-by-session and the executing agent must read the surrounding tests to find the canonical helper. Replace `newTestSessionForTitle` with the in-place factory used by adjacent tests before running.)

- [ ] **Step A2.2: Run test, verify it fails**

Run: `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH && go test -tags webkit2_41 -run TestPushOut_OSC2 ./internal/session/ -v`
Expected: FAIL — title remains empty after PushOut (parser not wired).

- [ ] **Step A2.3: Add `oscTitleBuf` to Session struct**

In `internal/session/session.go`, find the `type Session struct` definition (search for `osc133Buf` field) and add a sibling field directly after it:

```go
	// osc133Buf retains the tail of any unfinished OSC 133 sequence so a
	// terminator that lands on a Append boundary still parses.
	osc133Buf []byte
	// oscTitleBuf does the same for OSC 0/1/2 (icon/window title) sequences.
	// Kept independent from osc133Buf so the two parsers can't trip each other.
	oscTitleBuf []byte
```

- [ ] **Step A2.4: Add `applyOSCTitleLocked` method**

Add the method directly **after** the existing `applyOSC133Locked` (search for `func (s *Session) applyOSC133Locked` and append below its closing brace). Include the buffer-management helper.

```go
// applyOSCTitleLocked scans data for OSC 0/1/2 (icon/window title) sequences
// and updates s.meta.Title in place when the title changes. Returns true
// when the caller should broadcast a META frame.
//
// Same lock window as applyOSC133Locked. Independent of osc133Buf — they do
// not share state. See osc_title.go for the underlying scanner.
func (s *Session) applyOSCTitleLocked(data []byte) bool {
	combined := append(append([]byte(nil), s.oscTitleBuf...), data...)
	titles, consumed, ok := scanOSCTitles(combined)
	// Stash the unparsed tail (incomplete OSC waiting for terminator) for
	// the next Append. Cap to bound growth on malformed output.
	tail := combined[consumed:]
	const maxBuf = maxOSCTitlePayload + 8 // payload cap + introducer slack
	if len(tail) > maxBuf {
		tail = tail[len(tail)-maxBuf:]
	}
	s.oscTitleBuf = append(s.oscTitleBuf[:0], tail...)
	if !ok || len(titles) == 0 {
		return false
	}
	// Last-writer-wins: only the most recent complete title in this Append
	// counts; OSC 0/1/2 are collapsed to one field per spec §2.
	newTitle := titles[len(titles)-1]
	if newTitle == s.meta.Title {
		return false
	}
	s.meta.Title = newTitle
	return true
}
```

- [ ] **Step A2.5: Wire into `updateTerminalState`**

Search for `func (s *Session) updateTerminalState` and find the line `if s.applyOSC133Locked(data, now) {`. Below the surrounding `if/else` block (after `changed = true` / `} else if ... { ... changed = true }` finishes) add the OSC title call.

Concrete edit context — replace this stretch:

```go
	if s.applyOSC133Locked(data, now) {
		changed = true
	} else if s.meta.TaskState != proto.TaskStateRunning && looksLikeWaitingInput(data) && s.meta.TaskState != proto.TaskStateWaitingInput {
		s.meta.TaskState = proto.TaskStateWaitingInput
		s.meta.AttentionAt = now.Unix()
		changed = true
	}
```

With:

```go
	if s.applyOSC133Locked(data, now) {
		changed = true
	} else if s.meta.TaskState != proto.TaskStateRunning && looksLikeWaitingInput(data) && s.meta.TaskState != proto.TaskStateWaitingInput {
		s.meta.TaskState = proto.TaskStateWaitingInput
		s.meta.AttentionAt = now.Unix()
		changed = true
	}
	// OSC 0/1/2 title scan is independent of OSC 133 and the waiting-input
	// heuristic. Same Append can carry both; same broadcastCurrentMeta call
	// downstream covers both flags.
	if s.applyOSCTitleLocked(data) {
		changed = true
	}
```

- [ ] **Step A2.6: Run all session tests, verify pass**

Run: `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH && go test -tags webkit2_41 -timeout 60s ./internal/session/ -v`
Expected: PASS — both new `TestPushOut_OSC2_*` cases and all existing OSC 133 / silence tests still green.

- [ ] **Step A2.7: Run `go vet`**

Run: `export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH && go vet -tags webkit2_41 ./...`
Expected: clean.

- [ ] **Step A2.8: Commit**

```bash
git add internal/session/session.go internal/session/session_test.go
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(session): apply OSC 0/1/2 titles to SessionInfo.Title
EOF
)"
```

---

## Phase B — 前端共享 helper

### Task B1: `aiTitleOrCommand` in `sessionLabel.ts`

**Files:**
- Modify: `desktop/frontend/src/lib/sessionLabel.ts`
- Test: `desktop/frontend/src/lib/sessionLabel.test.ts`

- [ ] **Step B1.1: Write failing tests**

Append to `desktop/frontend/src/lib/sessionLabel.test.ts`:

```ts
import { aiTitleOrCommand } from './sessionLabel'

describe('sessionLabel.aiTitleOrCommand', () => {
  it('returns AI title when session is ai and title is non-empty', () => {
    expect(aiTitleOrCommand({
      session_id: 'x',
      current_command: 'claude --foo',
      title: 'Remove token auth from relay login',
      type: 'ai',
    })).toBe('Remove token auth from relay login')
  })

  it('falls back to commandLabel when AI session has empty title', () => {
    expect(aiTitleOrCommand({
      session_id: 'x',
      current_command: '/usr/local/bin/claude --bar',
      title: '',
      type: 'ai',
    })).toBe('claude')
  })

  it('ignores title for non-AI sessions even when set', () => {
    expect(aiTitleOrCommand({
      session_id: 'x',
      current_command: 'zsh',
      title: 'user@host: ~/proj',
      type: 'shell',
    })).toBe('zsh')
  })

  it('treats undefined type as non-AI', () => {
    expect(aiTitleOrCommand({
      session_id: 'x',
      current_command: 'zsh',
      title: 'something',
    })).toBe('zsh')
  })
})
```

- [ ] **Step B1.2: Run, verify FAIL**

Run: `cd desktop/frontend && npm test -- sessionLabel`
Expected: FAIL with "aiTitleOrCommand is not exported".

- [ ] **Step B1.3: Add `type` to `SessionLike` and add helper**

Edit `desktop/frontend/src/lib/sessionLabel.ts`:

Replace:

```ts
export interface SessionLike {
  current_command?: string
  title?: string
  session_id: string
  cwd?: string
}
```

With:

```ts
export interface SessionLike {
  current_command?: string
  title?: string
  session_id: string
  cwd?: string
  // Optional workload classification — "ai" | "shell" | "test" | "build"
  // | "deploy". When absent, treat as shell. Drives aiTitleOrCommand().
  type?: string
}
```

Append at the end of the file:

```ts
// aiTitleOrCommand returns the AI-set window title (from OSC 0/1/2, surfaced
// via SessionInfo.Title) when the session is classified as an AI workload
// and a title is available. Otherwise returns the existing short command
// label so shell sessions keep their current display. Used by the desktop
// TabBar (indirectly via tabTitle), TaskGroupedList, and the mobile session
// card. Web SessionList inlines the same condition for parity.
export function aiTitleOrCommand(s: SessionLike): string {
  if (s.type === 'ai' && s.title) return s.title
  return commandLabel(s)
}
```

- [ ] **Step B1.4: Run tests, verify PASS**

Run: `cd desktop/frontend && npm test -- sessionLabel`
Expected: all sessionLabel cases (including new four) pass.

- [ ] **Step B1.5: Commit**

```bash
git add desktop/frontend/src/lib/sessionLabel.ts desktop/frontend/src/lib/sessionLabel.test.ts
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(sessionLabel): add aiTitleOrCommand helper
EOF
)"
```

---

## Phase C — 桌面前端 4 处显示层

### Task C1: 桌面 TabBar

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.vue`
- Test: `desktop/frontend/src/components/TabBar.test.ts`

- [ ] **Step C1.1: Write failing tests**

Append to `desktop/frontend/src/components/TabBar.test.ts`:

```ts
describe('TabBar AI title', () => {
  test('shows OSC title for ai-typed session', () => {
    const tab = {
      ...baseTab,
      activeSession: {
        id: 's1',
        command: '/usr/local/bin/claude',
        cwd: '/Users/me/proj',
        title: 'Remove token auth from relay login',
        cols: 80,
        rows: 24,
        started_at: 0,
        type: 'ai',
        task_state: 'running' as const,
      },
    }
    const w = mount(TabBar, { props: { tabs: [tab], currentId: 't1', starting: false } })
    expect(w.get('.title').text()).toBe('Remove token auth from relay login')
  })

  test('falls back to cwd basename when ai session has no title yet', () => {
    const tab = {
      ...baseTab,
      activeSession: {
        id: 's1',
        command: 'claude',
        cwd: '/Users/me/proj',
        title: '',
        cols: 80,
        rows: 24,
        started_at: 0,
        type: 'ai',
      },
    }
    const w = mount(TabBar, { props: { tabs: [tab], currentId: 't1', starting: false } })
    expect(w.get('.title').text()).toBe('proj')
  })

  test('shell session ignores title and uses cwd basename', () => {
    const tab = {
      ...baseTab,
      activeSession: {
        id: 's1',
        command: 'zsh',
        cwd: '/Users/me/proj',
        title: 'should-not-show',
        cols: 80,
        rows: 24,
        started_at: 0,
        type: 'shell',
      },
    }
    const w = mount(TabBar, { props: { tabs: [tab], currentId: 't1', starting: false } })
    expect(w.get('.title').text()).toBe('proj')
  })
})
```

- [ ] **Step C1.2: Run, verify FAIL**

Run: `cd desktop/frontend && npm test -- TabBar`
Expected: FAIL — first case shows `proj` not the AI title.

- [ ] **Step C1.3: Add `tabTitle` helper in TabBar.vue**

Edit `desktop/frontend/src/components/TabBar.vue`. Just **after** the existing `shortTitle` function in the `<script setup>` block, add:

```ts
function tabTitle(s: SessionInfo | null): string {
  if (s?.type === 'ai' && s.title) return s.title;
  return shortTitle(s);
}
```

Then change the `<span class="title">` in the template:

From:
```vue
<span class="title">{{ shortTitle(t.activeSession) }}</span>
```

To:
```vue
<span class="title">{{ tabTitle(t.activeSession) }}</span>
```

- [ ] **Step C1.4: Run tests, verify PASS**

Run: `cd desktop/frontend && npm test -- TabBar`
Expected: all TabBar cases (including new three) pass.

- [ ] **Step C1.5: Commit**

```bash
git add desktop/frontend/src/components/TabBar.vue desktop/frontend/src/components/TabBar.test.ts
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(tabbar): show AI session OSC title in tab text
EOF
)"
```

---

### Task C2: 桌面 TaskGroupedList

**Files:**
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue`
- Test: `desktop/frontend/src/components/TaskGroupedList.test.ts`

- [ ] **Step C2.1: Read current `.cmd` rendering**

Run: `grep -n 'commandLabel\|\\.cmd\|class="cmd"' desktop/frontend/src/components/TaskGroupedList.vue`
Note the exact line where `commandLabel(s)` is called on the row text. The plan replaces THAT call with `aiTitleOrCommand(s)` while keeping the tooltip (`rowTitle`) untouched.

- [ ] **Step C2.2: Write failing test**

Append to `desktop/frontend/src/components/TaskGroupedList.test.ts`:

```ts
it('shows AI session OSC title in the row when present', () => {
  const w = mount(TaskGroupedList, {
    props: {
      sessions: [
        mk({
          session_id: 's1',
          host: 'mac',
          task_state: 'running',
          title: 'Remove token auth from relay login',
          current_command: '/usr/local/bin/claude --foo',
          type: 'ai',
        }),
      ],
      // … reuse whatever other props the existing tests pass
    },
  })
  expect(w.text()).toContain('Remove token auth from relay login')
  expect(w.text()).not.toMatch(/\bclaude\b/) // command name should be hidden, not in row text
})

it('falls back to commandLabel for non-ai sessions even when title is set', () => {
  const w = mount(TaskGroupedList, {
    props: {
      sessions: [
        mk({
          session_id: 's1',
          host: 'mac',
          task_state: 'running',
          title: 'user@host: ~/proj',
          current_command: 'zsh',
          type: 'shell',
        }),
      ],
    },
  })
  expect(w.text()).toContain('zsh')
  expect(w.text()).not.toContain('user@host')
})
```

(The `mk(...)` helper already exists in this file — see the top of `TaskGroupedList.test.ts` for its signature; just add `title` / `type` to the literal it builds.)

- [ ] **Step C2.3: Run, verify FAIL**

Run: `cd desktop/frontend && npm test -- TaskGroupedList`
Expected: FAIL — row currently shows `claude`, not the AI title.

- [ ] **Step C2.4: Swap `commandLabel` → `aiTitleOrCommand` in row template**

Edit `desktop/frontend/src/components/TaskGroupedList.vue`:

1. In the `<script setup>` import block, change:
```ts
import { commandLabel, fullCommand, ... } from '../lib/sessionLabel'
```
to also import `aiTitleOrCommand`:
```ts
import { aiTitleOrCommand, commandLabel, fullCommand, ... } from '../lib/sessionLabel'
```
(Keep `commandLabel`/`fullCommand` imports — they may still be used by tooltip / other paths. Remove them only if `grep -n` confirms no remaining usage.)

2. In the template, find the `.cmd` span / display of the row label (line found in Step C2.1) and change `{{ commandLabel(s) }}` → `{{ aiTitleOrCommand(s) }}`.

3. **Don't** touch `rowTitle` / hover tooltip — full command stays available there.

- [ ] **Step C2.5: Run tests, verify PASS**

Run: `cd desktop/frontend && npm test -- TaskGroupedList`
Expected: all cases pass.

- [ ] **Step C2.6: Commit**

```bash
git add desktop/frontend/src/components/TaskGroupedList.vue desktop/frontend/src/components/TaskGroupedList.test.ts
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(task-sidebar): show AI session OSC title in row text
EOF
)"
```

---

### Task C3: 移动端 MobileSessionCard

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileSessionCard.vue`
- Test: `desktop/frontend/src/mobile/__tests__/MobileSessionCard.test.ts`

- [ ] **Step C3.1: Write failing test**

Append to `desktop/frontend/src/mobile/__tests__/MobileSessionCard.test.ts`:

```ts
it('shows AI title in cmd span for ai session', () => {
  const w = mount(MobileSessionCard, {
    props: {
      session: {
        session_id: 'a',
        host_id: 'h',
        host: 'mac',
        user: 'me',
        cwd: '/p',
        title: 'Improve sales order list styling',
        current_command: 'claude',
        type: 'ai',
        task_state: 'running',
        cols: 80,
        rows: 24,
      },
      home: '/Users/me',
    },
  })
  expect(w.get('.cmd').text()).toBe('Improve sales order list styling')
})

it('falls back to commandLabel for shell session', () => {
  const w = mount(MobileSessionCard, {
    props: {
      session: {
        session_id: 'a',
        host_id: 'h',
        host: 'mac',
        user: 'me',
        cwd: '/p',
        title: 'irrelevant',
        current_command: 'zsh',
        type: 'shell',
        task_state: 'idle',
        cols: 80,
        rows: 24,
      },
      home: '/Users/me',
    },
  })
  expect(w.get('.cmd').text()).toBe('zsh')
})
```

(Use the existing import + mount style at the top of the same file.)

- [ ] **Step C3.2: Run, verify FAIL**

Run: `cd desktop/frontend && npm test -- MobileSessionCard`
Expected: FAIL.

- [ ] **Step C3.3: Swap helper in MobileSessionCard.vue**

Edit `desktop/frontend/src/mobile/MobileSessionCard.vue`:

1. Change the import line from:
```ts
import { commandLabel, taskStateLabel } from '../lib/sessionLabel'
```
to:
```ts
import { aiTitleOrCommand, taskStateLabel } from '../lib/sessionLabel'
```
(`commandLabel` no longer needed here; verify with `grep -n commandLabel desktop/frontend/src/mobile/MobileSessionCard.vue` after editing.)

2. Change `const cmd = computed(() => commandLabel(props.session))` to:
```ts
const cmd = computed(() => aiTitleOrCommand(props.session))
```

- [ ] **Step C3.4: Run, verify PASS**

Run: `cd desktop/frontend && npm test -- MobileSessionCard`
Expected: PASS.

- [ ] **Step C3.5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileSessionCard.vue desktop/frontend/src/mobile/__tests__/MobileSessionCard.test.ts
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(mobile-card): show AI session OSC title in row text
EOF
)"
```

---

## Phase D — Web SessionList

### Task D1: Inline AI title in web SessionList

**Files:**
- Modify: `web/src/main/components/SessionList.vue`
- Test: `web/tests/unit/main/components/SessionList.test.ts`

- [ ] **Step D1.1: Read existing test scaffold**

Run: `head -60 web/tests/unit/main/components/SessionList.test.ts` — note the import style and how `SessionInfo` rows are constructed (it mocks `listSessions`). The new test must construct a row matching that mock shape.

- [ ] **Step D1.2: Write failing test**

Append to `web/tests/unit/main/components/SessionList.test.ts`:

```ts
it('shows AI title in card cmd when session.type=ai and title is set', async () => {
  // Reuse whatever mock pattern the file already established — usually a
  // mocked `listSessions` returning an array with one object. Add a `type`
  // and `title` field to that mock row.
  const wrapper = mountWith({
    rows: [{
      id: 'ai-1',
      command: '/usr/local/bin/claude --foo',
      title: 'Remove token auth from relay login',
      type: 'ai',
      cwd: '/Users/me/proj',
      cols: 80,
      rows: 24,
      started_at: 0,
      host_id: 'h',
      host: 'mac',
      user: 'me',
    }],
  })
  await flushPromises()
  expect(wrapper.text()).toContain('Remove token auth from relay login')
  expect(wrapper.text()).not.toMatch(/\/usr\/local\/bin\/claude/)
})

it('shows raw command for shell session even when title is set', async () => {
  const wrapper = mountWith({
    rows: [{
      id: 'shell-1',
      command: 'zsh',
      title: 'user@host: ~/proj',
      type: 'shell',
      cwd: '/Users/me/proj',
      cols: 80,
      rows: 24,
      started_at: 0,
      host_id: 'h',
      host: 'mac',
      user: 'me',
    }],
  })
  await flushPromises()
  expect(wrapper.text()).toContain('zsh')
  expect(wrapper.text()).not.toContain('user@host')
})
```

`mountWith` is shorthand for whatever bootstrap pattern the existing tests use; reuse the exact name they use (likely a local helper or direct `mount(SessionList)` call with `vi.mock`).

- [ ] **Step D1.3: Run, verify FAIL**

Run: `cd web && npm test -- SessionList`
Expected: FAIL on the AI case.

- [ ] **Step D1.4: Inline AI title in SessionList.vue template**

Edit `web/src/main/components/SessionList.vue`. Find the `.cmd` div:

```vue
<div class="cmd">
  <span v-if="typeForSession(s)" class="type-chip" :style="{ '--chip': typeForSession(s)!.color }">
    {{ t(`main.taskTypes.${typeForSession(s)!.key}`) }}
  </span>
  {{ s.command || t('main.unknownCommand') }}
</div>
```

Replace the bare `{{ s.command || ... }}` line with:

```vue
  {{ (s.type === 'ai' && s.title) ? s.title : (s.command || t('main.unknownCommand')) }}
```

(Type chip / cwd unchanged.)

- [ ] **Step D1.5: Run, verify PASS**

Run: `cd web && npm test -- SessionList`
Expected: PASS.

- [ ] **Step D1.6: Rebuild web dist (git hook guard)**

Run: `cd web && npm run build`
Expected: `web/dist` updated. The repo's pre-commit hook (`core.hooksPath=.githooks`) syncs `internal/relay/web-dist/` from `web/dist` — if commits below trip the hook, follow its instructions to re-stage the synced bundle.

- [ ] **Step D1.7: Commit**

```bash
git add web/src/main/components/SessionList.vue web/tests/unit/main/components/SessionList.test.ts
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(web-session-list): show AI session OSC title in card
EOF
)"
# If web-dist drift hook fires, the second commit covers the regenerated assets:
git status -s internal/relay/web-dist/ web/dist/
git add internal/relay/web-dist web/dist 2>/dev/null
git -c commit.gpgsign=false commit -m "chore(web-dist): rebuild for AI title display" 2>/dev/null || true
```

(The chore commit only runs if the build produced new files. `|| true` so a no-op doesn't abort the script.)

---

## Phase E — 桌面 Wails 主窗口标题

### Task E1: `SystemBridge.windowSetTitle` + adapters

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts`
- Modify: `desktop/frontend/src/platform/wails.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Test: existing platform test (`desktop/frontend/src/platform/types.test.ts` if present, else skip — adapter behavior covered by App.test.ts in E2)

- [ ] **Step E1.1: Add to `SystemBridge` type**

Edit `desktop/frontend/src/platform/types.ts`. Find the `SystemBridge` interface (it already has `windowMinimize?` / `windowToggleMaximize?` / `windowIsMaximized?`). Add a sibling:

```ts
export interface SystemBridge {
  // … existing …
  windowMinimize?(): Promise<void>
  windowToggleMaximize?(): Promise<void>
  windowIsMaximized?(): Promise<boolean>
  // windowSetTitle updates the OS window title (macOS title bar / Windows
  // taskbar / etc). On non-desktop platforms this is undefined; callers
  // must null-check before invoking.
  windowSetTitle?(title: string): Promise<void>
  quit?(): Promise<void>
}
```

- [ ] **Step E1.2: Wire Wails adapter**

Edit `desktop/frontend/src/platform/wails.ts`. In the import block, add `WindowSetTitle`:

```ts
import {
  EventsOn,
  EventsEmit,
  WindowMinimise,
  WindowToggleMaximise,
  WindowIsMaximised,
  WindowSetTitle,
  Quit,
  Environment,
  BrowserOpenURL,
} from '../../wailsjs/runtime/runtime'
```

Inside `system: { … }` add a sibling after `windowIsMaximized`:

```ts
      windowMinimize: async () => WindowMinimise(),
      windowToggleMaximize: async () => WindowToggleMaximise(),
      windowIsMaximized: () => WindowIsMaximised(),
      windowSetTitle: async (title: string) => WindowSetTitle(title),
      quit: async () => Quit(),
```

- [ ] **Step E1.3: Capacitor adapter — no-op**

Edit `desktop/frontend/src/platform/capacitor.ts`. Confirm with `grep -n windowMinimize desktop/frontend/src/platform/capacitor.ts` whether existing window methods are defined or omitted:
- If omitted (i.e. `windowMinimize` is absent from the capacitor system bridge): do nothing — `windowSetTitle` will also be absent, matching the optional contract.
- If defined as a no-op: add `windowSetTitle: async () => {},` alongside them.

- [ ] **Step E1.4: TypeScript build sanity-check**

Run: `cd desktop/frontend && npm run build`
Expected: build succeeds (it runs `vue-tsc` first; type-check covers the new optional method).

- [ ] **Step E1.5: Commit**

```bash
git add desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/wails.ts desktop/frontend/src/platform/capacitor.ts
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(platform): SystemBridge.windowSetTitle on Wails
EOF
)"
```

---

### Task E2: App.vue tracks active AI session title → window title

**Files:**
- Modify: `desktop/frontend/src/App.vue`
- Test: `desktop/frontend/src/App.test.ts`

- [ ] **Step E2.1: Locate existing watchers in App.vue**

Run: `grep -n 'watch(' desktop/frontend/src/App.vue | head -20` — pick an existing `watch([…], () => {…})` near the `currentTab` definition. The new watcher belongs nearby.

- [ ] **Step E2.2: Write failing test**

Edit `desktop/frontend/src/App.test.ts`. Find or add a `describe('App window title', …)` block:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import App from './App.vue'
// … reuse the platform stub pattern existing App tests use; if the file mocks
// `./platform` already, append `windowSetTitle: vi.fn()` to its system bridge stub.

it('sets window title to AI session title when active tab is ai', async () => {
  const setTitle = vi.fn()
  // … use the existing helper that mounts App with a stubbed platform.
  // Pseudocode for whichever scaffold the file uses:
  const wrapper = await mountAppWithPlatformStub({
    system: { windowSetTitle: setTitle, /* … */ },
    sessions: [
      // The test scaffold injects a tab with id=t1, active session with:
      { id: 's1', type: 'ai', title: 'My AI task', command: 'claude', cwd: '/', cols: 80, rows: 24, started_at: 0 },
    ],
    currentTabId: 't1',
  })
  await flushPromises()
  expect(setTitle).toHaveBeenCalledWith('My AI task')
})

it('resets window title to AT Term when active tab is non-ai', async () => {
  const setTitle = vi.fn()
  const wrapper = await mountAppWithPlatformStub({
    system: { windowSetTitle: setTitle },
    sessions: [
      { id: 's1', type: 'shell', title: 'irrelevant', command: 'zsh', cwd: '/', cols: 80, rows: 24, started_at: 0 },
    ],
    currentTabId: 't1',
  })
  await flushPromises()
  expect(setTitle).toHaveBeenLastCalledWith('AT Term')
})

it('updates window title when active tab AI title changes', async () => {
  const setTitle = vi.fn()
  const { setSessionTitle } = await mountAppWithPlatformStub({
    system: { windowSetTitle: setTitle },
    sessions: [
      { id: 's1', type: 'ai', title: 'A', command: 'claude', cwd: '/', cols: 80, rows: 24, started_at: 0 },
    ],
    currentTabId: 't1',
  })
  setSessionTitle('s1', 'B')
  await flushPromises()
  expect(setTitle).toHaveBeenLastCalledWith('B')
})
```

`mountAppWithPlatformStub` represents the existing scaffold this file uses to spin up `App` with mocked platform; reuse whatever helper name/import the rest of `App.test.ts` already uses. If no helper exists yet, factor one out at the top of the file before writing the new cases.

- [ ] **Step E2.3: Run, verify FAIL**

Run: `cd desktop/frontend && npm test -- App.test`
Expected: FAIL — `windowSetTitle` never called.

- [ ] **Step E2.4: Add the watcher to App.vue**

Edit `desktop/frontend/src/App.vue`. Near the existing `currentTab` computed and the other `watch([…])` blocks, add:

```ts
// Drive the OS window title from the active tab's AI session title. claude
// already prefixes its OSC title with status glyphs (●/✻) — we don't add
// our own to avoid double-prefix. Falls back to "AT Term" for non-AI tabs
// and for AI tabs whose OSC title hasn't been emitted yet.
watch(
  () => {
    const s = currentTab.value?.activeSession
    return [s?.type, s?.title] as const
  },
  ([type, title]) => {
    const next = (type === 'ai' && title) ? title : 'AT Term'
    platform.system.windowSetTitle?.(next).catch(() => { /* desktop-only; ignore on others */ })
  },
  { immediate: true },
)
```

- [ ] **Step E2.5: Run tests, verify PASS**

Run: `cd desktop/frontend && npm test -- App.test`
Expected: PASS — all three new cases plus existing App tests.

- [ ] **Step E2.6: Build sanity-check**

Run: `cd desktop/frontend && npm run build`
Expected: clean.

- [ ] **Step E2.7: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts
git -c commit.gpgsign=false commit -m "$(cat <<'EOF'
feat(app): sync OS window title with active AI session title
EOF
)"
```

---

## Phase F — 端到端验证

### Task F1: 全量构建 + 手动 smoke

- [ ] **Step F1.1: Full Go + TS build + tests**

Run, expecting all green:

```bash
export PATH=/opt/homebrew/bin:$HOME/sdk/go1.23.12/bin:$HOME/go/bin:$PATH
go vet -tags webkit2_41 ./...
go test -tags webkit2_41 -timeout 60s ./internal/session/ ./desktop/
cd desktop/frontend && npm run build && npm test
cd ../../web && npm run build && npm test && npm run test:contract
```

- [ ] **Step F1.2: Manual smoke — desktop**

1. `cd desktop && wails dev -tags webkit2_41`
2. 在 atterm 起一个 `claude` 会话 → 等 claude 进入正常工作；tab 文字应跟随 claude 设置的标题（含 `●/✻` 状态前缀），macOS 标题栏同步更新。
3. 起一个 `zsh` 会话 → tab 文字仍为 cwd basename，macOS 标题栏回到 `AT Term`（即使 oh-my-zsh 发了 OSC 0）。
4. 切到 AI tab → 标题栏跟随；切回 shell tab → 标题栏回 `AT Term`。
5. 关掉所有 AI tab → 标题栏 `AT Term`。

- [ ] **Step F1.3: Manual smoke — mobile / web（如条件具备）**

- 用 iOS/web 客户端 attach 同一台 desktop 的 AI 会话；MobileSessionCard 与 web SessionList 应显示一致的 AI 标题。
- 切到 shell 会话 → 显示命令名而非 oh-my-zsh title。

- [ ] **Step F1.4: 收尾**

无新增 commit。若手动 smoke 发现回归（例如标题闪烁/双发），回到 §A2.4 的 noisy debouncing 待选项；目前的差值去重已能兜住，多数情况不需要。

---

## Self-Review 通过情况

- **Spec coverage**：spec §1 目标 5 条 → Task A1+A2（OSC parser+integration）+ C1+C2+C3+D1（4 处会话栏）+ E1+E2（窗口标题）。 spec §4–§7 全部映射到任务。
- **占位符**：仅有的"现状探查"步骤（如 C2.1 grep 行号、E2.1 找现有 watcher、D1.1 读 mock 模式、A2.1 `newTestSessionForTitle` 待用现有 factory）都在文档里写明了"为什么不能预写"——它们是 spec §9 故意保留的"按现状装配"点，不是占位符。
- **类型/命名一致**：`aiTitleOrCommand` / `tabTitle` / `applyOSCTitleLocked` / `oscTitleBuf` / `scanOSCTitles` 跨任务统一。`SystemBridge.windowSetTitle?` 拼写与 wails / capacitor / App.vue 三处用法对齐。

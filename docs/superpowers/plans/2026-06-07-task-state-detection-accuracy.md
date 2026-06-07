# Task State Detection Accuracy — Alt-Screen Silence Heuristic Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect AI / TUI sessions that go quiet in alt-screen mode and flip `task_state` from `running` → `waiting_input` (bumping `AttentionAt` for the inbox), and back to `running` when output resumes — so the desktop sidebar correctly badges sessions where Claude / Codex / Aider have finished a response and are ready for the next prompt.

**Architecture:** Per-session `*time.Timer` armed inside `updateTerminalState`; on fire, recheck guards under the session lock and (if still in `running + alt-screen + LastOutputAt ≥ threshold`) flip to `waiting_input` and broadcast META. Output arriving while in heuristic-`waiting_input` restores `running`. A `waitingFromSilence` flag scopes the auto-restore so it never undoes the existing keyword-based `waiting_input` (`password:`, `[y/n]`, …).

**Tech Stack:** Go (`internal/session/session.go`), `time.AfterFunc`, env-var configuration (`ATTERM_TASK_SILENCE_DETECT`, `ATTERM_TASK_SILENCE_THRESHOLD_MS`). No protocol, no migrations, no frontend changes.

**Spec:** `docs/superpowers/specs/2026-06-07-task-state-detection-accuracy-design.md`.

---

## File structure

- `internal/session/session.go` (modify): add `silenceTimer`, `waitingFromSilence`, `silenceThresholdMS`, `silenceDetectEnabled` fields; read env in `New`; new helpers `rescheduleSilenceTimerLocked` and `onSilenceFired`; wire `updateTerminalState` / `applyOSC133Locked('D')` / `Close()`.
- `internal/session/silence_test.go` (new): table-driven tests for the heuristic.

No other files change. Existing tests must remain green (`go test -race ./internal/session/...`).

---

## Task 1: Session fields + env-driven configuration

Adds the four new fields and wires env reads at construction. Pure additive — no behavior change yet (no calls into the new fields).

**Files:**
- Modify: `internal/session/session.go` (`Session` struct ~lines 34-76; `New` constructor ~lines 98-115)
- Test: `internal/session/silence_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/session/silence_test.go`:

```go
package session

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestSilence_DefaultsApplied(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	if !s.silenceDetectEnabled {
		t.Fatalf("default ATTERM_TASK_SILENCE_DETECT should be true; got false")
	}
	if s.silenceThresholdMS != 5000 {
		t.Fatalf("default ATTERM_TASK_SILENCE_THRESHOLD_MS should be 5000; got %d", s.silenceThresholdMS)
	}
}

func TestSilence_EnvOverrides(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_DETECT", "0")
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "250")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	if s.silenceDetectEnabled {
		t.Fatalf("DETECT=0 should disable")
	}
	if s.silenceThresholdMS != 250 {
		t.Fatalf("THRESHOLD_MS=250 should set field to 250; got %d", s.silenceThresholdMS)
	}
}

func TestSilence_InvalidThresholdFallsBack(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "not-a-number")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	if s.silenceThresholdMS != 5000 {
		t.Fatalf("invalid threshold should fall back to 5000; got %d", s.silenceThresholdMS)
	}
}
```

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestSilence_ -v
```
Expected: FAIL — fields not defined.

- [ ] **Step 3: Add the fields**

In `internal/session/session.go`, inside the `Session` struct (place after the existing `osc133Buf` / `cmdStarted` block, before `driverSubscriber`):

```go
	// Silence-detection heuristic state. silenceTimer is armed by
	// rescheduleSilenceTimerLocked whenever the session is in running + alt
	// screen; on fire onSilenceFired re-checks guards and flips to
	// waiting_input. waitingFromSilence remembers that the current
	// waiting_input came from this heuristic (so output arriving while in
	// waiting_input restores running only for us, never undoing a
	// looksLikeWaitingInput match). silenceThresholdMS and silenceDetectEnabled
	// are populated from env at New() and cached per session so tests can
	// inject short thresholds via t.Setenv.
	silenceTimer         *time.Timer
	waitingFromSilence   bool
	silenceThresholdMS   int64
	silenceDetectEnabled bool
```

- [ ] **Step 4: Wire env reads in New + add helpers**

In `internal/session/session.go`, just above `func New(...)`, add:

```go
const defaultSilenceThresholdMS int64 = 5000

func envSilenceDetectEnabled() bool {
	v := os.Getenv("ATTERM_TASK_SILENCE_DETECT")
	if v == "" {
		return true
	}
	return v != "0" && !strings.EqualFold(v, "false")
}

func envSilenceThresholdMS() int64 {
	v := os.Getenv("ATTERM_TASK_SILENCE_THRESHOLD_MS")
	if v == "" {
		return defaultSilenceThresholdMS
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return defaultSilenceThresholdMS
	}
	return n
}
```

Update the imports block at the top of the file to add `"os"`, `"strconv"`, and confirm `"strings"` is present (it already is — used by ClassifyCommand etc.).

In `func New(...)`, change the returned struct literal to populate the new fields:

```go
	return &Session{
		ID:                   id,
		StartedAt:            time.Now(),
		meta:                 meta,
		subs:                 make(map[*Subscriber]struct{}),
		scroll:               ringbuf.New(scrollbackBytes),
		inbound:              make(chan proto.Frame, inboundQueueDepth),
		silenceThresholdMS:   envSilenceThresholdMS(),
		silenceDetectEnabled: envSilenceDetectEnabled(),
	}
```

- [ ] **Step 5: Run, expect pass**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestSilence_ -v
```
Expected: PASS — 3/3.

- [ ] **Step 6: Run the whole session package to catch regressions**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/session/session.go internal/session/silence_test.go
git commit -m "session: silence-detect config fields + env reads (no wiring yet)"
```

---

## Task 2: rescheduleSilenceTimerLocked + onSilenceFired

Adds the two helpers that implement the heuristic core. They are not yet called from anywhere — Task 3 wires them in. Splitting it lets each commit be reviewable and lets the test in this task pin the helpers' semantics directly.

**Files:**
- Modify: `internal/session/session.go` (add the two helpers near the existing `updateTerminalState` helpers, around line 700)
- Test: `internal/session/silence_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/session/silence_test.go`:

```go
import (
	"sync/atomic"
	"time"
)

// directFireSilenceTimer constructs a session, sets the heuristic to fire
// after 50ms, drops the session into running + alt-screen, calls
// rescheduleSilenceTimerLocked(), waits long enough for the timer to fire,
// and returns the session. Tests then inspect Info() / waitingFromSilence.
func directFireSilenceTimer(t *testing.T, prep func(*Session)) *Session {
	t.Helper()
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	prep(s)
	s.mu.Lock()
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	// Wait until the timer fires (or give up after a generous bound).
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return s
}

func TestSilence_TimerFlipsRunningToWaiting(t *testing.T) {
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = true
		// Treat the session as having had output 10s ago so we're already
		// past the threshold.
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	})
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("silence timer should have flipped state to waiting_input; got %q", s.Info().TaskState)
	}
	if !s.waitingFromSilence {
		t.Fatalf("waitingFromSilence should be true after silence flip")
	}
	if s.Info().AttentionAt == 0 {
		t.Fatalf("AttentionAt should be bumped by silence flip")
	}
}

func TestSilence_TimerNoopWhenNotAltScreen(t *testing.T) {
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = false
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	})
	if s.Info().TaskState == proto.TaskStateWaitingInput {
		t.Fatalf("silence timer must not fire outside alt-screen")
	}
}

func TestSilence_TimerNoopWhenDetectDisabled(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_DETECT", "0")
	s := directFireSilenceTimer(t, func(s *Session) {
		s.meta.TaskState = proto.TaskStateRunning
		s.altScreen = true
		s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	})
	if s.Info().TaskState == proto.TaskStateWaitingInput {
		t.Fatalf("silence timer must not fire when detection is disabled")
	}
}

func TestSilence_TimerReschedulesWhenOutputTooRecent(t *testing.T) {
	// Set LastOutputAt to now (i.e. NOT silent yet). onSilenceFired should
	// re-arm the timer rather than transition state.
	var fireCount int32
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "30")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Unix() // not silent
	// instrument: wrap onSilenceFired via a sentinel field set by it
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	time.Sleep(120 * time.Millisecond)
	// Push LastOutputAt back in time so the NEXT check will see "silent"
	s.mu.Lock()
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.mu.Unlock()
	// Wait again — the rescheduled timer should now flip us.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("after delay + back-dated LastOutputAt, silence timer should flip; got %q", s.Info().TaskState)
	}
	_ = atomic.LoadInt32(&fireCount) // suppress unused-import for atomic if removed
}
```

> NOTE: the `sync/atomic` import is only used by the last test's sentinel. If you drop that test or simplify it, also drop the import.

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestSilence_Timer -v
```
Expected: FAIL — `rescheduleSilenceTimerLocked` undefined.

- [ ] **Step 3: Implement the two helpers**

Add to `internal/session/session.go`, after the existing `looksLikeWaitingInput` function (around line 780):

```go
// rescheduleSilenceTimerLocked arms (or re-arms) the per-session silence
// timer. Caller must hold s.mu.Lock(). Stops any existing timer first; only
// arms a new one when the session is currently running, in alt-screen, not
// closed, and detection is enabled. Callers reach here after every output
// chunk, after applyOSC133Locked, and from updateTerminalState's tail.
func (s *Session) rescheduleSilenceTimerLocked() {
	if s.silenceTimer != nil {
		s.silenceTimer.Stop()
		s.silenceTimer = nil
	}
	if !s.silenceDetectEnabled {
		return
	}
	if s.closed {
		return
	}
	if s.meta.TaskState != proto.TaskStateRunning {
		return
	}
	if !s.altScreen {
		return
	}
	d := time.Duration(s.silenceThresholdMS) * time.Millisecond
	if d <= 0 {
		return
	}
	s.silenceTimer = time.AfterFunc(d, s.onSilenceFired)
}

// onSilenceFired runs in the timer's own goroutine after the configured
// silence threshold has elapsed since the last output. It takes the lock
// and re-checks every guard — state, altScreen, closed, and the actual
// silence duration — because anything could have changed between arming
// and firing. If the session is still genuinely silent in an alt-screen
// running task, flip to waiting_input, bump AttentionAt, mark the
// transition as "from silence", and broadcast META. If it isn't silent
// long enough yet (e.g. output arrived after the timer was scheduled but
// before LastOutputAt updates raced), simply re-arm and let the next fire
// settle it.
func (s *Session) onSilenceFired() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if !s.silenceDetectEnabled {
		s.mu.Unlock()
		return
	}
	if s.meta.TaskState != proto.TaskStateRunning {
		s.mu.Unlock()
		return
	}
	if !s.altScreen {
		s.mu.Unlock()
		return
	}
	now := time.Now()
	idleSec := now.Unix() - s.meta.LastOutputAt
	if idleSec*1000 < s.silenceThresholdMS {
		// Not silent long enough; re-arm.
		s.rescheduleSilenceTimerLocked()
		s.mu.Unlock()
		return
	}
	s.meta.TaskState = proto.TaskStateWaitingInput
	s.meta.AttentionAt = now.Unix()
	s.waitingFromSilence = true
	s.mu.Unlock()
	s.broadcastCurrentMeta()
}
```

- [ ] **Step 4: Run, expect pass**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run TestSilence_Timer -v
```
Expected: PASS — 4/4.

- [ ] **Step 5: Run the whole package**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/session/session.go internal/session/silence_test.go
git commit -m "session: rescheduleSilenceTimerLocked + onSilenceFired helpers"
```

---

## Task 3: Wire into updateTerminalState (output-restores-running + tail reschedule)

Now connects the helpers to the existing output path. Two changes inside `updateTerminalState`: restore from heuristic `waiting_input` when output arrives, and call `rescheduleSilenceTimerLocked` at the tail so every output chunk re-arms.

**Files:**
- Modify: `internal/session/session.go` (`updateTerminalState`, lines ~575-605)
- Test: `internal/session/silence_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/session/silence_test.go`:

```go
func TestSilence_OutputRestoresRunningFromSilenceWaiting(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	// Drive into the heuristic waiting state.
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	// Wait for the flip.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup precondition: expected waiting_input, got %q", s.Info().TaskState)
	}
	att := s.Info().AttentionAt
	if att == 0 {
		t.Fatalf("setup precondition: expected AttentionAt to be bumped")
	}
	// Now arrive output. State should flip back to running; AttentionAt MUST
	// remain at the bumped value (the spec is explicit on this).
	s.updateTerminalState([]byte("hello"))
	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatalf("expected output to restore running; got %q", s.Info().TaskState)
	}
	if s.waitingFromSilence {
		t.Fatalf("waitingFromSilence should be cleared after restore")
	}
	if s.Info().AttentionAt != att {
		t.Fatalf("AttentionAt should not roll back on restore; was %d, now %d", att, s.Info().AttentionAt)
	}
}

func TestSilence_KeywordWaitingNotRestoredByOutput(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_DETECT", "0") // isolate the keyword path
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	// Trigger keyword-based waiting_input.
	s.updateTerminalState([]byte("Continue? [y/N] "))
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup: expected keyword to flip to waiting_input")
	}
	if s.waitingFromSilence {
		t.Fatalf("keyword path must not set waitingFromSilence")
	}
	// More output arrives. State should NOT auto-restore.
	s.updateTerminalState([]byte("more text"))
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("keyword waiting_input must persist across output; got %q", s.Info().TaskState)
	}
}
```

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run "TestSilence_OutputRestoresRunningFromSilenceWaiting|TestSilence_KeywordWaitingNotRestoredByOutput" -v
```
Expected: FAIL — output does not restore running yet.

- [ ] **Step 3: Wire updateTerminalState**

In `internal/session/session.go`, locate the `updateTerminalState` function (around line 572) and modify the body. The current body is:

```go
func (s *Session) updateTerminalState(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	now := time.Now()
	if s.meta.TaskState == "" {
		s.meta.TaskState = proto.TaskStateIdle
		changed = true
	}
	s.meta.LastOutputAt = now.Unix()
	if s.applyOSC133Locked(data, now) {
		changed = true
	} else if s.meta.TaskState != proto.TaskStateRunning && looksLikeWaitingInput(data) && s.meta.TaskState != proto.TaskStateWaitingInput {
		s.meta.TaskState = proto.TaskStateWaitingInput
		s.meta.AttentionAt = now.Unix()
		changed = true
	}
	// ... termTail / altScreen scan ...
	return changed
}
```

Insert the silence-restore block **after** the OSC 133 + keyword branch but **before** the termTail block — and call `rescheduleSilenceTimerLocked` at the very tail. Find the line `s.altScreen = scanAltScreenMode(s.altScreen, data)` and (a) make sure all `altScreen` updates run first, then (b) add the silence-restore logic and the reschedule.

Replace the function body with:

```go
func (s *Session) updateTerminalState(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	now := time.Now()
	if s.meta.TaskState == "" {
		s.meta.TaskState = proto.TaskStateIdle
		changed = true
	}
	s.meta.LastOutputAt = now.Unix()
	if s.applyOSC133Locked(data, now) {
		changed = true
	} else if s.meta.TaskState != proto.TaskStateRunning && looksLikeWaitingInput(data) && s.meta.TaskState != proto.TaskStateWaitingInput {
		s.meta.TaskState = proto.TaskStateWaitingInput
		s.meta.AttentionAt = now.Unix()
		changed = true
	}
	const tailLen = 32
	prevTail := s.termTail
	if len(prevTail) > 0 {
		prefixLen := tailLen - len(prevTail)
		if prefixLen > len(data) {
			prefixLen = len(data)
		}
		prefix := append(append([]byte(nil), prevTail...), data[:prefixLen]...)
		s.altScreen = scanAltScreenMode(s.altScreen, prefix)
	}
	s.altScreen = scanAltScreenMode(s.altScreen, data)
	s.termTail = appendTrailingBytes(s.termTail[:0], prevTail, data, tailLen)

	// Silence heuristic: output arriving while we were heuristic-waiting
	// restores running. AttentionAt is intentionally NOT rolled back (see
	// 2026-06-07 spec §6). Existing keyword-based waiting_input is left
	// alone because waitingFromSilence == false.
	if s.waitingFromSilence && s.meta.TaskState == proto.TaskStateWaitingInput {
		s.meta.TaskState = proto.TaskStateRunning
		s.waitingFromSilence = false
		changed = true
	}
	// Always reschedule at the tail. The helper itself decides whether to
	// arm based on the post-update state + altScreen + detect-enabled flag.
	s.rescheduleSilenceTimerLocked()
	return changed
}
```

- [ ] **Step 4: Run, expect pass**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run "TestSilence_OutputRestoresRunningFromSilenceWaiting|TestSilence_KeywordWaitingNotRestoredByOutput" -v
```
Expected: PASS — 2/2.

- [ ] **Step 5: Run the whole package**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/session/session.go internal/session/silence_test.go
git commit -m "session: wire silence heuristic into updateTerminalState (restore + reschedule)"
```

---

## Task 4: OSC 133 D + Close cancel the timer and clear the flag

`applyOSC133Locked('D')` is the high-confidence "command finished" event; it must stop the timer and clear `waitingFromSilence`. `Close()` must stop the timer to avoid leaking goroutines / races with the closed channel.

**Files:**
- Modify: `internal/session/session.go` (`applyOSC133Locked` ~line 660 'D' case; `Close()` ~line 1043)
- Test: `internal/session/silence_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/session/silence_test.go`:

```go
func TestSilence_OSC133DOverridesSilenceWaiting(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.meta.CommandStartedAt = time.Now().Unix() // satisfy 'D' early-return guard
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		s.mu.RLock()
		state := s.meta.TaskState
		s.mu.RUnlock()
		if state == proto.TaskStateWaitingInput {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if s.Info().TaskState != proto.TaskStateWaitingInput {
		t.Fatalf("setup: expected silence flip; got %q", s.Info().TaskState)
	}
	// OSC 133 D arrives. State should win regardless and clear waitingFromSilence.
	s.updateTerminalState(osc("D;0"))
	if s.Info().TaskState != proto.TaskStateCompleted {
		t.Fatalf("expected D to flip to completed; got %q", s.Info().TaskState)
	}
	if s.waitingFromSilence {
		t.Fatalf("waitingFromSilence should be cleared after D")
	}
}

func TestSilence_CloseStopsTimer(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "50")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	s.Close()
	// Wait past the threshold; the (already-stopped) timer must not flip state.
	time.Sleep(200 * time.Millisecond)
	if s.Info().TaskState == proto.TaskStateWaitingInput {
		t.Fatalf("Close() should have stopped the silence timer")
	}
}
```

- [ ] **Step 2: Run, expect failure**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run "TestSilence_OSC133DOverridesSilenceWaiting|TestSilence_CloseStopsTimer" -v
```
Expected: FAIL — OSC D doesn't clear `waitingFromSilence`; Close doesn't stop the timer.

- [ ] **Step 3: Clear in OSC D**

In `internal/session/session.go`, inside `applyOSC133Locked` at the `case 'D':` branch — the existing code already does:

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
			if s.meta.TaskState != state {
				s.meta.TaskState = state
				changed = true
			}
			// ... existing duration/exit/summary/AttentionAt block ...
			changed = true
```

Inside the same `case 'D':` block, just BEFORE the trailing `changed = true`, add:

```go
			if s.waitingFromSilence {
				s.waitingFromSilence = false
				changed = true
			}
			if s.silenceTimer != nil {
				s.silenceTimer.Stop()
				s.silenceTimer = nil
			}
```

NOTE: the existing 'D' guard skips the body when `state != running && CommandStartedAt == 0`. Our silence flip set state to `waiting_input` while leaving `CommandStartedAt` from the earlier OSC C, so the guard passes (`CommandStartedAt != 0`). Good.

- [ ] **Step 4: Stop timer in Close**

In `internal/session/session.go`, modify `Close()`:

```go
func (s *Session) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	if s.silenceTimer != nil {
		s.silenceTimer.Stop()
		s.silenceTimer = nil
	}
	subs := s.subs
	s.subs = nil
	close(s.inbound)
	s.mu.Unlock()
	for sub := range subs {
		sub.close()
	}
}
```

- [ ] **Step 5: Run, expect pass**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/ -run "TestSilence_OSC133DOverridesSilenceWaiting|TestSilence_CloseStopsTimer" -v
```
Expected: PASS — 2/2.

- [ ] **Step 6: Run the whole package**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./internal/session/
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/session/session.go internal/session/silence_test.go
git commit -m "session: stop silence timer + clear waitingFromSilence on OSC D + Close"
```

---

## Task 5: Race detector + monotonic AttentionAt

Add one stress test for the data race detector and one for the "repeat silence is monotone" requirement from spec §7. These exercise the lock discipline and the spec's monotonicity guarantee.

**Files:**
- Test: `internal/session/silence_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/session/silence_test.go`:

```go
func TestSilence_RepeatSilenceIsMonotone(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "40")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && s.Info().TaskState != proto.TaskStateWaitingInput {
		time.Sleep(5 * time.Millisecond)
	}
	firstAtt := s.Info().AttentionAt
	if firstAtt == 0 {
		t.Fatalf("setup: expected silence flip to bump AttentionAt")
	}
	// Pretend a full second passes (use a back-dated LastOutputAt so the
	// next silence fire sees a fresh "now"). Need at least 1s so that
	// AttentionAt (unix seconds) is strictly greater.
	time.Sleep(1100 * time.Millisecond)
	// Output arrives → restore to running.
	s.updateTerminalState([]byte("x"))
	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatalf("expected restore to running; got %q", s.Info().TaskState)
	}
	// Re-prime silence: back-date LastOutputAt and let the timer fire again.
	s.mu.Lock()
	s.meta.LastOutputAt = time.Now().Add(-10 * time.Second).Unix()
	s.rescheduleSilenceTimerLocked()
	s.mu.Unlock()
	deadline = time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) && s.Info().TaskState != proto.TaskStateWaitingInput {
		time.Sleep(5 * time.Millisecond)
	}
	secondAtt := s.Info().AttentionAt
	if secondAtt <= firstAtt {
		t.Fatalf("second silence AttentionAt must be > first; %d vs %d", secondAtt, firstAtt)
	}
}

func TestSilence_NoRaceUnderConcurrentOutput(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "20")
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.TaskState = proto.TaskStateRunning
	s.altScreen = true
	s.mu.Unlock()
	done := make(chan struct{})
	for i := 0; i < 4; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < 200; j++ {
				s.updateTerminalState([]byte("x"))
			}
		}()
	}
	for i := 0; i < 4; i++ {
		<-done
	}
	s.Close()
}
```

- [ ] **Step 2: Run with race detector**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test -race ./internal/session/ -run TestSilence_ -v
```
Expected: PASS — all silence tests, no race report.

- [ ] **Step 3: Run the whole package with race detector**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test -race ./internal/session/
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/session/silence_test.go
git commit -m "session/test: monotone AttentionAt + race-free concurrent output for silence heuristic"
```

---

## Final verification

- [ ] **Whole module, race detector on:**

```bash
cd /Users/attson/code/github.com.attson/atterm && go test -race ./...
```
Expected: PASS.

- [ ] **Vet:**

```bash
cd /Users/attson/code/github.com.attson/atterm && go vet ./internal/...
```
Expected: clean.

---

## Spec coverage check

| Spec section | Task |
| --- | --- |
| §1 Goal — the AI/TUI silence pain | Tasks 1-5 collectively |
| §2 Architecture (fields + helpers + wiring) | Task 1 (fields/env) + Task 2 (helpers) + Task 3 (wiring) + Task 4 (D + Close) |
| §3 Configuration (env defaults + parsing) | Task 1 |
| §4 State transition table | Tasks 2-4 (one per transition row group) |
| §5 `waitingFromSilence` separation rule | Task 3 (keyword-not-restored test) |
| §6 AttentionAt not rolled back on restore | Task 3 (test explicitly asserts) |
| §6 AttentionAt monotone across silence cycles | Task 5 |
| §7 Testing matrix (most rows) | Distributed across Tasks 1-5 |
| §7 race detector | Task 5 + final verification |
| §8 Migration / compatibility (no protocol change) | Implicit — no protocol/migration touched |

Out of scope for this plan (per spec §9): foreground process introspection, multi-signal scoring, cross-session correlation, Web Push deduplication — all noted as v2 candidates.

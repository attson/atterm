# Hook-Driven task_state Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Claude Code and Codex report `running` / `waiting_input` through their hook systems, so AI sessions stop having their state guessed from output silence.

**Architecture:** A session latches into "hook-driven" the first time a hook event arrives for it; from then on the silence timer never arms again for that session and `task_state` comes only from hook events. The latch clears when the AI command exits (OSC 133 D) or the session closes. The existing hook ingress (`atterm-hook` → `HookServer`) grows a second consumer alongside the Feishu one.

**Tech Stack:** Go 1.x, `internal/session`, `desktop/feishu` (hook ingress), `desktop/hookinstall`, `cmd/atterm-hook`.

**Spec:** `docs/superpowers/specs/2026-08-16-hook-driven-task-state-design.md`

## Global Constraints

- No new `TaskState` enum values. Only `proto.TaskStateRunning` and `proto.TaskStateWaitingInput` are produced by hooks.
- Hook events apply only while `s.meta.Type == SessionTypeAI`. A late event must never move a plain shell.
- `waiting_input` from a hook bumps `meta.AttentionAt`, exactly as the silence path does today — downstream unread/notification semantics are unchanged.
- No feature flag, no migration, no backward-compat shim for the old `atterm-hook` argv (`hookinstall` rewrites config on every launch).
- Codex config is written to **user-level** `~/.codex/hooks.json` only (repo-local config does not fire in interactive sessions: openai/codex#17532).
- Comments explain *why*, matching the density of the file being edited. Chinese in user-facing strings only; code and comments in English.

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/session/hookstate.go` (new) | The latch and `ApplyHookState`; the only place hook events touch session state |
| `internal/session/session.go` (modify) | Two new fields on `Session` |
| `internal/session/silence.go` (modify) | One guard: never arm while latched |
| `internal/session/osc133.go` (modify) | Clear the latch on `D` |
| `desktop/feishu/hook_state.go` (new) | Maps a hook payload to a task state, per agent kind |
| `desktop/feishu/hook_server.go` (modify) | Call the new sink alongside the existing two paths |
| `desktop/relay_host_hookstate.go` (new) | `relayHost` implements the sink: uuid → `*session.Session` |
| `cmd/atterm-hook/main.go` (modify) | `--agent` flag replaces the hardcoded kind |
| `desktop/hookinstall/codex.go` (new) | Write/repair `~/.codex/hooks.json` |
| `desktop/hookinstall/install.go` (modify) | Call the codex writer alongside the claude one |

---

### Task 1: The latch and ApplyHookState

**Files:**
- Create: `internal/session/hookstate.go`
- Modify: `internal/session/session.go` (struct fields, near `waitingFromSilence`)
- Test: `internal/session/hookstate_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func (s *Session) ApplyHookState(next string)`, `func (s *Session) HookDriven() bool`, `func (s *Session) clearHookDrivenLocked()`.

- [ ] **Step 1: Write the failing test**

```go
package session

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func aiSession(t *testing.T) *Session {
	t.Helper()
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.Type = SessionTypeAI
	s.meta.TaskState = proto.TaskStateRunning
	s.mu.Unlock()
	return s
}

func TestApplyHookState_SetsStateAndLatches(t *testing.T) {
	s := aiSession(t)
	s.ApplyHookState(proto.TaskStateWaitingInput)
	if got := s.Info().TaskState; got != proto.TaskStateWaitingInput {
		t.Fatalf("state = %q, want waiting_input", got)
	}
	if !s.HookDriven() {
		t.Fatal("first hook event must latch the session")
	}
}

func TestApplyHookState_WaitingBumpsAttention(t *testing.T) {
	s := aiSession(t)
	s.ApplyHookState(proto.TaskStateWaitingInput)
	if s.Info().AttentionAt == 0 {
		t.Fatal("waiting_input must raise attention, like the silence path")
	}
}

func TestApplyHookState_RunningDoesNotBumpAttention(t *testing.T) {
	s := aiSession(t)
	s.ApplyHookState(proto.TaskStateRunning)
	if s.Info().AttentionAt != 0 {
		t.Fatal("running is not a call for the user")
	}
}

// A hook event that arrives after the CLI has exited must not drag an ordinary
// shell into an AI state.
func TestApplyHookState_IgnoredForNonAISessions(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})
	s.mu.Lock()
	s.meta.Type = SessionTypeShell
	s.meta.TaskState = proto.TaskStateIdle
	s.mu.Unlock()

	s.ApplyHookState(proto.TaskStateRunning)
	if s.Info().TaskState != proto.TaskStateIdle {
		t.Fatal("hook events must not touch a shell session")
	}
	if s.HookDriven() {
		t.Fatal("a rejected event must not latch")
	}
}

func TestApplyHookState_IgnoresUnknownState(t *testing.T) {
	s := aiSession(t)
	s.ApplyHookState("banana")
	if s.Info().TaskState != proto.TaskStateRunning {
		t.Fatal("unknown states must be dropped, not stored")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestApplyHookState -v`
Expected: compile failure — `s.ApplyHookState undefined`.

- [ ] **Step 3: Add the fields**

In `internal/session/session.go`, next to `waitingFromSilence`:

```go
	// hookDriven latches once an AI client's hook has reported state for this
	// session. From then on the silence heuristic is off for good (see
	// rescheduleSilenceTimerLocked): the client tells us when a turn starts and
	// ends, so inferring it from output gaps can only add noise. Cleared when
	// the AI command exits (OSC 133 D) and the session goes back to being a
	// plain shell.
	hookDriven bool
```

- [ ] **Step 4: Write the implementation**

Create `internal/session/hookstate.go`:

```go
package session

import (
	"time"

	"github.com/attson/atterm/internal/proto"
)

// ApplyHookState records a task state reported by the AI client's own hooks.
//
// This is the authoritative path for AI sessions. OSC 133 cannot help here —
// claude and codex render inline and run as a single long shell command, so no
// command boundary is ever reported between turns — and inferring the state
// from output silence cannot tell "answered you, now waiting" apart from
// "thinking, or running a quiet tool".
//
// Only running and waiting_input are accepted: those are the two states a turn
// moves between. Anything else is a caller bug and is dropped rather than
// stored, so a malformed payload cannot invent a state.
func (s *Session) ApplyHookState(next string) {
	if next != proto.TaskStateRunning && next != proto.TaskStateWaitingInput {
		return
	}
	s.mu.Lock()
	// Only AI sessions. A hook event can arrive just after the CLI exited, and
	// moving the shell that took its place would be worse than losing the event.
	if s.meta.Type != SessionTypeAI {
		s.mu.Unlock()
		return
	}
	prev := s.meta.TaskState
	s.hookDriven = true
	// The heuristic's own bookkeeping is now dead weight; clear it so a later
	// unlatch starts from a clean slate rather than a stale accumulator.
	s.waitingFromSilence = false
	s.resetSilenceRestoreLocked()
	s.resetSilenceActivityBurstLocked()
	if s.silenceTimer != nil {
		s.silenceTimer.Stop()
		s.silenceTimer = nil
	}
	changed := prev != next
	s.meta.TaskState = next
	if next == proto.TaskStateWaitingInput {
		// Same contract as the silence path: waiting means the session wants
		// the user, which is what drives unread, the widget and the cards.
		s.meta.AttentionAt = time.Now().Unix()
	}
	if changed {
		s.fireTaskStateLocked(prev, next, TaskMeta{})
	}
	metaHook := s.onMetaChanged
	s.mu.Unlock()

	if changed {
		s.broadcastCurrentMeta()
		if metaHook != nil {
			metaHook()
		}
	}
}

// HookDriven reports whether this session's state comes from its AI client's
// hooks rather than from the silence heuristic.
func (s *Session) HookDriven() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hookDriven
}

// clearHookDrivenLocked returns the session to heuristic control. Caller holds
// s.mu.
func (s *Session) clearHookDrivenLocked() {
	s.hookDriven = false
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/session/ -run TestApplyHookState -v`
Expected: all five PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/session/hookstate.go internal/session/hookstate_test.go internal/session/session.go
git commit -m "feat(session): accept task state reported by AI client hooks"
```

---

### Task 2: The silence heuristic yields to the latch

**Files:**
- Modify: `internal/session/silence.go` (`rescheduleSilenceTimerLocked`)
- Test: `internal/session/hookstate_test.go` (append)

**Interfaces:**
- Consumes: `s.hookDriven` from Task 1.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

```go
// The whole point of the latch: once the client reports state, the timer that
// used to guess it must never arm again for this session.
func TestHookDriven_SilenceTimerNeverArms(t *testing.T) {
	t.Setenv("ATTERM_TASK_SILENCE_THRESHOLD_MS", "40")
	s := aiSession(t)
	s.ApplyHookState(proto.TaskStateRunning)

	s.mu.Lock()
	s.lastOutputMono = time.Now().Add(-10 * time.Second)
	s.rescheduleSilenceTimerLocked()
	armed := s.silenceTimer != nil
	s.mu.Unlock()
	if armed {
		t.Fatal("silence timer must not arm for a hook-driven session")
	}

	time.Sleep(150 * time.Millisecond)
	if got := s.Info().TaskState; got != proto.TaskStateRunning {
		t.Fatalf("state drifted to %q — the heuristic is still running", got)
	}
}
```

Add `"time"` to the test file's imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestHookDriven_SilenceTimerNeverArms -v`
Expected: FAIL — "silence timer must not arm for a hook-driven session".

- [ ] **Step 3: Add the guard**

In `internal/session/silence.go`, inside `rescheduleSilenceTimerLocked`, immediately after the `s.closed` check:

```go
	if s.hookDriven {
		silenceDebugLocked(s, "arm-skip: hook-driven")
		return
	}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/ -v`
Expected: PASS, including every pre-existing `TestSilence_*` case.

- [ ] **Step 5: Commit**

```bash
git add internal/session/silence.go internal/session/hookstate_test.go
git commit -m "feat(session): stop arming the silence timer once hooks drive a session"
```

---

### Task 3: OSC 133 D returns the session to the heuristic

**Files:**
- Modify: `internal/session/osc133.go` (case `'D'`, beside the existing `waitingFromSilence` reset)
- Test: `internal/session/hookstate_test.go` (append)

**Interfaces:**
- Consumes: `clearHookDrivenLocked` from Task 1.
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

```go
// When the AI CLI exits, the shell underneath takes over and there are no more
// hooks. The session has to go back to the heuristic or it would freeze at
// whatever the last hook said.
func TestHookDriven_ClearedWhenCommandExits(t *testing.T) {
	s := aiSession(t)
	s.mu.Lock()
	s.meta.CommandStartedAt = time.Now().Unix()
	s.mu.Unlock()
	s.ApplyHookState(proto.TaskStateRunning)

	s.updateTerminalState([]byte("\x1b]133;D;0\x07"))

	if s.HookDriven() {
		t.Fatal("command exit must return the session to the heuristic")
	}
	if got := s.Info().TaskState; got != proto.TaskStateCompleted {
		t.Fatalf("state = %q, want completed", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestHookDriven_ClearedWhenCommandExits -v`
Expected: FAIL — "command exit must return the session to the heuristic".

- [ ] **Step 3: Clear the latch on D**

In `internal/session/osc133.go`, in `case 'D':`, next to the existing `if s.waitingFromSilence { … }` block:

```go
			// The AI CLI just exited; its hooks are gone with it. Hand the
			// session back to the heuristic, or it would sit on whatever state
			// the last hook reported forever.
			s.clearHookDrivenLocked()
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/session/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/session/osc133.go internal/session/hookstate_test.go
git commit -m "feat(session): return a session to the silence heuristic when its AI command exits"
```

---

### Task 4: Map hook payloads to task states

**Files:**
- Create: `desktop/feishu/hook_state.go`
- Test: `desktop/feishu/hook_state_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func TaskStateForHook(agentKind string, hookInput json.RawMessage) (string, bool)`.

- [ ] **Step 1: Write the failing test**

```go
package feishu

import (
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/proto"
)

func TestTaskStateForHook_ClaudeCode(t *testing.T) {
	cases := map[string]string{
		`{"hook_event_name":"UserPromptSubmit"}`: proto.TaskStateRunning,
		`{"hook_event_name":"PreToolUse"}`:       proto.TaskStateRunning,
		`{"hook_event_name":"PostToolUse"}`:      proto.TaskStateRunning,
		`{"hook_event_name":"Stop"}`:             proto.TaskStateWaitingInput,
		`{"hook_event_name":"Notification"}`:     proto.TaskStateWaitingInput,
	}
	for raw, want := range cases {
		got, ok := TaskStateForHook("claude-code", json.RawMessage(raw))
		if !ok || got != want {
			t.Fatalf("%s → (%q,%v), want (%q,true)", raw, got, ok, want)
		}
	}
}

// Codex uses the same event vocabulary, plus PermissionRequest where claude
// says Notification.
func TestTaskStateForHook_Codex(t *testing.T) {
	cases := map[string]string{
		`{"hook_event_name":"UserPromptSubmit"}`:  proto.TaskStateRunning,
		`{"hook_event_name":"PreToolUse"}`:        proto.TaskStateRunning,
		`{"hook_event_name":"PostToolUse"}`:       proto.TaskStateRunning,
		`{"hook_event_name":"Stop"}`:              proto.TaskStateWaitingInput,
		`{"hook_event_name":"PermissionRequest"}`: proto.TaskStateWaitingInput,
	}
	for raw, want := range cases {
		got, ok := TaskStateForHook("codex", json.RawMessage(raw))
		if !ok || got != want {
			t.Fatalf("%s → (%q,%v), want (%q,true)", raw, got, ok, want)
		}
	}
}

// Turn-scoped events already cover the state machine; decoding more of them
// would only add ways for the two to disagree.
func TestTaskStateForHook_IgnoresEverythingElse(t *testing.T) {
	for _, raw := range []string{
		`{"hook_event_name":"SessionStart"}`,
		`{"hook_event_name":"SubagentStop"}`,
		`{"hook_event_name":"PreCompact"}`,
		`{"hook_event_name":""}`,
		`{`,
	} {
		if got, ok := TaskStateForHook("claude-code", json.RawMessage(raw)); ok {
			t.Fatalf("%s → %q, want ignored", raw, got)
		}
	}
}

func TestTaskStateForHook_UnknownAgent(t *testing.T) {
	if _, ok := TaskStateForHook("aider", json.RawMessage(`{"hook_event_name":"Stop"}`)); ok {
		t.Fatal("unknown agent kinds must be ignored")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./desktop/feishu/ -run TestTaskStateForHook -v`
Expected: compile failure — `undefined: TaskStateForHook`.

- [ ] **Step 3: Write the implementation**

Create `desktop/feishu/hook_state.go`:

```go
package feishu

import (
	"encoding/json"

	"github.com/attson/atterm/internal/proto"
)

// hookStateByEvent maps the hook events that mark a turn boundary onto the
// task state they imply. Claude Code and Codex share the vocabulary except for
// the permission prompt, so one table serves both and the agent kind only
// gates which name means "the client is asking you something".
var hookStateByEvent = map[string]string{
	"UserPromptSubmit": proto.TaskStateRunning,
	"PreToolUse":       proto.TaskStateRunning,
	"PostToolUse":      proto.TaskStateRunning,
	"Stop":             proto.TaskStateWaitingInput,
}

// hookAttentionEvent names the "needs you" event per agent.
var hookAttentionEvent = map[string]string{
	"claude-code": "Notification",
	"codex":       "PermissionRequest",
}

// TaskStateForHook returns the task state a hook payload implies, and whether
// it implies one at all. Events outside the turn boundary — SessionStart,
// subagent and compaction events — are deliberately ignored: the turn-scoped
// ones already describe the state machine completely, and decoding more would
// only add ways for the two sources to disagree.
func TaskStateForHook(agentKind string, hookInput json.RawMessage) (string, bool) {
	attention, known := hookAttentionEvent[agentKind]
	if !known {
		return "", false
	}
	var p struct {
		HookEventName string `json:"hook_event_name"`
	}
	if err := json.Unmarshal(hookInput, &p); err != nil {
		return "", false
	}
	if p.HookEventName == attention {
		return proto.TaskStateWaitingInput, true
	}
	state, ok := hookStateByEvent[p.HookEventName]
	return state, ok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./desktop/feishu/ -run TestTaskStateForHook -v`
Expected: all four PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/feishu/hook_state.go desktop/feishu/hook_state_test.go
git commit -m "feat(feishu): map claude and codex hook events onto task states"
```

---

### Task 5: Route hook events into the session

**Files:**
- Modify: `desktop/feishu/hook_server.go` (interface + `ServeHTTP`)
- Create: `desktop/relay_host_hookstate.go`
- Test: `desktop/feishu/hook_server_test.go` (append)

**Interfaces:**
- Consumes: `TaskStateForHook` (Task 4), `Session.ApplyHookState` (Task 1).
- Produces: `type TaskStateSink interface { ApplyHookTaskState(sid uuid.UUID, state string) }`, `func (h *HookServer) SetTaskStateSink(sink TaskStateSink)`, `func (h *relayHost) ApplyHookTaskState(sid uuid.UUID, state string)`.

- [ ] **Step 1: Write the failing test**

```go
type fakeTaskStateSink struct {
	calls []string
}

func (f *fakeTaskStateSink) ApplyHookTaskState(sid uuid.UUID, state string) {
	f.calls = append(f.calls, state)
}

func TestHookServer_ForwardsTaskState(t *testing.T) {
	sink := &fakeTaskStateSink{}
	sid := uuid.New()
	h := NewHookServer(nil, allowAllSessions{})
	h.SetTaskStateSink(sink)

	body := `{"session_id":"` + sid.String() + `","agent_kind":"claude-code",` +
		`"hook_input":{"hook_event_name":"Stop"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/atterm-hook/notify", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1234"
	h.ServeHTTP(rec, req)

	if len(sink.calls) != 1 || sink.calls[0] != proto.TaskStateWaitingInput {
		t.Fatalf("sink calls = %v, want [waiting_input]", sink.calls)
	}
}

// Events with no state meaning must not reach the sink at all.
func TestHookServer_IgnoresNonStateEvents(t *testing.T) {
	sink := &fakeTaskStateSink{}
	h := NewHookServer(nil, allowAllSessions{})
	h.SetTaskStateSink(sink)

	body := `{"session_id":"` + uuid.New().String() + `","agent_kind":"claude-code",` +
		`"hook_input":{"hook_event_name":"SessionStart"}}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/atterm-hook/notify", strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:1234"
	h.ServeHTTP(rec, req)

	if len(sink.calls) != 0 {
		t.Fatalf("sink calls = %v, want none", sink.calls)
	}
}
```

Add a minimal `allowAllSessions` helper if the file does not already have one:

```go
type allowAllSessions struct{}

func (allowAllSessions) Exists(uuid.UUID) bool         { return true }
func (allowAllSessions) Inject(uuid.UUID, string) error { return nil }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./desktop/feishu/ -run TestHookServer_ForwardsTaskState -v`
Expected: compile failure — `h.SetTaskStateSink undefined`.

- [ ] **Step 3: Add the sink to HookServer**

In `desktop/feishu/hook_server.go`, beside the existing `WaitingDispatcher` interface:

```go
// TaskStateSink receives task states reported by an AI client's hooks. Kept as
// a narrow interface for the same reason SessionLookup is: this package
// terminates the HTTP request, it does not know what a session is.
type TaskStateSink interface {
	ApplyHookTaskState(sid uuid.UUID, state string)
}
```

Add the field to the struct (beside `sessions`), guarded by the existing `mu`:

```go
	taskSink TaskStateSink
```

And the setter plus accessor, next to `SetSuspectCallback`:

```go
// SetTaskStateSink registers where hook-derived task states go. Swappable for
// the same reason the dispatcher is: the listener outlives service rebuilds.
func (h *HookServer) SetTaskStateSink(sink TaskStateSink) {
	h.mu.Lock()
	h.taskSink = sink
	h.mu.Unlock()
}

func (h *HookServer) taskStateSink() TaskStateSink {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.taskSink
}
```

- [ ] **Step 4: Call it from ServeHTTP**

In `ServeHTTP`, after the `adapter, ok := LookupHookAdapter(req.AgentKind)` block and before the WaitingInput path:

```go
	// Task-state path, independent of the two Feishu paths below: the same
	// event can carry state without being a waiting-input card or a streaming
	// chunk. This is the authoritative source for AI sessions — see
	// docs/superpowers/specs/2026-08-16-hook-driven-task-state-design.md.
	if state, ok := TaskStateForHook(req.AgentKind, req.HookInput); ok {
		if sink := h.taskStateSink(); sink != nil {
			sink.ApplyHookTaskState(sid, state)
		}
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./desktop/feishu/ -v`
Expected: PASS.

- [ ] **Step 6: Implement the sink on relayHost**

Create `desktop/relay_host_hookstate.go`:

```go
package main

import (
	"github.com/google/uuid"
)

// ApplyHookTaskState satisfies feishu.TaskStateSink: it resolves the session id
// the hook reported against the local registry and hands the state to the
// session model, which owns the latch that switches the silence heuristic off.
//
// Unknown ids are dropped silently. A hook can outlive its session by a moment
// (the CLI exits, the PTY is reaped, a last Stop is still in flight) and that
// is not worth a log line on every AI turn.
func (h *relayHost) ApplyHookTaskState(sid uuid.UUID, state string) {
	if h.server == nil {
		return
	}
	sess, ok := h.server.Registry().Get(sid)
	if !ok {
		return
	}
	sess.ApplyHookState(state)
}
```

- [ ] **Step 7: Wire it at startup**

In `desktop/app_feishu.go`, in `startFeishu`, next to the existing `svc.HookServer().SetSuspectCallback(...)`:

```go
	// The hook listener is started unconditionally (it predates Feishu being
	// optional), which is what lets task state ride the same ingress whether or
	// not the user has a Feishu binding.
	svc.HookServer().SetTaskStateSink(a.host)
```

- [ ] **Step 8: Run the full Go suite**

Run: `go build ./... && go test ./internal/... ./desktop/...`
Expected: PASS (`internal/logging`'s `TestNoBareStdlibLogCalls` may fail if a stale `.worktrees/` copy exists — unrelated).

- [ ] **Step 9: Commit**

```bash
git add desktop/feishu/hook_server.go desktop/feishu/hook_server_test.go desktop/relay_host_hookstate.go desktop/app_feishu.go
git commit -m "feat(desktop): route hook-reported task state into the session model"
```

---

### Task 6: `atterm-hook --agent`

**Files:**
- Modify: `cmd/atterm-hook/main.go` (the hardcoded `AgentKind: "claude-code"` at ~line 64)
- Test: `cmd/atterm-hook/main_test.go` (append)

**Interfaces:**
- Consumes: nothing.
- Produces: `func agentKindFromArgs(args []string) string`.

- [ ] **Step 1: Write the failing test**

```go
func TestAgentKindFromArgs(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--agent", "codex"}, "codex"},
		{[]string{"--agent=codex"}, "codex"},
		{[]string{"--agent", "claude-code"}, "claude-code"},
		{nil, "claude-code"},        // installers before this flag existed
		{[]string{"--agent"}, "claude-code"}, // missing value
	}
	for _, c := range cases {
		if got := agentKindFromArgs(c.args); got != c.want {
			t.Fatalf("agentKindFromArgs(%v) = %q, want %q", c.args, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/atterm-hook/ -run TestAgentKindFromArgs -v`
Expected: compile failure — `undefined: agentKindFromArgs`.

- [ ] **Step 3: Write the implementation**

In `cmd/atterm-hook/main.go`:

```go
const defaultAgentKind = "claude-code"

// agentKindFromArgs reads --agent from argv. The installer always passes it
// explicitly; the default only covers a settings.json written by an older
// build that has not been rewritten yet, which happens on the next launch.
func agentKindFromArgs(args []string) string {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, "--agent="); ok && v != "" {
			return v
		}
		if a == "--agent" && i+1 < len(args) && args[i+1] != "" {
			return args[i+1]
		}
	}
	return defaultAgentKind
}
```

Replace the hardcoded field:

```go
		AgentKind: agentKindFromArgs(os.Args[1:]),
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/atterm-hook/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/atterm-hook/main.go cmd/atterm-hook/main_test.go
git commit -m "feat(atterm-hook): take the agent kind from --agent instead of hardcoding it"
```

---

### Task 7: Install codex hooks

**Files:**
- Create: `desktop/hookinstall/codex.go`
- Modify: `desktop/hookinstall/install.go` (call the new writer where the claude one is called)
- Test: `desktop/hookinstall/codex_test.go`

**Interfaces:**
- Consumes: the materialized binary path from `ensureBinary` (already used by the claude writer).
- Produces: `func writeCodexHooks(binPath string) error`, `func codexHooksPath() (string, error)`.

- [ ] **Step 1: Write the failing test**

```go
package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteCodexHooks_CreatesEveryEvent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := writeCodexHooks("/tmp/atterm-hook"); err != nil {
		t.Fatalf("writeCodexHooks: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	if err != nil {
		t.Fatalf("read hooks.json: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v", err)
	}
	for _, event := range []string{
		"UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "PermissionRequest",
	} {
		if _, ok := got[event]; !ok {
			t.Fatalf("hooks.json missing %s: %s", event, raw)
		}
	}
	if !bytes.Contains(raw, []byte("--agent")) {
		t.Fatalf("command must carry --agent codex: %s", raw)
	}
}

// Runs on every launch, so it has to be idempotent.
func TestWriteCodexHooks_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".codex", "hooks.json")

	if err := writeCodexHooks("/tmp/atterm-hook"); err != nil {
		t.Fatalf("first write: %v", err)
	}
	first, _ := os.ReadFile(path)
	if err := writeCodexHooks("/tmp/atterm-hook"); err != nil {
		t.Fatalf("second write: %v", err)
	}
	second, _ := os.ReadFile(path)
	if !bytes.Equal(first, second) {
		t.Fatal("second write changed the file")
	}
}

// The user's own hooks live in the same file; ours are additive.
func TestWriteCodexHooks_PreservesForeignEntries(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{"SessionStart":[{"hooks":[{"type":"command","command":"/usr/bin/true"}]}]}`
	if err := os.WriteFile(filepath.Join(dir, "hooks.json"), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := writeCodexHooks("/tmp/atterm-hook"); err != nil {
		t.Fatalf("writeCodexHooks: %v", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "hooks.json"))
	if !bytes.Contains(raw, []byte("/usr/bin/true")) {
		t.Fatalf("clobbered the user's own hook: %s", raw)
	}
}
```

Add `"bytes"` to the test imports.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./desktop/hookinstall/ -run TestWriteCodexHooks -v`
Expected: compile failure — `undefined: writeCodexHooks`.

- [ ] **Step 3: Write the implementation**

Create `desktop/hookinstall/codex.go`:

```go
package hookinstall

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// codexHookEvents are the events whose payloads carry a task-state meaning.
// Everything else codex offers (SessionStart, subagent and compaction events)
// is deliberately left alone — see the design doc.
var codexHookEvents = []string{
	"UserPromptSubmit",
	"PreToolUse",
	"PostToolUse",
	"Stop",
	"PermissionRequest",
}

// codexHooksPath is the user-level hooks file. Repo-local .codex/config.toml is
// not an option: it does not fire in interactive sessions (openai/codex#17532).
func codexHooksPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex", "hooks.json"), nil
}

type codexHookHandler struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type codexHookEntry struct {
	Matcher string             `json:"matcher,omitempty"`
	Hooks   []codexHookHandler `json:"hooks"`
}

// writeCodexHooks makes ~/.codex/hooks.json name our binary for every event we
// read. It merges rather than replaces: the file is the user's, and codex loads
// every entry in it.
func writeCodexHooks(binPath string) error {
	path, err := codexHooksPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	doc := map[string][]codexHookEntry{}
	if raw, err := os.ReadFile(path); err == nil && len(raw) > 0 {
		// A hand-broken file is not a reason to refuse to install; start clean.
		_ = json.Unmarshal(raw, &doc)
	}

	want := codexHookEntry{
		Hooks: []codexHookHandler{{Type: "command", Command: binPath + " --agent codex"}},
	}
	for _, event := range codexHookEvents {
		kept := doc[event][:0:0]
		for _, entry := range doc[event] {
			if len(entry.Hooks) == 1 && isAttermHookCommand(entry.Hooks[0].Command) {
				continue // ours from a previous run; replaced below
			}
			kept = append(kept, entry)
		}
		doc[event] = append(kept, want)
	}

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(out, '\n'), 0o600)
}

// isAttermHookCommand recognises our own entry across binary hash changes, so a
// version bump replaces the old line instead of stacking a second one.
func isAttermHookCommand(cmd string) bool {
	return strings.Contains(cmd, "atterm-hook")
}
```

Add `"strings"` to the imports.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./desktop/hookinstall/ -run TestWriteCodexHooks -v`
Expected: all three PASS.

- [ ] **Step 5: Call it from Install**

In `desktop/hookinstall/install.go`, after the claude settings write succeeds, add the codex write. A codex failure must not fail the whole install — a user may not have codex at all:

```go
	if err := writeCodexHooks(binPath); err != nil {
		logging.Warn("hookinstall", "codex hooks: %v", err)
	}
```

- [ ] **Step 6: Run the package suite**

Run: `go test ./desktop/hookinstall/ -v`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/hookinstall/codex.go desktop/hookinstall/codex_test.go desktop/hookinstall/install.go
git commit -m "feat(hookinstall): install atterm-hook into codex as well as claude"
```

---

### Task 8: Claude install passes `--agent` too

**Files:**
- Modify: `desktop/hookinstall/settings.go` (the command string written into `~/.claude/settings.json`)
- Test: `desktop/hookinstall/settings_test.go` (append)

**Interfaces:**
- Consumes: `agentKindFromArgs` behaviour from Task 6 (the flag it parses).
- Produces: nothing new.

- [ ] **Step 1: Write the failing test**

```go
// Both installers name their agent explicitly. Leaving claude on the implicit
// default would make the two paths differ for no reason, and the default only
// exists to cover configs written before the flag.
func TestClaudeHookCommandNamesItsAgent(t *testing.T) {
	cmd := attermHookCommand("/tmp/atterm-hook")
	if !strings.Contains(cmd, "--agent claude-code") {
		t.Fatalf("command = %q, want --agent claude-code", cmd)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./desktop/hookinstall/ -run TestClaudeHookCommandNamesItsAgent -v`
Expected: FAIL (or compile failure if `attermHookCommand` does not exist yet — introduce it in step 3).

- [ ] **Step 3: Write the implementation**

In `desktop/hookinstall/settings.go`, extract the command string into one helper and use it everywhere the entry is built:

```go
// attermHookCommand is the command line written into settings.json. The agent
// kind is explicit so the receiving end never has to guess which CLI called it.
func attermHookCommand(binPath string) string {
	return binPath + " --agent claude-code"
}
```

- [ ] **Step 4: Run the package suite**

Run: `go test ./desktop/hookinstall/ -v`
Expected: PASS, including the pre-existing install/repair tests.

- [ ] **Step 5: Commit**

```bash
git add desktop/hookinstall/settings.go desktop/hookinstall/settings_test.go
git commit -m "feat(hookinstall): name the agent explicitly in the claude hook command"
```

---

### Task 9: Mutation check and docs

**Files:**
- Modify: `docs/superpowers/specs/2026-08-16-hook-driven-task-state-design.md` (Status line)
- Modify: `docs/spec/architecture.md` (component matrix row for `internal/session`)

**Interfaces:**
- Consumes: everything above.
- Produces: nothing.

- [ ] **Step 1: Prove the latch test bites**

Temporarily delete the `if s.hookDriven { … }` guard added in Task 2, then run:

Run: `go test ./internal/session/ -run TestHookDriven_SilenceTimerNeverArms -v`
Expected: FAIL. If it passes, the test is not actually pinning the behaviour — fix the test before restoring the guard.

Restore the guard with `git checkout internal/session/silence.go` **only if nothing else in that file is uncommitted**; otherwise re-add the lines by hand.

- [ ] **Step 2: Prove the non-AI guard bites**

Temporarily delete the `if s.meta.Type != SessionTypeAI` guard in `ApplyHookState`, then run:

Run: `go test ./internal/session/ -run TestApplyHookState_IgnoredForNonAISessions -v`
Expected: FAIL. Restore the guard.

- [ ] **Step 3: Update the spec status**

Change `Status: Design` to `Status: Implemented` in the design doc.

- [ ] **Step 4: Record the new authority in the architecture doc**

In `docs/spec/architecture.md`, in the component matrix row for `session`, append to the 职责 cell:

```
；AI 会话的 task_state 由客户端 hook 驱动（hookDriven 闩锁关闭静默启发式）
```

- [ ] **Step 5: Run everything**

Run: `go build ./... && go test ./internal/... ./desktop/... ./cmd/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add docs/superpowers/specs/2026-08-16-hook-driven-task-state-design.md docs/spec/architecture.md
git commit -m "docs: record hook-driven task state as implemented"
```

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
|---|---|
| 权威归属（方案 A） | 1, 2 |
| 事件映射表 | 4 |
| 闩锁生命周期 | 1 (set), 3 (clear on D), 1 (close is covered — `Close()` drops the session) |
| 入口与安装 / `--agent` | 6, 7, 8 |
| 边界：late 事件 | 1 (`SessionTypeAI` guard) |
| 边界：mirror / 远程 | no code — state rides ANNOUNCE unchanged; nothing to build |
| 测试要求（含变异验证） | 1–8 per task, 9 for the two mutation checks |

**Placeholder scan:** none — every step carries the code it needs.

**Type consistency:** `ApplyHookState(next string)` (Task 1) is called by `ApplyHookTaskState(sid, state string)` (Task 5) which is named by `TaskStateSink` (Task 5) and produced by `TaskStateForHook(agentKind string, hookInput json.RawMessage) (string, bool)` (Task 4). `writeCodexHooks(binPath string) error` (Task 7) and `attermHookCommand(binPath string) string` (Task 8) both take the path `ensureBinary` returns.

**Known gap:** the session-close path clears nothing explicitly because `Close()` removes the session from the registry, so a later hook event finds no session and is dropped in Task 5's sink. No task needed.

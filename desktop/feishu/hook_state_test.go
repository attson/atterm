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
			t.Fatalf("%s -> (%q,%v), want (%q,true)", raw, got, ok, want)
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
			t.Fatalf("%s -> (%q,%v), want (%q,true)", raw, got, ok, want)
		}
	}
}

// One agent's attention event is not the other's: claude never sends
// PermissionRequest, and reading it there would be guessing.
func TestTaskStateForHook_AttentionEventIsPerAgent(t *testing.T) {
	if _, ok := TaskStateForHook("claude-code", json.RawMessage(`{"hook_event_name":"PermissionRequest"}`)); ok {
		t.Fatal("claude-code has no PermissionRequest event")
	}
	if _, ok := TaskStateForHook("codex", json.RawMessage(`{"hook_event_name":"Notification"}`)); ok {
		t.Fatal("codex has no Notification event")
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
			t.Fatalf("%s -> %q, want ignored", raw, got)
		}
	}
}

func TestTaskStateForHook_UnknownAgent(t *testing.T) {
	if _, ok := TaskStateForHook("aider", json.RawMessage(`{"hook_event_name":"Stop"}`)); ok {
		t.Fatal("unknown agent kinds must be ignored")
	}
}

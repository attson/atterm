// desktop/feishu/hook_adapter_test.go
package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeCodeAdapter_PreToolUseAskUserQuestion_EmitsWithOptions(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "hook_event_name": "PreToolUse",
	  "session_id": "s-1",
	  "cwd": "/tmp",
	  "tool_name": "AskUserQuestion",
	  "tool_input": {"questions": [
	    {"question": "Deploy now?", "header": "Deploy", "multiSelect": false,
	     "options": [
	       {"label": "Yes, deploy", "description": "ship it"},
	       {"label": "No, wait",   "description": "hold"}
	     ]}
	  ]}
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatalf("expected emit")
	}
	if ev.QuestionText != "Deploy now?" {
		t.Fatalf("QuestionText = %q", ev.QuestionText)
	}
	if len(ev.Options) != 2 || ev.Options[0].Label != "Yes, deploy" || ev.Options[1].Label != "No, wait" {
		t.Fatalf("Options = %+v", ev.Options)
	}
	if !strings.HasPrefix(ev.DedupKey, "claude-code:pretooluse:askuserquestion:") {
		t.Fatalf("unexpected dedup key: %q", ev.DedupKey)
	}
}

func TestClaudeCodeAdapter_PreToolUseAskUserQuestion_OptionsKeptWhenQuestionEmpty(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "hook_event_name": "PreToolUse",
	  "tool_name": "AskUserQuestion",
	  "tool_input": {"questions": [
	    {"question": "", "options": [{"label":"A","description":"a"}]}
	  ]}
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatal("expected emit")
	}
	if len(ev.Options) != 1 || ev.Options[0].Label != "A" {
		t.Fatalf("options dropped: %+v", ev.Options)
	}
	if ev.QuestionText != "Claude is waiting on a question." {
		t.Fatalf("expected placeholder question, got %q", ev.QuestionText)
	}
}

func TestClaudeCodeAdapter_PreToolUseAskUserQuestion_SingleQuestionFallback(t *testing.T) {
	// Older / single-question shape: {question: "..."} at the top of tool_input.
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "hook_event_name": "PreToolUse",
	  "tool_name": "AskUserQuestion",
	  "tool_input": {"question": "Continue refactor? (y/N)"}
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatal("expected emit")
	}
	if !strings.Contains(ev.QuestionText, "Continue refactor?") {
		t.Fatalf("question missing: %q", ev.QuestionText)
	}
	if len(ev.Options) != 0 {
		t.Fatalf("did not expect options: %+v", ev.Options)
	}
}

func TestClaudeCodeAdapter_PreToolUse_OtherTools_Skip(t *testing.T) {
	a := &claudeCodeAdapter{}
	// Even though our hook is registered with matcher=AskUserQuestion, a
	// malformed install or a different matcher could land here. Belt and
	// braces: drop any PreToolUse that isn't AskUserQuestion.
	in := json.RawMessage(`{
	  "hook_event_name": "PreToolUse",
	  "tool_name": "Bash",
	  "tool_input": {"command": "rm -rf /tmp/x"}
	}`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("PreToolUse for non-AskUserQuestion tool must skip")
	}
}

func TestClaudeCodeAdapter_NotificationPermissionPrompt_EmitsMessage(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "hook_event_name": "Notification",
	  "session_id": "s-1",
	  "notification_type": "permission_prompt",
	  "title": "Permission needed",
	  "message": "Claude needs your permission to use Bash"
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatal("expected emit")
	}
	if !strings.Contains(ev.QuestionText, "permission to use Bash") {
		t.Fatalf("QuestionText = %q", ev.QuestionText)
	}
	if len(ev.Options) != 0 {
		t.Fatalf("permission_prompt must not carry Options: %+v", ev.Options)
	}
	if !strings.HasPrefix(ev.DedupKey, "claude-code:notification:permission_prompt:") {
		t.Fatalf("dedup key: %q", ev.DedupKey)
	}
}

func TestClaudeCodeAdapter_NotificationIdlePrompt_Emits(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "hook_event_name": "Notification",
	  "notification_type": "idle_prompt",
	  "message": "Claude is waiting for your input"
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatal("expected emit")
	}
	if ev.QuestionText == "" {
		t.Fatal("QuestionText must not be empty")
	}
}

func TestClaudeCodeAdapter_NotificationEmptyMessage_HasPlaceholder(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "hook_event_name": "Notification",
	  "notification_type": "permission_prompt"
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatal("expected emit")
	}
	if ev.QuestionText == "" {
		t.Fatal("expected placeholder when message missing")
	}
}

func TestClaudeCodeAdapter_NotificationAuthSuccess_Skips(t *testing.T) {
	// auth_success / elicitation_* / unknown notification_type values
	// aren't "waiting on user" — don't render a card.
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "hook_event_name": "Notification",
	  "notification_type": "auth_success",
	  "message": "Logged in"
	}`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("auth_success must not emit")
	}
}

func TestClaudeCodeAdapter_UnknownHookEvent_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion"}`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("PostToolUse must not emit")
	}
}

func TestClaudeCodeAdapter_NoHookEventName_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{"tool_name":"AskUserQuestion"}`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("payload without hook_event_name must not emit")
	}
}

func TestClaudeCodeAdapter_MalformedJSON_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	if _, ok := a.Parse(json.RawMessage(`{`), ""); ok {
		t.Fatalf("malformed input must skip")
	}
}

func TestClaudeCodeAdapter_DedupKeysDiffer_ForDifferentQuestions(t *testing.T) {
	a := &claudeCodeAdapter{}
	mk := func(q string) string {
		in := json.RawMessage(`{
		  "hook_event_name": "PreToolUse",
		  "tool_name": "AskUserQuestion",
		  "tool_input": {"questions": [{"question": "` + q + `", "options": []}]}
		}`)
		ev, _ := a.Parse(in, "")
		return ev.DedupKey
	}
	if mk("A") == mk("B") {
		t.Fatalf("dedup key collided across different questions")
	}
}

func TestRegistryLookup(t *testing.T) {
	a, ok := LookupHookAdapter("claude-code")
	if !ok || a == nil {
		t.Fatalf("claude-code adapter must be registered")
	}
	if _, ok := LookupHookAdapter("nope"); ok {
		t.Fatalf("unknown agent_kind must return ok=false")
	}
}

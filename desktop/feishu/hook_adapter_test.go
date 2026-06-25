// desktop/feishu/hook_adapter_test.go
package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeCodeAdapter_AskUserQuestionEmits(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"idle_prompt", "tool":"AskUserQuestion"},
	  "prompt_id":"p-1",
	  "context": {"tool_input": {"question": "Continue refactor? (y/N)"}}
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatalf("expected emit")
	}
	if !strings.Contains(ev.QuestionText, "Continue refactor?") {
		t.Fatalf("question text missing: %q", ev.QuestionText)
	}
	if ev.DedupKey != "claude-code:p-1" {
		t.Fatalf("dedup key: %q", ev.DedupKey)
	}
}

func TestClaudeCodeAdapter_PermissionPromptEmits(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"permission_prompt"},
	  "prompt_id":"p-2",
	  "context": {
	    "tool_name":"Bash",
	    "tool_input": {"command": "rm -rf node_modules"}
	  }
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatalf("expected emit")
	}
	if !strings.Contains(ev.QuestionText, "Bash") || !strings.Contains(ev.QuestionText, "rm -rf node_modules") {
		t.Fatalf("question text should describe the tool + input: %q", ev.QuestionText)
	}
}

func TestClaudeCodeAdapter_IdleWithoutTool_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"idle_prompt"},
	  "prompt_id":"p-3"
	}`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("idle_prompt without AskUserQuestion must skip (false positive guard)")
	}
}

func TestClaudeCodeAdapter_UnknownMatcher_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"subagent_stop"},
	  "prompt_id":"p-4"
	}`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("unknown matcher must skip")
	}
}

func TestClaudeCodeAdapter_MalformedJSON_Skips(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{`)
	if _, ok := a.Parse(in, ""); ok {
		t.Fatalf("malformed input must skip")
	}
}

func TestClaudeCodeAdapter_DedupFallback(t *testing.T) {
	a := &claudeCodeAdapter{}
	in := json.RawMessage(`{
	  "matcher": {"type":"idle_prompt", "tool":"AskUserQuestion"},
	  "context": {"tool_input": {"question": "fallback question"}}
	}`)
	ev, ok := a.Parse(in, "")
	if !ok {
		t.Fatalf("expected emit")
	}
	if !strings.HasPrefix(ev.DedupKey, "claude-code:hash:") {
		t.Fatalf("expected fallback hash dedup key, got %q", ev.DedupKey)
	}
}

func TestClaudeCodeAdapter_AskUserQuestionOptions(t *testing.T) {
	raw := json.RawMessage(`{
	  "matcher": {"type":"idle_prompt", "tool":"AskUserQuestion"},
	  "prompt_id": "p1",
	  "context": {"tool_input": {"questions": [
	    {"question": "Deploy now?", "header": "Deploy", "multiSelect": false,
	     "options": [
	       {"label": "Yes, deploy", "description": "ship it"},
	       {"label": "No, wait", "description": "hold"}
	     ]}
	  ]}}
	}`)
	ev, ok := (&claudeCodeAdapter{}).Parse(raw, "")
	if !ok {
		t.Fatal("expected emit")
	}
	if ev.QuestionText != "Deploy now?" {
		t.Fatalf("QuestionText = %q", ev.QuestionText)
	}
	if len(ev.Options) != 2 || ev.Options[0].Label != "Yes, deploy" || ev.Options[1].Label != "No, wait" {
		t.Fatalf("Options = %+v", ev.Options)
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

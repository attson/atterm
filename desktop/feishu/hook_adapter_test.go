// desktop/feishu/hook_adapter_test.go
package feishu

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestClaudeCodeParseTurn_UserPrompt(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"UserPromptSubmit","prompt":"fix the bug"}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok {
		t.Fatal("expected ParseTurn to recognize UserPromptSubmit")
	}
	if ev.Kind != TurnUserPrompt {
		t.Errorf("kind = %v, want TurnUserPrompt", ev.Kind)
	}
	if ev.Text != "fix the bug" {
		t.Errorf("text = %q, want %q", ev.Text, "fix the bug")
	}
}

func TestClaudeCodeParseTurn_Stop(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"Stop","assistant_message":"done."}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok {
		t.Fatal("expected ParseTurn to recognize Stop")
	}
	if ev.Kind != TurnAssistantFinal {
		t.Errorf("kind = %v, want TurnAssistantFinal", ev.Kind)
	}
	if ev.Text != "done." {
		t.Errorf("text = %q, want %q", ev.Text, "done.")
	}
}

// claude-code 2.1.x leaves `assistant_message` empty in the Stop hook payload;
// the real reply lives in the transcript JSONL at `transcript_path`. When
// only the transcript path is present, ParseTurn must read the last
// assistant text-content block out of that file. Otherwise the anchor card
// renders 🤖 with no body.
func TestClaudeCodeParseTurn_StopFallsBackToTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	lines := []string{
		`{"type":"user","message":{"role":"user","content":[{"type":"text","text":"hi"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"first reply"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"final reply with the actual answer"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"Stop","assistant_message":"","transcript_path":"` + path + `"}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok || ev.Kind != TurnAssistantFinal {
		t.Fatalf("got (%v, %v); want TurnAssistantFinal", ev, ok)
	}
	if ev.Text != "final reply with the actual answer" {
		t.Errorf("text = %q; want last assistant text from transcript", ev.Text)
	}
}

// When neither assistant_message nor a readable transcript_path is present,
// ParseTurn still emits the Stop event (with empty Text) so downstream UI
// can render the 🤖 marker. Better an empty marker than a swallowed event.
func TestClaudeCodeParseTurn_StopEmitsEvenWhenTranscriptMissing(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"Stop","assistant_message":"","transcript_path":"/does/not/exist.jsonl"}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok || ev.Kind != TurnAssistantFinal {
		t.Fatalf("got (%v, %v); want TurnAssistantFinal", ev, ok)
	}
	if ev.Text != "" {
		t.Errorf("text = %q; want empty when transcript unreadable", ev.Text)
	}
}

// Malformed JSONL lines must not abort the scan — claude has been known to
// emit partial lines mid-write. The reader skips garbage and returns the
// best assistant text it could find.
func TestClaudeCodeParseTurn_StopSurvivesGarbageLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t.jsonl")
	body := strings.Join([]string{
		`not even json`,
		`{"message":{"role":"assistant","content":[{"type":"text","text":"good text"}]}}`,
		`{partial`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"Stop","assistant_message":"","transcript_path":"` + path + `"}`)
	ev, _ := a.ParseTurn(raw, "")
	if ev.Text != "good text" {
		t.Errorf("text = %q; want %q", ev.Text, "good text")
	}
}

func TestClaudeCodeParseTurn_PreToolUse(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok {
		t.Fatal("expected ParseTurn to recognize PreToolUse")
	}
	if ev.Kind != TurnToolStart {
		t.Errorf("kind = %v, want TurnToolStart", ev.Kind)
	}
	if ev.ToolName != "Bash" {
		t.Errorf("toolName = %q", ev.ToolName)
	}
}

func TestClaudeCodeParseTurn_PostToolUse(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_response":"output here"}`)
	ev, ok := a.ParseTurn(raw, "")
	if !ok {
		t.Fatal("expected ParseTurn to recognize PostToolUse")
	}
	if ev.Kind != TurnToolEnd {
		t.Errorf("kind = %v, want TurnToolEnd", ev.Kind)
	}
}

func TestClaudeCodeParseTurn_AskUserQuestionStaysOnAskPath(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"PreToolUse","tool_name":"AskUserQuestion","tool_input":{}}`)
	_, ok := a.ParseTurn(raw, "")
	if ok {
		t.Errorf("ParseTurn should not emit a TurnEvent for AskUserQuestion (existing AskQuestion path handles it)")
	}
}

func TestClaudeCodeParseTurn_UserPromptEmpty(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"UserPromptSubmit","prompt":""}`)
	_, ok := a.ParseTurn(raw, "")
	if ok {
		t.Error("ParseTurn should return (zero, false) for UserPromptSubmit with empty prompt")
	}
}

func TestClaudeCodeParseTurn_AskUserQuestionPostToolUseSkip(t *testing.T) {
	a := &claudeCodeAdapter{}
	raw := json.RawMessage(`{"hook_event_name":"PostToolUse","tool_name":"AskUserQuestion","tool_response":"unused"}`)
	_, ok := a.ParseTurn(raw, "")
	if ok {
		t.Error("ParseTurn should not emit a TurnEvent for AskUserQuestion PostToolUse")
	}
}

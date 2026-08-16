package feishu

import (
	"encoding/json"

	"github.com/attson/atterm/internal/proto"
)

// hookStateByEvent maps the hook events that mark a turn boundary onto the task
// state they imply. Claude Code and Codex share the vocabulary except for the
// permission prompt, so one table serves both and the agent kind only gates
// which name means "the client is asking you something".
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

// TaskStateForHook returns the task state a hook payload implies, and whether it
// implies one at all.
//
// Events outside the turn boundary — SessionStart, subagent and compaction
// events — are deliberately ignored: the turn-scoped ones already describe the
// state machine completely, and decoding more would only add ways for the two
// sources to disagree.
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

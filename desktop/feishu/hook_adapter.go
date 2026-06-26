package feishu

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// WaitingInputEvent is the normalized payload a HookAdapter emits when
// the underlying agent (claude-code, codex, …) signals it is waiting
// on the user. The dispatcher uses this to render + send a Feishu card.
type WaitingInputEvent struct {
	QuestionText string
	Options      []QuestionOption // populated only for AskUserQuestion PreToolUse events
	DedupKey     string
}

// QuestionOption is one selectable answer from an AskUserQuestion tool call.
type QuestionOption struct {
	Label       string
	Description string
}

// HookAdapter parses an agent-specific hook payload into a normalized
// WaitingInputEvent.
type HookAdapter interface {
	AgentKind() string
	Parse(hookInput json.RawMessage, hookVersion string) (WaitingInputEvent, bool)
}

var hookAdapters = map[string]HookAdapter{
	"claude-code": &claudeCodeAdapter{},
}

// LookupHookAdapter returns the registered adapter for an agent_kind.
func LookupHookAdapter(kind string) (HookAdapter, bool) {
	a, ok := hookAdapters[kind]
	return a, ok
}

// claudeCodeAdapter parses claude-code's real hook payloads. Two shapes:
//
//   - PreToolUse(AskUserQuestion): {hook_event_name:"PreToolUse",
//     tool_name:"AskUserQuestion", tool_input:{questions:[{question,options}]}}.
//     This is the ONLY place we learn about AskUserQuestion — claude-code's
//     Notification hook does NOT fire for this tool (anthropics/claude-code#13830).
//
//   - Notification: {hook_event_name:"Notification",
//     notification_type:"permission_prompt"|"idle_prompt"|…, message, title}.
type claudeCodeAdapter struct{}

func (*claudeCodeAdapter) AgentKind() string { return "claude-code" }

type ccHookPayload struct {
	HookEventName    string          `json:"hook_event_name"`
	NotificationType string          `json:"notification_type"`
	Message          string          `json:"message"`
	Title            string          `json:"title"`
	ToolName         string          `json:"tool_name"`
	ToolInput        json.RawMessage `json:"tool_input"`
}

func (a *claudeCodeAdapter) Parse(raw json.RawMessage, _ string) (WaitingInputEvent, bool) {
	var p ccHookPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return WaitingInputEvent{}, false
	}

	switch p.HookEventName {
	case "PreToolUse":
		if p.ToolName != "AskUserQuestion" {
			return WaitingInputEvent{}, false
		}
		q, opts := extractAskUserQuestion(p.ToolInput)
		// Emit even if the question text is empty, as long as we have
		// options to render — the card title carries the intent.
		if q == "" && len(opts) == 0 {
			return WaitingInputEvent{}, false
		}
		if q == "" {
			q = "Claude is waiting on a question."
		}
		return WaitingInputEvent{
			QuestionText: q,
			Options:      opts,
			DedupKey:     dedupKey("pretooluse:askuserquestion", p.ToolInput),
		}, true

	case "Notification":
		switch p.NotificationType {
		case "permission_prompt", "idle_prompt":
			msg := strings.TrimSpace(p.Message)
			if msg == "" {
				msg = "Claude is waiting on you."
			}
			return WaitingInputEvent{
				QuestionText: msg,
				DedupKey:     dedupKey("notification:"+p.NotificationType, []byte(p.Message)),
			}, true
		default:
			// auth_success, elicitation_*, anything unknown — not a
			// waiting-on-user signal, so don't render a card.
			return WaitingInputEvent{}, false
		}

	default:
		return WaitingInputEvent{}, false
	}
}

// dedupKey produces a stable per-event key. claude-code does not give
// hooks a stable identifier across retries, so we hash the content. The
// 8-byte prefix is enough to disambiguate concurrent prompts in the same
// session within the dedup window.
func dedupKey(scope string, content []byte) string {
	sum := sha256.Sum256(content)
	return "claude-code:" + scope + ":" + hex.EncodeToString(sum[:8])
}

func extractAskUserQuestion(rawToolInput json.RawMessage) (string, []QuestionOption) {
	if len(rawToolInput) == 0 {
		return "", nil
	}
	var p struct {
		Question  string `json:"question"`
		Questions []struct {
			Question string `json:"question"`
			Options  []struct {
				Label       string `json:"label"`
				Description string `json:"description"`
			} `json:"options"`
		} `json:"questions"`
	}
	if err := json.Unmarshal(rawToolInput, &p); err != nil {
		return "", nil
	}
	if len(p.Questions) > 0 {
		q0 := p.Questions[0]
		opts := make([]QuestionOption, 0, len(q0.Options))
		for _, o := range q0.Options {
			opts = append(opts, QuestionOption{Label: o.Label, Description: o.Description})
		}
		return q0.Question, opts
	}
	if p.Question != "" {
		return p.Question, nil
	}
	return "", nil
}

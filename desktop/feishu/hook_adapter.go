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

// TurnKind enumerates the AI streaming event types the chunker consumes.
type TurnKind int

const (
	TurnUserPrompt     TurnKind = iota // user submitted a prompt
	TurnAssistantFinal                 // assistant finished a turn
	TurnToolStart                      // tool call about to start
	TurnToolEnd                        // tool call returned
)

// TurnEvent is the normalized AI-streaming event a HookAdapter emits via
// ParseTurn. Separate from WaitingInputEvent because the consumer (outbound
// chunker) needs different shape than the AskQuestion card renderer.
type TurnEvent struct {
	Kind     TurnKind
	Text     string // for UserPrompt / AssistantFinal
	ToolName string // for ToolStart / ToolEnd
	ToolBody string // for ToolEnd (tool response preview)
}

// ParseTurn extends HookAdapter for the AI streaming path. Returns (event, true)
// when the hook is a per-turn streaming signal; (zero, false) otherwise.
// AskUserQuestion is intentionally skipped — the existing Parse method handles
// it via the AskQuestion card, which is a separate UX from the anchor stream.
func (a *claudeCodeAdapter) ParseTurn(raw json.RawMessage, _ string) (TurnEvent, bool) {
	var p struct {
		HookEventName    string          `json:"hook_event_name"`
		ToolName         string          `json:"tool_name"`
		ToolInput        json.RawMessage `json:"tool_input"`
		ToolResponse     string          `json:"tool_response"`
		Prompt           string          `json:"prompt"`
		AssistantMessage string          `json:"assistant_message"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return TurnEvent{}, false
	}
	switch p.HookEventName {
	case "UserPromptSubmit":
		if p.Prompt == "" {
			return TurnEvent{}, false
		}
		return TurnEvent{Kind: TurnUserPrompt, Text: p.Prompt}, true
	case "Stop":
		if p.AssistantMessage == "" {
			return TurnEvent{Kind: TurnAssistantFinal, Text: ""}, true
		}
		return TurnEvent{Kind: TurnAssistantFinal, Text: p.AssistantMessage}, true
	case "PreToolUse":
		if p.ToolName == "AskUserQuestion" {
			return TurnEvent{}, false
		}
		return TurnEvent{Kind: TurnToolStart, ToolName: p.ToolName}, true
	case "PostToolUse":
		if p.ToolName == "AskUserQuestion" {
			return TurnEvent{}, false
		}
		return TurnEvent{Kind: TurnToolEnd, ToolName: p.ToolName, ToolBody: p.ToolResponse}, true
	default:
		return TurnEvent{}, false
	}
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

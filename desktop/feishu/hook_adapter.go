package feishu

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// WaitingInputEvent is the normalized payload a HookAdapter emits when
// the underlying agent (claude-code, codex, …) signals it is waiting
// on the user. The dispatcher uses this to render + send a Feishu card.
type WaitingInputEvent struct {
	QuestionText string
	Options      []QuestionOption // populated only for AskUserQuestion idle_prompt
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

// claudeCodeAdapter parses claude-code's NotificationHookInput schema.
type claudeCodeAdapter struct{}

func (*claudeCodeAdapter) AgentKind() string { return "claude-code" }

type ccNotificationHookInput struct {
	Matcher  ccMatcher       `json:"matcher"`
	PromptID string          `json:"prompt_id"`
	Context  json.RawMessage `json:"context"`
}

type ccMatcher struct {
	Type string `json:"type"`
	Tool string `json:"tool"`
}

func (a *claudeCodeAdapter) Parse(raw json.RawMessage, _ string) (WaitingInputEvent, bool) {
	var in ccNotificationHookInput
	if err := json.Unmarshal(raw, &in); err != nil {
		return WaitingInputEvent{}, false
	}

	switch in.Matcher.Type {
	case "permission_prompt":
		q := summarizePermissionContext(in.Context)
		return mkEvent(in, q), true
	case "idle_prompt":
		if in.Matcher.Tool != "AskUserQuestion" {
			return WaitingInputEvent{}, false
		}
		q, opts := extractAskUserQuestion(in.Context)
		if q == "" {
			q = "Claude is waiting on a question."
		}
		ev := mkEvent(in, q)
		ev.Options = opts
		return ev, true
	default:
		return WaitingInputEvent{}, false
	}
}

func mkEvent(in ccNotificationHookInput, q string) WaitingInputEvent {
	dedup := "claude-code:" + in.PromptID
	if in.PromptID == "" {
		sum := sha256.Sum256([]byte(q))
		dedup = "claude-code:hash:" + hex.EncodeToString(sum[:8])
	}
	return WaitingInputEvent{
		QuestionText: q,
		DedupKey:     dedup,
	}
}

func summarizePermissionContext(ctx json.RawMessage) string {
	var p struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	}
	if err := json.Unmarshal(ctx, &p); err != nil {
		return "Claude wants permission for an action."
	}
	tool := p.ToolName
	if tool == "" {
		tool = "Tool"
	}
	argsLine := compactJSONOneLine(p.ToolInput)
	if argsLine == "" {
		return tool + " requested approval."
	}
	return fmt.Sprintf("%s wants to:\n%s", tool, argsLine)
}

func extractAskUserQuestion(ctx json.RawMessage) (string, []QuestionOption) {
	var p struct {
		ToolInput struct {
			Question  string `json:"question"`
			Questions []struct {
				Question string `json:"question"`
				Options  []struct {
					Label       string `json:"label"`
					Description string `json:"description"`
				} `json:"options"`
			} `json:"questions"`
		} `json:"tool_input"`
	}
	if err := json.Unmarshal(ctx, &p); err == nil {
		if len(p.ToolInput.Questions) > 0 {
			q0 := p.ToolInput.Questions[0]
			opts := make([]QuestionOption, 0, len(q0.Options))
			for _, o := range q0.Options {
				opts = append(opts, QuestionOption{Label: o.Label, Description: o.Description})
			}
			if q0.Question != "" {
				return q0.Question, opts
			}
		}
		if p.ToolInput.Question != "" {
			return p.ToolInput.Question, nil
		}
	}
	var fallback struct {
		ToolInput json.RawMessage `json:"tool_input"`
	}
	_ = json.Unmarshal(ctx, &fallback)
	return compactJSONOneLine(fallback.ToolInput), nil
}

func compactJSONOneLine(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	out, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(out)
}

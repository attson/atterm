package feishu

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
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
	// Options is populated when Kind=TurnAssistantFinal carries an
	// AskUserQuestion payload — the dispatcher uses this to swap the anchor
	// card's button row from the default keystrokes (^C/^D/Esc/Enter/结束) to
	// clickable option buttons. Empty for normal Stop / other events.
	Options []string
}

// ParseTurn extends HookAdapter for the AI streaming path. Returns (event, true)
// when the hook is a per-turn streaming signal; (zero, false) otherwise.
// AskUserQuestion is intentionally skipped — the existing Parse method handles
// it via the AskQuestion card, which is a separate UX from the anchor stream.
func (a *claudeCodeAdapter) ParseTurn(raw json.RawMessage, _ string) (TurnEvent, bool) {
	var p struct {
		HookEventName        string          `json:"hook_event_name"`
		ToolName             string          `json:"tool_name"`
		ToolInput            json.RawMessage `json:"tool_input"`
		ToolResponse         string          `json:"tool_response"`
		Prompt               string          `json:"prompt"`
		LastAssistantMessage string          `json:"last_assistant_message"`
		TranscriptPath       string          `json:"transcript_path"`
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
		// claude-code 2.1.x puts the final assistant reply in
		// `last_assistant_message` directly. Older / partial payloads may omit
		// it; the transcript JSONL is the documented authoritative source so
		// we fall back to scanning that.
		text := p.LastAssistantMessage
		if text == "" && p.TranscriptPath != "" {
			text = lastAssistantTextFromTranscript(p.TranscriptPath)
		}
		return TurnEvent{Kind: TurnAssistantFinal, Text: text}, true
	case "PreToolUse":
		if p.ToolName == "AskUserQuestion" {
			// Anchor mirrors every question + options block (❓ prefix). A
			// single-question payload also hands the option labels to the
			// dispatcher so the anchor's button row swaps to clickable
			// buttons; multi-question payloads leave Options empty because
			// one row of buttons can't represent N radio groups — users
			// answer those via the separate AskQuestion card or by typing
			// in the input box. The dedicated AskQuestion card always ships
			// too as a backup path.
			questions := extractAllAskUserQuestions(p.ToolInput)
			if len(questions) == 0 {
				return TurnEvent{}, false
			}
			var text string
			for qi, q := range questions {
				if qi > 0 {
					text += "\n\n"
				}
				text += "❓ " + q.Question
				for i, opt := range q.Options {
					text += fmt.Sprintf("\n%d. %s", i+1, opt.Label)
					if opt.Description != "" {
						text += " — " + opt.Description
					}
				}
			}
			var labels []string
			if len(questions) == 1 {
				labels = make([]string, 0, len(questions[0].Options))
				for _, opt := range questions[0].Options {
					labels = append(labels, opt.Label)
				}
			}
			return TurnEvent{Kind: TurnAssistantFinal, Text: text, Options: labels}, true
		}
		return TurnEvent{Kind: TurnToolStart, ToolName: p.ToolName}, true
	case "PostToolUse":
		if p.ToolName == "AskUserQuestion" {
			// The user's pick lands as a UserPromptSubmit shortly after; no
			// need to noise up the anchor with a duplicate "tool ended".
			return TurnEvent{}, false
		}
		return TurnEvent{Kind: TurnToolEnd, ToolName: p.ToolName, ToolBody: p.ToolResponse}, true
	default:
		return TurnEvent{}, false
	}
}

// lastAssistantTextFromTranscript scans a claude-code transcript JSONL and
// returns the text from the latest assistant entry's last text-type content
// block. Returns "" on any failure (missing file, unreadable, no qualifying
// entry, partial last line) — the Stop handler treats every error as
// "fall back to empty body" since the 🤖 marker is the floor, not the goal.
//
// Each JSONL line looks like:
//
//	{"message":{"role":"assistant","content":[{"type":"text","text":"..."}, {"type":"tool_use",...}]}}
//
// Assistant entries can interleave text + tool_use blocks; tool_use rows on
// their own are skipped, and within a multi-block assistant entry the last
// text block wins (matches what the user actually sees in the TUI).
func lastAssistantTextFromTranscript(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	// claude assistant text can be large; bump the scan buffer well past the
	// 64KB default. 4MB matches the platform-wide message ceiling.
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var lastText string
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var obj struct {
			Message struct {
				Role    string            `json:"role"`
				Content []json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &obj) != nil {
			continue // skip partial / garbage lines
		}
		if obj.Message.Role != "assistant" {
			continue
		}
		for _, c := range obj.Message.Content {
			var elem struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if json.Unmarshal(c, &elem) != nil {
				continue
			}
			if elem.Type == "text" && elem.Text != "" {
				lastText = elem.Text
			}
		}
	}
	return lastText
}

func extractAskUserQuestion(rawToolInput json.RawMessage) (string, []QuestionOption) {
	qs := extractAllAskUserQuestions(rawToolInput)
	if len(qs) == 0 {
		return "", nil
	}
	return qs[0].Question, qs[0].Options
}

// AskUserQuestionEntry is one question + its options from a
// PreToolUse(AskUserQuestion) payload. AskUserQuestion may bundle multiple
// (radio + multi-select) in a single hook call — each becomes one entry.
type AskUserQuestionEntry struct {
	Question string
	Options  []QuestionOption
}

func extractAllAskUserQuestions(rawToolInput json.RawMessage) []AskUserQuestionEntry {
	if len(rawToolInput) == 0 {
		return nil
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
		return nil
	}
	if len(p.Questions) > 0 {
		out := make([]AskUserQuestionEntry, 0, len(p.Questions))
		for _, q := range p.Questions {
			opts := make([]QuestionOption, 0, len(q.Options))
			for _, o := range q.Options {
				opts = append(opts, QuestionOption{Label: o.Label, Description: o.Description})
			}
			out = append(out, AskUserQuestionEntry{Question: q.Question, Options: opts})
		}
		return out
	}
	if p.Question != "" {
		return []AskUserQuestionEntry{{Question: p.Question}}
	}
	return nil
}

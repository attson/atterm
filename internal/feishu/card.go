// internal/feishu/card.go
package feishu

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CommandFinishedInput mirrors the relevant fields from
// internal/webhook/CommandFinished (and webpush's). When SealedBody is
// non-empty we render the E2EE-safe variant — no exit code, no label,
// no elapsed, only a generic "see your device" body.
type CommandFinishedInput struct {
	SessionID   uuid.UUID
	ExitCode    int
	ElapsedMS   int
	Label       string
	LastCommand string // for the retry button; empty disables retry
	SealedBody  []byte

	// Context fields rendered ONLY in the non-sealed variant. They MUST NOT
	// leak into the sealed card (E2EE: title/cwd/output are plaintext the
	// relay is structurally unable to read).
	SessionTitle string
	Cwd          string
	FailureCount int    // consecutive failures for this session; >1 → shown
	OutputTail   string // last lines of scrollback; empty disables summary
}

func (in CommandFinishedInput) sealed() bool { return len(in.SealedBody) > 0 }

type WaitingInputInput struct {
	SessionID      uuid.UUID
	IdleForSeconds int
	QuestionText   string // optional; populated by hook adapters
	SessionTitle   string // optional; session friendly name
	Cwd            string // optional; working directory
	CurrentCommand string // optional; the command the agent is running
	RecentOutput   string // optional; last lines of output (ANSI-stripped)
}

type AckUpdateInput struct {
	Event     string // "command_finished" or "waiting_input"
	SessionID string
}

// Card is the JSON message body we POST to im/v1/messages — wrapped
// in {msg_type:"interactive", card:{...}}.
type Card struct {
	MsgType string         `json:"msg_type"`
	Card    map[string]any `json:"card"`
}

// AckResponse is the inline reply for a card.action.trigger callback —
// Feishu reads the body and updates the original card if `card` is set.
type AckResponse struct {
	Toast map[string]any `json:"toast,omitempty"`
	Card  map[string]any `json:"card"`
}

func deepLink(sessionID uuid.UUID) string {
	return "atterm://session/" + sessionID.String()
}

func formatElapsed(ms int) string {
	if ms < 60_000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	totalSec := ms / 1000
	return fmt.Sprintf("%dm%02ds", totalSec/60, totalSec%60)
}

func RenderCommandFinishedCard(in CommandFinishedInput) Card {
	if in.sealed() {
		return Card{
			MsgType: "interactive",
			Card: map[string]any{
				"config": map[string]any{"wide_screen_mode": true},
				"header": map[string]any{
					"title":    map[string]any{"tag": "plain_text", "content": "命令完成（仅本机可见）"},
					"template": "grey",
				},
				"elements": []any{
					map[string]any{
						"tag":  "div",
						"text": map[string]any{"tag": "lark_md", "content": "命令详情仅本机可见 · 用本机端打开查看"},
					},
					actionRowOf(jumpButton(in.SessionID), ackButton(in.SessionID, "command_finished")),
				},
			},
		}
	}
	template := "green"
	if in.ExitCode != 0 {
		template = "red"
	}
	label := in.Label
	if label == "" {
		label = "command"
	}
	buttons := []map[string]any{jumpButton(in.SessionID)}
	if in.ExitCode != 0 && in.LastCommand != "" {
		buttons = append(buttons, injectButton(in.SessionID, "重试", in.LastCommand+"\n"))
	}
	buttons = append(buttons, ackButton(in.SessionID, "command_finished"))

	elements := []any{}

	// Context line: session title and/or cwd. Skip when both are empty so we
	// never emit a dangling " · " separator.
	if ctx := contextLine(in.SessionTitle, in.Cwd); ctx != "" {
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": ctx},
		})
	}

	statusLine := fmt.Sprintf("**`%s`** 退出码 `%d` · 用时 %s", label, in.ExitCode, formatElapsed(in.ElapsedMS))
	if in.ExitCode != 0 && in.FailureCount > 1 {
		statusLine += fmt.Sprintf(" · 连续第 %d 次失败", in.FailureCount)
	}
	elements = append(elements, map[string]any{
		"tag":  "div",
		"text": map[string]any{"tag": "lark_md", "content": statusLine},
	})

	if in.OutputTail != "" {
		body, truncated := truncateQuestion(in.OutputTail)
		content := "```\n" + body + "\n```"
		if truncated {
			content += "\n_（已截断）_"
		}
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": content},
		})
	}

	elements = append(elements, actionRowOf(buttons...))

	return Card{
		MsgType: "interactive",
		Card: map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "命令完成"},
				"template": template,
			},
			"elements": elements,
		},
	}
}

// contextLine renders the session-identity line for the command-finished card.
// Returns "" when both inputs are empty so callers can omit the div entirely.
func contextLine(title, cwd string) string {
	title = strings.TrimSpace(title)
	cwd = strings.TrimSpace(cwd)
	switch {
	case title != "" && cwd != "":
		return fmt.Sprintf("`%s` · %s", title, cwd)
	case title != "":
		return fmt.Sprintf("`%s`", title)
	case cwd != "":
		return cwd
	default:
		return ""
	}
}

func RenderWaitingInputCard(in WaitingInputInput) Card {
	var elements []any
	if ctx := contextLine(in.SessionTitle, in.Cwd); ctx != "" {
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": ctx},
		})
	}
	header := fmt.Sprintf("Agent 在等待你回复（已闲置 %ds）", in.IdleForSeconds)
	if cmd := strings.TrimSpace(in.CurrentCommand); cmd != "" {
		header += fmt.Sprintf("\n当前命令：`%s`", cmd)
	}
	elements = append(elements, map[string]any{
		"tag":  "div",
		"text": map[string]any{"tag": "lark_md", "content": header},
	})
	// Prefer an explicit agent question (hook path); otherwise show recent
	// output so the user knows what the session was doing when it went idle.
	if q := strings.TrimSpace(in.QuestionText); q != "" {
		body, truncated := truncateQuestion(q)
		content := "```\n" + body + "\n```"
		if truncated {
			content += "\n_（已截断）_"
		}
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": content},
		})
	} else if out := strings.TrimSpace(in.RecentOutput); out != "" {
		body, truncated := truncateQuestion(out)
		content := "```\n" + body + "\n```"
		if truncated {
			content += "\n_（已截断）_"
		}
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": content},
		})
	}
	elements = append(elements, actionRowOf(jumpButton(in.SessionID), injectButton(in.SessionID, "继续", "\n"), ackButton(in.SessionID, "waiting_input")))
	return Card{
		MsgType: "interactive",
		Card: map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Session 等待输入"},
				"template": "orange",
			},
			"elements": elements,
		},
	}
}

type AskOption struct {
	Label       string
	Description string
	InjectText  string // bytes to write into the PTY when this option is tapped
}

type AskQuestionInput struct {
	SessionID uuid.UUID
	Question  string
	Options   []AskOption
}

func RenderAskQuestionCard(in AskQuestionInput) Card {
	prompt := "**Agent 在向你提问：请选择一个选项**"
	if q := strings.TrimSpace(in.Question); q != "" {
		prompt = "**Agent 在向你提问：**\n" + q
	}
	elements := []any{
		map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": prompt},
		},
	}
	if len(in.Options) > 0 {
		var sb strings.Builder
		for i, o := range in.Options {
			if o.Description != "" {
				fmt.Fprintf(&sb, "%d. **%s** — %s\n", i+1, o.Label, o.Description)
			} else {
				fmt.Fprintf(&sb, "%d. **%s**\n", i+1, o.Label)
			}
		}
		elements = append(elements, map[string]any{
			"tag":  "div",
			"text": map[string]any{"tag": "lark_md", "content": sb.String()},
		})
	}
	elements = append(elements, map[string]any{
		"tag":      "note",
		"elements": []any{map[string]any{"tag": "plain_text", "content": "或直接回复本消息以自由作答"}},
	})
	buttons := []map[string]any{jumpButton(in.SessionID)}
	for _, o := range in.Options {
		buttons = append(buttons, injectButton(in.SessionID, o.Label, o.InjectText))
	}
	elements = append(elements, actionRowOf(buttons...))
	return Card{
		MsgType: "interactive",
		Card: map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Agent 提问"},
				"template": "blue",
			},
			"elements": elements,
		},
	}
}

func truncateQuestion(q string) (string, bool) {
	const (
		maxLines = 6
		maxChars = 1200
	)
	truncated := false
	lines := strings.Split(q, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		truncated = true
	}
	body := strings.Join(lines, "\n")
	if len(body) > maxChars {
		body = body[:maxChars]
		truncated = true
	}
	return body, truncated
}

// injectButton builds a card button whose tap injects text verbatim into the
// session's PTY (handled in the card.action.trigger consumer). A trailing "\n"
// in text submits the line — e.g. "重试" sends LastCommand+"\n", "继续" sends "\n".
func injectButton(sessionID uuid.UUID, label, text string) map[string]any {
	return map[string]any{
		"tag":  "button",
		"text": map[string]any{"tag": "plain_text", "content": label},
		"type": "default",
		"value": map[string]any{
			"kind":       "inject",
			"session_id": sessionID.String(),
			"text":       text,
		},
	}
}

func jumpButton(sessionID uuid.UUID) map[string]any {
	return map[string]any{
		"tag":  "button",
		"text": map[string]any{"tag": "plain_text", "content": "跳回打开 session"},
		"type": "primary",
		"url":  deepLink(sessionID),
	}
}

func ackButton(sessionID uuid.UUID, event string) map[string]any {
	return map[string]any{
		"tag":  "button",
		"text": map[string]any{"tag": "plain_text", "content": "确认"},
		"type": "default",
		"value": map[string]any{
			"kind":       "ack",
			"session_id": sessionID.String(),
			"event":      event,
		},
	}
}

func actionRowOf(buttons ...map[string]any) map[string]any {
	acts := make([]any, 0, len(buttons))
	for _, b := range buttons {
		acts = append(acts, b)
	}
	return map[string]any{"tag": "action", "actions": acts}
}

func RenderAckUpdateCard(in AckUpdateInput) AckResponse {
	shortID := in.SessionID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return AckResponse{
		Toast: map[string]any{"type": "success", "content": "已确认"},
		Card: map[string]any{
			"config": map[string]any{"update_multi": true, "wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": fmt.Sprintf("已确认（%s #%s）", in.Event, shortID)},
				"template": "grey",
			},
			"elements": []any{
				map[string]any{
					"tag":  "div",
					"text": map[string]any{"tag": "plain_text", "content": "你已在飞书确认此事件。"},
				},
			},
		},
	}
}

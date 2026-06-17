// internal/feishu/card.go
package feishu

import (
	"fmt"

	"github.com/google/uuid"
)

// CommandFinishedInput mirrors the relevant fields from
// internal/webhook/CommandFinished (and webpush's). When SealedBody is
// non-empty we render the E2EE-safe variant — no exit code, no label,
// no elapsed, only a generic "see your device" body.
type CommandFinishedInput struct {
	SessionID  uuid.UUID
	ExitCode   int
	ElapsedMS  int
	Label      string
	SealedBody []byte
}

func (in CommandFinishedInput) sealed() bool { return len(in.SealedBody) > 0 }

type WaitingInputInput struct {
	SessionID      uuid.UUID
	IdleForSeconds int
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
					actionRow(in.SessionID, "command_finished"),
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
	return Card{
		MsgType: "interactive",
		Card: map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "命令完成"},
				"template": template,
			},
			"elements": []any{
				map[string]any{
					"tag": "div",
					"text": map[string]any{
						"tag":     "lark_md",
						"content": fmt.Sprintf("**`%s`** 退出码 `%d` · 用时 %s", label, in.ExitCode, formatElapsed(in.ElapsedMS)),
					},
				},
				actionRow(in.SessionID, "command_finished"),
			},
		},
	}
}

func RenderWaitingInputCard(in WaitingInputInput) Card {
	return Card{
		MsgType: "interactive",
		Card: map[string]any{
			"config": map[string]any{"wide_screen_mode": true},
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": "Session 等待输入"},
				"template": "orange",
			},
			"elements": []any{
				map[string]any{
					"tag": "div",
					"text": map[string]any{
						"tag":     "lark_md",
						"content": fmt.Sprintf("Agent 在等待你回复（已闲置 %ds）", in.IdleForSeconds),
					},
				},
				actionRow(in.SessionID, "waiting_input"),
			},
		},
	}
}

func actionRow(sessionID uuid.UUID, event string) map[string]any {
	return map[string]any{
		"tag": "action",
		"actions": []any{
			map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "跳回打开 session"},
				"type": "primary",
				"url":  deepLink(sessionID),
			},
			map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": "确认"},
				"type": "default",
				"value": map[string]any{
					"kind":       "ack",
					"session_id": sessionID.String(),
					"event":      event,
				},
			},
		},
	}
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

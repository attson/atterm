// Package feishu integrates with the Feishu (Lark) Open Platform: encrypted
// event ingestion, card rendering, and IM message dispatch.
package feishu

// anchor_card.go renders the JSON 2.0 anchor card that represents a
// remote-terminal session in a Feishu DM.
//
// Schema layout:
//
//	header:  blue/green/grey/red color band with session label + status line
//	body:    single markdown element (streaming-mode constraint); patched
//	         live by the outbound chunker
//	input:   single-line text element; submits card.action.trigger with
//	         value.kind="input"
//	actions: five button row (^C, ^D, Esc, Enter, 结束)
//
// The archive variant strips the input element and action buttons, sets
// the template to grey, and appends a footer line.

import (
	"encoding/json"
	"fmt"
)

// AnchorState is the renderer input. The chunker keeps the latest snapshot
// per session and passes a fresh AnchorState on every PATCH.
type AnchorState struct {
	SessionID    string // atterm session UUID
	SessionLabel string // short identifier shown in title (cwd basename or command)
	StatusText   string // subtitle: "running · driver: me · 2m13s" etc.
	BodyMarkdown string // streaming tail body
	Template     string // "blue" | "green" | "grey" | "red"
}

// RenderAnchorCreate returns the full create-card POST body for SendInteractiveToOpenID.
// Use this only for the very first message; subsequent updates go through PatchBody.
func RenderAnchorCreate(s AnchorState) ([]byte, error) {
	if s.Template == "" {
		s.Template = "blue"
	}
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": true,
		},
		"header": anchorHeader(s),
		"body": map[string]any{
			"direction": "vertical",
			"elements": []any{
				bodyMarkdown(s.BodyMarkdown),
				inputElement(s.SessionID),
				buttonsRow(s.SessionID),
			},
		},
	}
	return marshalCard(card)
}

// RenderAnchorArchive returns a final-state card with input/buttons stripped
// and a footer line appended. Used when the session exits or the user clicks 结束.
func RenderAnchorArchive(s AnchorState, footer string) ([]byte, error) {
	s.Template = "grey"
	card := map[string]any{
		"schema": "2.0",
		"config": map[string]any{
			"streaming_mode": false,
		},
		"header": anchorHeader(s),
		"body": map[string]any{
			"direction": "vertical",
			"elements": []any{
				bodyMarkdown(s.BodyMarkdown),
				map[string]any{
					"tag":     "markdown",
					"content": fmt.Sprintf("**%s**", footer),
				},
			},
		},
	}
	return marshalCard(card)
}

func anchorHeader(s AnchorState) map[string]any {
	return map[string]any{
		"template": s.Template,
		"title": map[string]any{
			"tag":     "plain_text",
			"content": fmt.Sprintf("▸ session · %s", s.SessionLabel),
		},
		"subtitle": map[string]any{
			"tag":     "plain_text",
			"content": s.StatusText,
		},
	}
}

func bodyMarkdown(content string) map[string]any {
	if content == "" {
		content = "_(waiting for output)_"
	}
	return map[string]any{
		"tag":     "markdown",
		"content": content,
	}
}

func inputElement(sessionID string) map[string]any {
	return map[string]any{
		"tag":         "input",
		"placeholder": map[string]any{"tag": "plain_text", "content": "Type here…"},
		"value": map[string]any{
			"kind":       "input",
			"session_id": sessionID,
		},
	}
}

func buttonsRow(sessionID string) map[string]any {
	makeBtn := func(label, event string) map[string]any {
		return map[string]any{
			"tag":       "button",
			"text":      map[string]any{"tag": "plain_text", "content": label},
			"type":      "default",
			"behaviors": []any{map[string]any{"type": "callback"}},
			"value": map[string]any{
				"kind":       "key",
				"session_id": sessionID,
				"event":      event,
			},
		}
	}
	endBtn := map[string]any{
		"tag":       "button",
		"text":      map[string]any{"tag": "plain_text", "content": "结束"},
		"type":      "danger",
		"behaviors": []any{map[string]any{"type": "callback"}},
		"value": map[string]any{
			"kind":       "end",
			"session_id": sessionID,
		},
	}
	return map[string]any{
		"tag": "action",
		"actions": []any{
			makeBtn("^C", "ctrl_c"),
			makeBtn("^D", "ctrl_d"),
			makeBtn("Esc", "esc"),
			makeBtn("Enter", "enter"),
			endBtn,
		},
	}
}

func marshalCard(card map[string]any) ([]byte, error) {
	wrapper := map[string]any{"msg_type": "interactive", "card": card}
	return json.Marshal(wrapper)
}

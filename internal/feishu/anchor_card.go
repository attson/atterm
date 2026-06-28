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
//	buttons: five-column column_set (^C, ^D, Esc, Enter, 结束) — schema V2
//	         rejects the legacy `tag:"action"` container, so each button gets
//	         its own column.
//
// The archive variant strips the input element and action buttons, sets
// the template to grey, and appends a footer line.

import (
	"encoding/json"
	"fmt"
	"time"
)

// AnchorBodyElementID is the stable element_id assigned to the anchor card's
// body markdown element. PatchCard targets the element-level endpoint
// (/cards/{card_id}/elements/{element_id}); without an explicit element_id
// on the element at create time, V2 PATCHes silently no-op even when the
// open API returns code=0.
const AnchorBodyElementID = "anchor_body_md"

// AnchorInputElementID is the stable element_id on the input box. Used by
// the post-submit clear (PATCH default_value to "") so the textarea doesn't
// keep the previous reply visible after the user hits send.
const AnchorInputElementID = "anchor_input"

// AnchorButtonsElementID is the stable element_id on the bottom buttons row
// (the column_set). PATCHed at runtime to swap the default 5 keystroke
// buttons (^C ^D Esc Enter 结束) for AskUserQuestion option buttons.
const AnchorButtonsElementID = "anchor_buttons"

// AnchorState is the renderer input. The chunker keeps the latest snapshot
// per session and passes a fresh AnchorState on every PATCH.
type AnchorState struct {
	SessionID    string // atterm session UUID
	SessionLabel string // fallback identifier when Title is empty
	Title        string // human-facing session title (claude resume conversation, etc.)
	Cwd          string // working directory; shown in the subtitle
	StatusText   string // subtitle status: "running" / "ended" / etc. joined with Cwd
	BodyMarkdown string // streaming tail body
	Template     string // "blue" | "green" | "grey" | "red"
	Restored     bool   // true for AI sessions revived via --resume; swaps the empty-body hint
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
				bodyMarkdown(initialBodyContent(s)),
				inputElement(s.SessionID),
				buttonsRow(s.SessionID),
			},
		},
	}
	return marshalCard(card)
}

// initialBodyContent picks the seed shown in body[0] when BodyMarkdown is
// empty. Restored AI sessions don't replay history, so the bare "waiting
// for output" reads as broken; flag them explicitly.
func initialBodyContent(s AnchorState) string {
	if s.BodyMarkdown != "" {
		return s.BodyMarkdown
	}
	if s.Restored {
		return "_(已恢复 · 等你说话)_"
	}
	return ""
}

// PrependStatus wraps a body markdown string with a one-line status preamble
// that telegraphs the session's TaskState + elapsed runtime. The header can't
// be live-PATCHed at element granularity in V2 schema cards, so the body
// element doubles as a status surface.
//
// elapsed=0 omits the "· 已 …" suffix; unknown taskState falls through to
// "活跃" so the line always renders rather than vanishing.
func PrependStatus(taskState string, elapsed time.Duration, body string) string {
	label := statusLabel(taskState)
	header := "> " + label
	if elapsed > 0 {
		header += " · 已 " + formatElapsedDuration(elapsed)
	}
	if body == "" {
		return header + "\n"
	}
	return header + "\n\n" + body
}

func statusLabel(taskState string) string {
	switch taskState {
	case "running":
		return "🤖 处理中"
	case "waiting_input":
		return "⏸ 等待输入"
	case "completed":
		return "✓ 完成"
	case "failed":
		return "✗ 错误"
	default:
		return "▸ 活跃"
	}
}

func formatElapsedDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	mins := int((d - time.Duration(hours)*time.Hour).Minutes())
	return fmt.Sprintf("%dh%dm", hours, mins)
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
	title := s.Title
	if title == "" {
		title = "session · " + s.SessionLabel
	}
	subtitle := s.StatusText
	if s.Cwd != "" {
		if subtitle != "" {
			subtitle = s.Cwd + " · " + subtitle
		} else {
			subtitle = s.Cwd
		}
	}
	return map[string]any{
		"template": s.Template,
		"title": map[string]any{
			"tag":     "plain_text",
			"content": "▸ " + title,
		},
		"subtitle": map[string]any{
			"tag":     "plain_text",
			"content": subtitle,
		},
	}
}

func bodyMarkdown(content string) map[string]any {
	if content == "" {
		content = "_(waiting for output)_"
	}
	return map[string]any{
		"tag":        "markdown",
		"element_id": AnchorBodyElementID,
		"content":    content,
	}
}

func inputElement(sessionID string) map[string]any {
	// V2 schema requires the callback payload to live inside behaviors[0].value
	// — not as a top-level `value` field. Top-level value is silently ignored,
	// no card.action.trigger fires, the textarea looks "dead" on submit.
	return map[string]any{
		"tag":         "input",
		"element_id":  AnchorInputElementID,
		"placeholder": map[string]any{"tag": "plain_text", "content": "Type here…"},
		"behaviors": []any{map[string]any{
			"type": "callback",
			"value": map[string]any{
				"kind":       "input",
				"session_id": sessionID,
			},
		}},
	}
}

func buttonsRow(sessionID string) map[string]any {
	// V2 callback contract: the click payload sits inside behaviors[0].value.
	// A top-level `value` on the button is silently ignored — no
	// card.action.trigger fires, the click looks dead. (Verified against the
	// "Configuring card interactions" V2 doc.)
	makeBtn := func(label, event string) map[string]any {
		return map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": label},
			"type": "default",
			"behaviors": []any{map[string]any{
				"type": "callback",
				"value": map[string]any{
					"kind":       "key",
					"session_id": sessionID,
					"event":      event,
				},
			}},
		}
	}
	endBtn := map[string]any{
		"tag":  "button",
		"text": map[string]any{"tag": "plain_text", "content": "结束"},
		"type": "danger",
		"behaviors": []any{map[string]any{
			"type": "callback",
			"value": map[string]any{
				"kind":       "end",
				"session_id": sessionID,
			},
		}},
	}
	column := func(btn map[string]any) map[string]any {
		return map[string]any{
			"tag":      "column",
			"width":    "weighted",
			"weight":   1,
			"elements": []any{btn},
		}
	}
	return map[string]any{
		"tag":                "column_set",
		"element_id":         AnchorButtonsElementID,
		"horizontal_spacing": "small",
		"columns": []any{
			column(makeBtn("^C", "ctrl_c")),
			column(makeBtn("^D", "ctrl_d")),
			column(makeBtn("Esc", "esc")),
			column(makeBtn("Enter", "enter")),
			column(endBtn),
		},
	}
}

// AskOptionsColumnSet builds the column_set payload that replaces the default
// buttons row when claude fires AskUserQuestion. Each button submits the
// option label as if the user typed it into the input box.
func AskOptionsColumnSet(sessionID string, optionLabels []string) map[string]any {
	cols := make([]any, 0, len(optionLabels))
	for _, label := range optionLabels {
		// V2 schema requires the callback `value` to live inside the
		// behaviors entry, not at the button's top level. See buttonsRow
		// for the same fix on the default keystroke row.
		btn := map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": label},
			"type": "primary",
			"behaviors": []any{map[string]any{
				"type": "callback",
				"value": map[string]any{
					"kind":       "input",
					"session_id": sessionID,
					"text":       label,
				},
			}},
		}
		cols = append(cols, map[string]any{
			"tag":      "column",
			"width":    "weighted",
			"weight":   1,
			"elements": []any{btn},
		})
	}
	return map[string]any{
		"tag":                "column_set",
		"horizontal_spacing": "small",
		"columns":            cols,
	}
}

// DefaultButtonsColumnSet returns the default keystroke buttons (^C ^D Esc
// Enter 结束). PATCH-set back into the anchor after AskUserQuestion resolves.
func DefaultButtonsColumnSet(sessionID string) map[string]any {
	// Reuse the same renderer used at card create time so the two stay in
	// sync; just drop the element_id field (PATCH addresses by element_id
	// already, no need to repeat it in the partial body).
	cs := buttonsRow(sessionID)
	delete(cs, "element_id")
	return cs
}

func marshalCard(card map[string]any) ([]byte, error) {
	wrapper := map[string]any{"msg_type": "interactive", "card": card}
	return json.Marshal(wrapper)
}

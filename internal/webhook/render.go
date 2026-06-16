package webhook

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// CommandFinished is the event shape, identical to webpush.CommandFinished so
// the dispatch site can construct both from the same decoded uplink payload.
//
// When SealedBody is set (M6-final), the agent has stripped the plaintext
// Label/ExitCode/ElapsedMS fields before sending the CommandEvent up;
// receivers can only learn what command ran by decrypting SealedBody with
// the user's account_key.
type CommandFinished struct {
	SessionID  uuid.UUID
	HostID     uuid.UUID
	ExitCode   int
	ElapsedMS  int
	Label      string
	SealedBody []byte
}

// formatElapsed renders milliseconds as "2.3s" (<60s) or "1m04s" (>=60s),
// matching the desktop frontend's command-finish formatting.
func formatElapsed(ms int) string {
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", float64(ms)/1000)
	}
	totalSec := ms / 1000
	return fmt.Sprintf("%dm%02ds", totalSec/60, totalSec%60)
}

// sealed reports whether this event was emitted under E2EE — i.e. the
// agent attached an opaque sealedBody and stripped the plaintext fields.
func (ev CommandFinished) sealed() bool { return len(ev.SealedBody) > 0 }

func humanText(ev CommandFinished) string {
	if ev.sealed() {
		return "Session command finished"
	}
	label := ev.Label
	if label == "" {
		label = "command"
	}
	return fmt.Sprintf("%s finished (exit %d, %s)", label, ev.ExitCode, formatElapsed(ev.ElapsedMS))
}

func renderFeishu(ev CommandFinished) []byte {
	payload := map[string]any{
		"msg_type": "text",
		"content":  map[string]string{"text": humanText(ev)},
	}
	b, _ := json.Marshal(payload)
	return b
}

func renderGeneric(ev CommandFinished) []byte {
	payload := map[string]any{
		"event":      "command_finished",
		"session_id": ev.SessionID.String(),
		"host_id":    ev.HostID.String(),
		"text":       humanText(ev),
	}
	if ev.sealed() {
		payload["sealed_body"] = base64.StdEncoding.EncodeToString(ev.SealedBody)
	} else {
		payload["exit_code"] = ev.ExitCode
		payload["elapsed_ms"] = ev.ElapsedMS
		payload["label"] = ev.Label
	}
	b, _ := json.Marshal(payload)
	return b
}

// renderForFormat selects the renderer; unknown formats fall back to generic.
func renderForFormat(format string, ev CommandFinished) []byte {
	if format == "feishu" {
		return renderFeishu(ev)
	}
	return renderGeneric(ev)
}

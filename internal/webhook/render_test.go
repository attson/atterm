package webhook

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func sampleEvent() CommandFinished {
	return CommandFinished{
		SessionID: uuid.MustParse("00000000-0000-0000-0000-0000000000a1"),
		HostID:    uuid.MustParse("00000000-0000-0000-0000-0000000000b2"),
		ExitCode:  0,
		ElapsedMS: 2300,
		Label:     "npm test",
	}
}

func TestRenderFeishu(t *testing.T) {
	body := renderFeishu(sampleEvent())
	var parsed struct {
		MsgType string `json:"msg_type"`
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("feishu body not valid json: %v", err)
	}
	if parsed.MsgType != "text" {
		t.Fatalf("msg_type = %q, want text", parsed.MsgType)
	}
	if !strings.Contains(parsed.Content.Text, "npm test") || !strings.Contains(parsed.Content.Text, "exit 0") {
		t.Fatalf("feishu text missing label/exit: %q", parsed.Content.Text)
	}
	if !strings.Contains(parsed.Content.Text, "2.3s") {
		t.Fatalf("feishu text missing formatted elapsed: %q", parsed.Content.Text)
	}
}

func TestRenderGeneric(t *testing.T) {
	body := renderGeneric(sampleEvent())
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("generic body not valid json: %v", err)
	}
	if parsed["label"] != "npm test" || parsed["exit_code"].(float64) != 0 || parsed["elapsed_ms"].(float64) != 2300 {
		t.Fatalf("generic payload wrong: %+v", parsed)
	}
	if parsed["session_id"] == "" || parsed["host_id"] == "" {
		t.Fatalf("generic payload missing ids: %+v", parsed)
	}
}

// TestRenderFeishu_Sealed: when the event carries an E2EE sealedBody
// (M6-final), feishu/text renderers must emit a generic line that
// does NOT include the plaintext label or any timing/exit info. The
// downstream relay never sees those fields once the agent has sealed.
func TestRenderFeishu_Sealed(t *testing.T) {
	ev := sampleEvent()
	ev.SealedBody = []byte{0x01, 0x02, 0x03}
	body := renderFeishu(ev)
	var parsed struct {
		Content struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("feishu body not valid json: %v", err)
	}
	if parsed.Content.Text != "Session command finished" {
		t.Fatalf("feishu sealed text = %q; want generic", parsed.Content.Text)
	}
	if strings.Contains(parsed.Content.Text, "npm test") || strings.Contains(parsed.Content.Text, "exit") || strings.Contains(parsed.Content.Text, "2.3") {
		t.Fatalf("feishu sealed text leaked plaintext: %q", parsed.Content.Text)
	}
}

// TestRenderGeneric_Sealed: generic JSON renderer must omit label/exit/
// elapsed when sealed and instead carry a base64 sealed_body the
// receiver can decrypt out-of-band.
func TestRenderGeneric_Sealed(t *testing.T) {
	ev := sampleEvent()
	ev.SealedBody = []byte{0x01, 0x02, 0x03, 0x04}
	body := renderGeneric(ev)
	var parsed map[string]any
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("generic body not valid json: %v", err)
	}
	if _, ok := parsed["label"]; ok {
		t.Fatalf("generic sealed payload leaked label: %+v", parsed)
	}
	if _, ok := parsed["exit_code"]; ok {
		t.Fatalf("generic sealed payload leaked exit_code: %+v", parsed)
	}
	if _, ok := parsed["elapsed_ms"]; ok {
		t.Fatalf("generic sealed payload leaked elapsed_ms: %+v", parsed)
	}
	got, _ := parsed["sealed_body"].(string)
	if got != "AQIDBA==" { // base64.StdEncoding of {0x01,0x02,0x03,0x04}
		t.Fatalf("sealed_body = %q; want base64 of envelope", got)
	}
	if parsed["text"] != "Session command finished" {
		t.Fatalf("text = %v; want generic", parsed["text"])
	}
}

func TestFormatElapsed(t *testing.T) {
	cases := map[int]string{500: "0.5s", 2300: "2.3s", 64000: "1m04s", 3600000: "60m00s"}
	for ms, want := range cases {
		if got := formatElapsed(ms); got != want {
			t.Errorf("formatElapsed(%d) = %q, want %q", ms, got, want)
		}
	}
}

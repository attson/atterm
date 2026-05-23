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

func TestFormatElapsed(t *testing.T) {
	cases := map[int]string{500: "0.5s", 2300: "2.3s", 64000: "1m04s", 3600000: "60m00s"}
	for ms, want := range cases {
		if got := formatElapsed(ms); got != want {
			t.Errorf("formatElapsed(%d) = %q, want %q", ms, got, want)
		}
	}
}

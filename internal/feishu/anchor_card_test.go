package feishu

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRenderAnchorCreate_HasRequiredStructure(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{
		SessionID:    "abc",
		SessionLabel: "go-build",
		StatusText:   "运行中 · driver: me · 2m13s",
		BodyMarkdown: "PASS: TestA",
		Template:     "blue",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var probe map[string]any
	if err := json.Unmarshal(body, &probe); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if probe["msg_type"] != "interactive" {
		t.Errorf("msg_type = %v, want interactive", probe["msg_type"])
	}
	card, ok := probe["card"].(map[string]any)
	if !ok {
		t.Fatalf("card not an object: %T", probe["card"])
	}
	if _, ok := card["header"]; !ok {
		t.Errorf("card.header missing")
	}
	if !strings.Contains(string(body), "go-build") {
		t.Errorf("body missing session label")
	}
	if !strings.Contains(string(body), "PASS: TestA") {
		t.Errorf("body missing body markdown")
	}
	if !strings.Contains(string(body), `"session_id":"abc"`) {
		t.Errorf("body missing session_id in action values: %s", body)
	}
}

func TestRenderAnchorArchive_Greys(t *testing.T) {
	body, err := RenderAnchorArchive(AnchorState{
		SessionID:    "abc",
		SessionLabel: "go-build",
		StatusText:   "已结束",
	}, "结束 at 2026-06-26 19:40")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(body), `"template":"grey"`) {
		t.Errorf("archive should use grey template, got: %s", body)
	}
	if !strings.Contains(string(body), "结束 at") {
		t.Errorf("archive should include archived footer")
	}
	if strings.Contains(string(body), `"tag":"input"`) {
		t.Errorf("archive must not include input element")
	}
}

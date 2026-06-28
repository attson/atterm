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

// V2 schema rejects the legacy `tag:"action"` button container with
// 230099 / "cards of schema V2 no longer support this capability". The
// renderer must avoid it entirely; buttons live in a column_set instead.
func TestRenderAnchorCreate_NoLegacyActionTag(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{SessionID: "abc"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(body), `"tag":"action"`) {
		t.Errorf("body contains forbidden V2 tag `action`: %s", body)
	}
}

// All five button labels (^C, ^D, Esc, Enter, 结束) and their event payloads
// must round-trip through the renderer regardless of container shape.
func TestRenderAnchorCreate_ContainsAllButtons(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{SessionID: "abc"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(body)
	for _, want := range []string{
		`"content":"^C"`, `"event":"ctrl_c"`,
		`"content":"^D"`, `"event":"ctrl_d"`,
		`"content":"Esc"`, `"event":"esc"`,
		`"content":"Enter"`, `"event":"enter"`,
		`"content":"结束"`, `"kind":"end"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("body missing %q: %s", want, s)
		}
	}
}

// V2 streaming-mode PATCH targets elements by element_id (the JSON-path
// "body.elements[0].content" shape is silently no-op in V2). The body
// markdown element must carry a stable element_id so the chunker's flush
// path can address it.
func TestRenderAnchorCreate_BodyMarkdownHasElementID(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{SessionID: "abc"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(body), `"element_id":"`+AnchorBodyElementID+`"`) {
		t.Errorf("body markdown missing element_id=%q: %s", AnchorBodyElementID, body)
	}
}

// The schema V2 button container must be column_set (per Feishu's V2 card
// docs); regress-guard so a future refactor can't silently fall back to a
// shape the open API rejects.
func TestRenderAnchorCreate_UsesColumnSetContainer(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{SessionID: "abc"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(body), `"tag":"column_set"`) {
		t.Errorf("body missing column_set button container: %s", body)
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

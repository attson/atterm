package feishu

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
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

// Restored AI sessions never replay history, so the anchor body stays empty
// until the user sends a fresh prompt. Telegraphing this with a "已恢复"
// hint avoids the "card is broken?" confusion that the bare
// "(waiting for output)" produces.
func TestRenderAnchorCreate_RestoredHintWhenBodyEmpty(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{SessionID: "abc", Restored: true})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(body), "已恢复") {
		t.Errorf("body should carry restored hint, got: %s", body)
	}
	if strings.Contains(string(body), "waiting for output") {
		t.Errorf("body should drop default fresh hint when restored, got: %s", body)
	}
}

func TestRenderAnchorCreate_FreshShowsWaitingForOutput(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{SessionID: "abc"})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(string(body), "waiting for output") {
		t.Errorf("body should show waiting for output, got: %s", body)
	}
}

// The header should surface enough context (title + cwd) that the user can
// identify a session at a glance, instead of staring at an 8-char UUID prefix.
func TestRenderAnchorCreate_HeaderRendersTitleAndCwd(t *testing.T) {
	body, err := RenderAnchorCreate(AnchorState{
		SessionID: "abc",
		Title:     "Claude Code",
		Cwd:       "/Users/x/projects/foo",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "Claude Code") {
		t.Errorf("header should contain Title, got: %s", s)
	}
	if !strings.Contains(s, "/Users/x/projects/foo") {
		t.Errorf("header should contain Cwd, got: %s", s)
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

// The status preamble is a short markdown line at the top of body[0] that
// telegraphs the session's TaskState + how long it's been running. Header
// can't be PATCHed at element-level granularity in V2, so the body element
// is the only live surface. Helper is pure so it's the canonical place to
// pin emoji choices + elapsed-time formatting.
func TestPrependStatus_RunningWithElapsed(t *testing.T) {
	got := PrependStatus("running", 90*time.Second, "👤 hi\n\n🤖 hello\n")
	if !strings.HasPrefix(got, "> 🤖 处理中 · 已 1m") {
		t.Errorf("missing running status prefix: %q", got)
	}
	if !strings.Contains(got, "👤 hi") {
		t.Errorf("inner body lost: %q", got)
	}
}

func TestPrependStatus_TaskStateMapping(t *testing.T) {
	cases := []struct {
		state string
		want  string
	}{
		{"running", "🤖 处理中"},
		{"waiting_input", "⏸ 等待输入"},
		{"completed", "✓ 完成"},
		{"failed", "✗ 错误"},
		{"", "▸ 活跃"}, // unknown / pre-classified
	}
	for _, c := range cases {
		got := PrependStatus(c.state, 0, "")
		if !strings.Contains(got, c.want) {
			t.Errorf("state=%q: missing label %q in %q", c.state, c.want, got)
		}
	}
}

func TestPrependStatus_ElapsedFormatting(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{30 * time.Second, "已 0m"},
		{90 * time.Second, "已 1m"},
		{2*time.Hour + 5*time.Minute, "已 2h5m"},
		{0, ""}, // zero elapsed → omit "已 ..." suffix entirely
	}
	for _, c := range cases {
		got := PrependStatus("running", c.d, "")
		if c.want == "" {
			if strings.Contains(got, "已 ") {
				t.Errorf("d=%v: expected no elapsed suffix, got %q", c.d, got)
			}
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("d=%v: missing %q in %q", c.d, c.want, got)
		}
	}
}

func TestPrependStatus_EmptyBodyKeepsRestoredHint(t *testing.T) {
	// When inner is "" and we're not in a fresh stream, status preamble
	// still renders + a marker line stays below it so the card isn't blank.
	got := PrependStatus("waiting_input", 0, "")
	if !strings.Contains(got, "⏸ 等待输入") {
		t.Errorf("status missing: %q", got)
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

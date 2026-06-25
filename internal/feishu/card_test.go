// internal/feishu/card_test.go
package feishu

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestRenderCommandFinishedCard_Success(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	card := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: sid,
		ExitCode:  0,
		ElapsedMS: 2500,
		Label:     "go test",
	})
	s := mustJSON(t, card)
	for _, want := range []string{`"interactive"`, `"green"`, "`go test`", "atterm://session/" + sid.String(), `"kind":"ack"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in: %s", want, s)
		}
	}
}

func TestRenderCommandFinishedCard_Failure(t *testing.T) {
	card := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(),
		ExitCode:  1,
		ElapsedMS: 60500,
		Label:     "make build",
	})
	s := mustJSON(t, card)
	if !strings.Contains(s, `"red"`) {
		t.Fatalf("non-zero exit should color the header red: %s", s)
	}
	if !strings.Contains(s, "1m00s") {
		t.Fatalf("elapsed should render as 1m00s; got %s", s)
	}
}

func TestRenderCommandFinishedCard_Sealed(t *testing.T) {
	card := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID:  uuid.New(),
		SealedBody: []byte{0xAA, 0xBB}, // any non-empty
	})
	s := mustJSON(t, card)
	if !strings.Contains(s, "仅本机可见") {
		t.Fatalf("sealed variant should include 仅本机可见; got %s", s)
	}
	// MUST NOT leak exit_code/label values in sealed variant.
	if strings.Contains(s, `"exit"`) || strings.Contains(s, "make build") {
		t.Fatalf("sealed variant must not include plaintext fields: %s", s)
	}
}

func TestRenderWaitingInputCard(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	card := RenderWaitingInputCard(WaitingInputInput{
		SessionID:      sid,
		IdleForSeconds: 42,
	})
	s := mustJSON(t, card)
	for _, want := range []string{`"orange"`, "42", "atterm://session/" + sid.String(), "waiting_input"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in: %s", want, s)
		}
	}
}

func TestRenderAckUpdateCard(t *testing.T) {
	out := RenderAckUpdateCard(AckUpdateInput{
		Event:     "command_finished",
		SessionID: uuid.MustParse("00000000-0000-0000-0000-000000000003").String(),
	})
	s := mustJSON(t, out)
	for _, want := range []string{`"update_multi":true`, `"toast"`, "已确认", `"grey"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in: %s", want, s)
		}
	}
}

func TestRenderWaitingInputCard_WithQuestion(t *testing.T) {
	sid := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	card := RenderWaitingInputCard(WaitingInputInput{
		SessionID:      sid,
		IdleForSeconds: 12,
		QuestionText:   "Run rm -rf node_modules? (y/N)",
	})
	s := mustJSON(t, card)
	if !strings.Contains(s, "```") {
		t.Fatalf("expected code fence in card body: %s", s)
	}
	if !strings.Contains(s, "Run rm -rf node_modules? (y/N)") {
		t.Fatalf("expected question text in body: %s", s)
	}
	if !strings.Contains(s, `"orange"`) {
		t.Fatalf("waiting card must still use orange template")
	}
}

func TestRenderWaitingInputCard_QuestionTruncation(t *testing.T) {
	long := strings.Repeat("x", 2000)
	card := RenderWaitingInputCard(WaitingInputInput{
		SessionID:    uuid.New(),
		QuestionText: long,
	})
	s := mustJSON(t, card)
	if !strings.Contains(s, "已截断") {
		t.Fatalf("expected truncation marker `已截断` in body: %s", s)
	}
	if len(s) >= len(long)+512 {
		t.Fatalf("body length %d suggests truncation did not happen", len(s))
	}
}

func TestRenderWaitingInputCard_QuestionLineTruncation(t *testing.T) {
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i+1)
	}
	long := strings.Join(lines, "\n")
	card := RenderWaitingInputCard(WaitingInputInput{
		SessionID:    uuid.New(),
		QuestionText: long,
	})
	s := mustJSON(t, card)
	if !strings.Contains(s, "已截断") {
		t.Fatalf("expected truncation marker: %s", s)
	}
	if !strings.Contains(s, "line 1") || !strings.Contains(s, "line 6") {
		t.Fatalf("expected lines 1..6 retained: %s", s)
	}
	if strings.Contains(s, "line 7") {
		t.Fatalf("expected line 7 dropped: %s", s)
	}
}

func TestRenderWaitingInputCard_EmptyQuestionStillRenders(t *testing.T) {
	card := RenderWaitingInputCard(WaitingInputInput{
		SessionID:      uuid.New(),
		IdleForSeconds: 30,
	})
	s := mustJSON(t, card)
	if strings.Contains(s, "```") || strings.Contains(s, "已截断") {
		t.Fatalf("empty question must NOT render a code block or truncation marker: %s", s)
	}
	if !strings.Contains(s, "已闲置") {
		t.Fatalf("generic waiting copy still expected: %s", s)
	}
}

func actions(c Card) []any {
	els := c.Card["elements"].([]any)
	last := els[len(els)-1].(map[string]any)
	return last["actions"].([]any)
}

func buttonText(b any) string {
	m := b.(map[string]any)
	return m["text"].(map[string]any)["content"].(string)
}

func TestCommandFinishedCard_FailureHasRetry(t *testing.T) {
	c := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(), ExitCode: 1, ElapsedMS: 2000, Label: "go test",
		LastCommand: "go test ./...",
	})
	var labels []string
	for _, b := range actions(c) {
		labels = append(labels, buttonText(b))
	}
	want := []string{"跳回打开 session", "重试", "确认"}
	if len(labels) != 3 || labels[0] != want[0] || labels[1] != want[1] || labels[2] != want[2] {
		t.Fatalf("buttons = %v, want %v", labels, want)
	}
	retry := actions(c)[1].(map[string]any)["value"].(map[string]any)
	if retry["kind"] != "inject" || retry["text"] != "go test ./...\n" {
		t.Fatalf("retry value = %+v", retry)
	}
}

func TestCommandFinishedCard_SuccessNoRetry(t *testing.T) {
	c := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(), ExitCode: 0, ElapsedMS: 1000, Label: "ls",
	})
	if got := len(actions(c)); got != 2 {
		t.Fatalf("success buttons = %d, want 2 (跳回/确认)", got)
	}
}

func TestSealedCard_NoInjectButton(t *testing.T) {
	c := RenderCommandFinishedCard(CommandFinishedInput{
		SessionID: uuid.New(), SealedBody: []byte("x"),
	})
	for _, b := range actions(c) {
		if v, ok := b.(map[string]any)["value"].(map[string]any); ok {
			if v["kind"] == "inject" {
				t.Fatal("sealed card must not contain inject buttons")
			}
		}
	}
}

func TestAskQuestionCard_OneButtonPerOption(t *testing.T) {
	c := RenderAskQuestionCard(AskQuestionInput{
		SessionID: uuid.New(),
		Question:  "Deploy now?",
		Options: []AskOption{
			{Label: "Yes", InjectText: "1\n"},
			{Label: "No", InjectText: "2\n"},
		},
	})
	if c.Card["header"].(map[string]any)["template"] != "blue" {
		t.Fatal("AskQuestion card must be blue")
	}
	var labels []string
	for _, b := range actions(c) {
		labels = append(labels, buttonText(b))
	}
	// 跳回 + Yes + No(自由回复用引导文本 note,不是按钮)。
	if len(labels) != 3 || labels[1] != "Yes" || labels[2] != "No" {
		t.Fatalf("labels = %v", labels)
	}
	yes := actions(c)[1].(map[string]any)["value"].(map[string]any)
	if yes["kind"] != "inject" || yes["text"] != "1\n" {
		t.Fatalf("yes value = %+v", yes)
	}
}

func TestAskQuestionCard_EmptyOptions(t *testing.T) {
	c := RenderAskQuestionCard(AskQuestionInput{
		SessionID: uuid.New(),
		Question:  "Anything?",
	})
	// 无选项:只有跳回按钮一个。
	if got := len(actions(c)); got != 1 {
		t.Fatalf("empty-options buttons = %d, want 1 (跳回)", got)
	}
	if buttonText(actions(c)[0]) != "跳回打开 session" {
		t.Fatalf("only button should be jump, got %q", buttonText(actions(c)[0]))
	}
}

func TestAskQuestionCard_EmptyQuestionNoDanglingTitle(t *testing.T) {
	c := RenderAskQuestionCard(AskQuestionInput{
		SessionID: uuid.New(),
		Question:  "",
		Options:   []AskOption{{Label: "A", InjectText: "1\n"}},
	})
	// 第一个 div 正文不应以裸标题 + 空内容结尾。
	els := c.Card["elements"].([]any)
	first := els[0].(map[string]any)["text"].(map[string]any)["content"].(string)
	if first == "**Agent 在向你提问：**\n" || first == "**Agent 在向你提问:**\n" {
		t.Fatalf("dangling empty title: %q", first)
	}
	// 选项仍在。
	if buttonText(actions(c)[1]) != "A" {
		t.Fatalf("option button missing")
	}
}

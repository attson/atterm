// internal/feishu/card_test.go
package feishu

import (
	"encoding/json"
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

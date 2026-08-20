package feishu

import (
	"strings"
	"testing"
	"time"
)

type stubSubscriber struct {
	openID  string
	sentIn  [][]byte
	claimed bool
}

func (s *stubSubscriber) ClaimDriver() { s.claimed = true }
func (s *stubSubscriber) SendInput(b []byte) bool {
	s.sentIn = append(s.sentIn, append([]byte(nil), b...))
	return true
}
func (s *stubSubscriber) OwnerOpenID() string       { return s.openID }
func (s *stubSubscriber) CurrentDriverName() string { return "" }

func TestRouter_ReplyHappyPath(t *testing.T) {
	idx := NewCardIndex()
	anchor := &CardAnchor{SessionID: "sess1", CardMsgID: "msg1", CardToken: "tok1", OwnerOpenID: "ou_owner", CreatedAt: time.Now()}
	idx.Put(anchor)

	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(sessID string) Subscriber { return stub })

	dec := r.RouteReply("msg1", "ou_owner", "go test ./...")
	if dec.Action != ActionInject {
		t.Fatalf("action = %v, want inject", dec.Action)
	}
	if !stub.claimed {
		t.Errorf("expected ClaimDriver call when no current driver (router should call it)")
	}
	time.Sleep(80 * time.Millisecond) // CR is sent on a delayed goroutine
	if len(stub.sentIn) != 2 || string(stub.sentIn[0]) != "go test ./..." || stub.sentIn[1][0] != 0x0d {
		t.Errorf("sentIn = %q, want [text, {0x0d}] split", stub.sentIn)
	}
}

func TestRouter_ReplyOpenIDMismatch(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	dec := r.RouteReply("m", "ou_attacker", "rm -rf /")
	if dec.Action != ActionReject {
		t.Fatalf("action = %v, want reject", dec.Action)
	}
	if !strings.Contains(dec.Toast, "无权限") {
		t.Errorf("toast = %q, want it to mention 无权限", dec.Toast)
	}
	if len(stub.sentIn) != 0 {
		t.Errorf("input should not have been forwarded, got: %q", stub.sentIn)
	}
}

func TestRouter_ReplyUnknownTarget(t *testing.T) {
	idx := NewCardIndex()
	r := NewRouter(idx, func(string) Subscriber { return nil })
	dec := r.RouteReply("nonexistent", "ou_owner", "hi")
	if dec.Action != ActionReject {
		t.Fatalf("action = %v, want reject", dec.Action)
	}
	if !strings.Contains(dec.Toast, "找不到") {
		t.Errorf("toast = %q, want it to mention 找不到", dec.Toast)
	}
}

func TestRouter_CardActionKey(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	dec := r.RouteCardAction("t", "ou_owner", "key", "ctrl_c", "")
	if dec.Action != ActionInject {
		t.Fatalf("action = %v, want inject", dec.Action)
	}
	if len(stub.sentIn) != 1 || stub.sentIn[0][0] != 0x03 {
		t.Errorf("sentIn = %v, want one entry starting 0x03", stub.sentIn)
	}
}

func TestRouter_500msBudget(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	start := time.Now()
	_ = r.RouteReply("m", "ou_owner", "hi")
	elapsed := time.Since(start)
	if elapsed > 500*time.Millisecond {
		t.Errorf("route took %v, want ≤500ms", elapsed)
	}
}

type stubSubscriberDriverAware struct {
	stubSubscriber
	currentDriverName string
}

func (s *stubSubscriberDriverAware) CurrentDriverName() string { return s.currentDriverName }

// Single-user atterm has no multi-driver UX yet, and the previous preempt-
// gate dropped Feishu replies on the floor whenever the local terminal was
// the active driver (handleCardAction never surfaced the ActionPreempt path
// as a user-visible toast, so the reply just vanished). Until a real
// multi-driver flow ships, treat Feishu input as "take over silently" —
// the user is in Feishu because they aren't at the local terminal.
// Bundling "text\r" or "text\n" into one SendInput call reads as a paste in
// claude/codex TUIs — the trailing CR/LF lands in the input buffer but never
// triggers submit (memory: see feedback_template_send_split_cr). The router
// must split text replies into two SendInput calls: the text first, then a
// bare CR (0x0d) after a short delay.
func TestRouter_InputSubmitSplitsTextAndEnter(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	dec := r.RouteCardAction("t", "ou_owner", "input", "", "say hi")
	if dec.Action != ActionInject {
		t.Fatalf("action = %v, want inject", dec.Action)
	}
	// The CR send is scheduled on a goroutine with ~16ms delay; give it time.
	time.Sleep(80 * time.Millisecond)

	if len(stub.sentIn) != 2 {
		t.Fatalf("sentIn count = %d, want 2 (text + CR): %q", len(stub.sentIn), stub.sentIn)
	}
	if string(stub.sentIn[0]) != "say hi" {
		t.Errorf("sentIn[0] = %q, want %q (text without trailing CR/LF)", stub.sentIn[0], "say hi")
	}
	if len(stub.sentIn[1]) != 1 || stub.sentIn[1][0] != 0x0d {
		t.Errorf("sentIn[1] = %v, want one byte 0x0d (Enter)", stub.sentIn[1])
	}
}

// Same split for IM-message replies (user typed text in a Feishu DM thread)
// — same TUI, same bundling-as-paste issue.
func TestRouter_ReplySubmitSplitsTextAndEnter(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriber{openID: "ou_owner"}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	r.RouteReply("m", "ou_owner", "go test")
	time.Sleep(80 * time.Millisecond)

	if len(stub.sentIn) != 2 {
		t.Fatalf("sentIn count = %d, want 2: %q", len(stub.sentIn), stub.sentIn)
	}
	if string(stub.sentIn[0]) != "go test" || stub.sentIn[1][0] != 0x0d {
		t.Errorf("split shape wrong: %q", stub.sentIn)
	}
}

func TestRouter_TakesOverFromExistingDriver(t *testing.T) {
	idx := NewCardIndex()
	idx.Put(&CardAnchor{SessionID: "s", CardMsgID: "m", CardToken: "t", OwnerOpenID: "ou_owner"})
	stub := &stubSubscriberDriverAware{
		stubSubscriber:    stubSubscriber{openID: "ou_owner"},
		currentDriverName: "local-terminal",
	}
	r := NewRouter(idx, func(string) Subscriber { return stub })

	dec := r.RouteReply("m", "ou_owner", "go test")
	if dec.Action != ActionInject {
		t.Fatalf("action = %v, want inject (silent takeover)", dec.Action)
	}
	if !stub.claimed {
		t.Errorf("ClaimDriver should have been called to seize driver from local-terminal")
	}
	time.Sleep(80 * time.Millisecond) // CR is sent on a delayed goroutine
	if len(stub.sentIn) != 2 || string(stub.sentIn[0]) != "go test" || stub.sentIn[1][0] != 0x0d {
		t.Errorf("sentIn = %q, want [text, {0x0d}] split", stub.sentIn)
	}
}

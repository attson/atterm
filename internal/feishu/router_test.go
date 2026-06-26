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
func (s *stubSubscriber) OwnerOpenID() string { return s.openID }

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
	if len(stub.sentIn) != 1 || string(stub.sentIn[0]) != "go test ./...\n" {
		t.Errorf("sentIn = %q, want one entry with trailing newline", stub.sentIn)
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

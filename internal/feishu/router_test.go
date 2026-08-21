package feishu

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// stubSubscriber is written from two goroutines, so it is mutex-guarded and
// its fields are read through accessors.
//
// This is not defensive: the router deliberately sends a template's text and
// its carriage return as two SendInput calls 16ms apart (see the comment on
// that goroutine, and feedback_template_send_split_cr — bundling them makes
// Codex read the pair as a paste). The second call therefore lands on a timer
// goroutine while the test is reading. The sleep the tests used made that work
// in practice and left the read unsynchronised, which -race reported as a data
// race on every run — the whole package was red under -race.
//
// Production is unaffected: FeishuSubscriber.SendInput forwards to
// Session.SendInbound, a channel send.
type stubSubscriber struct {
	mu      sync.Mutex
	openID  string
	sentIn  [][]byte
	claimed bool
}

func (s *stubSubscriber) ClaimDriver() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claimed = true
}

func (s *stubSubscriber) SendInput(b []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sentIn = append(s.sentIn, append([]byte(nil), b...))
	return true
}

// inputs returns a snapshot of everything SendInput has received so far.
func (s *stubSubscriber) inputs() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.sentIn...)
}

// didClaim reports whether ClaimDriver was called.
func (s *stubSubscriber) didClaim() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.claimed
}

// waitForInputs polls until the stub has received want calls, or fails the
// test. Replaces a fixed sleep: the CR arrives on a 16ms timer, so a sleep
// either wastes time or — on a loaded machine — is simply too short.
func (s *stubSubscriber) waitForInputs(t *testing.T, want int) [][]byte {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := s.inputs()
		if len(got) >= want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d SendInput calls; got %d: %q", want, len(got), got)
		}
		time.Sleep(time.Millisecond)
	}
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
	if !stub.didClaim() {
		t.Errorf("expected ClaimDriver call when no current driver (router should call it)")
	}
	// The CR rides a 16ms timer goroutine; wait for it rather than sleeping.
	sent := stub.waitForInputs(t, 2)
	if len(sent) != 2 || string(sent[0]) != "go test ./..." || sent[1][0] != 0x0d {
		t.Errorf("sentIn = %q, want [text, {0x0d}] split", sent)
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
	if got := stub.inputs(); len(got) != 0 {
		t.Errorf("input should not have been forwarded, got: %q", got)
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
	if got := stub.inputs(); len(got) != 1 || got[0][0] != 0x03 {
		t.Errorf("sentIn = %v, want one entry starting 0x03", got)
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
	// The CR send is scheduled on a goroutine with ~16ms delay.
	sent := stub.waitForInputs(t, 2)

	if len(sent) != 2 {
		t.Fatalf("sentIn count = %d, want 2 (text + CR): %q", len(sent), sent)
	}
	if string(sent[0]) != "say hi" {
		t.Errorf("sentIn[0] = %q, want %q (text without trailing CR/LF)", sent[0], "say hi")
	}
	if len(sent[1]) != 1 || sent[1][0] != 0x0d {
		t.Errorf("sentIn[1] = %v, want one byte 0x0d (Enter)", sent[1])
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
	sent := stub.waitForInputs(t, 2)

	if len(sent) != 2 {
		t.Fatalf("sentIn count = %d, want 2: %q", len(sent), sent)
	}
	if string(sent[0]) != "go test" || sent[1][0] != 0x0d {
		t.Errorf("split shape wrong: %q", sent)
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
	if !stub.didClaim() {
		t.Errorf("ClaimDriver should have been called to seize driver from local-terminal")
	}
	sent := stub.waitForInputs(t, 2)
	if len(sent) != 2 || string(sent[0]) != "go test" || sent[1][0] != 0x0d {
		t.Errorf("sentIn = %q, want [text, {0x0d}] split", sent)
	}
}

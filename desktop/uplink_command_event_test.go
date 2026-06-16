package main

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestSendCommandEventDropsWhenNotConnected(t *testing.T) {
	// A zero-valued *uplink with no out chan must not panic.
	u := &uplink{}
	u.SendCommandEvent(uuid.New(), 0, 12500, "atterm")
}

func TestSendCommandEventQueuesFrameOnOutChan(t *testing.T) {
	ch := make(chan proto.Frame, 4)
	u := &uplink{}
	u.out = ch
	sid := uuid.New()
	u.SendCommandEvent(sid, 0, 12500, "atterm")
	select {
	case f := <-ch:
		if f.Type != proto.TypeCommandEvent {
			t.Fatalf("Type = %v; want TypeCommandEvent", f.Type)
		}
		if f.SessionID != sid {
			t.Fatalf("SessionID = %v; want %v", f.SessionID, sid)
		}
		payload, err := proto.DecodeCommandEvent(f)
		if err != nil {
			t.Fatalf("DecodeCommandEvent: %v", err)
		}
		if payload.ExitCode != 0 || payload.ElapsedMS != 12500 || payload.Label != "atterm" {
			t.Fatalf("payload = %+v", payload)
		}
	default:
		t.Fatal("no frame queued")
	}
}

// TestSendCommandEvent_SealStripsPlaintext: M6-final — once the body
// is successfully sealed under the user's account_key, the outgoing
// CommandEventPayload MUST have its Label, ExitCode, and ElapsedMS
// fields zeroed. Otherwise the relay still sees plaintext alongside
// the envelope and the seal accomplishes nothing.
func TestSendCommandEvent_SealStripsPlaintext(t *testing.T) {
	ak := mustAccountKey32(t)
	ch := make(chan proto.Frame, 4)
	u := &uplink{accountKey: func() []byte { return ak }}
	u.out = ch
	sid := uuid.New()
	u.SendCommandEvent(sid, 127, 65000, "deploy")
	select {
	case f := <-ch:
		payload, err := proto.DecodeCommandEvent(f)
		if err != nil {
			t.Fatalf("DecodeCommandEvent: %v", err)
		}
		if payload.Label != "" {
			t.Fatalf("Label not stripped: %q", payload.Label)
		}
		if payload.ExitCode != 0 {
			t.Fatalf("ExitCode not stripped: %d", payload.ExitCode)
		}
		if payload.ElapsedMS != 0 {
			t.Fatalf("ElapsedMS not stripped: %d", payload.ElapsedMS)
		}
		if len(payload.SealedBody) == 0 {
			t.Fatalf("SealedBody missing — strip should only fire on successful seal")
		}
	default:
		t.Fatal("no frame queued")
	}
}

// TestSendCommandEvent_NoAccountKey_KeepsPlaintext: when no account_key
// is available (legacy / dev path) the agent must NOT strip plaintext —
// otherwise the relay's legacy webhook + push composition gets empty
// fields and the dev experience regresses.
func TestSendCommandEvent_NoAccountKey_KeepsPlaintext(t *testing.T) {
	ch := make(chan proto.Frame, 4)
	u := &uplink{}
	u.out = ch
	sid := uuid.New()
	u.SendCommandEvent(sid, 127, 65000, "deploy")
	select {
	case f := <-ch:
		payload, err := proto.DecodeCommandEvent(f)
		if err != nil {
			t.Fatalf("DecodeCommandEvent: %v", err)
		}
		if payload.Label != "deploy" || payload.ExitCode != 127 || payload.ElapsedMS != 65000 {
			t.Fatalf("plaintext fields tampered with: %+v", payload)
		}
		if len(payload.SealedBody) != 0 {
			t.Fatalf("SealedBody set without account_key: %x", payload.SealedBody)
		}
	default:
		t.Fatal("no frame queued")
	}
}

func TestSendCommandEventDropsWhenBufferFull(t *testing.T) {
	// 0-capacity buffer means any non-receiving channel is "full" from the
	// sender's perspective. Must not block or panic.
	ch := make(chan proto.Frame)
	u := &uplink{}
	u.out = ch
	u.SendCommandEvent(uuid.New(), 0, 12500, "atterm")
	// If the impl blocked, we would never get here. The test passing is
	// itself the assertion. Add a explicit no-receive check anyway:
	select {
	case <-ch:
		t.Fatal("frame somehow delivered to unbuffered chan without receiver")
	default:
		// expected — frame dropped
	}
}

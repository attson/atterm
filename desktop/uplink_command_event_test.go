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

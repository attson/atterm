package main

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestBroadcastCommandFinishedSilentWithNilUplink(t *testing.T) {
	a := &App{}
	// Must not panic; success path is "nothing observable".
	a.BroadcastCommandFinished(uuid.New().String(), 0, 12500, "atterm")
}

func TestBroadcastCommandFinishedSendsThroughUplink(t *testing.T) {
	ch := make(chan proto.Frame, 4)
	u := &uplink{}
	u.out = ch
	a := &App{uplink: u}
	sid := uuid.New()
	a.BroadcastCommandFinished(sid.String(), 0, 12500, "atterm")
	select {
	case f := <-ch:
		if f.Type != proto.TypeCommandEvent || f.SessionID != sid {
			t.Fatalf("unexpected frame: type=%v sid=%v", f.Type, f.SessionID)
		}
	default:
		t.Fatal("uplink received no frame")
	}
}

func TestBroadcastCommandFinishedSilentOnInvalidUUID(t *testing.T) {
	ch := make(chan proto.Frame, 4)
	u := &uplink{}
	u.out = ch
	a := &App{uplink: u}
	a.BroadcastCommandFinished("not-a-uuid", 0, 12500, "atterm")
	select {
	case <-ch:
		t.Fatal("frame queued despite invalid session id")
	default:
		// ok
	}
}

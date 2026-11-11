package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func TestUplinkSessionCreateReturnsFeatureRemovedWithoutSessionGrowth(t *testing.T) {
	sessionID := uuid.New()
	responses := make(chan proto.Frame, 4)

	addr := startFSTestRelay(t, func(ctx context.Context, c *websocket.Conn) {
		reqPayload, err := json.Marshal(proto.SessionCreatePayload{
			RequestID: "retired-1", HostID: "host-1", ProfileID: "p1",
		})
		if err != nil {
			return
		}
		// Malformed legacy requests must be dropped before the valid correlated reply.
		for _, payload := range [][]byte{
			nil,
			[]byte("{"),
			[]byte(`{"host_id":"host-1","profile_id":"p1"}`),
			[]byte(`{"request_id":"unknown-field","host_id":"host-1","profile_id":"p1","shell":"/bin/sh"}`),
			append(append([]byte(nil), reqPayload...), []byte(` {}`)...),
		} {
			if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeSessionCreate, Payload: payload})); err != nil {
				return
			}
		}
		if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
			Type: proto.TypeSessionCreate, Payload: reqPayload,
		})); err != nil {
			return
		}
		// Retired requests must leave ordinary input processing live.
		if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
			Type: proto.TypeIn, SessionID: sessionID, Payload: []byte("k"),
		})); err != nil {
			return
		}
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			f, err := proto.Unmarshal(data)
			if err != nil || f.Type != proto.TypeSessionCreated {
				continue
			}
			var p proto.SessionCreatedPayload
			if err := json.Unmarshal(f.Payload, &p); err != nil {
				continue
			}
			select {
			case responses <- f:
			default:
			}
		}
	})

	host := newTestRelayHost(t)
	sess := session.New(sessionID, proto.SessionInfo{ID: sessionID.String(), Cols: 80, Rows: 24})
	if _, err := host.server.Registry().Add(sess); err != nil {
		t.Fatalf("registry add: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	u := newUplink("ws://"+addr, "tok", proto.RemotePermissionFull, host, nil, nil, false)
	u.eventsEmit = func(context.Context, string, ...interface{}) {}

	done := make(chan struct{})
	go func() { defer close(done); _ = u.runOnce(ctx) }()

	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn || string(f.Payload) != "k" {
			t.Fatalf("inbound frame = type %v payload %q, want IN %q", f.Type, f.Payload, "k")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("IN frame was not processed after the retired create request")
	}

	select {
	case frame := <-responses:
		var response proto.SessionCreatedPayload
		if err := json.Unmarshal(frame.Payload, &response); err != nil {
			t.Fatal(err)
		}
		if frame.SessionID != uuid.Nil || response.RequestID != "retired-1" || response.OK || response.SessionID != "" || response.Error != "feature_removed" {
			t.Fatalf("retired response frame=%+v payload=%+v", frame, response)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("retired session-create response never arrived")
	}
	if got := len(host.server.Registry().List()); got != 1 {
		t.Fatalf("registry grew to %d sessions", got)
	}
	if got := len(host.Snapshot()); got != 1 {
		t.Fatalf("host snapshot grew to %d sessions", got)
	}

	cancel()
	<-done
}

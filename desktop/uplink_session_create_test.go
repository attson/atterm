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

// TestUplinkWiresSessionCreateOffTheReadLoop pins the wiring, not the
// handler. Every property asserted in session_create_handler_test.go
// constructs sessionCreateHandler directly, so all of them would still pass
// if runOnce regressed to handling TypeSessionCreate inline. That is
// specifically the shape TestUplinkWiresFSRequestsThroughTheBoundedPool
// exists to catch for the FS path — this is the same test for the
// session-create path: a real frame, over a real uplink, must not block a
// keystroke on the same connection while the PTY fork is still "running".
func TestUplinkWiresSessionCreateOffTheReadLoop(t *testing.T) {
	sessionID := uuid.New()
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	responses := make(chan proto.SessionCreatedPayload, 4)

	addr := startFSTestRelay(t, func(ctx context.Context, c *websocket.Conn) {
		reqPayload, err := json.Marshal(proto.SessionCreatePayload{
			RequestID: "slow-1", HostID: "host-1", ProfileID: "p1",
		})
		if err != nil {
			return
		}
		if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
			Type: proto.TypeSessionCreate, Payload: reqPayload,
		})); err != nil {
			return
		}
		// The keystroke is written immediately behind the slow create, on an
		// unrelated, already-existing session.
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
			case responses <- p:
			default:
			}
		}
	})

	host := newTestRelayHost(t)
	cfg := host.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "p1", Name: "P1"}}
	if err := host.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}
	sess := session.New(sessionID, proto.SessionInfo{ID: sessionID.String(), Cols: 80, Rows: 24})
	if _, err := host.server.Registry().Add(sess); err != nil {
		t.Fatalf("registry add: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	u := newUplink("ws://"+addr, "tok", proto.RemotePermissionFull, host, nil, nil, false)
	u.eventsEmit = func(context.Context, string, ...interface{}) {}
	u.sessionCreateExec = func(ctx context.Context, req NewSessionReq) (uuid.UUID, error) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
		return uuid.New(), nil
	}

	done := make(chan struct{})
	go func() { defer close(done); _ = u.runOnce(ctx) }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("session-create request never reached the executor")
	}

	select {
	case f := <-sess.Inbound():
		if f.Type != proto.TypeIn || string(f.Payload) != "k" {
			t.Fatalf("inbound frame = type %v payload %q, want IN %q", f.Type, f.Payload, "k")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("IN frame was not processed while a session-create was in flight: the uplink read loop is blocked on the fork")
	}

	select {
	case resp := <-responses:
		t.Fatalf("session-create response arrived before the executor was released: %+v", resp)
	default:
	}

	close(release)
	select {
	case resp := <-responses:
		if !resp.OK || resp.RequestID != "slow-1" {
			t.Fatalf("released response = %+v, want ok for slow-1", resp)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("session-create response never arrived after the executor was released")
	}

	cancel()
	<-done
}

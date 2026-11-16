package main

import (
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

func TestRelayHostUplinkOutputLanesPreserveDriver(t *testing.T) {
	for _, removeDriver := range []bool{false, true} {
		name := "remove-viewer-lane"
		if removeDriver {
			name = "remove-driver-lane"
		}
		t.Run(name, func(t *testing.T) {
			host := newTestRelayHost(t)
			id := uuid.New()
			sess := session.New(id, proto.SessionInfo{ID: id.String(), Cols: 80, Rows: 24})
			if _, err := host.server.Registry().Add(sess); err != nil {
				t.Fatal(err)
			}
			first, _, err := host.SubscribeLocal(id, 0)
			if err != nil {
				t.Fatal(err)
			}
			second, _, err := host.SubscribeLocal(id, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err := host.ClaimLocalDriver(id, "remote-user", "phone"); err != nil {
				t.Fatal(err)
			}
			driver, viewer := first, second
			if !sess.IsDriver(driver) {
				driver, viewer = second, first
			}
			if !sess.IsDriver(driver) {
				t.Fatal("claim did not select an active proxy")
			}
			removed, remaining := viewer, driver
			if removeDriver {
				removed, remaining = driver, viewer
			}
			host.UnsubscribeLocal(id, removed)
			if !sess.IsDriver(remaining) || sess.DriverClientID() != "remote-user" || sess.DriverClientName() != "phone" {
				t.Fatal("stopping one output lane lost the remote driver")
			}
			if err := host.ClaimLocalDriver(id, "other-user", "tablet"); err != nil {
				t.Fatal(err)
			}
			if !sess.IsDriver(remaining) || sess.DriverClientID() != "other-user" {
				t.Fatal("claim failed on surviving lane")
			}
			input := proto.Frame{Type: proto.TypeIn, SessionID: id, Payload: []byte("echo ready\r")}
			if err := host.SendLocalInbound(id, input); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-sess.Inbound():
				if got.Type != input.Type || string(got.Payload) != string(input.Payload) {
					t.Fatal("input changed")
				}
			default:
				t.Fatal("surviving lane did not route input")
			}
			host.UnsubscribeLocal(id, remaining)
			if sess.DriverClientID() != "" || sess.IsDriver(remaining) {
				t.Fatal("final lane removal left a driver")
			}
			if err := host.ClaimLocalDriver(id, "late", "late"); err == nil {
				t.Fatal("claim succeeded after final lane stopped")
			}
		})
	}
}

func TestRelayHostSubscribeLocalRejectsClosedSubscriber(t *testing.T) {
	host := newTestRelayHost(t)
	id := uuid.New()
	sess := session.New(id, proto.SessionInfo{ID: id.String()})
	if _, err := host.server.Registry().Add(sess); err != nil {
		t.Fatal(err)
	}
	// Closing the undrained subscriber synchronously before Subscribe returns
	// reproduces the same API result as a failed snapshot/catch-up enqueue.
	sess.SetSubscriberCountHook(func(count int) {
		if count != 1 {
			return
		}
		sess.MarkContentOpaque()
		for seq := uint64(1); seq <= 5000 && sess.SubscriberCount() != 0; seq++ {
			sess.PushOut(seq, []byte("opaque"))
		}
	})
	sub, _, err := host.SubscribeLocal(id, 0)
	if err == nil || sub != nil {
		t.Fatal("SubscribeLocal accepted a subscriber that had already closed")
	}
	if sess.IsClosed() {
		t.Fatal("fixture closed the session instead of the subscriber")
	}
	host.mu.Lock()
	count := len(host.uplinkSubs[id])
	host.mu.Unlock()
	if count != 0 {
		t.Fatal("closed subscriber was retained as an active uplink proxy")
	}
}

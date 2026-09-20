package relay

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// TestServiceReapUsesIdleTTL verifies a paired, idle-but-alive lease survives
// well past the old 60s window (ping keepalive keeps its transport up), and is
// only reaped after the generous idle TTL. Pending (unpaired) leases still fall
// to the short pending TTL.
func TestServiceReapUsesIdleTTL(t *testing.T) {
	h := &serviceHub{
		services: make(map[uuid.UUID]*serviceLease),
		requests: make(map[string]*serviceLease),
		sessions: make(map[uuid.UUID]serviceUplinkRoute),
	}
	now := time.Now()
	paired := &serviceLease{
		id:          uuid.New(),
		ready:       true,
		clientConn:  &websocket.Conn{},
		hostConn:    &websocket.Conn{},
		lastActive:  now.Add(-20 * time.Minute),
		done:        make(chan struct{}),
		connections: map[uint32]struct{}{},
	}
	h.services[paired.id] = paired

	if closing := h.reapExpiredLocked(now); len(closing) != 0 {
		t.Fatalf("paired lease idle 20m was reaped (idle TTL is %v)", serviceIdleTTL)
	}

	paired.lastActive = now.Add(-(serviceIdleTTL + time.Minute))
	if closing := h.reapExpiredLocked(now); len(closing) != 1 {
		t.Fatalf("paired lease idle past TTL should be reaped, got %d", len(closing))
	}

	pending := &serviceLease{
		id:          uuid.New(),
		lastActive:  now.Add(-(servicePendingTTL + time.Second)),
		done:        make(chan struct{}),
		connections: map[uint32]struct{}{},
	}
	h.services[pending.id] = pending
	if closing := h.reapExpiredLocked(now); len(closing) != 1 {
		t.Fatalf("unpaired lease past pending TTL should be reaped, got %d", len(closing))
	}
}

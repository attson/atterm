package relay

import (
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/webpush"
	"github.com/google/uuid"
)

// TestNotifSuppressWebPushWhenWatched verifies that WebPush is suppressed
// while a session is being watched (≥1 subscriber) and fires once the last
// subscriber leaves.
func TestNotifSuppressWebPushWhenWatched(t *testing.T) {
	const ownerUserID = "user_notify_suppress_test"

	// --- WebPush spy: real *webpush.Service with recording HTTP transport ---
	svc, err := webpush.Open(t.TempDir(), "mailto:test@example.com")
	if err != nil {
		t.Fatalf("webpush.Open: %v", err)
	}
	pushRec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, pushRec)
	// Register a subscription so DispatchCommandFinished actually fans out.
	addRelayWebPushSubscription(t, svc, ownerUserID)

	// --- Server under test ---
	srv := NewServer(Config{WebPush: svc})

	// --- Build mirrorState with a session owned by ownerUserID ---
	sid := uuid.New()
	info := proto.SessionInfo{
		Command:          "bash",
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionFull,
	}
	mirrorSess := session.New(sid, info)
	mirrorSess.OwnerUserID = ownerUserID
	ms := &mirrorState{sess: mirrorSess}
	mirrors := map[uuid.UUID]*mirrorState{sid: ms}
	var mu sync.Mutex

	frame, _ := proto.EncodeCommandEvent(sid, proto.CommandEventPayload{
		ExitCode: 0, ElapsedMS: 1000, Label: "suppress-test",
	})

	// ----------------------------------------------------------------
	// Phase 1: session HAS a subscriber — WebPush must NOT fire.
	// ----------------------------------------------------------------
	sub, _ := mirrorSess.Subscribe(0, "client-1", "viewer")
	defer mirrorSess.Unsubscribe(sub)

	if n := mirrorSess.SubscriberCount(); n != 1 {
		t.Fatalf("want 1 subscriber before test; got %d", n)
	}

	srv.handleUplinkCommandEvent(frame, mirrors, &mu)

	if got := pushRec.count(); got != 0 {
		t.Fatalf("Phase 1: expected 0 WebPush calls with subscriber attached; got %d", got)
	}

	// ----------------------------------------------------------------
	// Phase 2: remove subscriber — WebPush MUST now fire.
	// ----------------------------------------------------------------
	mirrorSess.Unsubscribe(sub)

	if n := mirrorSess.SubscriberCount(); n != 0 {
		t.Fatalf("want 0 subscribers after Unsubscribe; got %d", n)
	}

	srv.handleUplinkCommandEvent(frame, mirrors, &mu)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if pushRec.count() >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := pushRec.count(); got < 1 {
		t.Fatalf("Phase 2: expected ≥1 WebPush call after subscriber removed; got %d", got)
	}
}

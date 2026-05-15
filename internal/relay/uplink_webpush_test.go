package relay

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/webpush"
	"github.com/google/uuid"
)

type recordingHTTPClientForRelayTest struct {
	mu sync.Mutex
	n  int
}

func (r *recordingHTTPClientForRelayTest) Do(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	return &http.Response{
		StatusCode: 201,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     http.Header{},
	}, nil
}

func (r *recordingHTTPClientForRelayTest) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func TestUplinkCommandEventTriggersDispatchWhenSessionInManifest(t *testing.T) {
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	srv := NewServer(Config{
		Token:   "write-token",
		WebPush: svc,
	})
	svc.SetSessionResolver(srv.WebPushSessionResolver)
	// Register a subscription that will be the fanout target.
	sub := webpush.Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk"
	sub.Keys.Auth = "zqbxT6JKstKSY9JKibZLSQ"
	_ = svc.AddSubscription(tokenHash("write-token"), sub)
	// Set up a synthetic mirrors map containing one session id; register a
	// session in the registry so the resolver can find it.
	sid := uuid.New()
	info := proto.SessionInfo{
		Command:          "bash",
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionFull,
	}
	srv.Registry().Add(session.New(sid, info))
	defer srv.Registry().Remove(sid)
	ms := &mirrorState{sess: session.New(sid, info)}
	mirrors := map[uuid.UUID]*mirrorState{sid: ms}
	var mu sync.Mutex
	// Encode the frame.
	frame, _ := proto.EncodeCommandEvent(sid, proto.CommandEventPayload{ExitCode: 0, ElapsedMS: 12500, Label: "atterm"})
	// Call the handler.
	srv.handleUplinkCommandEvent(frame, mirrors, &mu)
	// Wait for at least one HTTP push.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.count() >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("uplink command event did not trigger Web Push dispatch; got %d", rec.count())
	_ = context.Background
}

func TestUplinkCommandEventDropsUnknownSession(t *testing.T) {
	svc, _ := webpush.Open(t.TempDir(), "mailto:test@example.com")
	rec := &recordingHTTPClientForRelayTest{}
	webpush.InjectTransportForTesting(svc, rec)
	srv := NewServer(Config{
		Token:   "write-token",
		WebPush: svc,
	})
	sub := webpush.Subscription{Endpoint: "https://push.example/abc"}
	sub.Keys.P256dh = "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk"
	sub.Keys.Auth = "zqbxT6JKstKSY9JKibZLSQ"
	_ = svc.AddSubscription(tokenHash("write-token"), sub)
	// Mirrors map is empty (no sessions known to this uplink).
	mirrors := map[uuid.UUID]*mirrorState{}
	var mu sync.Mutex
	frame, _ := proto.EncodeCommandEvent(uuid.New(), proto.CommandEventPayload{ExitCode: 0})
	srv.handleUplinkCommandEvent(frame, mirrors, &mu)
	// Allow goroutines a moment.
	time.Sleep(150 * time.Millisecond)
	if rec.count() != 0 {
		t.Fatalf("dispatched %d pushes for unknown session; want 0", rec.count())
	}
}

func TestUplinkCommandEventNoOpWhenWebPushNil(t *testing.T) {
	srv := NewServer(Config{Token: "write-token", WebPush: nil})
	mirrors := map[uuid.UUID]*mirrorState{}
	var mu sync.Mutex
	frame, _ := proto.EncodeCommandEvent(uuid.New(), proto.CommandEventPayload{ExitCode: 0})
	// Must not panic.
	srv.handleUplinkCommandEvent(frame, mirrors, &mu)
}

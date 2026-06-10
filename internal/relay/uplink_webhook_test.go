package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/webhook"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// relayFakeWebhookStore is a minimal WebhookStore implementation used only in
// relay-level tests. It is purposely separate from the one in the webhook
// package (which is unexported) to avoid import cycles.
type relayFakeWebhookStore struct {
	hooks []webhook.Webhook
}

func (f *relayFakeWebhookStore) ListWebhooks(_ context.Context, _ string) ([]webhook.Webhook, error) {
	return f.hooks, nil
}

// TestUplink_CommandEvent_FiresWebhook verifies that when Config.Webhook is set,
// a TypeCommandEvent frame received from an authenticated uplink connection
// results in a POST to the registered webhook URL.
func TestUplink_CommandEvent_FiresWebhook(t *testing.T) {
	var mu sync.Mutex
	postCount := 0

	// Stand up a fake HTTP sink that counts incoming POST requests.
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		postCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()

	// Build a store + user for the uplink auth.
	store, userID, apiToken := newUplinkTestStore(t)
	_ = userID

	// Wire a real webhook.Service backed by a fake store that returns one hook
	// pointing at the sink server.
	fakeStore := &relayFakeWebhookStore{
		hooks: []webhook.Webhook{
			{ID: "wh-1", URL: sink.URL, Format: "generic"},
		},
	}
	webhookSvc := webhook.New(fakeStore)

	resolver := NewIdentityResolver(store)
	srv := NewServer(Config{
		Resolver: resolver,
		Store:    store,
		Webhook:  webhookSvc,
	})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect the uplink with a valid session token.
	conn, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+apiToken)
	if err != nil {
		t.Fatalf("dial uplink: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	// Announce a session so the relay can route the command_event.
	sid := uuid.New()
	sendAnnounce(t, ctx, conn, sid)

	// Wait for the session to appear in the registry (announced → mirrored).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.Registry().Get(sid); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := srv.Registry().Get(sid); !ok {
		t.Fatal("session not found in registry after ANNOUNCE")
	}

	// Drain the AUTH_INFO frame before sending the command event.
	readAndDiscardAuthInfo(t, ctx, conn)

	// Send a TypeCommandEvent frame for the announced session.
	evFrame, err := proto.EncodeCommandEvent(sid, proto.CommandEventPayload{
		ExitCode:  0,
		ElapsedMS: 1234,
		Label:     "test-label",
	})
	if err != nil {
		t.Fatalf("EncodeCommandEvent: %v", err)
	}
	raw := proto.Marshal(evFrame)
	if err := conn.Write(ctx, websocket.MessageBinary, raw); err != nil {
		t.Fatalf("write command_event: %v", err)
	}

	// Wait for the webhook POST to arrive at the sink (dispatched asynchronously).
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := postCount
		mu.Unlock()
		if n >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	mu.Lock()
	n := postCount
	mu.Unlock()
	t.Fatalf("expected at least 1 webhook POST; got %d", n)
}

// TestUplink_CommandEvent_NilWebhook_NoPanic verifies that when Config.Webhook
// is nil, a TypeCommandEvent frame is handled without panic (the nil guard works).
func TestUplink_CommandEvent_NilWebhook_NoPanic(t *testing.T) {
	store, _, apiToken := newUplinkTestStore(t)

	resolver := NewIdentityResolver(store)
	// No Webhook in Config — must not panic.
	srv := NewServer(Config{
		Resolver: resolver,
		Store:    store,
		// Webhook intentionally left nil.
	})
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+apiToken)
	if err != nil {
		t.Fatalf("dial uplink: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	sid := uuid.New()
	sendAnnounce(t, ctx, conn, sid)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := srv.Registry().Get(sid); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	readAndDiscardAuthInfo(t, ctx, conn)

	evFrame, err := proto.EncodeCommandEvent(sid, proto.CommandEventPayload{
		ExitCode:  1,
		ElapsedMS: 500,
		Label:     "nil-webhook-test",
	})
	if err != nil {
		t.Fatalf("EncodeCommandEvent: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, proto.Marshal(evFrame)); err != nil {
		t.Fatalf("write command_event: %v", err)
	}

	// Give the handler a moment to process; if it panics the server will crash
	// and the next read/operation would fail.
	time.Sleep(200 * time.Millisecond)
	// Verify the connection is still alive by checking the session is in the registry.
	if _, ok := srv.Registry().Get(sid); !ok {
		t.Error("session disappeared from registry unexpectedly")
	}
}

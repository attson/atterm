package webpush

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingHTTPClient struct {
	mu       sync.Mutex
	requests []*http.Request
	statuses []int
}

func (c *recordingHTTPClient) Do(req *http.Request) (*http.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.requests = append(c.requests, req)
	status := 201
	if len(c.statuses) > 0 {
		status = c.statuses[0]
		c.statuses = c.statuses[1:]
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewReader(nil)),
		Header:     http.Header{},
	}, nil
}

func newServiceWithFakeTransport(t *testing.T, statuses ...int) (*Service, *recordingHTTPClient) {
	t.Helper()
	dir := t.TempDir()
	svc, err := Open(dir, "mailto:test@example.com")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	rec := &recordingHTTPClient{statuses: statuses}
	svc.tr = newTransport(svc.vapidPriv, svc.vapidPub, svc.subject, rec)
	return svc, rec
}

// validP256SubKey is a real P-256 public key (base64url) usable as a
// subscription's p256dh value so webpush-go's encryption can succeed.
const (
	validP256SubKey = "BNNL5ZaTfK81qhXOx23-wewhigUeFb632jN6LvRWCFH1ubQr77FE_9qV1FuojuRmHP42zmf34rXgW80OvUVDgTk"
	validAuthKey    = "zqbxT6JKstKSY9JKibZLSQ"
)

func subWithEndpoint(endpoint string) Subscription {
	sub := Subscription{Endpoint: endpoint}
	sub.Keys.P256dh = validP256SubKey
	sub.Keys.Auth = validAuthKey
	return sub
}

func TestDispatchCommandFinishedFansOutToAllSubscriptions(t *testing.T) {
	svc, rec := newServiceWithFakeTransport(t)
	_ = svc.AddSubscription("user1", subWithEndpoint("https://push.example/a"))
	_ = svc.AddSubscription("user1", subWithEndpoint("https://push.example/b"))
	sid := uuid.New()
	svc.DispatchCommandFinished("user1", CommandFinished{
		SessionID: sid,
		HostID:    uuid.New(),
		ExitCode:  0,
		ElapsedMS: 12500,
		Label:     "atterm",
	})
	// Wait briefly for goroutine fanout.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		n := len(rec.requests)
		rec.mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	rec.mu.Lock()
	n := len(rec.requests)
	rec.mu.Unlock()
	if n != 2 {
		t.Fatalf("requests = %d; want 2", n)
	}
}

func TestDispatchCommandFinishedReturnsImmediately(t *testing.T) {
	svc, _ := newServiceWithFakeTransport(t)
	_ = svc.AddSubscription("user1", subWithEndpoint("https://push.example/a"))
	start := time.Now()
	svc.DispatchCommandFinished("user1", CommandFinished{SessionID: uuid.New()})
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("DispatchCommandFinished took %v; expected to return immediately", elapsed)
	}
}

func TestDispatch410PrunesSubscription(t *testing.T) {
	svc, _ := newServiceWithFakeTransport(t, 410)
	_ = svc.AddSubscription("user1", subWithEndpoint("https://push.example/gone"))
	svc.DispatchCommandFinished("user1", CommandFinished{SessionID: uuid.New()})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(svc.subStore.ByUser("user1")) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("subscription not pruned after 410; got %v", svc.subStore.ByUser("user1"))
}

func TestDispatch429KeepsSubscription(t *testing.T) {
	svc, _ := newServiceWithFakeTransport(t, 429)
	_ = svc.AddSubscription("user1", subWithEndpoint("https://push.example/throttled"))
	svc.DispatchCommandFinished("user1", CommandFinished{SessionID: uuid.New()})
	// Give the goroutine a moment then assert sub still present.
	time.Sleep(150 * time.Millisecond)
	if len(svc.subStore.ByUser("user1")) != 1 {
		t.Fatalf("subscription was pruned on 429; want kept")
	}
}

func TestDispatchEmitsPayloadWithSessionTagAndExpectedFields(t *testing.T) {
	sid := uuid.New()
	hid := uuid.New()
	body := payloadJSON(CommandFinished{
		SessionID: sid, HostID: hid, ExitCode: 127, ElapsedMS: 65000, Label: "build", RemotePermission: "view",
	})
	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Tag   string `json:"tag"`
		Data  struct {
			NotificationType string `json:"notificationType"`
			ClickURL         string `json:"clickUrl"`
			RemotePermission string `json:"remotePermission"`
			ExitCode         int    `json:"exitCode"`
			ElapsedMs        int    `json:"elapsedMs"`
			SessionID        string `json:"sessionId"`
			HostID           string `json:"hostId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if !strings.Contains(payload.Title, "AT Term") || !strings.Contains(payload.Title, "build") {
		t.Fatalf("title = %q; want contains 'AT Term' and 'build'", payload.Title)
	}
	if !strings.Contains(payload.Body, "exit 127") || !strings.Contains(payload.Body, "1m5s") {
		t.Fatalf("body = %q; want contains 'exit 127' and '1m5s'", payload.Body)
	}
	if payload.Tag != sid.String() {
		t.Fatalf("tag = %q; want %s", payload.Tag, sid)
	}
	if payload.Data.SessionID != sid.String() || payload.Data.HostID != hid.String() {
		t.Fatalf("data ids mismatch: %+v", payload.Data)
	}
	if payload.Data.NotificationType != NotificationCommandFailed {
		t.Fatalf("notificationType = %q; want %q", payload.Data.NotificationType, NotificationCommandFailed)
	}
	if payload.Data.ClickURL != "/#/s/"+sid.String()+"?notification=command_failed&permission=view" {
		t.Fatalf("clickUrl = %q", payload.Data.ClickURL)
	}
	if payload.Data.RemotePermission != "view" {
		t.Fatalf("remotePermission = %q; want view", payload.Data.RemotePermission)
	}
}

func TestDispatchCommandFinishedUsesCompletedTypeForZeroExit(t *testing.T) {
	sid := uuid.New()
	body := payloadJSON(CommandFinished{SessionID: sid, ExitCode: 0})
	var payload struct {
		Data struct {
			NotificationType string `json:"notificationType"`
			ClickURL         string `json:"clickUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if payload.Data.NotificationType != NotificationCommandCompleted {
		t.Fatalf("notificationType = %q; want %q", payload.Data.NotificationType, NotificationCommandCompleted)
	}
	if payload.Data.ClickURL != "/#/s/"+sid.String()+"?notification=command_completed" {
		t.Fatalf("clickUrl = %q", payload.Data.ClickURL)
	}
}

func TestDispatchSessionNotificationBuildsWaitingInputDeepLink(t *testing.T) {
	sid := uuid.New()
	body := sessionNotificationPayloadJSON(SessionNotification{
		SessionID:        sid,
		HostID:           uuid.New(),
		NotificationType: NotificationWaitingInput,
		Label:            "codex",
		RemotePermission: "view",
	})
	var payload struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Tag   string `json:"tag"`
		Data  struct {
			NotificationType string `json:"notificationType"`
			ClickURL         string `json:"clickUrl"`
			RemotePermission string `json:"remotePermission"`
			SessionID        string `json:"sessionId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if payload.Data.NotificationType != NotificationWaitingInput {
		t.Fatalf("notificationType = %q; want %q", payload.Data.NotificationType, NotificationWaitingInput)
	}
	if payload.Data.ClickURL != "/#/s/"+sid.String()+"?notification=waiting_input&focus=input&permission=view" {
		t.Fatalf("clickUrl = %q", payload.Data.ClickURL)
	}
	if payload.Data.RemotePermission != "view" {
		t.Fatalf("remotePermission = %q; want view", payload.Data.RemotePermission)
	}
	if payload.Tag != sid.String() || payload.Data.SessionID != sid.String() {
		t.Fatalf("session id not routed: tag=%q data=%q", payload.Tag, payload.Data.SessionID)
	}
	if !strings.Contains(payload.Title, "codex") || !strings.Contains(payload.Body, "waiting for input") {
		t.Fatalf("unexpected copy: title=%q body=%q", payload.Title, payload.Body)
	}
}

func TestDispatchSessionNotificationBuildsIdleAndDisconnectedTypes(t *testing.T) {
	for _, tc := range []struct {
		name             string
		notificationType string
		wantURL          string
	}{
		{
			name:             "idle",
			notificationType: NotificationIdleTimeout,
			wantURL:          "?notification=idle_timeout",
		},
		{
			name:             "uplink disconnected",
			notificationType: NotificationUplinkDisconnected,
			wantURL:          "?notification=uplink_disconnected",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sid := uuid.New()
			body := sessionNotificationPayloadJSON(SessionNotification{
				SessionID:        sid,
				NotificationType: tc.notificationType,
				IdleForSeconds:   90,
			})
			var payload struct {
				Data struct {
					NotificationType string `json:"notificationType"`
					ClickURL         string `json:"clickUrl"`
					SessionID        string `json:"sessionId"`
					IdleForSeconds   int    `json:"idleForSeconds"`
				} `json:"data"`
			}
			if err := json.Unmarshal(body, &payload); err != nil {
				t.Fatalf("payload not valid JSON: %v", err)
			}
			if payload.Data.NotificationType != tc.notificationType {
				t.Fatalf("notificationType = %q; want %q", payload.Data.NotificationType, tc.notificationType)
			}
			if payload.Data.ClickURL != "/#/s/"+sid.String()+tc.wantURL {
				t.Fatalf("clickUrl = %q", payload.Data.ClickURL)
			}
			if payload.Data.SessionID != sid.String() {
				t.Fatalf("sessionId = %q; want %s", payload.Data.SessionID, sid)
			}
			if tc.notificationType == NotificationIdleTimeout && payload.Data.IdleForSeconds != 90 {
				t.Fatalf("idleForSeconds = %d; want 90", payload.Data.IdleForSeconds)
			}
		})
	}
}

func TestDispatchTruncatesLabelTo256(t *testing.T) {
	huge := strings.Repeat("a", 1000)
	body := payloadJSON(CommandFinished{Label: huge})
	if !strings.Contains(string(body), strings.Repeat("a", 256)) {
		t.Fatal("payload missing truncated label")
	}
	if strings.Contains(string(body), strings.Repeat("a", 257)) {
		t.Fatal("payload was not truncated to 256")
	}
}

func TestDispatchSendTest(t *testing.T) {
	svc, rec := newServiceWithFakeTransport(t)
	_ = svc.AddSubscription("user1", subWithEndpoint("https://push.example/a"))
	n := svc.SendTest("user1")
	if n != 1 {
		t.Fatalf("SendTest returned %d; want 1", n)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rec.mu.Lock()
		got := len(rec.requests)
		rec.mu.Unlock()
		if got >= 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("SendTest did not POST a push")
}

func TestDispatchNoOpWhenEmptyUserID(t *testing.T) {
	svc, rec := newServiceWithFakeTransport(t)
	_ = svc.AddSubscription("user1", subWithEndpoint("https://push.example/a"))
	// Dispatch with empty ownerUserID — no subscribers matched, no push.
	svc.DispatchCommandFinished("", CommandFinished{SessionID: uuid.New()})
	time.Sleep(100 * time.Millisecond)
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.requests) != 0 {
		t.Fatalf("unexpected pushes with empty ownerUserID; %d requests", len(rec.requests))
	}
	_ = context.Background()
}

// TestDispatch_FilteredByOwner verifies that DispatchCommandFinished sends
// pushes only to the ownerUserID's subscriptions and not to other users'.
func TestDispatch_FilteredByOwner(t *testing.T) {
	svc, _ := newServiceWithFakeTransport(t)

	// Per-user atomic counters.
	var userACount, userBCount atomic.Int64

	// Build a per-user fake HTTP client that increments the appropriate counter.
	makeClient := func(counter *atomic.Int64) HTTPClient {
		return &funcHTTPClient{fn: func(req *http.Request) (*http.Response, error) {
			counter.Add(1)
			return &http.Response{
				StatusCode: 201,
				Body:       io.NopCloser(bytes.NewReader(nil)),
				Header:     http.Header{},
			}, nil
		}}
	}

	// We need separate transport per user to distinguish sends. Instead, use
	// a single shared transport that tracks per-endpoint invocations.
	// Reset and rebuild with a tracking client.
	type call struct{ endpoint string }
	var mu sync.Mutex
	var calls []call
	sharedClient := &funcHTTPClient{fn: func(req *http.Request) (*http.Response, error) {
		mu.Lock()
		calls = append(calls, call{endpoint: req.URL.String()})
		mu.Unlock()
		return &http.Response{
			StatusCode: 201,
			Body:       io.NopCloser(bytes.NewReader(nil)),
			Header:     http.Header{},
		}, nil
	}}
	InjectTransportForTesting(svc, sharedClient)

	// Suppress "unused" warnings.
	_ = makeClient(&userACount)
	_ = makeClient(&userBCount)

	// Register one sub each for userA and userB.
	endpointA := "https://push.example/userA"
	endpointB := "https://push.example/userB"
	_ = svc.AddSubscription("u_A", subWithEndpoint(endpointA))
	_ = svc.AddSubscription("u_B", subWithEndpoint(endpointB))

	// Dispatch for userA only.
	svc.DispatchCommandFinished("u_A", CommandFinished{
		SessionID: uuid.New(),
		HostID:    uuid.New(),
		ExitCode:  0,
		ElapsedMS: 1000,
		Label:     "test",
	})

	// Wait for the goroutine to fire.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(calls)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Allow a small grace period for any unexpected second call.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	got := append([]call(nil), calls...)
	mu.Unlock()

	if len(got) != 1 {
		t.Fatalf("expected exactly 1 push (userA only); got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].endpoint, "userA") {
		t.Fatalf("push went to wrong endpoint %q; want userA's endpoint", got[0].endpoint)
	}
}

// funcHTTPClient is a test helper that satisfies HTTPClient with a function.
type funcHTTPClient struct {
	fn func(*http.Request) (*http.Response, error)
}

func (f *funcHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return f.fn(req)
}

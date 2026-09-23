package relay

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func newDirectSignalTestServer(t *testing.T) (*Server, string, string, uuid.UUID, string) {
	t.Helper()
	store := userstore.NewInMemory(t)
	ctx := context.Background()
	user, err := store.CreateOpaqueUser(ctx, "direct@example.com")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.CreateSession(ctx, user.ID, "direct-test", "127.0.0.1", userstore.DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	srv := NewServer(Config{
		Store:               store,
		Resolver:            NewIdentityResolver(store),
		DirectSignalEnabled: true,
	})
	hostID := "host-direct-a"
	sessionID := uuid.New()
	sess := session.New(sessionID, proto.SessionInfo{
		ID:               sessionID.String(),
		HostID:           hostID,
		Command:          "bash",
		RemotePermission: proto.RemotePermissionControl,
	})
	sess.OwnerUserID = user.ID
	if _, err := srv.registry.Add(sess); err != nil {
		t.Fatal(err)
	}
	srv.sessionCreateRoutes().registerHost(hostID, make(chan proto.Frame, 1), user.ID)
	return srv, token, user.ID, sessionID, hostID
}

func dialDirectSignal(t *testing.T, ctx context.Context, server *httptest.Server, token string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/direct-signal"
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + token},
	}})
	if err != nil {
		t.Fatalf("dial direct signal: %v", err)
	}
	return conn
}

func writeDirectSignal(t *testing.T, ctx context.Context, conn *websocket.Conn, message directSignalMessage) {
	t.Helper()
	data, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("write direct signal: %v", err)
	}
}

func readDirectSignalTest(t *testing.T, ctx context.Context, conn *websocket.Conn) directSignalMessage {
	t.Helper()
	kind, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read direct signal: %v", err)
	}
	if kind != websocket.MessageText {
		t.Fatalf("message kind = %v", kind)
	}
	var message directSignalMessage
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	return message
}

func TestDirectSignalIssuesAndRoutesSingleUseAttempt(t *testing.T) {
	srv, token, userID, sessionID, hostID := newDirectSignalTestServer(t)
	httpServer := httptest.NewServer(srv)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	host := dialDirectSignal(t, ctx, httpServer, token)
	defer host.Close(websocket.StatusNormalClosure, "")
	writeDirectSignal(t, ctx, host, directSignalMessage{
		Version:    directSignalVersion,
		Kind:       "hello",
		Role:       "host",
		HostID:     hostID,
		SessionIDs: []string{sessionID.String()},
	})
	if got := readDirectSignalTest(t, ctx, host); got.Kind != "hello_ok" {
		t.Fatalf("host hello = %+v", got)
	}

	client := dialDirectSignal(t, ctx, httpServer, token)
	defer client.Close(websocket.StatusNormalClosure, "")
	writeDirectSignal(t, ctx, client, directSignalMessage{
		Version:          directSignalVersion,
		Kind:             "hello",
		Role:             "client",
		ClientInstanceID: "client-iphone",
	})
	if got := readDirectSignalTest(t, ctx, client); got.Kind != "hello_ok" {
		t.Fatalf("client hello = %+v", got)
	}

	writeDirectSignal(t, ctx, client, directSignalMessage{
		Version:   directSignalVersion,
		Kind:      "direct_request",
		RequestID: "request-1",
		SessionID: sessionID.String(),
		SinceSeq:  42,
	})
	clientAttempt := readDirectSignalTest(t, ctx, client)
	hostAttempt := readDirectSignalTest(t, ctx, host)
	if clientAttempt.Kind != "direct_attempt" || hostAttempt.Kind != "direct_attempt" {
		t.Fatalf("attempt kinds client=%q host=%q", clientAttempt.Kind, hostAttempt.Kind)
	}
	if clientAttempt.AttemptID == "" || clientAttempt.AttemptID != hostAttempt.AttemptID || clientAttempt.Ticket != hostAttempt.Ticket {
		t.Fatalf("attempt mismatch client=%+v host=%+v", clientAttempt, hostAttempt)
	}
	if clientAttempt.UserID != userID || clientAttempt.SessionID != sessionID.String() || clientAttempt.HostID != hostID ||
		clientAttempt.ClientInstanceID != "client-iphone" || clientAttempt.Permission != proto.RemotePermissionControl || clientAttempt.SinceSeq != 42 {
		t.Fatalf("attempt claims = %+v", clientAttempt)
	}
	ticket, err := base64.RawURLEncoding.DecodeString(clientAttempt.Ticket)
	if err != nil || len(ticket) != directTicketBytes {
		t.Fatalf("ticket: len=%d err=%v", len(ticket), err)
	}
	attemptID := uuid.MustParse(clientAttempt.AttemptID)
	srv.direct.mu.Lock()
	stored := srv.direct.attempts[attemptID]
	ticketHash := sha256.Sum256(ticket)
	if stored == nil || stored.ticketHash != ticketHash {
		t.Error("Relay did not retain matching ticket hash")
	}
	srv.direct.mu.Unlock()

	writeDirectSignal(t, ctx, client, directSignalMessage{
		Version:    directSignalVersion,
		Kind:       "signal",
		AttemptID:  clientAttempt.AttemptID,
		SignalType: "offer",
		Payload:    "opaque-sdp-offer",
	})
	if got := readDirectSignalTest(t, ctx, host); got.Kind != "signal" || got.SignalType != "offer" || got.Payload != "opaque-sdp-offer" || got.Ticket != "" {
		t.Fatalf("routed offer = %+v", got)
	}

	writeDirectSignal(t, ctx, host, directSignalMessage{
		Version:   directSignalVersion,
		Kind:      "consumed",
		AttemptID: clientAttempt.AttemptID,
	})
	if got := readDirectSignalTest(t, ctx, client); got.Kind != "consumed" || got.AttemptID != clientAttempt.AttemptID {
		t.Fatalf("consume ack = %+v", got)
	}
	srv.direct.mu.Lock()
	_, stillPresent := srv.direct.attempts[attemptID]
	srv.direct.mu.Unlock()
	if stillPresent {
		t.Fatal("consumed attempt remains registered")
	}
	metrics := srv.directStats.snapshot()
	if metrics.Attempts != 1 || metrics.Successes != 1 || metrics.Fallbacks != 0 {
		t.Fatalf("direct metrics after success = %+v", metrics)
	}
	writeDirectSignal(t, ctx, client, directSignalMessage{
		Version:   directSignalVersion,
		Kind:      "direct_stats",
		BytesSent: 1,
	})
	if got := readDirectSignalTest(t, ctx, client); got.Kind != "error" || got.Code != "invalid_stats" {
		t.Fatalf("client stats response = %+v", got)
	}
	writeDirectSignal(t, ctx, host, directSignalMessage{
		Version:   directSignalVersion,
		Kind:      "direct_stats",
		BytesSent: directMaxStatsDelta + 1,
	})
	if got := readDirectSignalTest(t, ctx, host); got.Kind != "error" || got.Code != "invalid_stats" {
		t.Fatalf("oversized stats response = %+v", got)
	}
	writeDirectSignal(t, ctx, host, directSignalMessage{
		Version:       directSignalVersion,
		Kind:          "direct_stats",
		BytesAvoided:  512 * 1024,
		BytesSent:     500 * 1024,
		BytesReceived: 12 * 1024,
	})
	writeDirectSignal(t, ctx, client, directSignalMessage{
		Version: directSignalVersion,
		Kind:    "direct_result",
		Code:    "route_lost",
	})
	deadline := time.Now().Add(time.Second)
	for {
		metrics = srv.directStats.snapshot()
		if metrics.BytesAvoided == 512*1024 && metrics.Fallbacks == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("direct metrics after reports = %+v", metrics)
		}
		time.Sleep(time.Millisecond)
	}
	pending := srv.directStats.drain()
	if len(pending) != 1 || pending[0].UserID != userID || pending[0].Attempts != 1 || pending[0].Successes != 1 ||
		pending[0].Fallbacks != 1 || pending[0].BytesSent != 500*1024 || pending[0].BytesReceived != 12*1024 {
		t.Fatalf("per-user direct metrics = %+v", pending)
	}

	writeDirectSignal(t, ctx, host, directSignalMessage{
		Version:   directSignalVersion,
		Kind:      "consumed",
		AttemptID: clientAttempt.AttemptID,
	})
	if got := readDirectSignalTest(t, ctx, host); got.Kind != "error" || got.Code != "attempt_unavailable" {
		t.Fatalf("second consume = %+v", got)
	}
}

func TestDirectSignalRejectsWrongOwnerAndInactiveHost(t *testing.T) {
	srv, token, userID, sessionID, hostID := newDirectSignalTestServer(t)
	store := srv.cfg.Store.(*userstore.DBStore)
	otherToken, _ := createUserWithSession(t, store, "other-direct@example.com")
	httpServer := httptest.NewServer(srv)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	other := dialDirectSignal(t, ctx, httpServer, otherToken)
	defer other.Close(websocket.StatusNormalClosure, "")
	writeDirectSignal(t, ctx, other, directSignalMessage{Version: directSignalVersion, Kind: "hello", Role: "client", ClientInstanceID: "other-client"})
	_ = readDirectSignalTest(t, ctx, other)
	writeDirectSignal(t, ctx, other, directSignalMessage{Version: directSignalVersion, Kind: "direct_request", RequestID: "wrong-owner", SessionID: sessionID.String()})
	if got := readDirectSignalTest(t, ctx, other); got.Kind != "error" || got.Code != "direct_unauthorized" {
		t.Fatalf("wrong owner response = %+v", got)
	}

	owner := dialDirectSignal(t, ctx, httpServer, token)
	defer owner.Close(websocket.StatusNormalClosure, "")
	writeDirectSignal(t, ctx, owner, directSignalMessage{Version: directSignalVersion, Kind: "hello", Role: "client", ClientInstanceID: "owner-client"})
	_ = readDirectSignalTest(t, ctx, owner)
	routes := srv.sessionCreateRoutes()
	routes.mu.Lock()
	delete(routes.hosts, hostKey{ownerUserID: userID, hostID: hostID})
	routes.mu.Unlock()
	writeDirectSignal(t, ctx, owner, directSignalMessage{Version: directSignalVersion, Kind: "direct_request", RequestID: "offline", SessionID: sessionID.String()})
	if got := readDirectSignalTest(t, ctx, owner); got.Kind != "error" || got.Code != "host_offline" {
		t.Fatalf("inactive host response = %+v", got)
	}
}

func TestDirectSignalDisabledByDefault(t *testing.T) {
	srv, token := serverWithSession(t)
	httpServer := httptest.NewServer(srv)
	defer httpServer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/direct-signal"
	conn, response, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: http.Header{
		"Authorization": []string{"Bearer " + token},
	}})
	if conn != nil {
		conn.CloseNow()
	}
	if err == nil || response == nil || response.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled dial err=%v status=%v", err, response)
	}
}

func TestDirectSignalAttemptExpiryAndSignalBounds(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	hub := newDirectSignalHub()
	hub.now = func() time.Time { return now }
	_, cancelClient := context.WithCancel(context.Background())
	defer cancelClient()
	_, cancelHost := context.WithCancel(context.Background())
	defer cancelHost()
	client := &directSignalPeer{ownerUserID: "owner", role: "client", clientInstanceID: "client", out: make(chan directSignalMessage, 8), cancel: cancelClient}
	host := &directSignalPeer{ownerUserID: "owner", role: "host", hostID: "host", out: make(chan directSignalMessage, 8), cancel: cancelHost}
	sessionID := uuid.New()
	hub.registerHost(host, "host", []uuid.UUID{sessionID})
	ticket := make([]byte, directTicketBytes)
	if err := hub.begin(client, sessionID, "host", proto.RemotePermissionControl, "request", 0, ticket); err != nil {
		t.Fatal(err)
	}
	clientAttempt := <-client.out
	<-host.out

	if err := hub.route(client, directSignalMessage{
		Kind:       "signal",
		AttemptID:  clientAttempt.AttemptID,
		SignalType: "offer",
		Payload:    strings.Repeat("x", directMaxSDPBytes+1),
	}); directSignalErrorCode(err) != "invalid_signal" {
		t.Fatalf("oversize offer err=%v code=%q", err, directSignalErrorCode(err))
	}

	now = now.Add(directTicketTTL + time.Millisecond)
	if err := hub.route(client, directSignalMessage{
		Kind:       "signal",
		AttemptID:  clientAttempt.AttemptID,
		SignalType: "offer",
		Payload:    "offer",
	}); directSignalErrorCode(err) != "attempt_unavailable" {
		t.Fatalf("expired attempt err=%v code=%q", err, directSignalErrorCode(err))
	}
	if got := <-client.out; got.Kind != "cancel" || got.Code != "ticket_expired" {
		t.Fatalf("client expiry = %+v", got)
	}
	if got := <-host.out; got.Kind != "cancel" || got.Code != "ticket_expired" {
		t.Fatalf("host expiry = %+v", got)
	}
}

func TestDirectSignalHostReconnectKeepsNewestRegistration(t *testing.T) {
	hub := newDirectSignalHub()
	sessionID := uuid.New()
	newPeer := func() *directSignalPeer {
		_, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		return &directSignalPeer{
			ownerUserID: "owner",
			role:        "host",
			out:         make(chan directSignalMessage, 1),
			cancel:      cancel,
		}
	}
	oldHost := newPeer()
	newHost := newPeer()

	hub.registerHost(oldHost, "host", []uuid.UUID{sessionID})
	hub.registerHost(newHost, "host", []uuid.UUID{sessionID})
	hub.unregister(oldHost)

	hub.mu.Lock()
	registered := hub.hosts[sessionID]
	hub.mu.Unlock()
	if registered != newHost {
		t.Fatal("stale host disconnect removed the replacement registration")
	}
}

func TestDirectSignalRejectsOversizeCancelCode(t *testing.T) {
	hub := newDirectSignalHub()
	_, cancelClient := context.WithCancel(context.Background())
	defer cancelClient()
	_, cancelHost := context.WithCancel(context.Background())
	defer cancelHost()
	client := &directSignalPeer{ownerUserID: "owner", role: "client", clientInstanceID: "client", out: make(chan directSignalMessage, 2), cancel: cancelClient}
	host := &directSignalPeer{ownerUserID: "owner", role: "host", out: make(chan directSignalMessage, 2), cancel: cancelHost}
	sessionID := uuid.New()
	hub.registerHost(host, "host", []uuid.UUID{sessionID})
	if err := hub.begin(client, sessionID, "host", proto.RemotePermissionControl, "request", 0, make([]byte, directTicketBytes)); err != nil {
		t.Fatal(err)
	}
	attempt := <-client.out
	<-host.out

	err := hub.route(client, directSignalMessage{
		Kind:      "cancel",
		AttemptID: attempt.AttemptID,
		Code:      strings.Repeat("x", directMaxIdentifierBytes+1),
	})
	if directSignalErrorCode(err) != "invalid_request" {
		t.Fatalf("oversize cancel err=%v code=%q", err, directSignalErrorCode(err))
	}
}

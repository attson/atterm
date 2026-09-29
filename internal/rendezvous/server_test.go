package rendezvous

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"nhooyr.io/websocket"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestServerAuthenticatesPresenceAndRoutesOpaqueSignals(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	handler, err := New(Config{
		AllowedOrigins: []string{"https://app.example"},
		Now:            clock.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	topic := opaqueID(1)
	hostPresence := opaqueID(2)
	memberPresence := opaqueID(3)
	hostIdentity, _ := peercrypto.GenerateIdentity()
	memberIdentity, _ := peercrypto.GenerateIdentity()
	host := dialRegistered(t, server.URL, "https://app.example", topic, hostPresence, RoleHost, hostIdentity)
	defer host.Close(websocket.StatusNormalClosure, "")
	member := dialRegistered(t, server.URL, "https://app.example", topic, memberPresence, RoleMember, memberIdentity)
	defer member.Close(websocket.StatusNormalClosure, "")

	presence := readUntilKind(t, host, KindPresence)
	if presence.Event != PresenceOnline || presence.PresenceID != memberPresence || presence.Role != RoleMember {
		t.Fatalf("presence event=%+v", presence)
	}
	payload := base64.RawURLEncoding.EncodeToString([]byte("opaque-encrypted-sdp"))
	messageID := opaqueMessageID(4)
	writeJSON(t, member, PublishMessage{
		Version: Version, Kind: KindPublish, MessageID: messageID,
		To: hostPresence, Payload: payload,
	})
	got := readUntilKind(t, host, KindSignal)
	if got.MessageID != messageID || got.From != memberPresence || got.Payload != payload {
		t.Fatalf("signal=%+v", got)
	}
	ack := readUntilKind(t, member, KindAck)
	if ack.MessageID != messageID || ack.State != DeliveryDelivered {
		t.Fatalf("ack=%+v", ack)
	}

	metrics := getBody(t, server.URL+MetricsPath)
	for _, secret := range []string{topic, hostPresence, memberPresence, payload, hostIdentity.PeerID()} {
		if strings.Contains(metrics, secret) {
			t.Fatalf("metrics exposed routing or identity material %q", secret)
		}
	}
	if !strings.Contains(metrics, "atterm_rendezvous_forwarded_messages_total 1") {
		t.Fatalf("metrics missing forwarded counter:\n%s", metrics)
	}
	health := getBody(t, server.URL+HealthPath)
	if health != `{"status":"ok","protocol_version":1}`+"\n" {
		t.Fatalf("health=%q", health)
	}
}

func TestServerQueuesOfflineSignalsUntilTTLAndLosesThemOnRestart(t *testing.T) {
	clock := &testClock{now: time.Unix(1_800_000_000, 0)}
	handler, err := New(Config{Now: clock.Now, MailboxTTL: 120 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	topic := opaqueID(10)
	senderPresence := opaqueID(11)
	firstTarget := opaqueID(12)
	expiredTarget := opaqueID(13)
	restartTarget := opaqueID(14)
	senderIdentity, _ := peercrypto.GenerateIdentity()
	sender := dialRegistered(t, server.URL, "", topic, senderPresence, RoleMember, senderIdentity)
	defer sender.Close(websocket.StatusNormalClosure, "")

	queueSignal := func(target, messageID, body string) {
		t.Helper()
		writeJSON(t, sender, PublishMessage{
			Version: Version, Kind: KindPublish, MessageID: messageID,
			To: target, Payload: base64.RawURLEncoding.EncodeToString([]byte(body)),
		})
		if ack := readUntilKind(t, sender, KindAck); ack.State != DeliveryQueued {
			t.Fatalf("queued ack=%+v", ack)
		}
	}
	queueSignal(firstTarget, opaqueMessageID(15), "deliver-before-ttl")
	queueSignal(expiredTarget, opaqueMessageID(16), "expire-after-ttl")
	queueSignal(restartTarget, opaqueMessageID(17), "lost-on-restart")

	firstIdentity, _ := peercrypto.GenerateIdentity()
	first := dialRegistered(t, server.URL, "", topic, firstTarget, RoleHost, firstIdentity)
	defer first.Close(websocket.StatusNormalClosure, "")
	if got := readUntilKind(t, first, KindSignal); decodePayload(t, got.Payload) != "deliver-before-ttl" {
		t.Fatalf("queued signal=%+v", got)
	}

	clock.Advance(121 * time.Second)
	expiredIdentity, _ := peercrypto.GenerateIdentity()
	expired := dialRegistered(t, server.URL, "", topic, expiredTarget, RoleHost, expiredIdentity)
	defer expired.Close(websocket.StatusNormalClosure, "")
	assertNoKind(t, expired, KindSignal)

	restarted, err := New(Config{Now: clock.Now, MailboxTTL: 120 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	restartServer := httptest.NewServer(restarted)
	defer restartServer.Close()
	restartIdentity, _ := peercrypto.GenerateIdentity()
	restart := dialRegistered(t, restartServer.URL, "", topic, restartTarget, RoleHost, restartIdentity)
	defer restart.Close(websocket.StatusNormalClosure, "")
	assertNoKind(t, restart, KindSignal)
}

func TestServerRejectsForgedIdentityOriginAndBounds(t *testing.T) {
	handler, err := New(Config{
		AllowedOrigins:         []string{"https://allowed.example"},
		MaxConnections:         2,
		MaxConnectionsPerIP:    2,
		MaxConnectionsPerTopic: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, response, err := websocket.Dial(ctx, wsURL(server.URL), &websocket.DialOptions{
		Subprotocols: []string{Subprotocol},
		HTTPHeader:   http.Header{"Origin": []string{"https://evil.example"}},
	})
	if err == nil || response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong-origin dial response=%v err=%v", response, err)
	}

	topic := opaqueID(21)
	identity, _ := peercrypto.GenerateIdentity()
	conn, challenge := dialChallenge(t, server.URL, "https://allowed.example")
	request, err := SignRegistration(identity, challenge.Challenge, topic, opaqueID(22), RoleMember)
	if err != nil {
		t.Fatal(err)
	}
	request.PresenceID = opaqueID(23)
	writeJSON(t, conn, request)
	if got := readEvent(t, conn); got.Kind != KindError || got.Code != CodeUnauthorized {
		t.Fatalf("forged registration response=%+v", got)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")

	firstIdentity, _ := peercrypto.GenerateIdentity()
	first := dialRegistered(t, server.URL, "https://allowed.example", topic, opaqueID(24), RoleHost, firstIdentity)
	defer first.Close(websocket.StatusNormalClosure, "")
	secondIdentity, _ := peercrypto.GenerateIdentity()
	second, secondChallenge := dialChallenge(t, server.URL, "https://allowed.example")
	secondRequest, _ := SignRegistration(secondIdentity, secondChallenge.Challenge, topic, opaqueID(25), RoleMember)
	writeJSON(t, second, secondRequest)
	if got := readEvent(t, second); got.Kind != KindError || got.Code != CodeTopicCapacity {
		t.Fatalf("topic-capacity response=%+v", got)
	}
	_ = second.Close(websocket.StatusNormalClosure, "")

	writeJSON(t, first, PublishMessage{
		Version: Version, Kind: KindPublish, MessageID: opaqueMessageID(26),
		To: opaqueID(27), Payload: base64.RawURLEncoding.EncodeToString(make([]byte, MaxPayloadBytes+1)),
	})
	if got := readEvent(t, first); got.Kind != KindError || got.Code != CodeMessageTooLarge {
		t.Fatalf("oversize response=%+v", got)
	}
}

func TestServerDeduplicatesPublishRetriesAndEnforcesGlobalConnections(t *testing.T) {
	handler, err := New(Config{
		MaxConnections: 2, MaxConnectionsPerIP: 2, MaxConnectionsPerTopic: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	topic := opaqueID(31)
	firstPresence := opaqueID(32)
	secondPresence := opaqueID(33)
	firstIdentity, _ := peercrypto.GenerateIdentity()
	secondIdentity, _ := peercrypto.GenerateIdentity()
	first := dialRegistered(t, server.URL, "", topic, firstPresence, RoleHost, firstIdentity)
	defer first.Close(websocket.StatusNormalClosure, "")
	second := dialRegistered(t, server.URL, "", topic, secondPresence, RoleMember, secondIdentity)
	defer second.Close(websocket.StatusNormalClosure, "")
	_ = readUntilKind(t, first, KindPresence)

	publish := PublishMessage{
		Version: Version, Kind: KindPublish, MessageID: opaqueMessageID(34), To: firstPresence,
		Payload: base64.RawURLEncoding.EncodeToString([]byte("one-delivery")),
	}
	writeJSON(t, second, publish)
	if got := readUntilKind(t, first, KindSignal); decodePayload(t, got.Payload) != "one-delivery" {
		t.Fatalf("first delivery=%+v", got)
	}
	if ack := readUntilKind(t, second, KindAck); ack.State != DeliveryDelivered {
		t.Fatalf("first ack=%+v", ack)
	}
	writeJSON(t, second, publish)
	if ack := readUntilKind(t, second, KindAck); ack.State != DeliveryDelivered {
		t.Fatalf("retry ack=%+v", ack)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, response, err := websocket.Dial(ctx, wsURL(server.URL), &websocket.DialOptions{Subprotocols: []string{Subprotocol}})
	if err == nil || response == nil || response.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("global-capacity response=%v err=%v", response, err)
	}
	assertNoKind(t, first, KindSignal)
}

func TestServerCloseDisconnectsUnauthenticatedWebSockets(t *testing.T) {
	handler, err := New(Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	conn, _ := dialChallenge(t, server.URL, "")

	handler.Close()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("unauthenticated websocket remained open after server close")
	}
}

func TestExternalServiceContract(t *testing.T) {
	serverURL := strings.TrimSpace(os.Getenv("ATTERM_RENDEZVOUS_TEST_URL"))
	if serverURL == "" {
		t.Skip("ATTERM_RENDEZVOUS_TEST_URL is not set")
	}
	topic := opaqueID(41)
	hostPresence := opaqueID(42)
	memberPresence := opaqueID(43)
	hostIdentity, _ := peercrypto.GenerateIdentity()
	memberIdentity, _ := peercrypto.GenerateIdentity()
	host := dialRegistered(t, serverURL, "", topic, hostPresence, RoleHost, hostIdentity)
	defer host.Close(websocket.StatusNormalClosure, "")
	member := dialRegistered(t, serverURL, "", topic, memberPresence, RoleMember, memberIdentity)
	defer member.Close(websocket.StatusNormalClosure, "")
	_ = readUntilKind(t, host, KindPresence)

	payload := base64.RawURLEncoding.EncodeToString([]byte("external-contract-ciphertext"))
	writeJSON(t, member, PublishMessage{
		Version: Version, Kind: KindPublish, MessageID: opaqueMessageID(44),
		To: hostPresence, Payload: payload,
	})
	if got := readUntilKind(t, host, KindSignal); got.Payload != payload || got.From != memberPresence {
		t.Fatalf("external signal=%+v", got)
	}
	if ack := readUntilKind(t, member, KindAck); ack.State != DeliveryDelivered {
		t.Fatalf("external ack=%+v", ack)
	}
	if health := getBody(t, serverURL+HealthPath); health != `{"status":"ok","protocol_version":1}`+"\n" {
		t.Fatalf("external health=%q", health)
	}
}

func dialRegistered(t *testing.T, serverURL, origin, topic, presenceID string, role Role, identity *peercrypto.Identity) *websocket.Conn {
	t.Helper()
	conn, challenge := dialChallenge(t, serverURL, origin)
	request, err := SignRegistration(identity, challenge.Challenge, topic, presenceID, role)
	if err != nil {
		t.Fatal(err)
	}
	writeJSON(t, conn, request)
	if got := readEvent(t, conn); got.Kind != KindRegistered {
		t.Fatalf("registration response=%+v", got)
	}
	return conn
}

func dialChallenge(t *testing.T, serverURL, origin string) (*websocket.Conn, EventMessage) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	header := make(http.Header)
	if origin != "" {
		header.Set("Origin", origin)
	}
	conn, response, err := websocket.Dial(ctx, wsURL(serverURL), &websocket.DialOptions{
		Subprotocols: []string{Subprotocol}, HTTPHeader: header,
	})
	if err != nil {
		t.Fatalf("dial response=%v: %v", response, err)
	}
	challenge := readEvent(t, conn)
	if challenge.Kind != KindChallenge || challenge.Challenge == "" {
		t.Fatalf("challenge=%+v", challenge)
	}
	return conn, challenge
}

func wsURL(serverURL string) string {
	return "ws" + strings.TrimPrefix(serverURL, "http") + ConnectPath
}

func writeJSON(t *testing.T, conn *websocket.Conn, value any) {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := conn.Write(ctx, websocket.MessageText, payload); err != nil {
		t.Fatal(err)
	}
}

func readEvent(t *testing.T, conn *websocket.Conn) EventMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, payload, err := conn.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var event EventMessage
	if err := json.Unmarshal(payload, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func readUntilKind(t *testing.T, conn *websocket.Conn, kind string) EventMessage {
	t.Helper()
	for {
		event := readEvent(t, conn)
		if event.Kind == kind {
			return event
		}
	}
}

func assertNoKind(t *testing.T, conn *websocket.Conn, kind string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	for {
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var event EventMessage
		if json.Unmarshal(payload, &event) == nil && event.Kind == kind {
			t.Fatalf("unexpected %s event: %+v", kind, event)
		}
	}
}

func getBody(t *testing.T, url string) string {
	t.Helper()
	response, err := http.Get(url) //nolint:gosec -- httptest URL
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func opaqueID(seed byte) string {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = seed
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func opaqueMessageID(seed byte) string {
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = seed
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodePayload(t *testing.T, payload string) string {
	t.Helper()
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

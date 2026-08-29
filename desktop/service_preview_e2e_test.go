package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// TestServicePreviewLoopbackE2E exercises the complete endpoint data path:
// sealed SERVICE_OPEN -> owner service host -> encrypted multiplex WebSocket
// -> client loopback listener -> a real HTTP server bound on the owner.
// The relay fixture is intentionally a keyless byte pipe, matching production.
func TestServicePreviewLoopbackE2E(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "preview-through-owner")
	}))
	defer target.Close()
	targetURL, err := url.Parse(target.URL)
	if err != nil {
		t.Fatal(err)
	}
	portNumber, err := strconv.Atoi(targetURL.Port())
	if err != nil || portNumber < 1 || portNumber > 65535 {
		t.Fatalf("target port: %q %v", targetURL.Port(), err)
	}

	pipe := newServicePipeFixture()
	relay := httptest.NewServer(http.HandlerFunc(pipe.serveHTTP))
	defer relay.Close()
	relayWS := "ws" + strings.TrimPrefix(relay.URL, "http")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	accountKey := bytes.Repeat([]byte{0x42}, e2eecrypto.SessionKeySize)
	sessionID := uuid.New()
	serviceID := uuid.New()

	// A Preview is not a terminal viewer. Keeping lifecycle hooks at zero
	// proves this path never becomes a subscriber and therefore cannot trigger
	// lazy STREAM_REQUEST/STREAM_STOP.
	model := session.New(sessionID, proto.SessionInfo{RemotePermission: proto.RemotePermissionFull})
	defer model.Close()
	var firstSubscribers atomic.Int32
	var lastSubscribers atomic.Int32
	model.SetSubscriberLifecycle(func() { firstSubscribers.Add(1) }, func() { lastSubscribers.Add(1) })

	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, sessionID)
	if err != nil {
		t.Fatal(err)
	}
	fields, _ := json.Marshal(proto.SealedServiceOpenFields{Port: uint16(portNumber), Scheme: "http"})
	sealed, err := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(proto.TypeServiceOpen), fields)
	if err != nil {
		t.Fatal(err)
	}
	request := proto.ServiceOpenPayload{
		RequestID:  "request-1",
		ServiceID:  serviceID.String(),
		HostTicket: "host-ticket",
		Sealed:     sealed,
	}
	rawRequest, _ := json.Marshal(request)
	hostOut := make(chan proto.Frame, 1)
	hosts := newServiceHostManager(ctx, relayWS, "token", false, func() []byte { return accountKey }, hostOut, proto.RemotePermissionFull)
	defer hosts.close()
	hosts.open(proto.Frame{Type: proto.TypeServiceOpen, SessionID: sessionID, Payload: rawRequest})

	select {
	case frame := <-hostOut:
		var opened proto.ServiceOpenedPayload
		if err := json.Unmarshal(frame.Payload, &opened); err != nil || !opened.OK {
			t.Fatalf("host SERVICE_OPENED = %+v, decode=%v", opened, err)
		}
	case <-ctx.Done():
		t.Fatal("timeout waiting for owner host registration")
	}

	keys, err := e2eecrypto.DeriveServiceKeys(accountKey, serviceID)
	if err != nil {
		t.Fatal(err)
	}
	var previews servicePreviewManager
	local, err := previews.start(ctx, appConfig{
		RelayURL:          relayWS,
		RelaySessionToken: "token",
	}, ServicePreviewStartRequest{
		ServiceID:       serviceID.String(),
		ClientTicket:    "client-ticket",
		ClientToHostKey: keys.ClientToHost,
		HostToClientKey: keys.HostToClient,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer previews.stopAll()

	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Get(local.URL)
	if err != nil {
		t.Fatalf("GET preview loopback: %v", err)
	}
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(body); got != "preview-through-owner" {
		t.Fatalf("preview body = %q", got)
	}
	if firstSubscribers.Load() != 0 || lastSubscribers.Load() != 0 {
		t.Fatalf("Preview touched subscriber lifecycle: first=%d last=%d", firstSubscribers.Load(), lastSubscribers.Load())
	}
}

func TestServiceHostFailsClosedBeforeDial(t *testing.T) {
	sessionID := uuid.New()
	serviceID := uuid.New()
	accountKey := bytes.Repeat([]byte{0x42}, e2eecrypto.SessionKeySize)
	seal := func(fields proto.SealedServiceOpenFields) []byte {
		sessionKey, _ := e2eecrypto.DeriveSessionKey(accountKey, sessionID)
		plain, _ := json.Marshal(fields)
		sealed, _ := e2eecrypto.SealUnsequenced(sessionKey, sessionID, byte(proto.TypeServiceOpen), plain)
		return sealed
	}
	request := func(sealed []byte) proto.Frame {
		payload, _ := json.Marshal(proto.ServiceOpenPayload{
			RequestID: "request", ServiceID: serviceID.String(), HostTicket: "ticket", Sealed: sealed,
		})
		return proto.Frame{Type: proto.TypeServiceOpen, SessionID: sessionID, Payload: payload}
	}
	tests := []struct {
		name       string
		permission string
		key        func() []byte
		sealed     []byte
		want       string
	}{
		{name: "permission", permission: proto.RemotePermissionControl, key: func() []byte { return accountKey }, sealed: seal(proto.SealedServiceOpenFields{Port: 3000, Scheme: "http"}), want: "permission_denied"},
		{name: "key required", permission: proto.RemotePermissionFull, key: func() []byte { return nil }, sealed: seal(proto.SealedServiceOpenFields{Port: 3000, Scheme: "http"}), want: "e2ee_required"},
		{name: "http only", permission: proto.RemotePermissionFull, key: func() []byte { return accountKey }, sealed: seal(proto.SealedServiceOpenFields{Port: 3000, Scheme: "https"}), want: "invalid_request"},
		{name: "port required", permission: proto.RemotePermissionFull, key: func() []byte { return accountKey }, sealed: seal(proto.SealedServiceOpenFields{Scheme: "http"}), want: "invalid_request"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			out := make(chan proto.Frame, 1)
			manager := newServiceHostManager(ctx, "ws://127.0.0.1:1", "token", false, test.key, out, test.permission)
			manager.open(request(test.sealed))
			select {
			case response := <-out:
				var opened proto.ServiceOpenedPayload
				if err := json.Unmarshal(response.Payload, &opened); err != nil || opened.OK || opened.Error != test.want {
					t.Fatalf("SERVICE_OPENED = %+v err=%v", opened, err)
				}
			case <-ctx.Done():
				t.Fatal("host did not reject request")
			}
		})
	}
}

type servicePipeFixture struct {
	mu     sync.Mutex
	host   *websocket.Conn
	client *websocket.Conn
	paired chan struct{}
	once   sync.Once
}

func newServicePipeFixture() *servicePipeFixture {
	return &servicePipeFixture{paired: make(chan struct{})}
}

func (p *servicePipeFixture) serveHTTP(w http.ResponseWriter, r *http.Request) {
	role := strings.TrimPrefix(r.URL.Path, "/")
	if role != "service-host" && role != "service-client" {
		http.NotFound(w, r)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(64*1024 + 64)
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	_, registration, err := conn.Read(ctx)
	cancel()
	if err != nil || len(registration) == 0 {
		return
	}
	ack, _ := json.Marshal(serviceRegistrationAck{OK: true})
	if err := conn.Write(r.Context(), websocket.MessageBinary, ack); err != nil {
		return
	}
	p.mu.Lock()
	if role == "service-host" {
		p.host = conn
	} else {
		p.client = conn
	}
	if p.host != nil && p.client != nil {
		p.once.Do(func() { close(p.paired) })
	}
	p.mu.Unlock()
	select {
	case <-r.Context().Done():
		return
	case <-p.paired:
	}
	p.mu.Lock()
	var dst *websocket.Conn
	if role == "service-host" {
		dst = p.client
	} else {
		dst = p.host
	}
	p.mu.Unlock()
	if dst == nil {
		return
	}
	for {
		messageType, packet, err := conn.Read(r.Context())
		if err != nil {
			return
		}
		if err := dst.Write(r.Context(), messageType, packet); err != nil {
			return
		}
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/directsignal"
	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"nhooyr.io/websocket"
)

type directTestPeer struct {
	conn *websocket.Conn
	out  chan directsignal.Message
	in   chan directsignal.Message
}

type directTestBroker struct {
	ctx      context.Context
	hostID   string
	session  uuid.UUID
	userID   string
	ticket   []byte
	host     chan *directTestPeer
	client   chan *directTestPeer
	errors   chan error
	startOne sync.Once
}

func newDirectTestBroker(ctx context.Context, hostID string, sessionID uuid.UUID) *directTestBroker {
	return &directTestBroker{
		ctx: ctx, hostID: hostID, session: sessionID, userID: "native-user",
		ticket: bytes.Repeat([]byte{0x5a}, 32), host: make(chan *directTestPeer, 1),
		client: make(chan *directTestPeer, 1), errors: make(chan error, 8),
	}
}

func (b *directTestBroker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/direct-signal" || r.Header.Get("Authorization") != "Bearer native-token" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	peer := &directTestPeer{conn: conn, out: make(chan directsignal.Message, 32), in: make(chan directsignal.Message, 32)}
	hello, err := readDirectSignalHost(b.ctx, conn)
	if err != nil {
		b.report(err)
		return
	}
	if hello.Kind != "hello" {
		b.report(fmt.Errorf("unexpected hello: %+v", hello))
		return
	}
	if err := writeDirectSignalHost(b.ctx, conn, directsignal.Message{Version: directsignal.Version, Kind: "hello_ok"}); err != nil {
		b.report(err)
		return
	}
	switch hello.Role {
	case "host":
		b.host <- peer
	case "client":
		b.client <- peer
	default:
		b.report(fmt.Errorf("unexpected role %q", hello.Role))
		return
	}
	b.startOne.Do(func() { go b.route() })
	go func() {
		for {
			select {
			case <-b.ctx.Done():
				return
			case message := <-peer.out:
				if err := writeDirectSignalHost(b.ctx, conn, message); err != nil {
					b.report(err)
					return
				}
			}
		}
	}()
	for b.ctx.Err() == nil {
		message, err := readDirectSignalHost(b.ctx, conn)
		if err != nil {
			return
		}
		peer.in <- message
	}
}

func (b *directTestBroker) route() {
	var host, client *directTestPeer
	select {
	case <-b.ctx.Done():
		return
	case host = <-b.host:
	}
	select {
	case <-b.ctx.Done():
		return
	case client = <-b.client:
	}
	var request directsignal.Message
	select {
	case <-b.ctx.Done():
		return
	case request = <-client.in:
	}
	if request.Kind != "direct_request" || request.SessionID != b.session.String() {
		b.report(fmt.Errorf("unexpected direct request: %+v", request))
		return
	}
	attempt := directsignal.Message{
		Version: directsignal.Version, Kind: "direct_attempt", RequestID: request.RequestID,
		AttemptID: uuid.NewString(), Ticket: base64.RawURLEncoding.EncodeToString(b.ticket),
		SessionID: b.session.String(), SinceSeq: request.SinceSeq, UserID: b.userID,
		HostID: b.hostID, ClientInstanceID: request.ClientInstanceID,
		Permission: proto.RemotePermissionFull, ExpiresAtUnixM: time.Now().Add(time.Minute).UnixMilli(),
	}
	host.out <- attempt
	client.out <- attempt
	for {
		select {
		case <-b.ctx.Done():
			return
		case message := <-host.in:
			if message.Kind == "signal" || message.Kind == "consumed" || message.Kind == "cancel" {
				client.out <- message
			}
		case message := <-client.in:
			if message.Kind == "signal" || message.Kind == "cancel" {
				host.out <- message
			}
		}
	}
}

func (b *directTestBroker) report(err error) {
	select {
	case b.errors <- err:
	default:
	}
}

func TestNativeDirectClientConnectsToDesktopPionHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := newTestRelayHost(t)
	sessionID := uuid.New()
	sess := addDirectTestSession(t, host, sessionID)
	sess.PushOut(1, []byte("native output"))
	accountKey := bytes.Repeat([]byte{0x68}, e2eecrypto.SessionKeySize)
	broker := newDirectTestBroker(ctx, host.hostID, sessionID)
	server := httptest.NewServer(broker)
	defer server.Close()
	relayURL := "ws" + strings.TrimPrefix(server.URL, "http")

	directHost := newDirectSignalHost(relayURL, "native-token", proto.RemotePermissionFull, host, func() []byte { return accountKey }, false)
	directHost.webrtcConfig = webrtc.Configuration{}
	hostErr := make(chan error, 1)
	go func() { hostErr <- directHost.runOnce(ctx) }()
	defer directHost.closeAllAttempts()

	events := make(chan NativeDirectEvent, 32)
	app := &App{ctx: ctx, nativeDirect: make(map[string]*nativeDirectClient)}
	app.eventsEmitter = func(_ context.Context, _ string, data ...interface{}) {
		if len(data) == 1 {
			if event, ok := data[0].(NativeDirectEvent); ok {
				events <- event
			}
		}
	}
	clientCtx, clientCancel := context.WithCancel(ctx)
	client := &nativeDirectClient{
		id: uuid.NewString(), app: app, ctx: clientCtx, cancel: clientCancel,
		relayURL: relayURL, token: "native-token", sessionID: sessionID,
		clientInstanceID: "native-client", accountKey: append([]byte(nil), accountKey...),
		webrtcConfig: webrtc.Configuration{},
	}
	app.nativeDirect[client.id] = client
	clientErr := make(chan error, 1)
	go func() { clientErr <- client.run() }()
	defer client.stop()

	var gotAuthenticated, gotReady, sentInput bool
	for !gotAuthenticated || !gotReady || !sentInput {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case err := <-broker.errors:
			t.Fatal(err)
		case err := <-clientErr:
			if err != nil {
				t.Fatal(err)
			}
		case err := <-hostErr:
			if err != nil && ctx.Err() == nil {
				t.Fatal(err)
			}
		case event := <-events:
			switch event.Kind {
			case "failure":
				t.Fatal(event.Error)
			case "authenticated":
				gotAuthenticated = true
			case "ready":
				if event.LastReplayedSeq != 1 {
					t.Fatalf("ready seq=%d", event.LastReplayedSeq)
				}
				gotReady = true
				claim, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "native-client", ClientName: "native"})
				if err := client.sendFrame(proto.Marshal(proto.Frame{Type: proto.TypeClaimDriver, SessionID: sessionID, Payload: claim})); err != nil {
					t.Fatal(err)
				}
				if err := client.sendFrame(proto.Marshal(proto.Frame{Type: proto.TypeIn, SessionID: sessionID, Payload: []byte("native input")})); err != nil {
					t.Fatal(err)
				}
			case "frame":
				if event.FrameBase64 == "" {
					t.Fatal("empty native frame")
				}
			}
		case frame := <-sess.Inbound():
			if frame.Type == proto.TypeIn && bytes.Equal(frame.Payload, []byte("native input")) {
				sentInput = true
			}
		}
	}
}

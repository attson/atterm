package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/connhealth"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/relay"
	"github.com/attson/atterm/internal/userstore"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func TestUplinkLifecycleRecoversAfterRelayRestart(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e test")
	}

	store := userstore.NewInMemory(t)
	user, err := store.CreateOpaqueUser(context.Background(), "relay-restart@example.com")
	if err != nil {
		t.Fatal(err)
	}
	token, _, err := store.CreateSession(
		context.Background(), user.ID, "restart-test", "127.0.0.1", userstore.DefaultSessionTTL,
	)
	if err != nil {
		t.Fatal(err)
	}

	first := newRelayChaosServer(store)
	firstClosed := false
	endpoint := newRelayChaosEndpoint(t, first)
	var second *relay.Server
	t.Cleanup(func() {
		endpoint.close()
		if !firstClosed {
			first.Close()
		}
		if second != nil {
			second.Close()
		}
	})

	host := newTestRelayHost(t)
	sessionID, err := host.NewSession(context.Background(), NewSessionReq{
		Command: "bash",
		Args:    []string{"-c", "while read line; do echo got=$line; done"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	errorsSeen := make(chan error, 8)
	u := newUplink("ws://"+endpoint.addr(), token, proto.RemotePermissionFull, host, func(err error) {
		select {
		case errorsSeen <- err:
		default:
		}
	}, nil, false)
	u.eventsEmit = func(context.Context, string, ...interface{}) {}
	uplinkDone := make(chan struct{})
	go func() {
		u.Run(ctx)
		close(uplinkDone)
	}()
	defer func() {
		cancel()
		<-uplinkDone
	}()

	waitRelayChaosCondition(t, 5*time.Second, "initial uplink and mirror", func() bool {
		return first.UplinkCount() == 1 && relayChaosSessionVisible(endpoint.addr(), token, sessionID)
	})
	firstViewer := relayChaosAttachAndEcho(t, ctx, endpoint.addr(), token, sessionID, "before-restart")
	defer firstViewer.CloseNow()

	// A process restart tears down upgraded WebSockets as well as the HTTP
	// listener. The stable proxy makes that network effect deterministic while
	// allowing the replacement generation to come back at the same public URL.
	endpoint.disconnect()
	waitRelayChaosCondition(t, 3*time.Second, "old uplink teardown", func() bool {
		return first.UplinkCount() == 0
	})
	first.Close()
	firstClosed = true
	waitRelayChaosViewerClosed(t, firstViewer)
	waitRelayChaosCondition(t, 3*time.Second, "uplink reconnecting state", func() bool {
		return u.Health().State == connhealth.StateReconnecting
	})

	// Observe both the broken established connection and one failed dial while
	// the service is absent. This prevents an immediate backend swap from
	// accidentally testing only a clean handover.
	for attempt := 0; attempt < 2; attempt++ {
		select {
		case <-errorsSeen:
		case <-time.After(4 * time.Second):
			t.Fatalf("uplink did not observe relay outage attempt %d", attempt+1)
		}
	}

	second = newRelayChaosServer(store)
	endpoint.setBackend(second)
	waitRelayChaosCondition(t, 8*time.Second, "replacement uplink and mirror", func() bool {
		return second.UplinkCount() == 1 && relayChaosSessionVisible(endpoint.addr(), token, sessionID)
	})
	waitRelayChaosCondition(t, 2*time.Second, "connected health after restart", func() bool {
		health := u.Health()
		return health.State == connhealth.StateConnected && health.Reconnect.CountLastHour >= 1
	})

	secondViewer := relayChaosAttachAndEcho(t, ctx, endpoint.addr(), token, sessionID, "after-restart")
	defer secondViewer.CloseNow()
}

func newRelayChaosServer(store *userstore.DBStore) *relay.Server {
	return relay.NewServer(relay.Config{Store: store, Resolver: relay.NewIdentityResolver(store)})
}

type relayChaosEndpoint struct {
	httpServer  *httptest.Server
	backend     atomic.Pointer[relay.Server]
	mu          sync.Mutex
	connections map[net.Conn]struct{}
}

func newRelayChaosEndpoint(t *testing.T, initial *relay.Server) *relayChaosEndpoint {
	t.Helper()
	endpoint := &relayChaosEndpoint{connections: make(map[net.Conn]struct{})}
	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		backend := endpoint.backend.Load()
		if backend == nil {
			http.Error(w, "restarting", http.StatusServiceUnavailable)
			return
		}
		backend.ServeHTTP(w, request)
	}))
	httpServer.Config.ConnState = endpoint.observeConnection
	endpoint.httpServer = httpServer
	endpoint.backend.Store(initial)
	httpServer.Start()
	return endpoint
}

func (e *relayChaosEndpoint) addr() string {
	return strings.TrimPrefix(e.httpServer.URL, "http://")
}

func (e *relayChaosEndpoint) setBackend(server *relay.Server) {
	e.backend.Store(server)
}

func (e *relayChaosEndpoint) disconnect() {
	e.backend.Store(nil)
	e.closeConnections()
}

func (e *relayChaosEndpoint) close() {
	e.disconnect()
	e.httpServer.Close()
}

func (e *relayChaosEndpoint) observeConnection(connection net.Conn, state http.ConnState) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if state == http.StateClosed {
		delete(e.connections, connection)
		return
	}
	e.connections[connection] = struct{}{}
}

func (e *relayChaosEndpoint) closeConnections() {
	e.mu.Lock()
	connections := make([]net.Conn, 0, len(e.connections))
	for connection := range e.connections {
		connections = append(connections, connection)
	}
	e.connections = make(map[net.Conn]struct{})
	e.mu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

func relayChaosSessionVisible(addr, token string, sessionID uuid.UUID) bool {
	response, err := getRemoteSessions(addr, token)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return false
	}
	var sessions []proto.SessionInfo
	if json.NewDecoder(response.Body).Decode(&sessions) != nil {
		return false
	}
	for _, session := range sessions {
		if session.ID == sessionID.String() {
			return true
		}
	}
	return false
}

func relayChaosAttachAndEcho(
	t *testing.T,
	ctx context.Context,
	addr string,
	token string,
	sessionID uuid.UUID,
	input string,
) *websocket.Conn {
	t.Helper()
	connection, _, err := websocket.Dial(ctx, "ws://"+addr+"/client", &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	connection.SetReadLimit(2 * 1024 * 1024)
	attach, _ := json.Marshal(proto.AttachPayload{
		SessionID: sessionID.String(), ClientID: "restart-viewer", ClientName: "restart-viewer",
	})
	if err := connection.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeAttach, SessionID: sessionID, Payload: attach,
	})); err != nil {
		connection.CloseNow()
		t.Fatal(err)
	}
	claim, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "restart-viewer", ClientName: "restart-viewer"})
	for _, frame := range []proto.Frame{
		{Type: proto.TypeClaimDriver, SessionID: sessionID, Payload: claim},
		{Type: proto.TypeIn, SessionID: sessionID, Payload: []byte(input + "\n")},
	} {
		if err := connection.Write(ctx, websocket.MessageBinary, proto.Marshal(frame)); err != nil {
			connection.CloseNow()
			t.Fatal(err)
		}
	}

	readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var output strings.Builder
	for {
		messageType, data, err := connection.Read(readCtx)
		if err != nil {
			connection.CloseNow()
			t.Fatalf("read remote echo %q: %v (output=%q)", input, err, output.String())
		}
		if messageType != websocket.MessageBinary {
			continue
		}
		frame, err := proto.Unmarshal(data)
		if err != nil || frame.Type != proto.TypeOut {
			continue
		}
		_, payload, err := proto.DecodeOut(frame.Payload)
		if err != nil {
			continue
		}
		output.Write(payload)
		if strings.Contains(output.String(), "got="+input) {
			return connection
		}
	}
}

func waitRelayChaosViewerClosed(t *testing.T, connection *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, _, err := connection.Read(ctx)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			t.Fatal("viewer connection survived relay restart")
		}
		return
	}
}

func waitRelayChaosCondition(t *testing.T, timeout time.Duration, description string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", description)
}

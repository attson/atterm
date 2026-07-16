package relay

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func TestFSRouterRoutesResponseToRequesterOnly(t *testing.T) {
	r := newFSRouter()
	sessionID := uuid.New()
	requester := make(chan proto.Frame, 1)
	other := make(chan proto.Frame, 1)
	r.registerRequest(sessionID, "requester", requester)
	r.registerRequest(sessionID, "other", other)

	r.routeResponse(proto.Frame{
		Type:      proto.TypeFSResponse,
		SessionID: sessionID,
		Payload:   []byte(`{"request_id":"requester","ok":true}`),
	})

	select {
	case got := <-requester:
		if got.Type != proto.TypeFSResponse {
			t.Fatalf("response type = %v, want FS_RESPONSE", got.Type)
		}
	default:
		t.Fatal("requester did not receive response")
	}
	select {
	case got := <-other:
		t.Fatalf("other requester received response: %+v", got)
	default:
	}
}

func TestFSRouterRegistersWatchAfterSuccessfulResponse(t *testing.T) {
	r := newFSRouter()
	sessionID := uuid.New()
	out := make(chan proto.Frame, 2)
	r.registerRequest(sessionID, "watch-request", out)

	r.routeResponse(proto.Frame{
		Type:      proto.TypeFSResponse,
		SessionID: sessionID,
		Payload:   []byte(`{"request_id":"watch-request","ok":true,"watch_id":"watch-1"}`),
	})
	<-out
	r.routeEvent(proto.Frame{
		Type:      proto.TypeFSEvent,
		SessionID: sessionID,
		Payload:   []byte(`{"watch_id":"watch-1","path":"/tmp","event":"dir_changed"}`),
	})

	select {
	case got := <-out:
		if got.Type != proto.TypeFSEvent {
			t.Fatalf("event type = %v, want FS_EVENT", got.Type)
		}
	default:
		t.Fatal("watch owner did not receive event")
	}
}

func TestFSRouterUnregisterClientRemovesRequestAndWatchRoutes(t *testing.T) {
	r := newFSRouter()
	sessionID := uuid.New()
	removed := make(chan proto.Frame, 2)
	kept := make(chan proto.Frame, 2)
	r.registerRequest(sessionID, "request", removed)
	r.registerWatch(sessionID, "watch", removed)
	r.registerRequest(sessionID, "kept", kept)
	r.unregisterClient(removed)

	if r.routeResponse(proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: []byte(`{"request_id":"request","ok":true}`)}) {
		t.Fatal("unregistered request route accepted response")
	}
	if r.routeEvent(proto.Frame{Type: proto.TypeFSEvent, SessionID: sessionID, Payload: []byte(`{"watch_id":"watch"}`)}) {
		t.Fatal("unregistered watch route accepted event")
	}
	if !r.routeResponse(proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: []byte(`{"request_id":"kept","ok":true}`)}) {
		t.Fatal("unrelated request route was removed")
	}
}

func TestFSRouterIgnoresMalformedPayloads(t *testing.T) {
	r := newFSRouter()
	sessionID := uuid.New()
	out := make(chan proto.Frame, 1)
	r.registerRequest(sessionID, "request", out)
	r.registerWatch(sessionID, "watch", out)

	if r.routeResponse(proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: []byte(`{`)}) {
		t.Fatal("malformed response was routed")
	}
	if r.routeEvent(proto.Frame{Type: proto.TypeFSEvent, SessionID: sessionID, Payload: []byte(`{`)}) {
		t.Fatal("malformed event was routed")
	}
	if len(out) != 0 {
		t.Fatal("malformed payload delivered a frame")
	}
}

func TestFSRouterDropsWhenClientChannelIsFull(t *testing.T) {
	r := newFSRouter()
	sessionID := uuid.New()
	out := make(chan proto.Frame, 1)
	out <- proto.Frame{Type: proto.TypeMeta}
	r.registerRequest(sessionID, "request", out)

	if r.routeResponse(proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: []byte(`{"request_id":"request","ok":true}`)}) {
		t.Fatal("response was reported delivered to full channel")
	}
}

func TestClientFSRequestForwardsReadOnlyFullPermission(t *testing.T) {
	srv, token, userID := serverWithSessionAndUser(t)
	httpSrv := newRelayHTTPServer(t, srv)

	sessionID := uuid.New()
	sess := newClientSession(t, srv, sessionID, userID, proto.RemotePermissionFull)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	driver := dialClientAttach(t, ctx, httpSrv, token, sessionID, "driver")
	defer driver.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, driver)
	viewer := dialClientAttach(t, ctx, httpSrv, token, sessionID, "viewer")
	defer viewer.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, viewer)

	requestPayload, _ := json.Marshal(proto.FSRequestPayload{RequestID: "read-1", Op: "list_dir", Path: "/tmp"})
	writeClientFrame(t, ctx, viewer, proto.TypeFSRequest, sessionID, requestPayload)
	select {
	case got := <-sess.Inbound():
		if got.Type != proto.TypeFSRequest {
			t.Fatalf("inbound type = %v, want FS_REQUEST", got.Type)
		}
	case <-ctx.Done():
		t.Fatal("read-only FS request was not forwarded")
	}

	responsePayload, _ := json.Marshal(proto.FSResponsePayload{RequestID: "read-1", OK: true})
	if !srv.fsRoutes().routeResponse(proto.Frame{Type: proto.TypeFSResponse, SessionID: sessionID, Payload: responsePayload}) {
		t.Fatal("FS response route was not registered")
	}
	_, data, err := viewer.Read(ctx)
	if err != nil {
		t.Fatalf("read requester response: %v", err)
	}
	got, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal requester response: %v", err)
	}
	if got.Type != proto.TypeFSResponse {
		t.Fatalf("requester frame type = %v, want FS_RESPONSE", got.Type)
	}
}

func TestClientFSRequestRejectsInsufficientPermission(t *testing.T) {
	srv, token, userID := serverWithSessionAndUser(t)
	httpSrv := newRelayHTTPServer(t, srv)

	sessionID := uuid.New()
	sess := newClientSession(t, srv, sessionID, userID, proto.RemotePermissionControl)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	client := dialClientAttach(t, ctx, httpSrv, token, sessionID, "client")
	defer client.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, client)

	requestPayload, _ := json.Marshal(proto.FSRequestPayload{RequestID: "denied", Op: "list_dir", Path: "/tmp"})
	writeClientFrame(t, ctx, client, proto.TypeFSRequest, sessionID, requestPayload)
	assertFSClientError(t, ctx, client, "denied", "permission_denied")
	assertNoFSInbound(t, sess)
}

func TestClientFSOpenExternalRequiresDriver(t *testing.T) {
	srv, token, userID := serverWithSessionAndUser(t)
	httpSrv := newRelayHTTPServer(t, srv)

	sessionID := uuid.New()
	sess := newClientSession(t, srv, sessionID, userID, proto.RemotePermissionFull)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	driver := dialClientAttach(t, ctx, httpSrv, token, sessionID, "driver")
	defer driver.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, driver)
	viewer := dialClientAttach(t, ctx, httpSrv, token, sessionID, "viewer")
	defer viewer.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, viewer)

	requestPayload, _ := json.Marshal(proto.FSRequestPayload{RequestID: "open", Op: "open_external", Path: "/tmp/a"})
	writeClientFrame(t, ctx, viewer, proto.TypeFSRequest, sessionID, requestPayload)
	assertFSClientError(t, ctx, viewer, "open", "driver_required")
	assertNoFSInbound(t, sess)
}

func TestUplinkFSResponseRoutesToRequestingClient(t *testing.T) {
	store, userID, token := newUplinkTestStore(t)
	srv := newUplinkTestServer(t, store)
	httpSrv := newRelayHTTPServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uplink, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+token)
	if err != nil {
		t.Fatalf("dial uplink: %v", err)
	}
	defer uplink.Close(websocket.StatusNormalClosure, "")

	sessionID := uuid.New()
	announcePayload, _ := json.Marshal(proto.AnnouncePayload{Sessions: []proto.SessionInfo{{
		ID:               sessionID.String(),
		Command:          "bash",
		HostID:           uuid.New().String(),
		RemotePermission: proto.RemotePermissionFull,
	}}})
	if err := uplink.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeAnnounce, Payload: announcePayload})); err != nil {
		t.Fatalf("announce uplink session: %v", err)
	}
	for {
		sess, ok := srv.registry.Get(sessionID)
		if ok && sess.OwnerUserID == userID {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("uplink mirror was not registered")
		case <-time.After(10 * time.Millisecond):
		}
	}

	client := dialClientAttach(t, ctx, httpSrv, token, sessionID, "client")
	defer client.Close(websocket.StatusNormalClosure, "")
	drainAttachIntro(t, ctx, client)
	requestPayload, _ := json.Marshal(proto.FSRequestPayload{RequestID: "request", Op: "list_dir", Path: "/tmp"})
	writeClientFrame(t, ctx, client, proto.TypeFSRequest, sessionID, requestPayload)
	assertUplinkReceivesFSRequest(t, ctx, uplink, sessionID)

	responsePayload, _ := json.Marshal(proto.FSResponsePayload{RequestID: "request", OK: true})
	if err := uplink.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type:      proto.TypeFSResponse,
		SessionID: sessionID,
		Payload:   responsePayload,
	})); err != nil {
		t.Fatalf("write uplink FS response: %v", err)
	}
	_, data, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read client FS response: %v", err)
	}
	frame, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal client FS response: %v", err)
	}
	if frame.Type != proto.TypeFSResponse {
		t.Fatalf("client frame type = %v, want FS_RESPONSE", frame.Type)
	}
}

func newRelayHTTPServer(t *testing.T, srv *Server) *httptest.Server {
	t.Helper()
	httpSrv := httptest.NewServer(srv)
	t.Cleanup(httpSrv.Close)
	return httpSrv
}

func newClientSession(t *testing.T, srv *Server, sessionID uuid.UUID, ownerUserID, permission string) *session.Session {
	t.Helper()
	sess := session.New(sessionID, proto.SessionInfo{Command: "bash", Cols: 80, Rows: 24, RemotePermission: permission})
	sess.OwnerUserID = ownerUserID
	if _, err := srv.registry.Add(sess); err != nil {
		t.Fatalf("add session: %v", err)
	}
	return sess
}

func assertFSClientError(t *testing.T, ctx context.Context, client *websocket.Conn, requestID, wantError string) {
	t.Helper()
	_, data, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("read FS error: %v", err)
	}
	frame, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal FS error: %v", err)
	}
	if frame.Type != proto.TypeFSResponse {
		t.Fatalf("error frame type = %v, want FS_RESPONSE", frame.Type)
	}
	var response proto.FSResponsePayload
	if err := json.Unmarshal(frame.Payload, &response); err != nil {
		t.Fatalf("decode FS error: %v", err)
	}
	if response.RequestID != requestID || response.OK || response.Error != wantError {
		t.Fatalf("FS error = %+v, want request_id=%q error=%q", response, requestID, wantError)
	}
}

func assertNoFSInbound(t *testing.T, sess *session.Session) {
	t.Helper()
	select {
	case got := <-sess.Inbound():
		t.Fatalf("unexpected FS inbound frame: %+v", got)
	case <-time.After(150 * time.Millisecond):
	}
}

func assertUplinkReceivesFSRequest(t *testing.T, ctx context.Context, uplink *websocket.Conn, sessionID uuid.UUID) {
	t.Helper()
	for {
		_, data, err := uplink.Read(ctx)
		if err != nil {
			t.Fatalf("read uplink frame: %v", err)
		}
		frame, err := proto.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal uplink frame: %v", err)
		}
		if frame.Type != proto.TypeFSRequest {
			continue
		}
		if frame.SessionID != sessionID {
			t.Fatalf("FS request session = %s, want %s", frame.SessionID, sessionID)
		}
		return
	}
}

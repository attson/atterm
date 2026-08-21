package relay

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

// ---- Router-level unit tests (no HTTP), mirroring fs_router_test.go's shape ----

func sessionCreatedPayload(t *testing.T, requestID string, ok bool) []byte {
	t.Helper()
	b, err := json.Marshal(proto.SessionCreatedPayload{RequestID: requestID, OK: ok, SessionID: "22222222-2222-2222-2222-222222222222"})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSessionCreateRouterRoutesResponseToRequesterOnly(t *testing.T) {
	r := newSessionCreateRouter()
	uplink := make(chan proto.Frame, 1)
	requester := make(chan proto.Frame, 1)
	other := make(chan proto.Frame, 1)
	if !r.registerRequest("requester", requester, uplink) {
		t.Fatal("requester route was not registered")
	}
	if !r.registerRequest("other", other, uplink) {
		t.Fatal("other route was not registered")
	}

	if !r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: sessionCreatedPayload(t, "requester", true)}, uplink) {
		t.Fatal("response for the requester was not routed")
	}

	select {
	case got := <-requester:
		if got.Type != proto.TypeSessionCreated {
			t.Fatalf("response type = %v, want SESSION_CREATED", got.Type)
		}
	default:
		t.Fatal("requester did not receive its response")
	}
	select {
	case got := <-other:
		t.Fatalf("a different requester received the response: %+v", got)
	default:
	}
}

// TestSessionCreateRouterRejectsResponseFromWrongUplink is the mutation test
// for "a response must reach the requesting client and nobody else" read the
// other way: an uplink that did NOT receive the forwarded request must not
// be able to answer it, even naming the correct request_id. If routeResponse
// stopped checking fromUplink (e.g. became sourceless, like a broadcast),
// this test fails.
func TestSessionCreateRouterRejectsResponseFromWrongUplink(t *testing.T) {
	r := newSessionCreateRouter()
	correctUplink := make(chan proto.Frame, 1)
	wrongUplink := make(chan proto.Frame, 1)
	client := make(chan proto.Frame, 1)
	if !r.registerRequest("req-1", client, correctUplink) {
		t.Fatal("route was not registered")
	}

	if r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: sessionCreatedPayload(t, "req-1", true)}, wrongUplink) {
		t.Fatal("response from the wrong uplink was routed")
	}
	select {
	case got := <-client:
		t.Fatalf("client received a response forged by the wrong uplink: %+v", got)
	default:
	}

	// The legitimate uplink can still answer — a forged attempt must not
	// consume or corrupt the real route.
	if !r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: sessionCreatedPayload(t, "req-1", true)}, correctUplink) {
		t.Fatal("response from the correct uplink was not routed after a forged attempt")
	}
	select {
	case <-client:
	default:
		t.Fatal("client did not receive the legitimate response")
	}
}

func TestSessionCreateRouterDuplicateRequestPreservesOriginalRequester(t *testing.T) {
	r := newSessionCreateRouter()
	uplink := make(chan proto.Frame, 1)
	first := make(chan proto.Frame, 1)
	second := make(chan proto.Frame, 1)
	if !r.registerRequest("duplicate", first, uplink) {
		t.Fatal("first request route was not registered")
	}
	if r.registerRequest("duplicate", second, uplink) {
		t.Fatal("duplicate request route was registered")
	}

	r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: sessionCreatedPayload(t, "duplicate", true)}, uplink)

	select {
	case <-first:
	default:
		t.Fatal("original requester did not receive response")
	}
	select {
	case got := <-second:
		t.Fatalf("duplicate requester received response: %+v", got)
	default:
	}
}

func TestSessionCreateRouterUnregisterClientRemovesOnlyItsRequests(t *testing.T) {
	r := newSessionCreateRouter()
	uplink := make(chan proto.Frame, 2)
	removed := make(chan proto.Frame, 1)
	kept := make(chan proto.Frame, 1)
	r.registerRequest("removed", removed, uplink)
	r.registerRequest("kept", kept, uplink)

	r.unregisterClient(removed)

	if r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: sessionCreatedPayload(t, "removed", true)}, uplink) {
		t.Fatal("unregistered request route accepted a response")
	}
	if !r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: sessionCreatedPayload(t, "kept", true)}, uplink) {
		t.Fatal("unrelated request route was removed")
	}
}

func TestSessionCreateRouterIgnoresMalformedPayload(t *testing.T) {
	r := newSessionCreateRouter()
	uplink := make(chan proto.Frame, 1)
	out := make(chan proto.Frame, 1)
	r.registerRequest("request", out, uplink)

	if r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: []byte(`{`)}, uplink) {
		t.Fatal("malformed response was routed")
	}
	if len(out) != 0 {
		t.Fatal("malformed payload delivered a frame")
	}
}

// TestSessionCreateRouterReapsExpiredRequestAfterTTL is the mutation test for
// the TTL sweep: it exercises reapExpiredRequests directly (as
// fs_router_test.go's equivalent does), so it fails both if the sweep never
// runs and if the sweep runs too eagerly (see the sibling
// DoesNotReapRequestWithinTTL test).
func TestSessionCreateRouterReapsExpiredRequestAfterTTL(t *testing.T) {
	buf := captureDebugLog(t)
	r := newSessionCreateRouter()
	uplink := make(chan proto.Frame, 1)
	stuck := make(chan proto.Frame, 1)
	if !r.registerRequest("stuck", stuck, uplink) {
		t.Fatal("request route was not registered")
	}

	// No response ever arrives. Sweep as if sessionCreateRequestTTL has
	// elapsed since registration — this is what a later touch of the map
	// (another register or routeResponse call) would trigger on its own.
	r.reapExpiredRequests(time.Now().Add(sessionCreateRequestTTL + time.Second))

	if r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: sessionCreatedPayload(t, "stuck", true)}, uplink) {
		t.Fatal("expired request route was still delivered")
	}
	select {
	case got := <-stuck:
		t.Fatalf("stuck requester received a frame after reap: %+v", got)
	default:
	}

	got := buf.String()
	if !strings.Contains(got, "reaped expired request") || !strings.Contains(got, "request_id=stuck") {
		t.Fatalf("reap was not logged: %q", got)
	}
}

func TestSessionCreateRouterDoesNotReapRequestWithinTTL(t *testing.T) {
	r := newSessionCreateRouter()
	uplink := make(chan proto.Frame, 1)
	out := make(chan proto.Frame, 1)
	if !r.registerRequest("in-time", out, uplink) {
		t.Fatal("request route was not registered")
	}

	// A sweep run just short of the TTL must be a no-op for this entry.
	r.reapExpiredRequests(time.Now().Add(sessionCreateRequestTTL - time.Second))

	if !r.routeResponse(proto.Frame{Type: proto.TypeSessionCreated, Payload: sessionCreatedPayload(t, "in-time", true)}, uplink) {
		t.Fatal("response within TTL was not routed")
	}
	select {
	case <-out:
	default:
		t.Fatal("requester did not receive response")
	}
}

func TestSessionCreateRouterHostLifecycle(t *testing.T) {
	r := newSessionCreateRouter()
	hostAOut := make(chan proto.Frame, 1)
	hostBOut := make(chan proto.Frame, 1)
	r.registerHost("host-a", hostAOut, "user-a")

	route, ok := r.lookupHost("host-a")
	if !ok || route.out != chan<- proto.Frame(hostAOut) || route.ownerUserID != "user-a" {
		t.Fatalf("lookupHost = %+v, %v; want host-a route for user-a", route, ok)
	}
	if _, ok := r.lookupHost("host-b"); ok {
		t.Fatal("lookupHost found a host that was never registered")
	}

	// A stale connection's teardown must not clobber a fresher registration
	// for the same host id.
	r.unregisterHost("host-a", hostBOut)
	if _, ok := r.lookupHost("host-a"); !ok {
		t.Fatal("unregisterHost with the wrong out channel removed the live route")
	}

	r.unregisterHost("host-a", hostAOut)
	if _, ok := r.lookupHost("host-a"); ok {
		t.Fatal("unregisterHost with the matching out channel did not remove the route")
	}
}

// ---- Integration tests over real /uplink + /client websockets ----

// announceHost writes the first (ANNOUNCE) frame an uplink connection must
// send, with an explicit top-level host_id. sessionCreateRouter.registerHost
// is keyed on this field, not on any per-session HostID, so tests must set
// it directly rather than going through uplink_conn_test.go's sendAnnounce
// helper (which only fills in the per-session field).
func announceHost(t *testing.T, ctx context.Context, c *websocket.Conn, hostID string) {
	t.Helper()
	payload, err := json.Marshal(proto.AnnouncePayload{HostID: hostID})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeAnnounce, Payload: payload})); err != nil {
		t.Fatalf("announceHost write: %v", err)
	}
}

// nextNonAdminUplinkFrame reads from an uplink connection until it sees a
// frame that is not one of the always-on connection-lifecycle types
// (AUTH_INFO, VIEWERS), matching the skip pattern uplink_conn_test.go's own
// helpers use.
func nextNonAdminUplinkFrame(t *testing.T, ctx context.Context, c *websocket.Conn) proto.Frame {
	t.Helper()
	for {
		_, data, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read uplink frame: %v", err)
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			t.Fatalf("unmarshal uplink frame: %v", err)
		}
		if f.Type == proto.TypeAuthInfo || f.Type == proto.TypeViewers {
			continue
		}
		return f
	}
}

func writeSessionCreate(t *testing.T, ctx context.Context, c *websocket.Conn, requestID, hostID, profileID string) {
	t.Helper()
	payload, err := json.Marshal(proto.SessionCreatePayload{RequestID: requestID, HostID: hostID, ProfileID: profileID})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeSessionCreate, Payload: payload})); err != nil {
		t.Fatalf("write SESSION_CREATE: %v", err)
	}
}

func readSessionCreated(t *testing.T, ctx context.Context, c *websocket.Conn) proto.SessionCreatedPayload {
	t.Helper()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read SESSION_CREATED: %v", err)
	}
	f, err := proto.Unmarshal(data)
	if err != nil {
		t.Fatalf("unmarshal SESSION_CREATED: %v", err)
	}
	if f.Type != proto.TypeSessionCreated {
		t.Fatalf("frame type = %v, want SESSION_CREATED", f.Type)
	}
	var p proto.SessionCreatedPayload
	if err := json.Unmarshal(f.Payload, &p); err != nil {
		t.Fatalf("decode SESSION_CREATED: %v", err)
	}
	return p
}

// TestSessionCreateRoutesToAnnouncedHost is the success path end to end: a
// desktop with zero open sessions (design doc §3 — host routing must work
// before any session exists) announces a host id, a client asks that host to
// create a session, and the reply comes back to the same client.
func TestSessionCreateRoutesToAnnouncedHost(t *testing.T) {
	store, userAID, tokenA, _, _ := newClientTestStore(t)
	srv := newClientTestServer(t, store)
	httpSrv := newRelayHTTPServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uplink, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+tokenA)
	if err != nil {
		t.Fatalf("dial uplink: %v", err)
	}
	defer uplink.Close(websocket.StatusNormalClosure, "")
	announceHost(t, ctx, uplink, "host-a")

	// Give registerHost a moment to land before the client asks for it.
	waitForSessionCreateHost(t, srv, "host-a", userAID)

	client, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenA)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer client.Close(websocket.StatusNormalClosure, "")

	writeSessionCreate(t, ctx, client, "req-1", "host-a", "profile-1")

	forwarded := nextNonAdminUplinkFrame(t, ctx, uplink)
	if forwarded.Type != proto.TypeSessionCreate {
		t.Fatalf("uplink frame type = %v, want SESSION_CREATE", forwarded.Type)
	}
	if forwarded.SessionID != uuid.Nil {
		t.Fatalf("forwarded SESSION_CREATE session id = %v, want nil (no session exists yet)", forwarded.SessionID)
	}
	var req proto.SessionCreatePayload
	if err := json.Unmarshal(forwarded.Payload, &req); err != nil {
		t.Fatalf("decode forwarded SESSION_CREATE: %v", err)
	}
	if req.RequestID != "req-1" || req.HostID != "host-a" || req.ProfileID != "profile-1" {
		t.Fatalf("forwarded payload = %+v, want req-1/host-a/profile-1", req)
	}

	newSid := uuid.New()
	respPayload, _ := json.Marshal(proto.SessionCreatedPayload{RequestID: "req-1", OK: true, SessionID: newSid.String()})
	if err := uplink.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeSessionCreated, SessionID: newSid, Payload: respPayload,
	})); err != nil {
		t.Fatalf("write SESSION_CREATED: %v", err)
	}

	got := readSessionCreated(t, ctx, client)
	if !got.OK || got.RequestID != "req-1" || got.SessionID != newSid.String() {
		t.Fatalf("client SESSION_CREATED = %+v, want ok request_id=req-1 session_id=%s", got, newSid)
	}
}

// TestSessionCreateCrossUserRefused is the test that matters most in this
// task: a client authenticated as user B must not be able to reach user A's
// host, even by naming its host_id correctly. A routing table keyed only on
// a guessable/observable id (rather than checked against the announcing
// uplink's owner) is exactly where this goes wrong.
func TestSessionCreateCrossUserRefused(t *testing.T) {
	store, userAID, tokenA, _, tokenB := newClientTestStore(t)
	srv := newClientTestServer(t, store)
	httpSrv := newRelayHTTPServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uplinkA, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+tokenA)
	if err != nil {
		t.Fatalf("dial uplink A: %v", err)
	}
	defer uplinkA.Close(websocket.StatusNormalClosure, "")
	announceHost(t, ctx, uplinkA, "host-a")
	waitForSessionCreateHost(t, srv, "host-a", userAID)

	clientB, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenB)
	if err != nil {
		t.Fatalf("dial client B: %v", err)
	}
	defer clientB.Close(websocket.StatusNormalClosure, "")

	writeSessionCreate(t, ctx, clientB, "req-cross-user", "host-a", "profile-1")

	got := readSessionCreated(t, ctx, clientB)
	if got.OK || got.Error != "unknown_host_id" {
		t.Fatalf("cross-user SESSION_CREATED = %+v, want ok=false error=unknown_host_id", got)
	}

	// user A's uplink must never have seen the request at all.
	assertNoUplinkSessionCreate(t, ctx, uplinkA)
}

// TestSessionCreateUnknownHostIDRefused pins "a request naming an
// unknown/disconnected host_id gets a prompt error, not silence."
func TestSessionCreateUnknownHostIDRefused(t *testing.T) {
	_, _, tokenA, _, _ := newClientTestStore(t)
	store, _, tokenA2, _, _ := newClientTestStore(t)
	_ = tokenA // unused placeholder to keep both stores independent; see below
	srv := newClientTestServer(t, store)
	httpSrv := newRelayHTTPServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenA2)
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	defer client.Close(websocket.StatusNormalClosure, "")

	writeSessionCreate(t, ctx, client, "req-unknown", "no-such-host", "profile-1")
	got := readSessionCreated(t, ctx, client)
	if got.OK || got.Error != "unknown_host_id" {
		t.Fatalf("unknown-host SESSION_CREATED = %+v, want ok=false error=unknown_host_id", got)
	}
}

// TestSessionCreateDuplicateRequestIDRejected mirrors fs_router_test.go's
// duplicate-request coverage for the create pair: a second requester using
// an in-flight request id is refused, and the original requester still gets
// its answer.
func TestSessionCreateDuplicateRequestIDRejected(t *testing.T) {
	store, userAID, tokenA, _, _ := newClientTestStore(t)
	srv := newClientTestServer(t, store)
	httpSrv := newRelayHTTPServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uplink, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+tokenA)
	if err != nil {
		t.Fatalf("dial uplink: %v", err)
	}
	defer uplink.Close(websocket.StatusNormalClosure, "")
	announceHost(t, ctx, uplink, "host-a")
	waitForSessionCreateHost(t, srv, "host-a", userAID)

	first, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenA)
	if err != nil {
		t.Fatalf("dial first client: %v", err)
	}
	defer first.Close(websocket.StatusNormalClosure, "")
	second, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenA)
	if err != nil {
		t.Fatalf("dial second client: %v", err)
	}
	defer second.Close(websocket.StatusNormalClosure, "")

	writeSessionCreate(t, ctx, first, "dup", "host-a", "profile-1")
	forwarded := nextNonAdminUplinkFrame(t, ctx, uplink)
	if forwarded.Type != proto.TypeSessionCreate {
		t.Fatalf("uplink frame type = %v, want SESSION_CREATE", forwarded.Type)
	}

	writeSessionCreate(t, ctx, second, "dup", "host-a", "profile-1")
	gotSecond := readSessionCreated(t, ctx, second)
	if gotSecond.OK || gotSecond.Error != "duplicate_request_id" {
		t.Fatalf("second requester SESSION_CREATED = %+v, want ok=false error=duplicate_request_id", gotSecond)
	}

	respPayload, _ := json.Marshal(proto.SessionCreatedPayload{RequestID: "dup", OK: true, SessionID: uuid.New().String()})
	if err := uplink.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeSessionCreated, Payload: respPayload})); err != nil {
		t.Fatalf("write SESSION_CREATED: %v", err)
	}
	gotFirst := readSessionCreated(t, ctx, first)
	if !gotFirst.OK || gotFirst.RequestID != "dup" {
		t.Fatalf("original requester SESSION_CREATED = %+v, want ok=true request_id=dup", gotFirst)
	}
}

// TestSessionCreateResponseNotBroadcast is the mutation test for "the
// response goes to the requesting client only, never broadcast": two clients
// authenticated as the same owner both talk to the relay, only one of them
// asks for a session, and the other must never see the reply.
func TestSessionCreateResponseNotBroadcast(t *testing.T) {
	store, userAID, tokenA, _, _ := newClientTestStore(t)
	srv := newClientTestServer(t, store)
	httpSrv := newRelayHTTPServer(t, srv)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	uplink, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+tokenA)
	if err != nil {
		t.Fatalf("dial uplink: %v", err)
	}
	defer uplink.Close(websocket.StatusNormalClosure, "")
	announceHost(t, ctx, uplink, "host-a")
	waitForSessionCreateHost(t, srv, "host-a", userAID)

	requester, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenA)
	if err != nil {
		t.Fatalf("dial requester: %v", err)
	}
	defer requester.Close(websocket.StatusNormalClosure, "")
	bystander, _, err := dialClientWSBearer(t, ctx, httpSrv, tokenA)
	if err != nil {
		t.Fatalf("dial bystander: %v", err)
	}
	defer bystander.Close(websocket.StatusNormalClosure, "")

	// The bystander has its own outstanding request against the same host,
	// still unanswered when the requester's reply lands. This is what makes
	// the assertion below actually exercise "per-requester routing, not
	// broadcast": a router that iterated every registered request instead of
	// looking up the one matching request_id would land the requester's
	// reply on the bystander's channel too, since both are registered at
	// once.
	writeSessionCreate(t, ctx, bystander, "req-bystander", "host-a", "profile-1")
	nextNonAdminUplinkFrame(t, ctx, uplink)

	writeSessionCreate(t, ctx, requester, "req-solo", "host-a", "profile-1")
	nextNonAdminUplinkFrame(t, ctx, uplink)

	respPayload, _ := json.Marshal(proto.SessionCreatedPayload{RequestID: "req-solo", OK: true, SessionID: uuid.New().String()})
	if err := uplink.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeSessionCreated, Payload: respPayload})); err != nil {
		t.Fatalf("write SESSION_CREATED: %v", err)
	}

	got := readSessionCreated(t, ctx, requester)
	if !got.OK || got.RequestID != "req-solo" {
		t.Fatalf("requester SESSION_CREATED = %+v, want ok=true request_id=req-solo", got)
	}

	bystanderCtx, bystanderCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer bystanderCancel()
	if _, _, err := bystander.Read(bystanderCtx); err == nil {
		t.Fatal("bystander client received a frame meant for the requester")
	}
}

// waitForSessionCreateHost polls until an uplink's ANNOUNCE has registered
// hostID with the given owner, so tests don't race the reader goroutine that
// parses ANNOUNCE and calls registerHost.
func waitForSessionCreateHost(t *testing.T, srv *Server, hostID, ownerUserID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if route, ok := srv.sessionCreateRoutes().lookupHost(hostID); ok && route.ownerUserID == ownerUserID {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("host %q for owner %q was never registered", hostID, ownerUserID)
}

// assertNoUplinkSessionCreate confirms no TypeSessionCreate frame arrives on
// c within a short window, skipping the admin frames every uplink connection
// gets on connect.
func assertNoUplinkSessionCreate(t *testing.T, ctx context.Context, c *websocket.Conn) {
	t.Helper()
	deadline := time.Now().Add(300 * time.Millisecond)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return
		}
		readCtx, cancel := context.WithTimeout(ctx, remaining)
		_, data, err := c.Read(readCtx)
		cancel()
		if err != nil {
			return
		}
		f, err := proto.Unmarshal(data)
		if err != nil {
			continue
		}
		if f.Type == proto.TypeAuthInfo || f.Type == proto.TypeViewers {
			continue
		}
		t.Fatalf("uplink unexpectedly received frame type %v", f.Type)
	}
}

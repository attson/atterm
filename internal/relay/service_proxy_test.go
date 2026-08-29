package relay

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/serviceproxy"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func TestServiceHubTicketsAreOwnerBoundAndSingleUse(t *testing.T) {
	hub := newServiceHub()
	sessionID := uuid.New()
	serviceID := uuid.New()
	uplink := make(chan proto.Frame, 1)
	requester := make(chan proto.Frame, 1)
	hub.registerSession(sessionID, "owner-a", uplink)
	forwarded, clientTicket, ok := hub.begin("owner-a", sessionID, proto.ServiceOpenPayload{
		RequestID: "req", ServiceID: serviceID.String(), Sealed: []byte{1},
	}, requester, nil)
	if !ok || forwarded.HostTicket == "" || clientTicket == "" {
		t.Fatalf("begin = %+v ticket=%q ok=%v", forwarded, clientTicket, ok)
	}
	if _, err := hub.attach("owner-b", serviceRoleHost, serviceRegistration{
		ServiceID: serviceID.String(), Ticket: forwarded.HostTicket,
	}, nil); err == nil {
		t.Fatal("another owner used the host ticket")
	}
	if _, err := hub.attach("owner-a", serviceRoleHost, serviceRegistration{
		ServiceID: serviceID.String(), Ticket: forwarded.HostTicket,
	}, nil); err != nil {
		t.Fatalf("first host ticket use: %v", err)
	}
	if _, err := hub.attach("owner-a", serviceRoleHost, serviceRegistration{
		ServiceID: serviceID.String(), Ticket: forwarded.HostTicket,
	}, nil); err == nil {
		t.Fatal("host ticket was reusable")
	}

	responseBody, _ := json.Marshal(proto.ServiceOpenedPayload{RequestID: "req", ServiceID: serviceID.String(), OK: true})
	response, route, _, ok := hub.finish(proto.Frame{Type: proto.TypeServiceOpened, SessionID: sessionID, Payload: responseBody}, uplink)
	if !ok || route != chan<- proto.Frame(requester) {
		t.Fatal("successful owner response was not routed to its requester")
	}
	var opened proto.ServiceOpenedPayload
	_ = json.Unmarshal(response.Payload, &opened)
	if opened.ClientTicket != clientTicket {
		t.Fatal("finish did not inject the requester's one-time ticket")
	}
	if _, err := hub.attach("owner-a", serviceRoleClient, serviceRegistration{
		ServiceID: serviceID.String(), Ticket: opened.ClientTicket,
	}, nil); err != nil {
		t.Fatalf("first client ticket use: %v", err)
	}
	if _, err := hub.attach("owner-a", serviceRoleClient, serviceRegistration{
		ServiceID: serviceID.String(), Ticket: opened.ClientTicket,
	}, nil); err == nil {
		t.Fatal("client ticket was reusable")
	}
}

func TestServiceHubLimitsDirectionAndExpiry(t *testing.T) {
	hub := newServiceHub()
	sessionID := uuid.New()
	uplink := make(chan proto.Frame, 1)
	requester := make(chan proto.Frame, 1)
	hub.registerSession(sessionID, "owner", uplink)
	serviceID := uuid.New()
	request := proto.ServiceOpenPayload{RequestID: "limits", ServiceID: serviceID.String(), Sealed: []byte{1}}
	if _, _, ok := hub.begin("owner", sessionID, request, requester, nil); !ok {
		t.Fatal("begin failed")
	}
	hub.mu.Lock()
	lease := hub.services[serviceID]
	hub.mu.Unlock()
	clientKey := bytes.Repeat([]byte{1}, 32)
	hostKey := bytes.Repeat([]byte{2}, 32)
	clientCodec, _ := serviceproxy.NewCodec(serviceID, clientKey, hostKey)
	hostCodec, _ := serviceproxy.NewCodec(serviceID, hostKey, clientKey)

	hostOpen, _ := hostCodec.Seal(serviceproxy.KindOpen, 1, nil)
	if err := hub.observePacket(lease, serviceRoleHost, hostOpen); err == nil {
		t.Fatal("host was allowed to create a connection id")
	}
	for connectionID := uint32(1); connectionID <= uint32(serviceMaxConnections); connectionID++ {
		packet, _ := clientCodec.Seal(serviceproxy.KindOpen, connectionID, nil)
		if err := hub.observePacket(lease, serviceRoleClient, packet); err != nil {
			t.Fatalf("open %d: %v", connectionID, err)
		}
	}
	overLimit, _ := clientCodec.Seal(serviceproxy.KindOpen, uint32(serviceMaxConnections+1), nil)
	if err := hub.observePacket(lease, serviceRoleClient, overLimit); err == nil {
		t.Fatal("17th multiplex connection was accepted")
	}
	hub.mu.Lock()
	lease.bytes = serviceMaxForwardBytes
	hub.mu.Unlock()
	data, _ := clientCodec.Seal(serviceproxy.KindData, 1, []byte("x"))
	if err := hub.observePacket(lease, serviceRoleClient, data); err == nil {
		t.Fatal("forward-byte cap was not enforced")
	}

	hub.mu.Lock()
	lease.bytes = 0
	lease.lastActive = time.Now().Add(-servicePendingTTL - time.Second)
	expired := hub.reapExpiredLocked(time.Now())
	hub.mu.Unlock()
	if len(expired) != 1 || expired[0] != lease {
		t.Fatal("unpaired service did not expire")
	}

	limitHub := newServiceHub()
	limitHub.registerSession(sessionID, "owner", uplink)
	for i := 0; i < serviceMaxPerUser; i++ {
		if _, _, ok := limitHub.begin("owner", sessionID, proto.ServiceOpenPayload{
			RequestID: "req-" + string(rune('a'+i)), ServiceID: uuid.New().String(), Sealed: []byte{1},
		}, requester, nil); !ok {
			t.Fatalf("service %d within per-user limit was rejected", i+1)
		}
	}
	if _, code, ok := limitHub.begin("owner", sessionID, proto.ServiceOpenPayload{
		RequestID: "req-over", ServiceID: uuid.New().String(), Sealed: []byte{1},
	}, requester, nil); ok || code != "service_limit" {
		t.Fatalf("fifth service = ok=%v code=%q", ok, code)
	}
}

// TestRemoteServiceControlAndDataE2E pins the relay's whole Preview contract:
// control is driver/full gated and routed over the session mirror; data is a
// separate opaque service socket pair; opening that pair does not create a
// second terminal subscriber/STREAM_REQUEST.
func TestRemoteServiceControlAndDataE2E(t *testing.T) {
	store, userID, token := newUplinkTestStore(t)
	srv := newUplinkTestServer(t, store)
	httpSrv := httptest.NewServer(srv)
	defer httpSrv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	uplink, _, err := dialUplinkWS(t, ctx, httpSrv, "Bearer "+token)
	if err != nil {
		t.Fatal(err)
	}
	defer uplink.CloseNow()
	sessionID := uuid.New()
	announce, _ := json.Marshal(proto.AnnouncePayload{
		HostID: "owner-host",
		Sessions: []proto.SessionInfo{{
			ID: sessionID.String(), HostID: "owner-host",
			RemotePermission: proto.RemotePermissionFull,
		}},
	})
	if err := uplink.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{Type: proto.TypeAnnounce, Payload: announce})); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if sess, ok := srv.Registry().Get(sessionID); ok && sess.OwnerUserID == userID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("announced session did not reconcile")
		}
		time.Sleep(10 * time.Millisecond)
	}

	client := dialClientAttach(t, ctx, httpSrv, token, sessionID, "preview-driver")
	defer client.CloseNow()
	drainAttachIntro(t, ctx, client)
	streamRequest := nextNonAdminUplinkFrame(t, ctx, uplink)
	if streamRequest.Type != proto.TypeStreamRequest {
		t.Fatalf("first uplink control = 0x%02x, want STREAM_REQUEST", streamRequest.Type)
	}

	claim, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "preview-driver"})
	writeClientFrame(t, ctx, client, proto.TypeClaimDriver, sessionID, claim)
	forwardedClaim := nextNonAdminUplinkFrame(t, ctx, uplink)
	if forwardedClaim.Type != proto.TypeClaimDriver {
		t.Fatalf("claim uplink control = 0x%02x, want CLAIM_DRIVER", forwardedClaim.Type)
	}
	for {
		_, raw, err := client.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := proto.Unmarshal(raw)
		if err == nil && frame.Type == proto.TypeMeta {
			var got proto.MetaPayload
			_ = json.Unmarshal(frame.Payload, &got)
			if got.DriverClientID == "preview-driver" {
				break
			}
		}
	}

	serviceID := uuid.New()
	openPayload, _ := json.Marshal(proto.ServiceOpenPayload{
		RequestID: "service-request",
		ServiceID: serviceID.String(),
		Sealed:    []byte{1, 2, 3},
	})
	writeClientFrame(t, ctx, client, proto.TypeServiceOpen, sessionID, openPayload)
	forwarded := nextNonAdminUplinkFrame(t, ctx, uplink)
	if forwarded.Type != proto.TypeServiceOpen || forwarded.SessionID != sessionID {
		t.Fatalf("forwarded frame = type 0x%02x sid=%s", forwarded.Type, forwarded.SessionID)
	}
	var ownerOpen proto.ServiceOpenPayload
	if err := json.Unmarshal(forwarded.Payload, &ownerOpen); err != nil || ownerOpen.HostTicket == "" {
		t.Fatalf("forwarded SERVICE_OPEN = %+v err=%v", ownerOpen, err)
	}

	hostService := dialServiceSocket(t, ctx, httpSrv, token, "/service-host", serviceID, ownerOpen.HostTicket)
	defer hostService.CloseNow()
	openedPayload, _ := json.Marshal(proto.ServiceOpenedPayload{
		RequestID: ownerOpen.RequestID,
		ServiceID: ownerOpen.ServiceID,
		OK:        true,
	})
	if err := uplink.Write(ctx, websocket.MessageBinary, proto.Marshal(proto.Frame{
		Type: proto.TypeServiceOpened, SessionID: sessionID, Payload: openedPayload,
	})); err != nil {
		t.Fatal(err)
	}
	var opened proto.ServiceOpenedPayload
	for {
		_, raw, err := client.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		frame, err := proto.Unmarshal(raw)
		if err == nil && frame.Type == proto.TypeServiceOpened {
			if err := json.Unmarshal(frame.Payload, &opened); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if !opened.OK || opened.ClientTicket == "" {
		t.Fatalf("SERVICE_OPENED = %+v", opened)
	}
	clientService := dialServiceSocket(t, ctx, httpSrv, token, "/service-client", serviceID, opened.ClientTicket)
	defer clientService.CloseNow()

	clientKey := bytes.Repeat([]byte{0x11}, 32)
	hostKey := bytes.Repeat([]byte{0x22}, 32)
	clientCodec, _ := serviceproxy.NewCodec(serviceID, clientKey, hostKey)
	hostCodec, _ := serviceproxy.NewCodec(serviceID, hostKey, clientKey)
	openPacket, _ := clientCodec.Seal(serviceproxy.KindOpen, 1, nil)
	if err := clientService.Write(ctx, websocket.MessageBinary, openPacket); err != nil {
		t.Fatal(err)
	}
	_, hostPacket, err := hostService.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	header, body, err := hostCodec.Open(hostPacket)
	if err != nil || header.Kind != serviceproxy.KindOpen || header.Connection != 1 || len(body) != 0 {
		t.Fatalf("host open = %+v body=%q err=%v", header, body, err)
	}
	replyPacket, _ := hostCodec.Seal(serviceproxy.KindData, 1, []byte("opaque-relay-ok"))
	if err := hostService.Write(ctx, websocket.MessageBinary, replyPacket); err != nil {
		t.Fatal(err)
	}
	_, clientPacket, err := clientService.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	header, body, err = clientCodec.Open(clientPacket)
	if err != nil || header.Kind != serviceproxy.KindData || string(body) != "opaque-relay-ok" {
		t.Fatalf("client data = %+v body=%q err=%v", header, body, err)
	}

	// The attach above caused exactly one lazy stream request. Pairing and
	// transferring service packets must not cause another.
	quietCtx, quietCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer quietCancel()
	for {
		_, raw, err := uplink.Read(quietCtx)
		if err != nil {
			if quietCtx.Err() != nil {
				break
			}
			t.Fatal(err)
		}
		frame, err := proto.Unmarshal(raw)
		if err == nil && frame.Type == proto.TypeStreamRequest {
			t.Fatal("service socket created an extra PTY STREAM_REQUEST")
		}
	}
}

func dialServiceSocket(t *testing.T, ctx context.Context, server *httptest.Server, token, path string, serviceID uuid.UUID, ticket string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + path
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		t.Fatal(err)
	}
	registration, _ := json.Marshal(serviceRegistration{ServiceID: serviceID.String(), Ticket: ticket})
	if err := conn.Write(ctx, websocket.MessageBinary, registration); err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	_, ackRaw, err := conn.Read(ctx)
	var ack serviceRegistrationAck
	if err != nil || json.Unmarshal(ackRaw, &ack) != nil || !ack.OK {
		conn.CloseNow()
		t.Fatalf("service registration ack: %q err=%v", ackRaw, err)
	}
	return conn
}

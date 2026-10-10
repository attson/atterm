package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func startPeerServiceAttempt(t *testing.T, permission peerproto.Permission, clientID string) (peerQuickTunnelFixture, *peerHostAttempt, *peerNativeTestChannel, context.Context) {
	t.Helper()
	fixture := newPeerQuickTunnelFixture(t, permission)
	ctx, cancel := context.WithCancel(context.Background())
	channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
	attempt := &peerHostAttempt{
		host: fixture.peerHost.runtime, sessionID: fixture.session.ID,
		permission: string(permission), clientInstanceID: clientID,
	}
	attempt.remove = func() { attempt.close(true) }
	if err := attempt.start(ctx, channel); err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		attempt.close(true)
		cancel()
	})
	return fixture, attempt, channel, ctx
}

func claimPeerServiceDriver(t *testing.T, fixture peerQuickTunnelFixture, attempt *peerHostAttempt) {
	t.Helper()
	attempt.mu.Lock()
	sub := attempt.sub
	attempt.mu.Unlock()
	if sub == nil {
		t.Fatal("Peer subscriber is unavailable")
	}
	fixture.session.ClaimDriver(sub, "preview-driver", "Preview driver")
}

func openPeerService(t *testing.T, ctx context.Context, attempt *peerHostAttempt, channel *peerNativeTestChannel, serviceID uuid.UUID, host string, port uint16) proto.ServiceOpenedPayload {
	t.Helper()
	requestID := uuid.NewString()
	payload, err := json.Marshal(proto.ServiceOpenPayload{
		RequestID: requestID, ServiceID: serviceID.String(),
		PeerFields: &proto.SealedServiceOpenFields{Host: host, Port: port, Scheme: "http"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.handleRecord(ctx, peertransport.RecordFrame, proto.Marshal(proto.Frame{
		Type: proto.TypeServiceOpen, SessionID: attempt.sessionID, Payload: payload,
	})); err != nil {
		t.Fatalf("open Peer service: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		channel.mu.Lock()
		frames := append([][]byte(nil), channel.frames...)
		channel.mu.Unlock()
		for _, raw := range frames {
			frame, err := proto.Unmarshal(raw)
			if err != nil || frame.Type != proto.TypeServiceOpened {
				continue
			}
			var response proto.ServiceOpenedPayload
			if json.Unmarshal(frame.Payload, &response) == nil && response.RequestID == requestID {
				return response
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for Peer service response")
	return proto.ServiceOpenedPayload{}
}

func nextPeerServiceMessage(t *testing.T, channel *peerNativeTestChannel, cursor *int, kind peertransport.ServiceKind) ([]byte, peertransport.ServiceMessage) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		channel.mu.Lock()
		payloads := append([][]byte(nil), channel.services...)
		channel.mu.Unlock()
		for *cursor < len(payloads) {
			raw := payloads[*cursor]
			(*cursor)++
			message, err := peertransport.DecodeServiceMessage(raw)
			if err != nil {
				t.Fatalf("decode Peer service message: %v", err)
			}
			if message.Kind == kind {
				return raw, message
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Peer service kind %d", kind)
	return nil, peertransport.ServiceMessage{}
}

func TestPeerServiceOpenRequiresFullPermissionAndCurrentDriver(t *testing.T) {
	tests := []struct {
		name       string
		permission peerproto.Permission
		claim      bool
		wantOK     bool
	}{
		{name: "view", permission: peerproto.PermissionView},
		{name: "control", permission: peerproto.PermissionControl},
		{name: "full without driver", permission: peerproto.PermissionFull},
		{name: "full current driver", permission: peerproto.PermissionFull, claim: true, wantOK: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, attempt, channel, ctx := startPeerServiceAttempt(t, test.permission, "preview-auth")
			if test.claim {
				claimPeerServiceDriver(t, fixture, attempt)
			}
			response := openPeerService(t, ctx, attempt, channel, uuid.New(), "localhost", 3000)
			if response.OK != test.wantOK {
				t.Fatalf("response=%+v want OK=%v", response, test.wantOK)
			}
			if !test.wantOK && response.Error != "permission_denied" {
				t.Fatalf("error=%q want permission_denied", response.Error)
			}
		})
	}
}

func TestPeerServiceOpenRestrictsTargetsToLoopback(t *testing.T) {
	for _, host := range []string{"", "localhost", "127.0.0.1", "::1"} {
		t.Run("allow "+host, func(t *testing.T) {
			fixture, attempt, channel, ctx := startPeerServiceAttempt(t, peerproto.PermissionFull, "preview-loopback")
			claimPeerServiceDriver(t, fixture, attempt)
			if response := openPeerService(t, ctx, attempt, channel, uuid.New(), host, 3000); !response.OK {
				t.Fatalf("loopback host %q rejected: %+v", host, response)
			}
		})
	}
	for _, host := range []string{"10.0.0.7", "192.168.1.2", "example.test"} {
		t.Run("reject "+host, func(t *testing.T) {
			fixture, attempt, channel, ctx := startPeerServiceAttempt(t, peerproto.PermissionFull, "preview-non-loopback")
			claimPeerServiceDriver(t, fixture, attempt)
			response := openPeerService(t, ctx, attempt, channel, uuid.New(), host, 3000)
			if response.OK || response.Error != "invalid_request" {
				t.Fatalf("non-loopback host %q response=%+v", host, response)
			}
		})
	}
}

func TestPeerServiceRouteLimits(t *testing.T) {
	fixture, attempt, channel, ctx := startPeerServiceAttempt(t, peerproto.PermissionFull, "preview-limits")
	claimPeerServiceDriver(t, fixture, attempt)
	for index := 0; index < maxPeerServicesPerRoute; index++ {
		if response := openPeerService(t, ctx, attempt, channel, uuid.New(), "localhost", uint16(3000+index)); !response.OK {
			t.Fatalf("service %d rejected: %+v", index, response)
		}
	}
	response := openPeerService(t, ctx, attempt, channel, uuid.New(), "localhost", 4000)
	if response.OK || response.Error != "service_limit" {
		t.Fatalf("over-limit response=%+v", response)
	}
}

func TestPeerServiceLateConnectionCloseIsIdempotent(t *testing.T) {
	fixture, attempt, channel, ctx := startPeerServiceAttempt(t, peerproto.PermissionFull, "preview-late-close")
	claimPeerServiceDriver(t, fixture, attempt)
	serviceID := uuid.New()
	if response := openPeerService(t, ctx, attempt, channel, serviceID, "localhost", 3000); !response.OK {
		t.Fatalf("service open response=%+v", response)
	}
	closePayload, err := json.Marshal(proto.ServiceClosePayload{ServiceID: serviceID.String()})
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.handleRecord(ctx, peertransport.RecordFrame, proto.Marshal(proto.Frame{
		Type: proto.TypeServiceClose, SessionID: fixture.session.ID, Payload: closePayload,
	})); err != nil {
		t.Fatal(err)
	}
	lateClose, err := peertransport.EncodeServiceMessage(peertransport.ServiceMessage{
		ServiceID: serviceID, Kind: peertransport.ServiceClose, Connection: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.handleRecord(ctx, peertransport.RecordService, lateClose); err != nil {
		t.Fatalf("late connection close rejected: %v", err)
	}
	unknownData, err := peertransport.EncodeServiceMessage(peertransport.ServiceMessage{
		ServiceID: serviceID, Kind: peertransport.ServiceData, Connection: 1, Data: []byte("late"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := attempt.handleRecord(ctx, peertransport.RecordService, unknownData); err == nil {
		t.Fatal("unknown service data did not fail closed")
	}
}

func TestPeerServicePreviewConnectionAndByteLimits(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionFull)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &peerNativeDirectClient{
		id: uuid.NewString(), app: fixture.app, ctx: ctx, cancel: cancel,
		authenticated: true, channel: &peerNativeTestChannel{remoteMembership: fixture.hostMembership},
	}
	preview, err := client.registerServicePreview(ctx, uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	defer preview.close()
	peers := make([]net.Conn, 0, 17)
	for index := 0; index < 17; index++ {
		local, remote := net.Pipe()
		peers = append(peers, remote)
		accepted := preview.acceptConn(local)
		if accepted != (index < 16) {
			t.Fatalf("connection %d accepted=%v", index+1, accepted)
		}
		if !accepted {
			_ = local.Close()
		}
	}
	for _, conn := range peers {
		_ = conn.Close()
	}

	preview.mu.Lock()
	preview.transferred = maxPeerServiceBytes - 1
	preview.mu.Unlock()
	if !preview.reserveBytes(1) || preview.reserveBytes(1) {
		t.Fatal("requester byte budget did not stop exactly at 512 MiB")
	}
	host := &peerServiceHost{}
	host.mu.Lock()
	host.transferred = maxPeerServiceBytes - 1
	host.mu.Unlock()
	if !host.reserveBytes(1) || host.reserveBytes(1) {
		t.Fatal("owner byte budget did not stop exactly at 512 MiB")
	}
}

func TestPeerServiceInboundDoesNotBlockTransportCallback(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serviceID := uuid.New()
	local, blockedPeer := net.Pipe()
	defer local.Close()
	defer blockedPeer.Close()
	preview := &peerServicePreview{
		id: serviceID, ctx: ctx, cancel: cancel,
		receive:     make(chan peertransport.ServiceMessage, 1),
		connections: map[uint32]net.Conn{1: local},
	}
	done := make(chan error, 1)
	go func() {
		done <- preview.handle(peertransport.ServiceMessage{
			ServiceID: serviceID, Kind: peertransport.ServiceData,
			Connection: 1, Data: []byte("blocked local reader"),
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Peer service handler blocked the shared transport callback on a local TCP write")
	}
	select {
	case message := <-preview.receive:
		if message.Kind != peertransport.ServiceData {
			t.Fatalf("queued kind=%d", message.Kind)
		}
	default:
		t.Fatal("inbound service data was not queued")
	}
}

func TestPeerServicePreviewCarriesBidirectionalTCPBytes(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	targetErr := make(chan error, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			targetErr <- acceptErr
			return
		}
		defer conn.Close()
		request := make([]byte, 4)
		if _, readErr := io.ReadFull(conn, request); readErr != nil {
			targetErr <- readErr
			return
		}
		if !bytes.Equal(request, []byte("ping")) {
			targetErr <- io.ErrUnexpectedEOF
			return
		}
		_, writeErr := conn.Write([]byte("pong"))
		targetErr <- writeErr
	}()

	fixture, attempt, hostChannel, ctx := startPeerServiceAttempt(t, peerproto.PermissionFull, "preview-e2e")
	claimPeerServiceDriver(t, fixture, attempt)
	port := listener.Addr().(*net.TCPAddr).Port
	serviceID := uuid.New()
	if response := openPeerService(t, ctx, attempt, hostChannel, serviceID, "127.0.0.1", uint16(port)); !response.OK {
		t.Fatalf("service open response=%+v", response)
	}

	clientCtx, clientCancel := context.WithCancel(ctx)
	defer clientCancel()
	clientChannel := &peerNativeTestChannel{remoteMembership: fixture.hostMembership}
	client := &peerNativeDirectClient{
		id: uuid.NewString(), app: fixture.app, ctx: clientCtx, cancel: clientCancel,
		authenticated: true, channel: clientChannel,
	}
	preview, err := client.registerServicePreview(clientCtx, serviceID)
	if err != nil {
		t.Fatal(err)
	}
	defer preview.close()
	go preview.run()
	local, browser := net.Pipe()
	defer browser.Close()
	if !preview.acceptConn(local) {
		t.Fatal("requester rejected first local connection")
	}
	clientCursor := 0
	hostCursor := 0
	openRaw, _ := nextPeerServiceMessage(t, clientChannel, &clientCursor, peertransport.ServiceOpen)
	if err := attempt.handleRecord(ctx, peertransport.RecordService, openRaw); err != nil {
		t.Fatal(err)
	}
	_ = browser.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := browser.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	dataRaw, _ := nextPeerServiceMessage(t, clientChannel, &clientCursor, peertransport.ServiceData)
	if err := attempt.handleRecord(ctx, peertransport.RecordService, dataRaw); err != nil {
		t.Fatal(err)
	}
	_, response := nextPeerServiceMessage(t, hostChannel, &hostCursor, peertransport.ServiceData)
	handleErr := make(chan error, 1)
	go func() { handleErr <- preview.handle(response) }()
	got := make([]byte, 4)
	if _, err := io.ReadFull(browser, got); err != nil {
		t.Fatal(err)
	}
	if err := <-handleErr; err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte("pong")) {
		t.Fatalf("preview response=%q", got)
	}
	if err := <-targetErr; err != nil {
		t.Fatal(err)
	}
}

func TestPeerServiceClosesWhenAuthorityOrRouteChanges(t *testing.T) {
	t.Run("driver loss", func(t *testing.T) {
		fixture, attempt, channel, ctx := startPeerServiceAttempt(t, peerproto.PermissionFull, "preview-driver-loss")
		claimPeerServiceDriver(t, fixture, attempt)
		serviceID := uuid.New()
		if response := openPeerService(t, ctx, attempt, channel, serviceID, "localhost", 3000); !response.OK {
			t.Fatalf("service open response=%+v", response)
		}
		attempt.mu.Lock()
		host := attempt.services[serviceID]
		attempt.mu.Unlock()
		other, _ := fixture.session.Subscribe(0, "other-driver", "Other driver")
		defer fixture.session.Unsubscribe(other)
		fixture.session.ClaimDriver(other, "other-driver", "Other driver")
		waitFor(t, 3*time.Second, "Peer service close after driver loss", func() bool { return host.ctx.Err() != nil })
	})

	t.Run("owner permission downgrade", func(t *testing.T) {
		fixture, attempt, channel, ctx := startPeerServiceAttempt(t, peerproto.PermissionFull, "preview-downgrade")
		claimPeerServiceDriver(t, fixture, attempt)
		serviceID := uuid.New()
		if response := openPeerService(t, ctx, attempt, channel, serviceID, "localhost", 3000); !response.OK {
			t.Fatalf("service open response=%+v", response)
		}
		attempt.mu.Lock()
		host := attempt.services[serviceID]
		attempt.mu.Unlock()
		fixture.app.cfgStore.mu.Lock()
		fixture.app.cfgStore.cfg.RemotePermission = proto.RemotePermissionControl
		fixture.app.cfgStore.mu.Unlock()
		waitFor(t, 3*time.Second, "Peer service close after permission downgrade", func() bool { return host.ctx.Err() != nil })
	})

	t.Run("route replacement", func(t *testing.T) {
		fixture, old, channel, ctx := startPeerServiceAttempt(t, peerproto.PermissionFull, "stable-preview-client")
		claimPeerServiceDriver(t, fixture, old)
		serviceID := uuid.New()
		if response := openPeerService(t, ctx, old, channel, serviceID, "localhost", 3000); !response.OK {
			t.Fatalf("service open response=%+v", response)
		}
		old.mu.Lock()
		host := old.services[serviceID]
		old.mu.Unlock()
		replacement := &peerHostAttempt{
			host: fixture.peerHost.runtime, sessionID: fixture.session.ID,
			permission: proto.RemotePermissionFull, clientInstanceID: "stable-preview-client",
		}
		replacement.remove = func() { replacement.close(true) }
		if err := replacement.start(ctx, &peerNativeTestChannel{remoteMembership: fixture.clientMembership}); err != nil {
			t.Fatal(err)
		}
		defer replacement.close(true)
		waitFor(t, time.Second, "Peer service close after route replacement", func() bool { return host.ctx.Err() != nil })
	})
}

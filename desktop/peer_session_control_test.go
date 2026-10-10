package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

func TestPeerHostInfoAdvertisesCreateOnlyForUnscopedControlMembership(t *testing.T) {
	unscoped := newPeerQuickTunnelFixtureWithScope(t, peerproto.PermissionControl, false)
	host, _, err := unscoped.peerHost.runtime.hostInfo(unscoped.clientIdentity.PeerID())
	if err != nil || host.Permission != peertransport.PermissionControl {
		t.Fatalf("unscoped host=%+v err=%v", host, err)
	}

	scoped := newPeerQuickTunnelFixture(t, peerproto.PermissionFull)
	host, _, err = scoped.peerHost.runtime.hostInfo(scoped.clientIdentity.PeerID())
	if err != nil || host.Permission != peertransport.PermissionView {
		t.Fatalf("scoped host=%+v err=%v", host, err)
	}
}

func TestPeerSessionControlCreatesWithoutTerminalSubscriberAndBoundsInflight(t *testing.T) {
	fixture := newPeerQuickTunnelFixtureWithScope(t, peerproto.PermissionControl, false)
	cfg := fixture.host.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "peer-profile", Name: "Peer profile", Shell: "/bin/sh"}}
	if err := fixture.host.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
	control, err := newPeerSessionControlHost(ctx, fixture.peerHost.runtime, channel, fixture.clientMembership)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	createdID := uuid.MustParse("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee")
	control.handler.newSession = func(context.Context, NewSessionReq) (uuid.UUID, error) {
		close(started)
		<-release
		return createdID, nil
	}
	send := func(requestID string) {
		payload, marshalErr := json.Marshal(proto.SessionCreatePayload{
			RequestID: requestID, HostID: fixture.host.hostID, ProfileID: "peer-profile",
		})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		handled, handleErr := control.Handle(peertransport.RecordSessionCreate, payload)
		if !handled || handleErr != nil {
			t.Fatalf("handle create %q: handled=%v err=%v", requestID, handled, handleErr)
		}
	}

	send("first")
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("first create did not start")
	}
	send("second")
	busy := waitForControlSessionCreated(t, channel, "second")
	if busy.OK || busy.Error != sessionCreateErrBusy {
		t.Fatalf("busy response=%+v", busy)
	}
	close(release)
	created := waitForControlSessionCreated(t, channel, "first")
	if !created.OK || created.SessionID != createdID.String() {
		t.Fatalf("created response=%+v", created)
	}
	if got := fixture.session.SubscriberCount(); got != 0 {
		t.Fatalf("host-control route created %d terminal subscribers", got)
	}
}

func TestPeerSessionControlRevalidatesAuthorizationBeforeFork(t *testing.T) {
	fixture := newPeerQuickTunnelFixtureWithScope(t, peerproto.PermissionControl, false)
	cfg := fixture.host.cfg.Get()
	cfg.Profiles = []SessionProfile{{ID: "peer-profile", Name: "Peer profile", Shell: "/bin/sh"}}
	if err := fixture.host.cfg.Set(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
	control, err := newPeerSessionControlHost(ctx, fixture.peerHost.runtime, channel, fixture.clientMembership)
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	control.handler.testHook = func() {
		close(started)
		<-release
	}
	control.handler.newSession = func(context.Context, NewSessionReq) (uuid.UUID, error) {
		t.Fatal("authorization downgrade reached PTY fork")
		return uuid.Nil, errors.New("unreachable")
	}
	payload, _ := json.Marshal(proto.SessionCreatePayload{
		RequestID: "downgrade", HostID: fixture.host.hostID, ProfileID: "peer-profile",
	})
	if handled, handleErr := control.Handle(peertransport.RecordSessionCreate, payload); !handled || handleErr != nil {
		t.Fatalf("handle create: handled=%v err=%v", handled, handleErr)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("create worker did not start")
	}
	fixture.app.cfgStore.mu.Lock()
	fixture.app.cfgStore.cfg.RemotePermission = proto.RemotePermissionView
	fixture.app.cfgStore.mu.Unlock()
	close(release)
	response := waitForControlSessionCreated(t, channel, "downgrade")
	if response.OK || response.Error != sessionCreateErrPermissionDenied {
		t.Fatalf("downgrade response=%+v", response)
	}
}

func TestPeerSessionControlRejectsScopedViewWrongHostAndRevokedMember(t *testing.T) {
	for _, permission := range []peerproto.Permission{peerproto.PermissionView, peerproto.PermissionControl} {
		fixture := newPeerQuickTunnelFixture(t, permission)
		if _, err := newPeerSessionControlHost(context.Background(), fixture.peerHost.runtime,
			&peerNativeTestChannel{remoteMembership: fixture.clientMembership}, fixture.clientMembership); err == nil {
			t.Fatalf("scoped %s membership opened host-control route", permission)
		}
	}

	fixture := newPeerQuickTunnelFixtureWithScope(t, peerproto.PermissionControl, false)
	channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
	control, err := newPeerSessionControlHost(context.Background(), fixture.peerHost.runtime, channel, fixture.clientMembership)
	if err != nil {
		t.Fatal(err)
	}
	wrongHost, _ := json.Marshal(proto.SessionCreatePayload{
		RequestID: "wrong-host", HostID: "another-host", ProfileID: "profile",
	})
	if handled, handleErr := control.Handle(peertransport.RecordSessionCreate, wrongHost); !handled || handleErr == nil {
		t.Fatalf("wrong host: handled=%v err=%v", handled, handleErr)
	}
	if err := fixture.app.RevokePeerMember(fixture.clientIdentity.PeerID()); err != nil {
		t.Fatal(err)
	}
	if _, err := newPeerSessionControlHost(context.Background(), fixture.peerHost.runtime,
		&peerNativeTestChannel{remoteMembership: fixture.clientMembership}, fixture.clientMembership); err == nil {
		t.Fatal("revoked member opened host-control route")
	}
}

func TestPeerSessionCreateExchangeCorrelatesResponseAndPreservesRemoteError(t *testing.T) {
	request := proto.SessionCreatePayload{RequestID: "request-1", HostID: "host", ProfileID: "profile"}
	exchange := newPeerSessionCreateExchange(context.Background(), request, "membership")
	wrong, _ := json.Marshal(proto.SessionCreatedPayload{
		RequestID: "request-2", OK: true, SessionID: uuid.NewString(),
	})
	if err := exchange.onConfigMessage(peertransport.RecordSessionCreated, wrong); err == nil {
		t.Fatal("mismatched response request id was accepted")
	}
	if _, err := exchange.wait(); err == nil {
		t.Fatal("mismatched response did not fail exchange")
	}

	exchange = newPeerSessionCreateExchange(context.Background(), request, "membership")
	remote, _ := json.Marshal(proto.SessionCreatedPayload{
		RequestID: request.RequestID, OK: false, Error: "profile cwd no longer exists",
	})
	if err := exchange.onConfigMessage(peertransport.RecordSessionCreated, remote); err != nil {
		t.Fatal(err)
	}
	_, err := exchange.wait()
	if got := peerSessionCreatePublicError(err).Error(); got != "profile cwd no longer exists" {
		t.Fatalf("remote error=%q", got)
	}
}

func waitForControlSessionCreated(t *testing.T, channel *peerNativeTestChannel, requestID string) proto.SessionCreatedPayload {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		channel.mu.Lock()
		kinds := append([]peertransport.RecordKind(nil), channel.configs...)
		payloads := append([][]byte(nil), channel.configPayloads...)
		channel.mu.Unlock()
		for index, kind := range kinds {
			if kind != peertransport.RecordSessionCreated || index >= len(payloads) {
				continue
			}
			var response proto.SessionCreatedPayload
			if json.Unmarshal(payloads[index], &response) == nil && response.RequestID == requestID {
				return response
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for session-created %q", requestID)
	return proto.SessionCreatedPayload{}
}

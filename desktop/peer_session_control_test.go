package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
)

func TestPeerHostInfoRequiresUnscopedControlMembership(t *testing.T) {
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

func TestPeerSessionCreateControlReturnsFeatureRemovedWithZeroSessions(t *testing.T) {
	fixture := newPeerQuickTunnelFixtureWithScope(t, peerproto.PermissionControl, false)
	fixture.host.server.Registry().Remove(fixture.session.ID)
	channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
	control, err := newPeerSessionControlHost(context.Background(), fixture.peerHost.runtime, channel, fixture.clientMembership)
	if err != nil {
		t.Fatal(err)
	}
	for _, requestID := range []string{"first", "second"} {
		payload, _ := json.Marshal(proto.SessionCreatePayload{RequestID: requestID, HostID: fixture.host.hostID, ProfileID: "missing"})
		if handled, err := control.Handle(peertransport.RecordSessionCreate, payload); !handled || err != nil {
			t.Fatalf("handle create: handled=%v err=%v", handled, err)
		}
		response := waitForControlSessionCreated(t, channel, requestID)
		if response.RequestID != requestID || response.OK || response.SessionID != "" || response.Error != "feature_removed" {
			t.Fatalf("response=%+v", response)
		}
	}
	channel.mu.Lock()
	if len(channel.frames) != 0 || len(channel.configs) != 2 {
		t.Errorf("host-control envelopes: frames=%d configs=%v, want two config records", len(channel.frames), channel.configs)
	}
	channel.mu.Unlock()
	if got := len(fixture.host.server.Registry().List()); got != 0 {
		t.Fatalf("registry grew to %d sessions", got)
	}
	if got := len(fixture.host.Snapshot()); got != 0 {
		t.Fatalf("snapshot grew to %d sessions", got)
	}
}

func TestPeerSessionCreateControlRejectsMalformedPayloadWithoutResponse(t *testing.T) {
	fixture := newPeerQuickTunnelFixtureWithScope(t, peerproto.PermissionControl, false)
	fixture.host.server.Registry().Remove(fixture.session.ID)
	channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
	control, err := newPeerSessionControlHost(context.Background(), fixture.peerHost.runtime, channel, fixture.clientMembership)
	if err != nil {
		t.Fatal(err)
	}
	valid, _ := json.Marshal(proto.SessionCreatePayload{RequestID: "valid", HostID: fixture.host.hostID, ProfileID: "missing"})
	for index, payload := range [][]byte{
		nil,
		make([]byte, maxPeerSessionCreatePayloadBytes+1),
		[]byte("{"),
		[]byte(`{"request_id":"unknown-field","host_id":"` + fixture.host.hostID + `","profile_id":"missing","shell":"/bin/sh"}`),
		[]byte(`{"host_id":"` + fixture.host.hostID + `","profile_id":"missing"}`),
		[]byte(`{"request_id":"missing-profile","host_id":"` + fixture.host.hostID + `"}`),
		append(append([]byte(nil), valid...), []byte(` {}`)...),
	} {
		if handled, err := control.Handle(peertransport.RecordSessionCreate, payload); !handled || err == nil {
			t.Fatalf("malformed %d: handled=%v err=%v", index, handled, err)
		}
	}
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if len(channel.configs) != 0 {
		t.Fatalf("malformed requests emitted records: %v", channel.configs)
	}
	if got := len(fixture.host.server.Registry().List()); got != 0 {
		t.Fatalf("registry grew to %d sessions", got)
	}
	if got := len(fixture.host.Snapshot()); got != 0 {
		t.Fatalf("snapshot grew to %d sessions", got)
	}
}

func TestPeerSessionCreateControlRevalidatesAuthorization(t *testing.T) {
	fixture := newPeerQuickTunnelFixtureWithScope(t, peerproto.PermissionControl, false)
	channel := &peerNativeTestChannel{remoteMembership: fixture.clientMembership}
	control, err := newPeerSessionControlHost(context.Background(), fixture.peerHost.runtime, channel, fixture.clientMembership)
	if err != nil {
		t.Fatal(err)
	}
	fixture.app.cfgStore.mu.Lock()
	fixture.app.cfgStore.cfg.RemotePermission = proto.RemotePermissionView
	fixture.app.cfgStore.mu.Unlock()
	payload, _ := json.Marshal(proto.SessionCreatePayload{RequestID: "downgrade", HostID: fixture.host.hostID, ProfileID: "missing"})
	if handled, err := control.Handle(peertransport.RecordSessionCreate, payload); !handled || err == nil {
		t.Fatalf("unauthorized create: handled=%v err=%v", handled, err)
	}
	channel.mu.Lock()
	defer channel.mu.Unlock()
	if len(channel.configs) != 0 {
		t.Fatalf("unauthorized route emitted records: %v", channel.configs)
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

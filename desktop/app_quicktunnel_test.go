package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type fakeQuickTunnelLifecycle struct {
	stops atomic.Int32
	err   error
}

func (f *fakeQuickTunnelLifecycle) Stop() error {
	f.stops.Add(1)
	return f.err
}

func TestShutdownStopsQuickTunnel(t *testing.T) {
	lifecycle := &fakeQuickTunnelLifecycle{}
	a := &App{quickTunnel: lifecycle}

	a.shutdown(context.Background())

	if got := lifecycle.stops.Load(); got != 1 {
		t.Fatalf("quick tunnel Stop calls = %d, want 1", got)
	}
}

func TestPeerLANOnlyStopsQuickTunnelBeforePersisting(t *testing.T) {
	a := newRelayTestApp(t)
	lifecycle := &fakeQuickTunnelLifecycle{}
	a.quickTunnel = lifecycle
	clientCtx, cancelClient := context.WithCancel(context.Background())
	client := &peerNativeDirectClient{id: "active-public-client", app: a, ctx: clientCtx, cancel: cancelClient}
	a.peerNative = map[string]*peerNativeDirectClient{client.id: client}
	rendezvousCtx, cancelRendezvous := context.WithCancel(context.Background())
	rendezvousDone := make(chan struct{})
	go func() {
		<-rendezvousCtx.Done()
		close(rendezvousDone)
	}()
	a.peerRendezvous = &peerRendezvousLifecycle{
		ctx: rendezvousCtx, cancel: cancelRendezvous, done: rendezvousDone,
	}

	if err := a.SetPeerLANConfig(SetPeerLANConfigReq{LANOnly: true}); err != nil {
		t.Fatal(err)
	}
	if got := lifecycle.stops.Load(); got != 1 {
		t.Fatalf("quick tunnel Stop calls = %d, want 1", got)
	}
	if !a.cfgStore.Get().PeerLANOnly {
		t.Fatal("LAN-only policy was not persisted after Quick Tunnel stopped")
	}
	if len(a.peerNative) != 0 || a.peerRendezvous != nil {
		t.Fatal("LAN-only policy left an existing public Peer route active")
	}
	select {
	case <-clientCtx.Done():
	default:
		t.Fatal("LAN-only policy did not cancel the active Peer client")
	}

	blocked := newRelayTestApp(t)
	blocked.quickTunnel = &fakeQuickTunnelLifecycle{err: errors.New("stop failed")}
	if err := blocked.SetPeerLANConfig(SetPeerLANConfigReq{LANOnly: true}); err == nil {
		t.Fatal("LAN-only policy persisted despite Quick Tunnel stop failure")
	}
	if blocked.cfgStore.Get().PeerLANOnly {
		t.Fatal("failed transition left LAN-only policy enabled")
	}
}

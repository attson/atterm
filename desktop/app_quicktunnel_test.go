package main

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/attson/atterm/internal/quicktunnel"
)

type fakeQuickTunnelLifecycle struct {
	stops atomic.Int32
	err   error
}

type fakeCloudflaredInstaller struct {
	mu       sync.Mutex
	status   quicktunnel.InstallStatus
	installs int
	err      error
}

func (f *fakeCloudflaredInstaller) Status() (quicktunnel.InstallStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, nil
}
func (f *fakeCloudflaredInstaller) ExecutablePath() string        { return "/managed/cloudflared" }
func (f *fakeCloudflaredInstaller) VerifyExecutable(string) error { return nil }
func (f *fakeCloudflaredInstaller) Install(context.Context) (quicktunnel.InstallStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installs++
	if f.err != nil {
		return quicktunnel.InstallStatus{}, f.err
	}
	f.status.Installed = true
	return f.status, nil
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

func TestInstallPeerCloudflaredIsExplicitAndReported(t *testing.T) {
	installer := &fakeCloudflaredInstaller{status: quicktunnel.InstallStatus{
		Supported: true, Version: quicktunnel.ManagedCloudflaredVersion,
		AssetName: "cloudflared-test", SHA256: "pinned-digest",
	}}
	a := &App{ctx: context.Background(), cloudflaredInstaller: installer}

	before := a.GetPeerQuickTunnelStatus()
	if before.ManagedInstalled || !before.ManagedSupported || installer.installs != 0 {
		t.Fatalf("status before explicit install = %+v, installs=%d", before, installer.installs)
	}
	after, err := a.InstallPeerCloudflared()
	if err != nil {
		t.Fatal(err)
	}
	if !after.ManagedInstalled || after.ManagedVersion != quicktunnel.ManagedCloudflaredVersion || installer.installs != 1 {
		t.Fatalf("status after install = %+v, installs=%d", after, installer.installs)
	}
}

func TestInstallPeerCloudflaredRejectsRunningTunnel(t *testing.T) {
	installer := &fakeCloudflaredInstaller{status: quicktunnel.InstallStatus{Supported: true}}
	a := &App{ctx: context.Background(), cloudflaredInstaller: installer}
	a.quickTunnel = &peerQuickTunnelHost{tunnel: &fakePeerQuickTunnelManager{status: quicktunnel.Status{Running: true}}}

	if _, err := a.InstallPeerCloudflared(); err == nil {
		t.Fatal("InstallPeerCloudflared succeeded while Quick Tunnel was running")
	}
	if installer.installs != 0 {
		t.Fatalf("installer calls = %d, want 0", installer.installs)
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

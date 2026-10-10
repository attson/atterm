package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/rendezvous"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/pion/webrtc/v4"
)

func TestPeerRendezvousLifecycleRecoversAfterServiceRestart(t *testing.T) {
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	first, err := rendezvous.New(rendezvous.Config{})
	if err != nil {
		t.Fatal(err)
	}
	var second *rendezvous.Server
	var backend atomic.Pointer[rendezvous.Server]
	backend.Store(first)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		service := backend.Load()
		if service == nil {
			http.Error(w, "restarting", http.StatusServiceUnavailable)
			return
		}
		service.ServeHTTP(w, request)
	}))
	defer proxy.Close()

	ctx, cancel := context.WithCancel(context.Background())
	lifecycle := &peerRendezvousLifecycle{
		app: fixture.app, host: fixture.host,
		resolved: rendezvousclient.ResolvedConfig{
			Mode: rendezvousclient.ModeCustom, BaseURL: proxy.URL,
		},
		ctx: ctx, cancel: cancel, done: make(chan struct{}),
		state: peerRendezvousRuntimeState{State: "connecting"},
		registerHost: func(
			ctx context.Context,
			registrationCtx context.Context,
			app *App,
			host *relayHost,
			presence rendezvousclient.PresenceConfig,
			webRTC webrtc.Configuration,
		) (*peerRendezvousHost, error) {
			presence.AllowInsecureLoopback = true
			return newPeerRendezvousHostWithRegistrationContext(ctx, registrationCtx, app, host, presence, webRTC)
		},
	}
	go lifecycle.run()
	defer func() {
		lifecycle.Stop()
		first.Close()
		if second != nil {
			second.Close()
		}
	}()

	initial := waitPeerRendezvousStatus(t, lifecycle, 5*time.Second, func(status PeerRendezvousStatus) bool {
		return status.State == "online"
	})
	lifecycle.mu.Lock()
	initialHost := lifecycle.active
	lifecycle.mu.Unlock()
	if initialHost == nil || initial.URL != proxy.URL {
		t.Fatalf("initial registration status=%+v host=%p", initial, initialHost)
	}

	backend.Store(nil)
	first.Close()
	wentOffline := waitPeerRendezvousStatus(t, lifecycle, 5*time.Second, func(status PeerRendezvousStatus) bool {
		return status.State == "error"
	})
	if wentOffline.LastErrorCode != "service_unavailable" || wentOffline.NextRetryAt == 0 {
		t.Fatalf("restart failure status=%+v", wentOffline)
	}

	second, err = rendezvous.New(rendezvous.Config{})
	if err != nil {
		t.Fatal(err)
	}
	backend.Store(second)
	recovered := waitPeerRendezvousStatus(t, lifecycle, 5*time.Second, func(status PeerRendezvousStatus) bool {
		return status.State == "online"
	})
	lifecycle.mu.Lock()
	recoveredHost := lifecycle.active
	lifecycle.mu.Unlock()
	if recoveredHost == nil || recoveredHost == initialHost || recovered.LastErrorCode != "" || recovered.NextRetryAt != 0 {
		t.Fatalf("recovered status=%+v host=%p initial=%p", recovered, recoveredHost, initialHost)
	}
	if route, ok := lifecycle.connectionRoute(); !ok || route.URL != proxy.URL || route.Topic == "" {
		t.Fatalf("recovered connection route=%+v ok=%v", route, ok)
	}
}

func waitPeerRendezvousStatus(
	t *testing.T,
	lifecycle *peerRendezvousLifecycle,
	timeout time.Duration,
	accept func(PeerRendezvousStatus) bool,
) PeerRendezvousStatus {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		status := lifecycle.snapshot()
		if accept(status) {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	status := lifecycle.snapshot()
	t.Fatalf("Rendezvous status timeout: %+v", status)
	return PeerRendezvousStatus{}
}

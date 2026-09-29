package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/quicktunnel"
	"github.com/attson/atterm/internal/rendezvousclient"
	"github.com/pion/webrtc/v4"
)

type peerConfigInventorySink struct {
	inventories atomic.Int32
}

func (s *peerConfigInventorySink) SendConfigMessage(_ context.Context, kind peertransport.RecordKind, _ []byte) error {
	if kind == peertransport.RecordConfigInventory {
		s.inventories.Add(1)
	}
	return nil
}

func (*peerConfigInventorySink) RemoteMembershipToken() (string, bool) { return "", false }

func TestPeerRendezvousStatusProjectsLifecycleWithoutOpaqueRoutingData(t *testing.T) {
	app := newRelayTestApp(t)
	if err := app.cfgStore.Set(appConfig{PeerRendezvousMode: "official"}); err != nil {
		t.Fatal(err)
	}
	lifecycle := &peerRendezvousLifecycle{
		resolved: rendezvousclient.ResolvedConfig{
			Mode: rendezvousclient.ModeOfficial, BaseURL: rendezvousclient.OfficialURL,
		},
		state: peerRendezvousRuntimeState{
			State: "online", LastRegisteredAt: 1_797_900_000, RegistrationMS: 37,
		},
		active: &peerRendezvousHost{serviceURL: rendezvousclient.OfficialURL, topic: "opaque-topic"},
	}
	app.peerRendezvous = lifecycle

	got := app.GetPeerRendezvousStatus()
	if got.Mode != "official" || got.State != "online" || got.URL != rendezvousclient.OfficialURL ||
		got.LastRegisteredAt != 1_797_900_000 || got.RegistrationMS != 37 {
		t.Fatalf("status=%+v", got)
	}
}

func TestPeerRendezvousLifecycleReportsRegistrationTimeoutAndRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	lifecycle := &peerRendezvousLifecycle{
		ctx: ctx, cancel: cancel, done: make(chan struct{}), registrationTimeout: 10 * time.Millisecond,
		resolved: rendezvousclient.ResolvedConfig{Mode: rendezvousclient.ModeCustom, BaseURL: "https://rv.example.com"},
		registerHost: func(_ context.Context, registrationCtx context.Context, _ *App, _ *relayHost, _ rendezvousclient.PresenceConfig, _ webrtc.Configuration) (*peerRendezvousHost, error) {
			<-registrationCtx.Done()
			return nil, registrationCtx.Err()
		},
	}
	go lifecycle.run()
	t.Cleanup(lifecycle.Stop)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		status := lifecycle.snapshot()
		if status.State == "error" {
			if status.LastErrorCode != "registration_timeout" || status.NextRetryAt == 0 {
				t.Fatalf("timeout status=%+v", status)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("registration timeout was not reported")
}

func TestClassifyPeerRendezvousErrorUsesStableCodes(t *testing.T) {
	for _, test := range []struct {
		err  error
		want string
	}{
		{context.DeadlineExceeded, "registration_timeout"},
		{rendezvousclient.ErrAuthentication, "authentication_failed"},
		{rendezvousclient.ErrServiceUnavailable, "service_unavailable"},
		{errors.New("dial tcp 203.0.113.1:443"), "registration_failed"},
	} {
		if got := classifyPeerRendezvousError(test.err); got != test.want {
			t.Fatalf("classify(%v)=%q want=%q", test.err, got, test.want)
		}
	}
}

func TestPeerRendezvousConnectingRetainsLastFailureUntilRegistrationSucceeds(t *testing.T) {
	lifecycle := &peerRendezvousLifecycle{
		state: peerRendezvousRuntimeState{
			State: "error", LastErrorCode: "service_unavailable", NextRetryAt: time.Now().Add(time.Second).Unix(),
		},
	}

	lifecycle.setConnecting()
	status := lifecycle.snapshot()
	if status.State != "connecting" || status.LastErrorCode != "service_unavailable" || status.NextRetryAt != 0 {
		t.Fatalf("connecting status=%+v", status)
	}
}

func TestSyncPeerConfigNowUsesOnlyAuthenticatedChannels(t *testing.T) {
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	if _, err := app.SyncPeerConfigNow(); !errors.Is(err, errNoAuthenticatedPeerConfig) {
		t.Fatalf("no-channel sync error=%v", err)
	}

	sink := &peerConfigInventorySink{}
	attempt := &peerHostAttempt{
		config:    &peerConfigChannel{app: app, transport: sink},
		streamCtx: context.Background(),
	}
	host := &peerQuickTunnelHost{attempts: map[*quicktunnel.SignalChannel]*peerHostAttempt{nil: attempt}}
	app.quickTunnel = host

	status, err := app.SyncPeerConfigNow()
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || sink.inventories.Load() != 1 {
		t.Fatalf("sync status=%+v inventories=%d", status, sink.inventories.Load())
	}
}

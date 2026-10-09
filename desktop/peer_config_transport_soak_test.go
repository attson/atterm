package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/quicktunnel"
	"github.com/pion/webrtc/v4"
)

type peerConfigAckDroppingTransport struct {
	channel    *peertransport.PionClientChannel
	dropAck    atomic.Bool
	droppedAck atomic.Int32
}

func (t *peerConfigAckDroppingTransport) RemoteMembershipToken() (string, bool) {
	return t.channel.RemoteMembershipToken()
}

func (t *peerConfigAckDroppingTransport) SendConfigMessage(ctx context.Context, kind peertransport.RecordKind, payload []byte) error {
	if kind == peertransport.RecordConfigAck && t.dropAck.CompareAndSwap(true, false) {
		t.droppedAck.Add(1)
		return nil
	}
	return t.channel.SendConfigMessage(ctx, kind, payload)
}

func TestPeerConfigChannelSoaksLargeSnapshotOverAuthenticatedPionRoute(t *testing.T) {
	isolateConfigDir(t)
	fixture := newPeerQuickTunnelFixture(t, peerproto.PermissionControl)
	largeTheme := strings.Repeat("peer-config-soak-", 24*1024)
	payload, err := json.Marshal(largeTheme)
	if err != nil {
		t.Fatal(err)
	}
	runtime := fixture.app.peerSpace.configReplica
	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, runtime.keys[configsync.KeyClassSync], configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences,
		RecordID:      "terminal_theme",
		Kind:          configsync.KindSet,
		Payload:       payload,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runtime.replica.Compact(runtime.identity); err != nil {
		t.Fatal(err)
	}
	sourceState, err := fixture.app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	destination := newIndependentPeerConfigSyncDestination(t, fixture.app, sourceState)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme"}}

	var firstSubscribers atomic.Int32
	var lastSubscribers atomic.Int32
	fixture.session.SetSubscriberLifecycle(func() { firstSubscribers.Add(1) }, func() { lastSubscribers.Add(1) })

	server := httptest.NewServer(fixture.peerHost.handler)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	errorsCh := make(chan error, 8)
	clientReady := make(chan struct{}, 1)
	var configMu sync.Mutex
	var clientConfig *peerConfigChannel
	var receivedBatches atomic.Int32
	var largestBatch atomic.Int64

	signal, err := quicktunnel.Dial(ctx, quicktunnel.ClientConfig{
		URL: server.URL, AllowInsecure: true, Identity: fixture.clientIdentity,
		GenesisToken: fixture.genesisToken, ClientMembershipToken: fixture.clientMembership,
		HostMembershipToken: fixture.hostMembership, SessionID: peertransport.ConfigSyncSessionID(),
		ClientInstanceID: "peer-config-soak",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer signal.Close()
	attempt, err := signal.BridgePionClient(ctx, quicktunnel.PionClientBridgeConfig{
		WebRTC: webrtc.Configuration{},
		OnAuthenticated: func(channel *peertransport.PionClientChannel) {
			transport := &peerConfigAckDroppingTransport{channel: channel}
			transport.dropAck.Store(true)
			config, configErr := newPeerConfigChannel(destination, transport)
			if configErr != nil {
				errorsCh <- configErr
				return
			}
			configMu.Lock()
			clientConfig = config
			configMu.Unlock()
			if configErr = config.Start(ctx); configErr != nil {
				errorsCh <- configErr
				return
			}
			clientReady <- struct{}{}
		},
		OnRecord: func(peertransport.RecordKind, []byte) {
			select {
			case errorsCh <- errors.New("terminal record received on config-only Peer route"):
			default:
			}
		},
		OnConfigMessage: func(kind peertransport.RecordKind, message []byte) error {
			if kind == peertransport.RecordConfigBatch {
				receivedBatches.Add(1)
				for size := int64(len(message)); ; {
					current := largestBatch.Load()
					if size <= current || largestBatch.CompareAndSwap(current, size) {
						break
					}
				}
			}
			configMu.Lock()
			config := clientConfig
			configMu.Unlock()
			if config == nil {
				return errors.New("Peer config client received a message before authentication")
			}
			return config.Handle(ctx, kind, message)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer attempt.Close()

	waitPeerConfigSoak(t, ctx, errorsCh, func() bool {
		select {
		case <-clientReady:
			return true
		default:
			return false
		}
	}, "Pion config client did not authenticate")

	var hostConfig *peerConfigChannel
	waitPeerConfigSoak(t, ctx, errorsCh, func() bool {
		hostConfig = currentPeerQuickTunnelControlConfig(fixture.peerHost)
		if hostConfig == nil {
			return false
		}
		configMu.Lock()
		config := clientConfig
		configMu.Unlock()
		transport, ok := config.transport.(*peerConfigAckDroppingTransport)
		return ok && transport.droppedAck.Load() == 1 && hostConfig.HasOutstanding()
	}, "dropped durable ACK did not leave a retryable Pion batch")

	if err := hostConfig.RetryOutstanding(ctx); err != nil {
		t.Fatal(err)
	}
	waitPeerConfigSoak(t, ctx, errorsCh, func() bool {
		return destination.cfgStore.Get().TerminalTheme == largeTheme && !hostConfig.HasOutstanding()
	}, "large Peer config snapshot did not converge after retry")

	if receivedBatches.Load() < 3 {
		t.Fatalf("Pion config transfer used %d batch deliveries, want retry plus multiple pages", receivedBatches.Load())
	}
	if largestBatch.Load() <= peertransport.MaxRecordPlaintext {
		t.Fatalf("largest Pion config batch=%d, want fragmented message larger than %d", largestBatch.Load(), peertransport.MaxRecordPlaintext)
	}
	if firstSubscribers.Load() != 0 || lastSubscribers.Load() != 0 || fixture.session.SubscriberCount() != 0 {
		t.Fatalf("config-only Pion route touched terminal subscribers: first=%d last=%d active=%d",
			firstSubscribers.Load(), lastSubscribers.Load(), fixture.session.SubscriberCount())
	}
}

func currentPeerQuickTunnelControlConfig(host *peerQuickTunnelHost) *peerConfigChannel {
	host.mu.Lock()
	defer host.mu.Unlock()
	for _, control := range host.controls {
		control.mu.Lock()
		config := control.config
		control.mu.Unlock()
		if config != nil {
			return config
		}
	}
	return nil
}

func waitPeerConfigSoak(t *testing.T, ctx context.Context, errorsCh <-chan error, condition func() bool, message string) {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if condition() {
			return
		}
		select {
		case err := <-errorsCh:
			t.Fatal(err)
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal(message)
		}
	}
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
)

type peerConfigLoopback struct {
	remoteMembership string
	peer             *peerConfigChannel
	dropAck          atomic.Int32
	batches          atomic.Int32
}

func (l *peerConfigLoopback) RemoteMembershipToken() (string, bool) {
	return l.remoteMembership, l.remoteMembership != ""
}

func (l *peerConfigLoopback) SendConfigMessage(ctx context.Context, kind peertransport.RecordKind, payload []byte) error {
	if kind == peertransport.RecordConfigBatch {
		l.batches.Add(1)
	}
	if kind == peertransport.RecordConfigAck && l.dropAck.CompareAndSwap(1, 0) {
		return nil
	}
	if l.peer == nil {
		return errors.New("loopback peer is not connected")
	}
	return l.peer.Handle(ctx, kind, append([]byte(nil), payload...))
}

func TestPeerConfigChannelRetriesDurableMultiBatchSyncWithoutSubscriber(t *testing.T) {
	isolateConfigDir(t)
	source, _ := newTestPeerApp(t)
	largeTheme := strings.Repeat("sync-value-", 4096)
	source.cfgStore = &configStore{cfg: appConfig{TerminalTheme: largeTheme}}
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := source.peerSpace.configReplica.replica.Compact(source.peerSpace.configReplica.identity); err != nil {
		t.Fatal(err)
	}
	sourceState, err := source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}

	destination := newIndependentPeerConfigSyncDestination(t, source, sourceState)
	destination.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "local-theme"}}

	model := session.New(uuid.New(), proto.SessionInfo{})
	defer model.Close()
	var firstSubscribers atomic.Int32
	var lastSubscribers atomic.Int32
	model.SetSubscriberLifecycle(func() { firstSubscribers.Add(1) }, func() { lastSubscribers.Add(1) })

	sourceTransport := &peerConfigLoopback{remoteMembership: sourceState.LocalMembership}
	destinationTransport := &peerConfigLoopback{remoteMembership: sourceState.LocalMembership}
	sourceChannel, err := newPeerConfigChannel(source, sourceTransport)
	if err != nil {
		t.Fatal(err)
	}
	destinationChannel, err := newPeerConfigChannel(destination, destinationTransport)
	if err != nil {
		t.Fatal(err)
	}
	sourceChannel.maxBatchBytes = configsync.MinAntiEntropyBatchSize
	sourceTransport.peer = destinationChannel
	destinationTransport.peer = sourceChannel
	destinationTransport.dropAck.Store(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := destinationChannel.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !sourceChannel.HasOutstanding() {
		t.Fatal("dropped durable ack did not leave a retryable batch")
	}
	if err := sourceChannel.RetryOutstanding(ctx); err != nil {
		t.Fatal(err)
	}
	if sourceChannel.HasOutstanding() {
		t.Fatal("completed config plan still has an outstanding batch")
	}
	if sourceTransport.batches.Load() < 3 {
		t.Fatalf("config transfer used %d batch sends, want retry plus multiple pages", sourceTransport.batches.Load())
	}
	if got := destination.cfgStore.Get().TerminalTheme; got != largeTheme {
		t.Fatalf("projected terminal theme length=%d, want=%d", len(got), len(largeTheme))
	}
	syncKey := source.peerSpace.configReplica.keys[configsync.KeyClassSync]
	if _, _, err := source.peerSpace.configReplica.replica.AppendEncrypted(source.peerSpace.configReplica.identity, syncKey, configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences,
		RecordID:      "terminal_font_size",
		Kind:          configsync.KindSet,
		Payload:       json.RawMessage(`19`),
	}); err != nil {
		t.Fatal(err)
	}
	if err := destinationChannel.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if got := destination.cfgStore.Get().TerminalFontSize; got != 19 {
		t.Fatalf("second config round projected font size=%d", got)
	}
	remotePeerID := source.peerSpace.configReplica.identity.PeerID()
	sourcePersisted, err := source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	sourceExchange, ok := sourcePersisted.ConfigSyncPeers[remotePeerID]
	if !ok || sourceExchange.LastExchangeAt != source.peerSpace.now().Unix() {
		t.Fatalf("source config exchange=%+v present=%v", sourceExchange, ok)
	}
	for actor, counter := range source.peerSpace.configReplica.replica.Vector() {
		if sourceExchange.Acknowledged[actor] != counter {
			t.Fatalf("source acknowledgement actor=%q got=%d want=%d", actor, sourceExchange.Acknowledged[actor], counter)
		}
	}
	destinationPersisted, err := destination.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	destinationExchange, ok := destinationPersisted.ConfigSyncPeers[remotePeerID]
	if !ok || destinationExchange.LastExchangeAt != destination.peerSpace.now().Unix() {
		t.Fatalf("destination config exchange=%+v present=%v", destinationExchange, ok)
	}
	if firstSubscribers.Load() != 0 || lastSubscribers.Load() != 0 || model.SubscriberCount() != 0 {
		t.Fatalf("config sync touched terminal subscribers: first=%d last=%d active=%d", firstSubscribers.Load(), lastSubscribers.Load(), model.SubscriberCount())
	}
}

func TestPeerConfigChannelRequiresHandshakeMembership(t *testing.T) {
	app, _ := newTestPeerApp(t)
	transport := &peerConfigLoopback{}
	if _, err := newPeerConfigChannel(app, transport); !errors.Is(err, errPeerConfigSyncDenied) {
		t.Fatalf("missing handshake membership error=%v", err)
	}
}

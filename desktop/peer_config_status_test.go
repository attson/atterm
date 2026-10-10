package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

func TestPeerConfigSyncStatusTracksDurableRemoteAcknowledgement(t *testing.T) {
	app, now := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	space, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	runtime := app.peerSpace.configReplica
	if _, _, err := runtime.replica.AppendEncrypted(runtime.identity, runtime.keys[configsync.KeyClassSync], configsync.Mutation{
		SchemaVersion: configsync.SchemaVersion,
		Collection:    configsync.CollectionPreferences,
		RecordID:      "terminal_font_size",
		Kind:          configsync.KindSet,
		Payload:       json.RawMessage(`17`),
	}); err != nil {
		t.Fatal(err)
	}

	before, err := app.GetPeerConfigSyncStatus()
	if err != nil {
		t.Fatal(err)
	}
	if !before.Configured || before.LocalOperations == 0 || before.PendingOperations != before.LocalOperations || before.ActiveRemoteMembers != 0 {
		t.Fatalf("initial sync status=%+v", before)
	}

	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, _ := peerproto.VerifyGenesis(state.GenesisToken)
	creatorMembership, _ := peerproto.VerifyGrant(state.LocalMembership, genesis, now)
	remote, _ := peercrypto.GenerateIdentity()
	remoteWrapping, _ := peercrypto.GenerateWrappingIdentity()
	remoteMembership := issueTestPeerMembership(t, runtime.identity, genesis, creatorMembership, remote, remoteWrapping, now)
	if _, err := app.peerSpace.store.ApplyMemberships([]string{remoteMembership.Token}, now); err != nil {
		t.Fatal(err)
	}
	exchangedAt := now.Add(time.Minute)
	if err := app.peerSpace.store.RecordConfigExchange(remote.PeerID(), runtime.replica.Vector(), exchangedAt); err != nil {
		t.Fatal(err)
	}

	after, err := app.GetPeerConfigSyncStatus()
	if err != nil {
		t.Fatal(err)
	}
	if after.PendingOperations != 0 || after.ActiveRemoteMembers != 1 || after.AcknowledgingPeers != 1 || after.LastExchangeAt != exchangedAt.Unix() {
		t.Fatalf("acknowledged sync status=%+v space=%s", after, space.SpaceID)
	}
}

func TestPeerConfigSyncStatusAndActionsExposePendingImportWithoutValues(t *testing.T) {
	app, _ := newTestPeerApp(t)
	app.cfgStore = &configStore{cfg: appConfig{TerminalTheme: "nord"}}
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	pendingConfig := appConfig{TerminalTheme: "local-secret-theme"}
	count, _, err := app.peerSpace.capturePendingPeerConfig(pendingConfig)
	if err != nil {
		t.Fatal(err)
	}
	status, err := app.GetPeerConfigSyncStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.PendingImportRecords != count || status.PendingImportCapturedAt == 0 {
		t.Fatalf("pending status=%+v", status)
	}
	publicJSON, _ := json.Marshal(status)
	if string(publicJSON) == "" || strings.Contains(string(publicJSON), "local-secret-theme") {
		t.Fatalf("status exposed pending config value: %s", publicJSON)
	}

	merged, err := app.AcceptPendingPeerConfig()
	if err != nil {
		t.Fatal(err)
	}
	if merged.PendingImportRecords != 0 || app.cfgStore.Get().TerminalTheme != "local-secret-theme" {
		t.Fatalf("accepted status=%+v config=%+v", merged, app.cfgStore.Get())
	}
	if discarded, err := app.DiscardPendingPeerConfig(); err != nil || discarded.PendingImportRecords != 0 {
		t.Fatalf("idempotent discard status=%+v err=%v", discarded, err)
	}
}

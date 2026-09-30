package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
	"github.com/attson/atterm/internal/quicktunnel"
	"github.com/attson/atterm/internal/safekeyring"
)

type peerJoinFixture struct {
	bundle      string
	bootstrap   quicktunnel.JoinBootstrap
	identity    *peercrypto.Identity
	wrapping    *peercrypto.WrappingIdentity
	now         time.Time
	fingerprint string
	destination *App
	storePath   string
	keyringPath string
}

const peerJoinSessionID = "00000000-0000-4000-8000-000000000001"

func newPeerJoinFixture(t *testing.T) peerJoinFixture {
	return newPeerJoinFixtureWithSessionScope(t, []string{peerJoinSessionID})
}

func newPeerJoinFixtureWithSessionScope(t *testing.T, allowedSessionIDs []string) peerJoinFixture {
	t.Helper()
	source, now := newTestPeerApp(t)
	if _, err := source.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	invitations, err := source.CreatePeerInvitations(CreatePeerInvitationsReq{
		Count: 1, Permission: string(peerproto.PermissionControl),
		AllowedSessionIDs: allowedSessionIDs, CanSyncSecrets: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	identity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	joinRequest, err := peerproto.NewJoinRequest(identity, wrapping.PublicBytes(), invitations[0].Token, now)
	if err != nil {
		t.Fatal(err)
	}
	result, err := source.peerSpace.redeemJoinRequest(joinRequest)
	if err != nil {
		t.Fatal(err)
	}
	state, err := source.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	sourceIdentity, err := source.peerSpace.loadIdentity()
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := peerproto.NewConnectionBundle(sourceIdentity, genesis, invitations[0].Token, []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://peer-join.trycloudflare.com",
	}}, now, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	destinationRoot := t.TempDir()
	keyringPath := filepath.Join(destinationRoot, "keyring")
	safekeyring.SetFileDirForTest(keyringPath)
	safekeyring.UseFileStore()
	if err := peerIdentitySlot().Save(identity.PrivateBytes()); err != nil {
		t.Fatal(err)
	}
	if err := peerWrappingIdentitySlot().Save(wrapping.PrivateBytes()); err != nil {
		t.Fatal(err)
	}
	storePath := filepath.Join(destinationRoot, "peer-space.json")
	manager := &peerSpaceManager{
		now: func() time.Time { return now }, bootstrapLockPath: storePath + ".bootstrap.lock",
		configRoot: filepath.Join(destinationRoot, "peer-spaces"),
	}
	manager.store = peerstore.New(storePath, func() ([]byte, error) {
		key, err := peerStoreKeySlot().Load()
		if err != nil {
			return nil, err
		}
		if len(key) == 0 {
			return nil, errors.New("missing test peer store key")
		}
		return key, nil
	})
	bootstrap := quicktunnel.JoinBootstrap{
		GenesisToken: result.GenesisToken, MembershipToken: result.MembershipToken,
		Memberships: result.Memberships, Revocations: result.Revocations,
		EpochRotations: result.EpochRotations, EpochEnvelopes: result.EpochEnvelopes,
	}
	manager.joinQuickTunnel = func(context.Context, quicktunnel.JoinClientConfig) (quicktunnel.JoinBootstrap, error) {
		return bootstrap, nil
	}
	return peerJoinFixture{
		bundle: bundle, bootstrap: bootstrap, identity: identity, wrapping: wrapping, now: now,
		fingerprint: "SHA256:" + genesis.Hash, destination: &App{ctx: context.Background(), peerSpace: manager},
		storePath: storePath, keyringPath: keyringPath,
	}
}

func TestPreviewPeerConnectionBundleNormalizesTokenAndFragment(t *testing.T) {
	fixture := newPeerJoinFixture(t)
	raw, err := fixture.destination.PreviewPeerConnectionBundle("  " + fixture.bundle + "\n")
	if err != nil {
		t.Fatal(err)
	}
	deepLink, err := fixture.destination.PreviewPeerConnectionBundle("atterm://peer/join#" + fixture.bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw, deepLink) {
		t.Fatalf("raw preview=%+v deep-link preview=%+v", raw, deepLink)
	}
	if raw.Fingerprint != fixture.fingerprint || raw.Permission != "control" || !reflect.DeepEqual(raw.AllowedSessionIDs, []string{peerJoinSessionID}) || raw.RouteKind != "quick_tunnel" {
		t.Fatalf("preview=%+v", raw)
	}
	if _, err := fixture.destination.PreviewPeerConnectionBundle("atterm://peer/join?bundle=" + fixture.bundle); err == nil {
		t.Fatal("query-carried invitation was accepted")
	}
}

func TestPreviewPeerConnectionBundleEncodesEmptySessionScopeAsArray(t *testing.T) {
	fixture := newPeerJoinFixtureWithSessionScope(t, nil)
	preview, err := fixture.destination.PreviewPeerConnectionBundle(fixture.bundle)
	if err != nil {
		t.Fatal(err)
	}
	if preview.AllowedSessionIDs == nil {
		t.Fatal("preview allowed_session_ids is nil; Wails would serialize it as null")
	}
	encoded, err := json.Marshal(preview)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"allowed_session_ids":[]`)) {
		t.Fatalf("preview must encode an empty session scope as an array: %s", encoded)
	}
}

func TestJoinPeerSpaceRequiresConfirmedFingerprintBeforeNetwork(t *testing.T) {
	fixture := newPeerJoinFixture(t)
	calls := 0
	fixture.destination.peerSpace.joinQuickTunnel = func(context.Context, quicktunnel.JoinClientConfig) (quicktunnel.JoinBootstrap, error) {
		calls++
		return fixture.bootstrap, nil
	}
	if _, err := fixture.destination.JoinPeerSpace(JoinPeerSpaceReq{
		ConnectionBundle: fixture.bundle, ExpectedFingerprint: "SHA256:not-confirmed",
	}); err == nil {
		t.Fatal("mismatched fingerprint was accepted")
	}
	if calls != 0 {
		t.Fatalf("join network calls=%d want=0", calls)
	}
	if _, err := os.Stat(fixture.storePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Peer store exists after rejected confirmation: %v", err)
	}
}

func TestJoinPeerSpaceRejectsMissingEpochEnvelopeWithoutStore(t *testing.T) {
	fixture := newPeerJoinFixture(t)
	fixture.destination.peerSpace.joinQuickTunnel = func(context.Context, quicktunnel.JoinClientConfig) (quicktunnel.JoinBootstrap, error) {
		bootstrap := fixture.bootstrap
		bootstrap.EpochEnvelopes = bootstrap.EpochEnvelopes[:1]
		return bootstrap, nil
	}
	if _, err := fixture.destination.JoinPeerSpace(JoinPeerSpaceReq{
		ConnectionBundle: fixture.bundle, ExpectedFingerprint: fixture.fingerprint,
	}); err == nil {
		t.Fatal("bootstrap with a missing epoch envelope was accepted")
	}
	if _, err := os.Stat(fixture.storePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Peer store exists after failed bootstrap: %v", err)
	}
}

func TestJoinPeerSpacePersistsBootstrapAndRecoversEpochKeys(t *testing.T) {
	fixture := newPeerJoinFixture(t)
	status, err := fixture.destination.JoinPeerSpace(JoinPeerSpaceReq{
		ConnectionBundle: fixture.bundle, ExpectedFingerprint: fixture.fingerprint,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !status.Configured || status.PeerID != fixture.identity.PeerID() {
		t.Fatalf("status=%+v", status)
	}
	state, err := fixture.destination.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(state.EpochEnvelopes, fixture.bootstrap.EpochEnvelopes) {
		t.Fatal("bootstrap epoch envelopes were not persisted")
	}
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		if err := peerEpochKeySlot(status.SpaceID, class).Clear(); err != nil {
			t.Fatal(err)
		}
	}

	reopened := &peerSpaceManager{
		now: func() time.Time { return fixture.now }, bootstrapLockPath: fixture.storePath + ".bootstrap.lock",
		configRoot: fixture.destination.peerSpace.configRoot,
	}
	reopened.store = peerstore.New(fixture.storePath, func() ([]byte, error) { return peerStoreKeySlot().Load() })
	reopenedStatus, err := (&App{ctx: context.Background(), peerSpace: reopened}).GetPeerSpaceStatus()
	if err != nil {
		t.Fatal(err)
	}
	if reopenedStatus.SpaceID != status.SpaceID || reopenedStatus.PeerID != status.PeerID {
		t.Fatalf("reopened status=%+v want=%+v", reopenedStatus, status)
	}
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		if _, err := loadPeerEpochKey(status.SpaceID, class); err != nil {
			t.Fatalf("recover %s epoch key: %v", class, err)
		}
	}
}

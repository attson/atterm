package main

import (
	"bytes"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peerstore"
	"github.com/attson/atterm/internal/safekeyring"
)

func newTestPeerApp(t *testing.T) (*App, time.Time) {
	t.Helper()
	dir := t.TempDir()
	safekeyring.SetFileDirForTest(filepath.Join(dir, "keyring"))
	safekeyring.UseFileStore()
	t.Cleanup(func() {
		safekeyring.Reset()
		safekeyring.SetFileDirForTest("")
	})
	now := time.Unix(1_800_000_000, 0)
	storePath := filepath.Join(dir, "peer-space.json")
	manager := &peerSpaceManager{now: func() time.Time { return now }, bootstrapLockPath: storePath + ".bootstrap.lock"}
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
	return &App{peerSpace: manager}, now
}

func TestConcurrentPeerSpaceBootstrapKeepsOneIdentity(t *testing.T) {
	app, now := newTestPeerApp(t)
	otherManager := &peerSpaceManager{
		now:               func() time.Time { return now },
		bootstrapLockPath: app.peerSpace.bootstrapLockPath,
	}
	storePath := strings.TrimSuffix(app.peerSpace.bootstrapLockPath, ".bootstrap.lock")
	otherManager.store = peerstore.New(storePath, func() ([]byte, error) {
		key, err := peerStoreKeySlot().Load()
		if err != nil {
			return nil, err
		}
		if len(key) == 0 {
			return nil, errors.New("missing test peer store key")
		}
		return key, nil
	})
	other := &App{peerSpace: otherManager}

	statuses := make([]PeerSpaceStatus, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for index, candidate := range []*App{app, other} {
		wg.Add(1)
		go func(index int, candidate *App) {
			defer wg.Done()
			statuses[index], errs[index] = candidate.CreatePeerSpace()
		}(index, candidate)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if statuses[0].PeerID != statuses[1].PeerID || statuses[0].SpaceID != statuses[1].SpaceID {
		t.Fatalf("concurrent bootstrap diverged: %+v vs %+v", statuses[0], statuses[1])
	}
}

func TestCreatePeerSpaceIsIdempotent(t *testing.T) {
	app, _ := newTestPeerApp(t)
	first, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Configured || first.PeerID == "" || first.SpaceID == "" || first.GenesisHash == "" {
		t.Fatalf("incomplete status: %+v", first)
	}
	firstWrapping, err := peerWrappingIdentitySlot().Load()
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	if second.PeerID != first.PeerID || second.SpaceID != first.SpaceID || second.GenesisHash != first.GenesisHash {
		t.Fatalf("CreatePeerSpace rotated identity: first=%+v second=%+v", first, second)
	}
	secondWrapping, err := peerWrappingIdentitySlot().Load()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstWrapping, secondWrapping) {
		t.Fatal("CreatePeerSpace rotated wrapping identity")
	}
}

func TestCreatePeerSpacePersistsAndRecoversInitialEpochKeys(t *testing.T) {
	app, _ := newTestPeerApp(t)
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.EpochRotations) != 2 {
		t.Fatalf("epoch rotations=%d want=2", len(state.EpochRotations))
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	rotations, err := currentEpochRotations(state.EpochRotations, genesis)
	if err != nil {
		t.Fatal(err)
	}
	wrapping, err := app.peerSpace.loadWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		stored, err := loadPeerEpochKey(status.SpaceID, class)
		if err != nil {
			t.Fatal(err)
		}
		opened, err := configsync.OpenRotationEpochKey(rotations[class], genesis, status.PeerID, wrapping)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Epoch != 1 || !bytes.Equal(stored.Bytes(), opened.Bytes()) {
			t.Fatalf("stored %s epoch key differs from rotation", class)
		}
		if err := peerEpochKeySlot(status.SpaceID, class).Clear(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := app.GetPeerSpaceStatus(); err != nil {
		t.Fatalf("recover epoch keys: %v", err)
	}
	recoveredState, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(recoveredState.EpochRotations, state.EpochRotations) {
		t.Fatal("epoch key recovery replaced signed rotations")
	}
	for _, class := range []configsync.KeyClass{configsync.KeyClassSync, configsync.KeyClassVault} {
		if _, err := loadPeerEpochKey(status.SpaceID, class); err != nil {
			t.Fatalf("recovered %s key: %v", class, err)
		}
	}
}

func TestCreateAndRevokePeerInvitationBatch(t *testing.T) {
	app, now := newTestPeerApp(t)
	status, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	invitations, err := app.CreatePeerInvitations(CreatePeerInvitationsReq{
		Count: 3, ValidForHours: 24, Permission: "control",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(invitations) != 3 {
		t.Fatalf("invitations = %d", len(invitations))
	}
	if invitations[0].BatchID == "" || invitations[1].BatchID != invitations[0].BatchID {
		t.Fatal("invitation batch ids differ")
	}
	state, err := app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(state.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	doc, _, err := peerproto.VerifyInvitation(invitations[0].Token, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	if doc.RedemptionPeerID != status.PeerID || doc.Permission != peerproto.PermissionControl {
		t.Fatalf("unexpected ticket: %+v", doc)
	}
	if err := app.RevokePeerInvitationBatch(invitations[0].BatchID); err != nil {
		t.Fatal(err)
	}
	state, err = app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Revocations) != 1 {
		t.Fatalf("signed revocations=%d want=1", len(state.Revocations))
	}
	revocation, err := peerproto.VerifyRevocation(state.Revocations[0], genesis)
	if err != nil {
		t.Fatal(err)
	}
	if revocation.Document.Kind != peerproto.RevocationInvitationBatch || revocation.Document.TargetID != invitations[0].BatchID {
		t.Fatalf("unexpected batch revocation: %+v", revocation.Document)
	}
	if err := app.RevokePeerInvitationBatch(invitations[0].BatchID); err != nil {
		t.Fatal(err)
	}
	state, err = app.peerSpace.store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Revocations) != 1 {
		t.Fatalf("idempotent batch revoke stored %d tokens", len(state.Revocations))
	}
	status, err = app.GetPeerSpaceStatus()
	if err != nil {
		t.Fatal(err)
	}
	if status.RevokedInvites != 3 || status.OpenInvitations != 0 {
		t.Fatalf("status after revoke: %+v", status)
	}
	listed, err := app.ListPeerInvitations()
	if err != nil {
		t.Fatal(err)
	}
	for _, invite := range listed {
		if invite.Token != "" {
			t.Fatal("revoked invitation still exposes its pairing secret")
		}
	}
	if err := app.RevokePeerInvitationBatch("db8f16c4-8bf5-47b6-8672-b5b75a70d82d"); !errors.Is(err, peerstore.ErrInviteInvalid) {
		t.Fatalf("unknown batch error=%v", err)
	}
}

func TestPeerInvitationsRequireSpace(t *testing.T) {
	app, _ := newTestPeerApp(t)
	if _, err := app.CreatePeerInvitations(CreatePeerInvitationsReq{Count: 1}); !errors.Is(err, peerstore.ErrNotInitialized) {
		t.Fatalf("CreatePeerInvitations without space error = %v", err)
	}
}

func TestRedeemPeerJoinRequestIsIdempotentAndRejectsReplay(t *testing.T) {
	app, now := newTestPeerApp(t)
	if _, err := app.CreatePeerSpace(); err != nil {
		t.Fatal(err)
	}
	invitations, err := app.CreatePeerInvitations(CreatePeerInvitationsReq{Count: 1, Permission: "control"})
	if err != nil {
		t.Fatal(err)
	}
	joiningIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	joiningWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	request, err := peerproto.NewJoinRequest(joiningIdentity, joiningWrapping.PublicBytes(), invitations[0].Token, now)
	if err != nil {
		t.Fatal(err)
	}
	first, err := app.peerSpace.redeemJoinRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := app.peerSpace.redeemJoinRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if first.GenesisToken == "" || first.MembershipToken == "" || second != first {
		t.Fatalf("idempotent join results differ: first=%+v second=%+v", first, second)
	}
	genesis, err := peerproto.VerifyGenesis(first.GenesisToken)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := peerproto.VerifyGrant(first.MembershipToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	if membership.Document.SubjectPeerID != joiningIdentity.PeerID() || membership.Document.Permission != peerproto.PermissionControl {
		t.Fatalf("unexpected joined membership: %+v", membership.Document)
	}
	if !bytes.Equal(membership.WrappingPublicKey, joiningWrapping.PublicBytes()) {
		t.Fatal("joined membership did not retain wrapping public key")
	}

	replayIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	replayWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	replay, err := peerproto.NewJoinRequest(replayIdentity, replayWrapping.PublicBytes(), invitations[0].Token, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.peerSpace.redeemJoinRequest(replay); !errors.Is(err, peerstore.ErrInviteConsumed) {
		t.Fatalf("cross-device replay error = %v", err)
	}
}

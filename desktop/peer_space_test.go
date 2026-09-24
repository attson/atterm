package main

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

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
	second, err := app.CreatePeerSpace()
	if err != nil {
		t.Fatal(err)
	}
	if second.PeerID != first.PeerID || second.SpaceID != first.SpaceID || second.GenesisHash != first.GenesisHash {
		t.Fatalf("CreatePeerSpace rotated identity: first=%+v second=%+v", first, second)
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
}

func TestPeerInvitationsRequireSpace(t *testing.T) {
	app, _ := newTestPeerApp(t)
	if _, err := app.CreatePeerInvitations(CreatePeerInvitationsReq{Count: 1}); !errors.Is(err, peerstore.ErrNotInitialized) {
		t.Fatalf("CreatePeerInvitations without space error = %v", err)
	}
}

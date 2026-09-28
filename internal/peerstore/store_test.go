package peerstore

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

func testStore(t *testing.T) (*Store, []byte) {
	t.Helper()
	key := bytes.Repeat([]byte{0x42}, 32)
	path := filepath.Join(t.TempDir(), "peer-space.json")
	return New(path, func() ([]byte, error) { return append([]byte(nil), key...), nil }), key
}

func TestEncryptedStoreAndAtomicConsumption(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "genesis-secret", LocalMembership: "membership-secret", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	invitation := Invitation{InviteID: "invite-1", BatchID: "batch-1", Token: "ticket-secret", ExpiresAt: now.Add(time.Hour).Unix()}
	if err := store.AddInvitations([]Invitation{invitation}, now); err != nil {
		t.Fatal(err)
	}
	blob, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, plaintext := range [][]byte{[]byte("genesis-secret"), []byte("membership-secret"), []byte("ticket-secret")} {
		if bytes.Contains(blob, plaintext) {
			t.Fatalf("encrypted store contains plaintext %q", plaintext)
		}
	}
	if _, err := store.RedeemInvitation("invite-1", "consumer-peer", "issued-membership", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RedeemInvitation("invite-1", "other-peer", "replacement-membership", now.Add(2*time.Minute)); !errors.Is(err, ErrInviteConsumed) {
		t.Fatalf("second consume error = %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Invitations[0].ConsumedByPeerID != "consumer-peer" {
		t.Fatalf("consumed by = %q", state.Invitations[0].ConsumedByPeerID)
	}
}

func TestInitializeEpochRotationsIsWriteOnceAndDetached(t *testing.T) {
	store, key := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "genesis", LocalMembership: "membership", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeEpochRotations([]string{"sync-rotation", "vault-rotation"}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.InitializeEpochRotations([]string{"replacement"}, now.Add(time.Minute)); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("replacement error=%v", err)
	}
	reopened := New(store.path, func() ([]byte, error) { return append([]byte(nil), key...), nil })
	state, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != Version || !reflect.DeepEqual(state.EpochRotations, []string{"sync-rotation", "vault-rotation"}) {
		t.Fatalf("state=%+v", state)
	}
	state.EpochRotations[0] = "caller-mutation"
	again, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if again.EpochRotations[0] != "sync-rotation" {
		t.Fatalf("Load returned aliased rotations: %+v", again.EpochRotations)
	}
}

func TestPendingConfigImportIsEncryptedWriteOnceAndDetached(t *testing.T) {
	store, key := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "genesis", LocalMembership: "membership", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"version":1,"value":"local-sensitive-config"}`)
	if err := store.SavePendingConfigImport(payload, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.SavePendingConfigImport(payload, now.Add(2*time.Minute)); err != nil {
		t.Fatalf("idempotent save error=%v", err)
	}
	if err := store.SavePendingConfigImport([]byte(`{"version":1,"value":"replacement"}`), now.Add(3*time.Minute)); !errors.Is(err, ErrPendingExists) {
		t.Fatalf("replacement error=%v", err)
	}
	blob, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, payload) || bytes.Contains(blob, []byte("local-sensitive-config")) {
		t.Fatal("pending config import was stored in plaintext")
	}

	reopened := New(store.path, func() ([]byte, error) { return append([]byte(nil), key...), nil })
	state, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(state.PendingConfigImport, payload) || state.Version != Version {
		t.Fatalf("pending config state=%+v", state)
	}
	state.PendingConfigImport[0] ^= 1
	again, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again.PendingConfigImport, payload) {
		t.Fatal("Load returned aliased pending config payload")
	}
	if err := reopened.ClearPendingConfigImport(now.Add(4 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	cleared, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.PendingConfigImport) != 0 {
		t.Fatalf("pending config import not cleared: %q", cleared.PendingConfigImport)
	}
}

func TestApplyEpochRotationsIsAtomicAndIdempotent(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "genesis", LocalMembership: "membership", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	allow := func(State, []string) error { return nil }
	if stored, err := store.ApplyEpochRotations([]string{"rotation-a", "rotation-b", "rotation-a"}, now.Add(time.Minute), allow); err != nil || stored != 2 {
		t.Fatalf("first apply stored=%d err=%v", stored, err)
	}
	if stored, err := store.ApplyEpochRotations([]string{"rotation-b"}, now.Add(2*time.Minute), allow); err != nil || stored != 0 {
		t.Fatalf("duplicate apply stored=%d err=%v", stored, err)
	}
	before, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if stored, err := store.ApplyEpochRotations([]string{"rotation-c", ""}, now.Add(3*time.Minute), allow); err == nil || stored != 0 {
		t.Fatalf("invalid apply stored=%d err=%v", stored, err)
	}
	after, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(after.EpochRotations, before.EpochRotations) {
		t.Fatalf("failed apply changed rotations: before=%v after=%v", before.EpochRotations, after.EpochRotations)
	}
	denied := errors.New("rotation denied")
	if stored, err := store.ApplyEpochRotations([]string{"rotation-c"}, now.Add(4*time.Minute), func(State, []string) error { return denied }); !errors.Is(err, denied) || stored != 0 {
		t.Fatalf("denied apply stored=%d err=%v", stored, err)
	}
	afterDenied, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(afterDenied.EpochRotations, before.EpochRotations) {
		t.Fatalf("denied apply changed rotations: before=%v after=%v", before.EpochRotations, afterDenied.EpochRotations)
	}
}

func TestRevocationIsIdempotentAndDenyWins(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "g", LocalMembership: "m", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	invite := Invitation{InviteID: "i", BatchID: "b", Token: "t", ExpiresAt: now.Add(time.Hour).Unix()}
	if err := store.AddInvitations([]Invitation{invite}, now); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeBatch("b", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := store.RevokeBatch("b", now.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RedeemInvitation("i", "peer", "membership", now.Add(3*time.Minute)); !errors.Is(err, ErrInviteRevoked) {
		t.Fatalf("consume revoked error = %v", err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := state.Invitations[0].RevokedAt, now.Add(time.Minute).Unix(); got != want {
		t.Fatalf("RevokedAt = %d, want first timestamp %d", got, want)
	}
}

func TestWrongKeyAndTamperFailClosed(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "g", LocalMembership: "m", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	wrong := New(store.path, func() ([]byte, error) { return bytes.Repeat([]byte{0x99}, 32), nil })
	if _, err := wrong.Load(); err == nil {
		t.Fatal("wrong key decrypted store")
	}
	blob, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	blob[len(blob)-3] ^= 1
	if err := os.WriteFile(store.path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(); err == nil {
		t.Fatal("tampered store loaded")
	}
}

func TestMissingStore(t *testing.T) {
	store, _ := testStore(t)
	if _, err := store.Load(); !errors.Is(err, ErrNotInitialized) {
		t.Fatalf("Load missing error = %v", err)
	}
}

func TestLegacyV1StateMigratesWithoutChangingEnvelopeAAD(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	legacy, err := json.Marshal(State{
		Version: legacyStateVersion, GenesisToken: "g", LocalMembership: "m",
		Invitations: []Invitation{}, CreatedAt: now.Unix(), UpdatedAt: now.Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	aead, err := store.aead()
	if err != nil {
		t.Fatal(err)
	}
	nonce := bytes.Repeat([]byte{0x23}, aead.NonceSize())
	wrapped, err := json.Marshal(envelope{
		Version: encryptedEnvelopeV1, Nonce: base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(aead.Seal(nil, nonce, legacy, storeAAD)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.path, wrapped, 0o600); err != nil {
		t.Fatal(err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != Version || state.GenesisToken != "g" || state.LocalMembership != "m" {
		t.Fatalf("migrated state=%+v", state)
	}
	if err := store.AddInvitations([]Invitation{{InviteID: "i", BatchID: "b", Token: "t", ExpiresAt: now.Add(time.Hour).Unix()}}, now); err != nil {
		t.Fatal(err)
	}
	state, err = store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Version != Version || len(state.Invitations) != 1 {
		t.Fatalf("persisted migrated state=%+v", state)
	}
}

func TestTwoStoreInstancesCannotRedeemOneInvitationForDifferentPeers(t *testing.T) {
	store, key := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "g", LocalMembership: "m", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddInvitations([]Invitation{{InviteID: "i", BatchID: "b", Token: "t", ExpiresAt: now.Add(time.Hour).Unix()}}, now); err != nil {
		t.Fatal(err)
	}
	other := New(store.path, func() ([]byte, error) { return append([]byte(nil), key...), nil })
	stores := []*Store{store, other}
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for index := range stores {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errs[index] = stores[index].RedeemInvitation(
				"i",
				"peer-"+string(rune('a'+index)),
				"membership-"+string(rune('a'+index)),
				now.Add(time.Minute),
			)
		}(index)
	}
	wg.Wait()
	successes := 0
	consumed := 0
	for _, err := range errs {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrInviteConsumed):
			consumed++
		default:
			t.Fatalf("unexpected consume error: %v", err)
		}
	}
	if successes != 1 || consumed != 1 {
		t.Fatalf("consume results = %#v", errs)
	}
}

func TestRedeemInvitationIsIdempotentForSamePeer(t *testing.T) {
	store, key := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	if err := store.Initialize(State{GenesisToken: "g", LocalMembership: "m", CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	if err := store.AddInvitations([]Invitation{{InviteID: "i", BatchID: "b", Token: "t", ExpiresAt: now.Add(time.Hour).Unix()}}, now); err != nil {
		t.Fatal(err)
	}
	other := New(store.path, func() ([]byte, error) { return append([]byte(nil), key...), nil })
	stores := []*Store{store, other}
	memberships := make([]string, len(stores))
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for index := range stores {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			memberships[index], errs[index] = stores[index].RedeemInvitation("i", "peer", "membership-"+string(rune('a'+index)), now.Add(time.Minute))
		}(index)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if memberships[0] == "" || memberships[0] != memberships[1] {
		t.Fatalf("idempotent memberships = %#v", memberships)
	}
	if _, err := store.RedeemInvitation("i", "different-peer", "replacement", now.Add(2*time.Minute)); !errors.Is(err, ErrInviteConsumed) {
		t.Fatalf("different peer redemption error = %v", err)
	}
	if err := store.RevokeInvitation("i", now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if state.Invitations[0].RevokedAt != 0 || state.Invitations[0].IssuedMembership != memberships[0] {
		t.Fatalf("consumed invitation changed by revoke: %+v", state.Invitations[0])
	}
}

func TestSignedRevocationsPersistAndMaterializeDenyWins(t *testing.T) {
	store, key := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	identity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	genesisToken, membershipToken, err := peerproto.NewSpace(identity, wrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(genesisToken)
	if err != nil {
		t.Fatal(err)
	}
	membership, err := peerproto.VerifyGrant(membershipToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Initialize(State{GenesisToken: genesisToken, LocalMembership: membershipToken, CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	tickets, err := peerproto.NewInvitationBatch(identity, genesis, membership, now, peerproto.InvitationOptions{Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	records := make([]Invitation, 0, len(tickets))
	var batchID string
	for _, token := range tickets {
		doc, _, err := peerproto.VerifyInvitation(token, genesis, now)
		if err != nil {
			t.Fatal(err)
		}
		batchID = doc.BatchID
		records = append(records, Invitation{InviteID: doc.InviteID, BatchID: doc.BatchID, Token: token, ExpiresAt: doc.ExpiresAt})
	}
	if err := store.AddInvitations(records, now); err != nil {
		t.Fatal(err)
	}
	batchRevocation, err := peerproto.NewRevocation(identity, genesis, membership, peerproto.RevocationInvitationBatch, batchID, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	memberTarget, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	memberRevocation, err := peerproto.NewRevocation(identity, genesis, membership, peerproto.RevocationMember, memberTarget.PeerID(), now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	grantTarget := "5e716a35-04ed-4ad8-8787-f73f44bb51c7"
	grantRevocation, err := peerproto.NewRevocation(identity, genesis, membership, peerproto.RevocationGrant, grantTarget, now.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if stored, err := store.ApplyRevocations([]string{grantRevocation.Token, batchRevocation.Token, memberRevocation.Token}, now.Add(4*time.Minute)); err != nil || stored != 3 {
		t.Fatalf("stored=%d err=%v", stored, err)
	}
	if stored, err := store.ApplyRevocations([]string{batchRevocation.Token}, now.Add(5*time.Minute)); err != nil || stored != 0 {
		t.Fatalf("duplicate stored=%d err=%v", stored, err)
	}

	reopened := New(store.path, func() ([]byte, error) { return append([]byte(nil), key...), nil })
	state, err := reopened.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Revocations) != 3 || state.RevokedMembers[memberTarget.PeerID()] != memberRevocation.Document.CreatedAt || state.RevokedGrantSerials[grantTarget] != grantRevocation.Document.CreatedAt {
		t.Fatalf("materialized state=%+v", state)
	}
	for _, invitation := range state.Invitations {
		if invitation.RevokedAt != batchRevocation.Document.CreatedAt {
			t.Fatalf("batch invitation not revoked: %+v", invitation)
		}
	}
	if _, err := reopened.RedeemInvitation(records[0].InviteID, "peer", "membership", now.Add(6*time.Minute)); !errors.Is(err, ErrInviteRevoked) {
		t.Fatalf("redeem signed-revoked invitation error=%v", err)
	}
}

func TestConcurrentSignedRevocationsMergeAcrossStoreInstances(t *testing.T) {
	store, key := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	identity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	genesisToken, membershipToken, err := peerproto.NewSpace(identity, wrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, _ := peerproto.VerifyGenesis(genesisToken)
	membership, _ := peerproto.VerifyGrant(membershipToken, genesis, now)
	if err := store.Initialize(State{GenesisToken: genesisToken, LocalMembership: membershipToken, CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	targets := []string{"35268ed0-edc7-44ca-97b5-2117cb48a7fc", "c598cf82-2387-432a-ad84-c6263cc47656"}
	tokens := make([]string, len(targets))
	for index, target := range targets {
		revocation, err := peerproto.NewRevocation(identity, genesis, membership, peerproto.RevocationGrant, target, now.Add(time.Duration(index)*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		tokens[index] = revocation.Token
	}
	stores := []*Store{
		store,
		New(store.path, func() ([]byte, error) { return append([]byte(nil), key...), nil }),
	}
	errs := make([]error, len(stores))
	var wg sync.WaitGroup
	for index := range stores {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, errs[index] = stores[index].ApplyRevocations([]string{tokens[index]}, now.Add(time.Hour))
		}(index)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Revocations) != 2 || len(state.RevokedGrantSerials) != 2 {
		t.Fatalf("concurrent revocations lost: %+v", state)
	}
}

func TestApplyRevocationsIsAtomicOnInvalidToken(t *testing.T) {
	store, _ := testStore(t)
	now := time.Unix(1_800_000_000, 0)
	identity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	wrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	genesisToken, membershipToken, err := peerproto.NewSpace(identity, wrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, _ := peerproto.VerifyGenesis(genesisToken)
	membership, _ := peerproto.VerifyGrant(membershipToken, genesis, now)
	if err := store.Initialize(State{GenesisToken: genesisToken, LocalMembership: membershipToken, CreatedAt: now.Unix()}); err != nil {
		t.Fatal(err)
	}
	revocation, err := peerproto.NewRevocation(identity, genesis, membership, peerproto.RevocationGrant, "d349a09d-253a-42c0-8ecc-d9a79cfa5a78", now)
	if err != nil {
		t.Fatal(err)
	}
	if stored, err := store.ApplyRevocations([]string{revocation.Token, "not-a-token"}, now); err == nil || stored != 0 {
		t.Fatalf("stored=%d err=%v", stored, err)
	}
	state, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Revocations) != 0 || len(state.RevokedGrantSerials) != 0 {
		t.Fatalf("partial revocation transaction persisted: %+v", state)
	}
}

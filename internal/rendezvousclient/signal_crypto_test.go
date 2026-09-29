package rendezvousclient

import (
	"bytes"
	"errors"
	"testing"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
)

func TestSignalCipherRoundTripBindsRouteAndPeerPair(t *testing.T) {
	epoch := testSignalEpoch(t, 7)
	alice := testWrappingIdentity(t)
	bob := testWrappingIdentity(t)
	mallory := testWrappingIdentity(t)
	aliceCipher := testSignalCipher(t, epoch, "space-one", "peer-alice", "peer-bob", alice, bob.PublicBytes())
	bobCipher := testSignalCipher(t, epoch, "space-one", "peer-bob", "peer-alice", bob, alice.PublicBytes())
	malloryCipher := testSignalCipher(t, epoch, "space-one", "peer-mallory", "peer-alice", mallory, alice.PublicBytes())

	route := SignalRouteBinding{
		Topic: encodedID(1, 32), FromPresenceID: encodedID(2, 32),
		ToPresenceID: encodedID(3, 32), MessageID: encodedID(4, 16),
	}
	plaintext := []byte(`{"v":1,"kind":"offer","payload":"private-sdp"}`)
	sealed, err := aliceCipher.Seal(route, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("private-sdp")) {
		t.Fatal("sealed signaling exposed SDP plaintext")
	}
	opened, err := bobCipher.Open(route, sealed)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("Open()=%q err=%v", opened, err)
	}

	tampered := route
	tampered.ToPresenceID = encodedID(9, 32)
	if _, err := bobCipher.Open(tampered, sealed); !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("route substitution error=%v", err)
	}
	if _, err := malloryCipher.Open(route, sealed); !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("third-member open error=%v", err)
	}
}

func TestSignalReplayWindowRejectsDuplicateMessageID(t *testing.T) {
	window := NewSignalReplayWindow(2)
	first := SignalRouteBinding{MessageID: encodedID(1, 16), FromPresenceID: encodedID(2, 32)}
	second := SignalRouteBinding{MessageID: encodedID(3, 16), FromPresenceID: encodedID(2, 32)}
	third := SignalRouteBinding{MessageID: encodedID(4, 16), FromPresenceID: encodedID(2, 32)}
	if err := window.Accept(first); err != nil {
		t.Fatal(err)
	}
	if err := window.Accept(first); !errors.Is(err, ErrSignalReplay) {
		t.Fatalf("duplicate error=%v", err)
	}
	if err := window.Accept(second); err != nil {
		t.Fatal(err)
	}
	if err := window.Accept(third); err != nil {
		t.Fatal(err)
	}
	if err := window.Accept(first); err != nil {
		t.Fatalf("evicted id should be accepted: %v", err)
	}
}

func testSignalEpoch(t *testing.T, epoch uint64) configsync.EpochKey {
	t.Helper()
	raw := bytes.Repeat([]byte{byte(epoch)}, configsync.EpochKeySize)
	key, err := configsync.ParseEpochKey(configsync.KeyClassSync, epoch, raw)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func testWrappingIdentity(t *testing.T) *peercrypto.WrappingIdentity {
	t.Helper()
	identity, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func testSignalCipher(t *testing.T, epoch configsync.EpochKey, spaceID, localPeerID, remotePeerID string, local *peercrypto.WrappingIdentity, remotePublic []byte) *SignalCipher {
	t.Helper()
	cipher, err := NewSignalCipher(SignalCipherConfig{
		EpochKey: epoch, SpaceID: spaceID, LocalPeerID: localPeerID, RemotePeerID: remotePeerID,
		LocalWrappingIdentity: local, RemoteWrappingPublicKey: remotePublic,
	})
	if err != nil {
		t.Fatal(err)
	}
	return cipher
}

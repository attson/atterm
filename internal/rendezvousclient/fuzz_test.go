package rendezvousclient

import (
	"testing"

	"github.com/attson/atterm/internal/configsync"
	"github.com/attson/atterm/internal/peercrypto"
)

func FuzzSignalEnvelope(f *testing.F) {
	epochBytes := make([]byte, configsync.EpochKeySize)
	for i := range epochBytes {
		epochBytes[i] = 7
	}
	epoch, err := configsync.ParseEpochKey(configsync.KeyClassSync, 7, epochBytes)
	if err != nil {
		f.Fatal(err)
	}
	alice, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		f.Fatal(err)
	}
	bob, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		f.Fatal(err)
	}
	aliceCipher, err := NewSignalCipher(SignalCipherConfig{
		EpochKey: epoch, SpaceID: "fuzz-space", LocalPeerID: "peer-alice", RemotePeerID: "peer-bob",
		LocalWrappingIdentity: alice, RemoteWrappingPublicKey: bob.PublicBytes(),
	})
	if err != nil {
		f.Fatal(err)
	}
	bobCipher, err := NewSignalCipher(SignalCipherConfig{
		EpochKey: epoch, SpaceID: "fuzz-space", LocalPeerID: "peer-bob", RemotePeerID: "peer-alice",
		LocalWrappingIdentity: bob, RemoteWrappingPublicKey: alice.PublicBytes(),
	})
	if err != nil {
		f.Fatal(err)
	}
	binding := SignalRouteBinding{
		Topic: encodedID(1, 32), FromPresenceID: encodedID(2, 32),
		ToPresenceID: encodedID(3, 32), MessageID: encodedID(4, 16),
	}
	sealed, err := aliceCipher.Seal(binding, []byte(`{"v":1,"kind":"offer","payload":"private-sdp"}`))
	if err != nil {
		f.Fatal(err)
	}
	f.Add(binding.Topic, binding.FromPresenceID, binding.ToPresenceID, binding.MessageID, sealed)
	f.Add("", "", "", "", []byte{})

	f.Fuzz(func(t *testing.T, topic, from, to, messageID string, envelope []byte) {
		route := SignalRouteBinding{
			Topic: topic, FromPresenceID: from, ToPresenceID: to, MessageID: messageID,
		}
		plaintext, err := bobCipher.Open(route, envelope)
		if err == nil && (len(plaintext) == 0 || len(plaintext) > maxSignalPlaintext) {
			t.Fatalf("accepted signal plaintext size=%d", len(plaintext))
		}
		window := NewSignalReplayWindow(2)
		_ = window.Accept(route)
		_ = window.Accept(route)
	})
}

package peercrypto

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestIdentityRoundTripAndSignature(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := ParseIdentity(id.PrivateBytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reloaded.PublicBytes(), id.PublicBytes()) || reloaded.PeerID() != id.PeerID() {
		t.Fatal("identity changed after private-key round trip")
	}

	payload := []byte(`{"v":1,"space_id":"test"}`)
	sig, err := id.Sign(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := Verify(id.PublicBytes(), payload, sig); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	payload[0] ^= 1
	if err := Verify(id.PublicBytes(), payload, sig); err == nil {
		t.Fatal("Verify accepted modified payload")
	}
}

func TestPeerIDUsesRawURLBase64SHA256(t *testing.T) {
	id, err := GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(id.PeerID())
	if err != nil || len(decoded) != 32 {
		t.Fatalf("PeerID decode = %d bytes, %v", len(decoded), err)
	}
}

func TestParseIdentityRejectsInvalidScalars(t *testing.T) {
	for _, raw := range [][]byte{nil, make([]byte, 31), make([]byte, 32)} {
		if _, err := ParseIdentity(raw); err == nil {
			t.Fatalf("ParseIdentity(%d zero bytes) succeeded", len(raw))
		}
	}
}

package peercrypto

import (
	"bytes"
	"errors"
	"testing"
)

func TestWrappingIdentityECDHRoundTrip(t *testing.T) {
	a, err := GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	aShared, err := a.ECDH(b.PublicBytes())
	if err != nil {
		t.Fatal(err)
	}
	bShared, err := b.ECDH(a.PublicBytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(aShared, bShared) || len(aShared) != 32 {
		t.Fatal("P-256 ECDH shared secrets differ")
	}
	restored, err := ParseWrappingIdentity(a.PrivateBytes())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.PublicBytes(), a.PublicBytes()) {
		t.Fatal("restored wrapping public key differs")
	}
}

func TestWrappingIdentityRejectsInvalidKeys(t *testing.T) {
	if _, err := ParseWrappingIdentity(make([]byte, WrappingPrivateKeySize-1)); !errors.Is(err, ErrInvalidWrappingKey) {
		t.Fatalf("private key error=%v", err)
	}
	if _, err := ValidateWrappingPublicKey(make([]byte, WrappingPublicKeySize)); !errors.Is(err, ErrInvalidWrappingKey) {
		t.Fatalf("public key error=%v", err)
	}
}

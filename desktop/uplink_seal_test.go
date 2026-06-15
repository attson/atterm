package main

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

// TestSealOutFrame_RoundTrip seals a TypeOut frame and verifies the
// resulting payload is opaque to the relay (decoded data differs from
// plaintext) and decryptable with the matching session_key.
func TestSealOutFrame_RoundTrip(t *testing.T) {
	accountKey := make([]byte, 32)
	if _, err := rand.Read(accountKey); err != nil {
		t.Fatalf("rand: %v", err)
	}
	sid := uuid.New()
	plaintext := []byte("\x1b[31mhello relay\x1b[0m\n")
	originalSeq := uint64(42)
	f := proto.EncodeOut(sid, originalSeq, plaintext)

	sealed, ok := sealOutFrame(f, accountKey)
	if !ok {
		t.Fatalf("sealOutFrame returned ok=false")
	}
	if sealed.Type != proto.TypeOut {
		t.Fatalf("sealed.Type = 0x%02x, want TypeOut", sealed.Type)
	}
	if sealed.SessionID != sid {
		t.Fatalf("sealed.SessionID changed")
	}

	gotSeq, gotData, err := proto.DecodeOut(sealed.Payload)
	if err != nil {
		t.Fatalf("DecodeOut: %v", err)
	}
	if gotSeq != originalSeq {
		t.Fatalf("seq = %d, want %d", gotSeq, originalSeq)
	}
	if bytes.Equal(gotData, plaintext) {
		t.Fatalf("sealed payload still contains plaintext")
	}

	sk, err := e2eecrypto.DeriveSessionKey(accountKey, sid)
	if err != nil {
		t.Fatalf("DeriveSessionKey: %v", err)
	}
	recovered, err := e2eecrypto.OpenOut(sk, sid, byte(proto.TypeOut), originalSeq, gotData)
	if err != nil {
		t.Fatalf("OpenOut: %v", err)
	}
	if !bytes.Equal(recovered, plaintext) {
		t.Fatalf("decrypted bytes differ from plaintext")
	}
}

// TestSealOutFrame_BypassesWithoutKey: account_key shorter than 32 bytes
// (or nil) should leave the frame untouched. This is the "user not
// logged in yet" path — must not break the in-process plaintext flow.
func TestSealOutFrame_BypassesWithoutKey(t *testing.T) {
	sid := uuid.New()
	plaintext := []byte("plain")
	f := proto.EncodeOut(sid, 1, plaintext)

	got, ok := sealOutFrame(f, nil)
	if ok {
		t.Fatalf("nil key produced sealed frame")
	}
	if !bytes.Equal(got.Payload, f.Payload) {
		t.Fatalf("nil key mutated the frame")
	}

	got2, ok := sealOutFrame(f, make([]byte, 16))
	if ok {
		t.Fatalf("short key produced sealed frame")
	}
	if !bytes.Equal(got2.Payload, f.Payload) {
		t.Fatalf("short key mutated the frame")
	}
}

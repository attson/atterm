package main

import (
	"bytes"
	"crypto/rand"
	"testing"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

func mustAccountKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

// TestOpenInboundFrame_RoundTrip seals a TypeIn payload with the M2a
// codec and verifies openInboundFrame returns the original plaintext.
// The OUT-side seal+open is already exercised in M2b/M2a tests; here
// the focus is the IN/PASTE direction the agent will receive.
func TestOpenInboundFrame_RoundTrip(t *testing.T) {
	ak := mustAccountKey(t)
	sid := uuid.New()
	keystroke := []byte("\x1b[A") // up arrow

	sk, err := e2eecrypto.DeriveSessionKey(ak, sid)
	if err != nil {
		t.Fatalf("DeriveSessionKey: %v", err)
	}
	env, err := e2eecrypto.SealUnsequenced(sk, sid, byte(proto.TypeIn), keystroke)
	if err != nil {
		t.Fatalf("SealUnsequenced: %v", err)
	}

	sealed := proto.Frame{
		Type:      proto.TypeIn,
		SessionID: sid,
		Payload:   env,
	}
	opened, ok := openInboundFrame(sealed, func() []byte { return ak })
	if !ok {
		t.Fatalf("openInboundFrame returned ok=false")
	}
	if opened.Type != proto.TypeIn {
		t.Fatalf("opened.Type = 0x%02x, want TypeIn", opened.Type)
	}
	if !bytes.Equal(opened.Payload, keystroke) {
		t.Fatalf("opened payload mismatch")
	}
}

// TestOpenInboundFrame_PasteImageRoundTrip seals + opens a PasteImage
// payload (JSON bytes are opaque to the codec).
func TestOpenInboundFrame_PasteImageRoundTrip(t *testing.T) {
	ak := mustAccountKey(t)
	sid := uuid.New()
	imageJSON := []byte(`{"filename":"x.png","content_type":"image/png","data":"AAA="}`)

	sk, _ := e2eecrypto.DeriveSessionKey(ak, sid)
	env, _ := e2eecrypto.SealUnsequenced(sk, sid, byte(proto.TypePasteImage), imageJSON)

	sealed := proto.Frame{Type: proto.TypePasteImage, SessionID: sid, Payload: env}
	opened, ok := openInboundFrame(sealed, func() []byte { return ak })
	if !ok {
		t.Fatalf("ok=false on PASTE_IMAGE")
	}
	if !bytes.Equal(opened.Payload, imageJSON) {
		t.Fatalf("PASTE_IMAGE round-trip mismatch")
	}
}

// TestOpenInboundFrame_PlaintextPassesThrough covers the legacy client
// case: a TypeIn frame whose payload is a raw keystroke (not an
// envelope) should be returned unchanged so the existing flow keeps
// working until every client speaks the encrypted dialect.
func TestOpenInboundFrame_PlaintextPassesThrough(t *testing.T) {
	plain := proto.Frame{
		Type:      proto.TypeIn,
		SessionID: uuid.New(),
		Payload:   []byte("x"),
	}
	out, ok := openInboundFrame(plain, func() []byte { return mustAccountKey(t) })
	if ok {
		t.Fatalf("expected ok=false for plaintext payload")
	}
	if !bytes.Equal(out.Payload, plain.Payload) {
		t.Fatalf("plaintext payload mutated")
	}
}

// TestOpenInboundFrame_NilAccountKeyBypass: when the user isn't logged
// in, accountKey returns nil and the frame must pass through unchanged
// so local-only / bootstrap-admin setups still work.
func TestOpenInboundFrame_NilAccountKeyBypass(t *testing.T) {
	// Build a real envelope so the heuristic doesn't reject by shape.
	ak := mustAccountKey(t)
	sid := uuid.New()
	sk, _ := e2eecrypto.DeriveSessionKey(ak, sid)
	env, _ := e2eecrypto.SealUnsequenced(sk, sid, byte(proto.TypeIn), []byte("x"))

	in := proto.Frame{Type: proto.TypeIn, SessionID: sid, Payload: env}
	out, ok := openInboundFrame(in, func() []byte { return nil })
	if ok {
		t.Fatalf("expected ok=false for nil account key")
	}
	if !bytes.Equal(out.Payload, in.Payload) {
		t.Fatalf("nil key path mutated the frame")
	}

	out2, ok2 := openInboundFrame(in, nil)
	if ok2 {
		t.Fatalf("expected ok=false for nil getter")
	}
	if !bytes.Equal(out2.Payload, in.Payload) {
		t.Fatalf("nil getter path mutated the frame")
	}
}

// TestOpenInboundFrame_ResizeNeverDecrypted: cols/rows is structural
// metadata, not content; even when the payload happens to be 41 bytes
// long and starts with 0x01 we don't try to decrypt.
func TestOpenInboundFrame_ResizeNeverDecrypted(t *testing.T) {
	// A 4-byte RESIZE payload won't pass the size gate, but use a
	// synthetic 41-byte payload starting with 0x01 to prove the type
	// gate (not the size gate) is doing the work.
	resize := proto.Frame{
		Type:      proto.TypeResize,
		SessionID: uuid.New(),
		Payload:   append([]byte{0x01}, make([]byte, 40)...),
	}
	out, ok := openInboundFrame(resize, func() []byte { return mustAccountKey(t) })
	if ok {
		t.Fatalf("RESIZE should never be decrypted")
	}
	if !bytes.Equal(out.Payload, resize.Payload) {
		t.Fatalf("RESIZE payload mutated")
	}
}

// TestOpenInboundFrame_WrongKey: an envelope sealed with key A but
// decrypted with key B must fall back to passing through (returning
// the envelope unchanged) rather than corrupting the agent's PTY with
// random bytes.
func TestOpenInboundFrame_WrongKey(t *testing.T) {
	akSealer := mustAccountKey(t)
	akOpener := mustAccountKey(t)
	sid := uuid.New()

	sk, _ := e2eecrypto.DeriveSessionKey(akSealer, sid)
	env, _ := e2eecrypto.SealUnsequenced(sk, sid, byte(proto.TypeIn), []byte("x"))

	in := proto.Frame{Type: proto.TypeIn, SessionID: sid, Payload: env}
	out, ok := openInboundFrame(in, func() []byte { return akOpener })
	if ok {
		t.Fatalf("expected ok=false on wrong key")
	}
	if !bytes.Equal(out.Payload, env) {
		t.Fatalf("wrong-key path mutated the envelope")
	}
}

package proto

import (
	"encoding/base64"
	"testing"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/google/uuid"
)

// The Go and TS envelope implementations are written independently. Only
// a fixture produced by one and opened by the other proves they agree on
// key derivation, AAD layout, and envelope framing. Keep the constants
// here in sync with desktop/frontend/src/lib/fsCrypto.vectors.test.ts.
const (
	vectorSessionID = "11111111-2222-3333-4444-555555555555"
	// Produced by TS: sealUnsequenced(KEY, SID, 0x38, "/home/u/.env")
	tsEnvelopeB64 = "AX9E//g1E9udbOXvoZXMIrZM0kkA8wbh9qNeyPH2PnlwxgbSEXWbhSNEXKbU0cCvfpJ/iXc="
)

func vectorAccountKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

func TestOpensTSSealedEnvelope(t *testing.T) {
	sid := uuid.MustParse(vectorSessionID)
	sk, err := e2eecrypto.DeriveSessionKey(vectorAccountKey(), sid)
	if err != nil {
		t.Fatal(err)
	}
	env, err := base64.StdEncoding.DecodeString(tsEnvelopeB64)
	if err != nil {
		t.Fatal(err)
	}
	out, err := e2eecrypto.OpenUnsequenced(sk, sid, byte(TypeFSRequest), env)
	if err != nil {
		t.Fatalf("could not open TS-sealed envelope: %v", err)
	}
	if string(out) != "/home/u/.env" {
		t.Fatalf("got %q, want %q", out, "/home/u/.env")
	}
}

func TestRejectsTSEnvelopeUnderWrongFrameType(t *testing.T) {
	sid := uuid.MustParse(vectorSessionID)
	sk, err := e2eecrypto.DeriveSessionKey(vectorAccountKey(), sid)
	if err != nil {
		t.Fatal(err)
	}
	env, err := base64.StdEncoding.DecodeString(tsEnvelopeB64)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e2eecrypto.OpenUnsequenced(sk, sid, byte(TypeFSResponse), env); err == nil {
		t.Fatal("TS FS_REQUEST envelope opened under the FS_RESPONSE AAD")
	}
}

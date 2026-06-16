package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

func TestSealCommandEventBody_RoundTrip(t *testing.T) {
	ak := mustAccountKey32(t)
	sid := uuid.New()
	payload := proto.CommandEventPayload{
		ExitCode:  0,
		ElapsedMS: 12_345,
		Label:     "build",
	}
	env, ok := sealCommandEventBody(sid, payload, ak)
	if !ok {
		t.Fatalf("sealCommandEventBody returned ok=false")
	}
	if len(env) == 0 {
		t.Fatalf("empty envelope")
	}

	sk, err := e2eecrypto.DeriveSessionKey(ak, sid)
	if err != nil {
		t.Fatalf("DeriveSessionKey: %v", err)
	}
	pt, err := e2eecrypto.OpenUnsequenced(sk, sid, sealedPushAADFrameType, env)
	if err != nil {
		t.Fatalf("OpenUnsequenced: %v", err)
	}
	var decoded sealedPushBody
	if err := json.Unmarshal(pt, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Label != payload.Label ||
		decoded.ExitCode != payload.ExitCode ||
		decoded.ElapsedMS != payload.ElapsedMS {
		t.Fatalf("decoded body mismatch: %+v", decoded)
	}
}

func TestSealCommandEventBody_ShortKey_NoOp(t *testing.T) {
	if _, ok := sealCommandEventBody(uuid.New(), proto.CommandEventPayload{Label: "x"}, make([]byte, 16)); ok {
		t.Fatalf("expected ok=false on short key")
	}
}

// TestSealCommandEventBody_AADBindsType: an envelope sealed under the
// CommandEvent AAD discriminator must NOT open under the SessionInfo or
// META AAD. Cross-type replay guard.
func TestSealCommandEventBody_AADBindsType(t *testing.T) {
	ak := mustAccountKey32(t)
	sid := uuid.New()
	env, ok := sealCommandEventBody(sid, proto.CommandEventPayload{Label: "x"}, ak)
	if !ok {
		t.Fatalf("seal failed")
	}
	sk, _ := e2eecrypto.DeriveSessionKey(ak, sid)
	if _, err := e2eecrypto.OpenUnsequenced(sk, sid, 0x12 /* TypeListResp */, env); err == nil {
		t.Fatalf("envelope opened under wrong AAD (SessionInfo)")
	}
	if _, err := e2eecrypto.OpenUnsequenced(sk, sid, 0x05 /* TypeMeta */, env); err == nil {
		t.Fatalf("envelope opened under wrong AAD (META)")
	}
}

func TestSealCommandEventBody_NoPlaintextLeak(t *testing.T) {
	const marker = "M6_PUSH_MARKER_3F77"
	ak := mustAccountKey32(t)
	sid := uuid.New()
	payload := proto.CommandEventPayload{
		ExitCode:  0,
		ElapsedMS: 1,
		Label:     marker,
	}
	env, ok := sealCommandEventBody(sid, payload, ak)
	if !ok {
		t.Fatalf("seal failed")
	}
	if strings.Contains(string(env), marker) {
		t.Fatalf("plaintext marker present in envelope")
	}
}

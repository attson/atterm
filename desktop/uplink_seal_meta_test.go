package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

func TestSealMetaContentFields_RoundTrip(t *testing.T) {
	ak := mustAccountKey32(t)
	sid := uuid.New()
	original := proto.MetaPayload{
		Title:          "atterm - bash",
		Cwd:            "/Users/alice/secrets",
		CurrentCommand: "rg api_key",
		TaskState:      proto.TaskStateRunning,
		Cols:           80,
	}
	payload, _ := json.Marshal(original)
	f := proto.Frame{Type: proto.TypeMeta, SessionID: sid, Payload: payload}

	sealed, ok := sealMetaContentFields(f, ak)
	if !ok {
		t.Fatalf("sealMetaContentFields returned ok=false")
	}
	var got proto.MetaPayload
	if err := json.Unmarshal(sealed.Payload, &got); err != nil {
		t.Fatalf("decode sealed: %v", err)
	}
	if len(got.Sealed) == 0 {
		t.Fatalf("Sealed envelope not populated")
	}
	// Plaintext fields still here pre-strip.
	if got.Title != original.Title || got.Cwd != original.Cwd {
		t.Fatalf("plaintext fields prematurely cleared")
	}

	sk, err := e2eecrypto.DeriveSessionKey(ak, sid)
	if err != nil {
		t.Fatalf("DeriveSessionKey: %v", err)
	}
	pt, err := e2eecrypto.OpenUnsequenced(sk, sid, byte(proto.TypeMeta), got.Sealed)
	if err != nil {
		t.Fatalf("OpenUnsequenced: %v", err)
	}
	var decoded sealedMetaFields
	if err := json.Unmarshal(pt, &decoded); err != nil {
		t.Fatalf("unmarshal sealed body: %v", err)
	}
	if decoded.Title != original.Title ||
		decoded.Cwd != original.Cwd ||
		decoded.CurrentCommand != original.CurrentCommand {
		t.Fatalf("decrypted body mismatch: %+v", decoded)
	}
}

func TestSealMetaContentFields_EmptyContent_NoOp(t *testing.T) {
	ak := mustAccountKey32(t)
	payload, _ := json.Marshal(proto.MetaPayload{TaskState: proto.TaskStateRunning, Cols: 80})
	f := proto.Frame{Type: proto.TypeMeta, SessionID: uuid.New(), Payload: payload}
	_, ok := sealMetaContentFields(f, ak)
	if ok {
		t.Fatalf("expected ok=false on empty content")
	}
}

func TestSealMetaContentFields_ShortKey_NoOp(t *testing.T) {
	payload, _ := json.Marshal(proto.MetaPayload{Title: "x"})
	f := proto.Frame{Type: proto.TypeMeta, SessionID: uuid.New(), Payload: payload}
	_, ok := sealMetaContentFields(f, make([]byte, 16))
	if ok {
		t.Fatalf("expected ok=false on short key")
	}
}

func TestSealMetaContentFields_BadJSON_NoOp(t *testing.T) {
	ak := mustAccountKey32(t)
	f := proto.Frame{Type: proto.TypeMeta, SessionID: uuid.New(), Payload: []byte("garbage")}
	_, ok := sealMetaContentFields(f, ak)
	if ok {
		t.Fatalf("expected ok=false on bad JSON")
	}
}

// TestSealThenStripMeta_RelayCantSeePlaintext mirrors the M3b-strip
// end-to-end check for live META frames. The marker MUST NOT appear in
// the wire bytes once seal + strip have run.
func TestSealThenStripMeta_RelayCantSeePlaintext(t *testing.T) {
	const marker = "M5_META_MARKER_8B11"
	ak := mustAccountKey32(t)
	sid := uuid.New()
	payload, _ := json.Marshal(proto.MetaPayload{
		Title:          marker + "-title",
		Cwd:            "/home/x/" + marker,
		CurrentCommand: marker + " run",
		TaskState:      proto.TaskStateRunning,
		Cols:           80,
	})
	f := proto.Frame{Type: proto.TypeMeta, SessionID: sid, Payload: payload}

	sealed, ok := sealMetaContentFields(f, ak)
	if !ok {
		t.Fatalf("seal failed")
	}
	stripped, ok := stripMetaContentFields(sealed)
	if !ok {
		t.Fatalf("strip failed")
	}
	if strings.Contains(string(stripped.Payload), marker) {
		t.Fatalf("relay would see plaintext marker in META payload: %s", stripped.Payload)
	}

	// Decrypt side recovers the fields.
	var meta proto.MetaPayload
	_ = json.Unmarshal(stripped.Payload, &meta)
	sk, _ := e2eecrypto.DeriveSessionKey(ak, sid)
	pt, err := e2eecrypto.OpenUnsequenced(sk, sid, byte(proto.TypeMeta), meta.Sealed)
	if err != nil {
		t.Fatalf("OpenUnsequenced: %v", err)
	}
	var decoded sealedMetaFields
	_ = json.Unmarshal(pt, &decoded)
	if decoded.Title != marker+"-title" {
		t.Fatalf("title mismatch: %q", decoded.Title)
	}
}

package main

import (
	"crypto/rand"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

func mustAccountKey32(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

func mustSessionInfo() proto.SessionInfo {
	return proto.SessionInfo{
		ID:             uuid.NewString(),
		HostID:         "host-1",
		Host:           "alice-laptop",
		User:           "alice",
		Title:          "atterm - bash",
		Cwd:            "/Users/alice/secrets",
		Command:        "bash",
		CurrentCommand: "rg api_key",
		Cols:           80,
		Rows:           24,
		StartedAt:      1700000000,
		TaskState:      proto.TaskStateRunning,
	}
}

// TestSealSessionInfoContent_RoundTrip seals one info under accountKey
// and verifies a holder of the same key can recover the original
// content-bearing fields byte-for-byte. The agent's local snapshot
// must be unchanged so the desktop's own renderer still shows the
// plaintext values.
func TestSealSessionInfoContent_RoundTrip(t *testing.T) {
	ak := mustAccountKey32(t)
	original := mustSessionInfo()
	originalTitle := original.Title
	originalCwd := original.Cwd
	sealed := sealSessionInfoContent([]proto.SessionInfo{original}, ak)

	if original.Title != originalTitle || original.Cwd != originalCwd {
		t.Fatalf("input mutated")
	}
	if len(sealed) != 1 {
		t.Fatalf("len(sealed) = %d, want 1", len(sealed))
	}
	if len(sealed[0].Sealed) == 0 {
		t.Fatalf("Sealed not populated")
	}
	// Plaintext fields stay during the additive rollout.
	if sealed[0].Title != original.Title || sealed[0].Cwd != original.Cwd {
		t.Fatalf("plaintext fields mutated unexpectedly: %+v", sealed[0])
	}

	id, _ := uuid.Parse(sealed[0].ID)
	sk, err := e2eecrypto.DeriveSessionKey(ak, id)
	if err != nil {
		t.Fatalf("DeriveSessionKey: %v", err)
	}
	pt, err := e2eecrypto.OpenUnsequenced(sk, id, sessionInfoSealedAADFrameType, sealed[0].Sealed)
	if err != nil {
		t.Fatalf("OpenUnsequenced: %v", err)
	}
	var got sealedSessionFields
	if err := json.Unmarshal(pt, &got); err != nil {
		t.Fatalf("unmarshal sealed: %v", err)
	}
	if got.Title != original.Title ||
		got.Cwd != original.Cwd ||
		got.Command != original.Command ||
		got.CurrentCommand != original.CurrentCommand {
		t.Fatalf("decrypted fields mismatch: %+v", got)
	}
}

// TestSealSessionInfoContent_NoKey passes a nil / short accountKey and
// asserts the snapshot is returned unchanged (Sealed empty). This is
// the legacy / bootstrap / pre-login path.
func TestSealSessionInfoContent_NoKey(t *testing.T) {
	original := []proto.SessionInfo{mustSessionInfo()}
	out := sealSessionInfoContent(original, nil)
	if len(out[0].Sealed) != 0 {
		t.Fatalf("nil key produced Sealed bytes")
	}
	short := make([]byte, 16)
	out = sealSessionInfoContent(original, short)
	if len(out[0].Sealed) != 0 {
		t.Fatalf("short key produced Sealed bytes")
	}
}

// TestSealSessionInfoContent_EmptyContent_NoSeal: if a session
// snapshot has no title/cwd/command, the helper does not seal —
// there is nothing sensitive to hide and emitting an empty envelope
// would just add a few bytes of ciphertext per session.
func TestSealSessionInfoContent_EmptyContent_NoSeal(t *testing.T) {
	ak := mustAccountKey32(t)
	in := []proto.SessionInfo{{
		ID:        uuid.NewString(),
		TaskState: proto.TaskStateIdle,
	}}
	out := sealSessionInfoContent(in, ak)
	if len(out[0].Sealed) != 0 {
		t.Fatalf("empty-content session got Sealed bytes")
	}
}

// TestSealSessionInfoContent_BadIDSkipsSilently: a SessionInfo whose
// ID can't parse is left unsealed. The agent will never produce one
// in practice (every local Session has a uuid), but the helper must
// not panic.
func TestSealSessionInfoContent_BadIDSkipsSilently(t *testing.T) {
	ak := mustAccountKey32(t)
	in := []proto.SessionInfo{{ID: "not-a-uuid", Title: "x"}}
	out := sealSessionInfoContent(in, ak)
	if len(out[0].Sealed) != 0 {
		t.Fatalf("bad-id session got Sealed bytes")
	}
	// Plaintext field still present (untouched).
	if out[0].Title != "x" {
		t.Fatalf("Title mutated")
	}
}

// TestSealSessionInfoContent_PerSessionKeyIsolation: two sessions
// with different ids produce envelopes that cannot decrypt under each
// other's key. This is the load-bearing property: leaking one
// session_key must not compromise another session's title/cwd.
func TestSealSessionInfoContent_PerSessionKeyIsolation(t *testing.T) {
	ak := mustAccountKey32(t)
	in := []proto.SessionInfo{
		{ID: uuid.NewString(), Title: "session-A"},
		{ID: uuid.NewString(), Title: "session-B"},
	}
	out := sealSessionInfoContent(in, ak)
	idA, _ := uuid.Parse(out[0].ID)
	idB, _ := uuid.Parse(out[1].ID)
	skB, _ := e2eecrypto.DeriveSessionKey(ak, idB)
	// Using B's key to open A's envelope must fail.
	if _, err := e2eecrypto.OpenUnsequenced(skB, idA, sessionInfoSealedAADFrameType, out[0].Sealed); err == nil {
		t.Fatalf("B's key opened A's envelope; isolation broken")
	}
}

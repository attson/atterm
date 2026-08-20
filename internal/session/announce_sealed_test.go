package session

import (
	"bytes"
	"testing"

	"github.com/attson/atterm/internal/proto"
	"github.com/google/uuid"
)

// TestUpdateAdvertisedInfo_AdoptsSealed guards the mirror-session data
// path used by mobile HTTP GET /api/sessions:
//
//	agent  ─ANNOUNCE(sealed + stripped)─►  relay mirror  ──ss.Info()──►  /api/sessions
//
// Because the agent's uplink strips plaintext title/cwd/command and
// carries the sealed envelope instead, the mirror's meta.Sealed MUST
// be populated from the ANNOUNCE — else /api/sessions serves rows with
// empty content fields to remote clients that can only reach the
// remote relay (mobile / web).
func TestUpdateAdvertisedInfo_AdoptsSealed(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{Cols: 80, Rows: 24})

	sealed := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	s.UpdateAdvertisedInfo(proto.SessionInfo{
		ID:     s.ID.String(),
		Sealed: sealed,
	})

	got := s.Info()
	if !bytes.Equal(got.Sealed, sealed) {
		t.Fatalf("Sealed not adopted: got %v, want %v", got.Sealed, sealed)
	}
}

// TestUpdateAdvertisedInfo_SealedIsDeepCopied: the caller's ANNOUNCE
// []byte must not alias into the mirror session's meta — a later
// mutation on that buffer would silently corrupt the served envelope.
func TestUpdateAdvertisedInfo_SealedIsDeepCopied(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})

	sealed := []byte{0xaa, 0xbb, 0xcc}
	s.UpdateAdvertisedInfo(proto.SessionInfo{
		ID:     s.ID.String(),
		Sealed: sealed,
	})

	// Mutate the caller's buffer after the update.
	sealed[0] = 0xff

	got := s.Info()
	if got.Sealed[0] != 0xaa {
		t.Fatalf("Sealed[0] = 0x%x after caller mutation, want 0xaa (deep copy broken)", got.Sealed[0])
	}
}

// TestUpdateAdvertisedInfo_EmptySealedDoesNotClobber mirrors the guard
// on Title/Cwd: an ANNOUNCE without Sealed shouldn't wipe a
// previously-set envelope. This matters because some legacy code paths
// still send an unsealed snapshot alongside sealed ones.
func TestUpdateAdvertisedInfo_EmptySealedDoesNotClobber(t *testing.T) {
	s := New(uuid.New(), proto.SessionInfo{})

	initial := []byte{0x10, 0x20, 0x30}
	s.UpdateAdvertisedInfo(proto.SessionInfo{
		ID:     s.ID.String(),
		Sealed: initial,
	})
	// Second announce without Sealed — must keep the first.
	s.UpdateAdvertisedInfo(proto.SessionInfo{
		ID: s.ID.String(),
		// Sealed intentionally omitted.
	})

	got := s.Info()
	if !bytes.Equal(got.Sealed, initial) {
		t.Fatalf("Sealed clobbered by empty announce: got %v, want %v", got.Sealed, initial)
	}
}

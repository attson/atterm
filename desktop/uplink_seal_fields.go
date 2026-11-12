package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

// sealedSessionFields is the JSON document we encrypt into
// SessionInfo.Sealed when E2EE is unlocked. Exactly the
// content-bearing fields the relay should not see; structural metadata
// (id, host, cols/rows, timestamps, task_state, etc.) stays on the
// outer SessionInfo so the relay can still route, schedule push,
// and order the session list.
type sealedSessionFields struct {
	Title          string                            `json:"title,omitempty"`
	Cwd            string                            `json:"cwd,omitempty"`
	Command        string                            `json:"command,omitempty"`
	CurrentCommand string                            `json:"current_command,omitempty"`
	SSHHostAlias   string                            `json:"ssh_host_alias,omitempty"`
	SSHAppearance  *proto.TerminalAppearanceOverride `json:"ssh_appearance,omitempty"`
}

type sessionInfoSealCacheEntry struct {
	contentMAC [sha256.Size]byte
	sealed     []byte
}

// sessionInfoSealCache keeps randomized SessionInfo envelopes stable while
// their plaintext is unchanged. Besides making ANNOUNCE deduplication work,
// this lets the relay distinguish an activity-only update from a real content
// update without learning the content. The keyed fingerprint avoids retaining
// an extra plaintext copy or exposing a dictionary-testable unkeyed digest.
type sessionInfoSealCache struct {
	entries map[uuid.UUID]sessionInfoSealCacheEntry
}

func (c *sessionInfoSealCache) seal(snapshot []proto.SessionInfo, accountKey []byte) []proto.SessionInfo {
	if len(accountKey) < e2eecrypto.SessionKeySize {
		return snapshot
	}
	out := make([]proto.SessionInfo, len(snapshot))
	next := make(map[uuid.UUID]sessionInfoSealCacheEntry, len(snapshot))
	for i, s := range snapshot {
		id, payload, ok := sessionInfoSealedPayload(s)
		if !ok {
			out[i] = s
			continue
		}
		contentMAC := sessionInfoContentMAC(accountKey, id, payload)
		entry, cached := c.entries[id]
		if !cached || !hmac.Equal(entry.contentMAC[:], contentMAC[:]) {
			sealed, err := sealSessionInfoPayload(id, payload, accountKey)
			if err != nil {
				out[i] = s
				continue
			}
			entry = sessionInfoSealCacheEntry{contentMAC: contentMAC, sealed: sealed}
		}
		s.Sealed = append([]byte(nil), entry.sealed...)
		next[id] = entry
		out[i] = s
	}
	c.entries = next
	return out
}

func sessionInfoContentMAC(accountKey []byte, id uuid.UUID, payload []byte) [sha256.Size]byte {
	mac := hmac.New(sha256.New, accountKey)
	_, _ = mac.Write(id[:])
	_, _ = mac.Write(payload)
	var result [sha256.Size]byte
	copy(result[:], mac.Sum(nil))
	return result
}

// sealSessionInfoContent populates the Sealed field of each session in
// snapshot using HKDF-derived per-session keys from accountKey. Returns
// a fresh slice so the agent's local renderer (which still uses the
// plaintext fields) is not mutated.
//
// accountKey shorter than the required minimum is treated as "key not
// available" — the input snapshot is returned unchanged. This is the
// clean fallback for legacy / bootstrap-admin / pre-login paths.
//
// This helper only creates envelopes. writeAnnounce subsequently calls
// stripContentFieldsFromSnapshot so the relay never receives a plaintext copy
// after sealing succeeds.
func sealSessionInfoContent(snapshot []proto.SessionInfo, accountKey []byte) []proto.SessionInfo {
	var cache sessionInfoSealCache
	return cache.seal(snapshot, accountKey)
}

func sessionInfoSealedPayload(s proto.SessionInfo) (uuid.UUID, []byte, bool) {
	if s.Title == "" && s.Cwd == "" && s.Command == "" && s.CurrentCommand == "" &&
		s.SSHHostAlias == "" && s.SSHAppearance == nil {
		// Nothing sensitive to hide. Emitting an empty Sealed envelope
		// would just leak that a session_uuid is in use, which the
		// outer struct already broadcasts.
		return uuid.Nil, nil, false
	}
	id, err := uuid.Parse(s.ID)
	if err != nil {
		return uuid.Nil, nil, false
	}
	payload, err := json.Marshal(sealedSessionFields{
		Title:          s.Title,
		Cwd:            s.Cwd,
		Command:        s.Command,
		CurrentCommand: s.CurrentCommand,
		SSHHostAlias:   s.SSHHostAlias,
		SSHAppearance:  s.SSHAppearance,
	})
	if err != nil {
		return uuid.Nil, nil, false
	}
	return id, payload, true
}

func sealSessionInfoPayload(id uuid.UUID, payload, accountKey []byte) ([]byte, error) {
	sk, err := e2eecrypto.DeriveSessionKey(accountKey, id)
	if err != nil {
		return nil, err
	}
	return e2eecrypto.SealUnsequenced(sk, id, sessionInfoSealedAADFrameType, payload)
}

// sessionInfoSealedAADFrameType is the AAD discriminator bound into
// SessionInfo.Sealed envelopes. Distinct from any real wire frame type
// so a Sealed blob from a SessionInfo cannot be replayed as a TypeIn
// chunk (or vice versa).
const sessionInfoSealedAADFrameType byte = 0x12 // matches TypeListResp's id

package main

import (
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
	Title          string `json:"title,omitempty"`
	Cwd            string `json:"cwd,omitempty"`
	Command        string `json:"command,omitempty"`
	CurrentCommand string `json:"current_command,omitempty"`
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
// During the M3b-additive rollout the plaintext fields stay in place
// so older clients without decrypt support keep working. A later
// slice (M3b-strip) will drop them once the three client SDKs all
// decrypt reliably.
func sealSessionInfoContent(snapshot []proto.SessionInfo, accountKey []byte) []proto.SessionInfo {
	if len(accountKey) < e2eecrypto.SessionKeySize {
		return snapshot
	}
	out := make([]proto.SessionInfo, len(snapshot))
	for i, s := range snapshot {
		sealed, ok := sealOneSessionInfo(s, accountKey)
		if ok {
			s.Sealed = sealed
		}
		out[i] = s
	}
	return out
}

// sealOneSessionInfo derives the per-session key from accountKey, marshals
// {title, cwd, command, current_command} as JSON, and AEAD-seals the
// bytes using the unsequenced envelope variant. Returns ok == false if
// the session id cannot be parsed or any cipher operation fails — the
// caller leaves Sealed unset in that case.
//
// frameType byte for the AAD is 0x12 (TypeListResp), chosen because the
// sealed bytes ride on the SessionInfo struct which is the payload of
// /api/sessions and TypeListResp / TypeAnnounce. The constant is local
// to this file — defining a dedicated AAD discriminator avoids leaking
// SessionInfo encryption details into proto/codec.go's frame-type table.
func sealOneSessionInfo(s proto.SessionInfo, accountKey []byte) ([]byte, bool) {
	if s.Title == "" && s.Cwd == "" && s.Command == "" && s.CurrentCommand == "" {
		// Nothing sensitive to hide. Emitting an empty Sealed envelope
		// would just leak that a session_uuid is in use, which the
		// outer struct already broadcasts.
		return nil, false
	}
	id, err := uuid.Parse(s.ID)
	if err != nil {
		return nil, false
	}
	sk, err := e2eecrypto.DeriveSessionKey(accountKey, id)
	if err != nil {
		return nil, false
	}
	payload, err := json.Marshal(sealedSessionFields{
		Title:          s.Title,
		Cwd:            s.Cwd,
		Command:        s.Command,
		CurrentCommand: s.CurrentCommand,
	})
	if err != nil {
		return nil, false
	}
	env, err := e2eecrypto.SealUnsequenced(sk, id, sessionInfoSealedAADFrameType, payload)
	if err != nil {
		return nil, false
	}
	return env, true
}

// sessionInfoSealedAADFrameType is the AAD discriminator bound into
// SessionInfo.Sealed envelopes. Distinct from any real wire frame type
// so a Sealed blob from a SessionInfo cannot be replayed as a TypeIn
// chunk (or vice versa).
const sessionInfoSealedAADFrameType byte = 0x12 // matches TypeListResp's id

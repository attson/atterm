package main

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

// sealedPushBody is the agent-composed payload that will end up inside
// a Web Push notification body after the service worker decrypts it
// (M6-sw, follow-up). For v1 the agent ships exactly the structured
// fields a typical "command finished" notification needs; the worker
// renders them as a human-readable line, e.g.:
//
//	"build (12.3s) — exit 0"
//
// Keeping this struct narrow keeps the envelope tiny (push payloads
// have a per-provider size cap; Apple is ~4 KiB, Chrome is generous,
// the lowest common denominator is ~3 KiB).
type sealedPushBody struct {
	Label      string `json:"label,omitempty"`
	ExitCode   int    `json:"exit_code"`
	ElapsedMS  int    `json:"elapsed_ms"`
}

// sealedPushAADFrameType is the AAD discriminator bound into the
// CommandEvent's sealed envelope. Distinct from the SessionInfo and
// MetaPayload discriminators so a stolen envelope cannot be replayed
// across types. 0x35 matches proto.TypeCommandEvent.
const sealedPushAADFrameType byte = 0x35

// sealCommandEventBody seals the payload fields the relay will
// otherwise re-compose itself into a generic push string. Returns
// ok == false when the account_key is too short, the session id is
// somehow zero, or any cipher op fails — in those cases the caller
// forwards the CommandEvent without SealedBody and the relay falls
// back to the legacy Label-based push body composition.
func sealCommandEventBody(sessionID uuid.UUID, payload proto.CommandEventPayload, accountKey []byte) ([]byte, bool) {
	if len(accountKey) < e2eecrypto.SessionKeySize {
		return nil, false
	}
	body, err := json.Marshal(sealedPushBody{
		Label:     payload.Label,
		ExitCode:  payload.ExitCode,
		ElapsedMS: payload.ElapsedMS,
	})
	if err != nil {
		return nil, false
	}
	sk, err := e2eecrypto.DeriveSessionKey(accountKey, sessionID)
	if err != nil {
		return nil, false
	}
	env, err := e2eecrypto.SealUnsequenced(sk, sessionID, sealedPushAADFrameType, body)
	if err != nil {
		return nil, false
	}
	return env, true
}

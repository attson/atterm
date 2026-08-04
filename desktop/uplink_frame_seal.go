package main

import (
	"encoding/json"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/proto"
)

// sealOutFrame wraps a TypeOut payload's bytes with an AEAD envelope so
// the remote relay carries only opaque ciphertext. The wire layout of
// TypeOut stays seq(8B BE) || data — only the data portion is replaced
// with the envelope. accountKey == nil (user not logged in / no remote
// E2EE relay) is a clean bypass: the original plaintext frame is sent
// through.
//
// ok == false on any sealing error; caller falls back to plaintext. This
// is the right call for the M2b agent path because losing one chunk is
// preferable to dropping the session, and the only failure modes here are
// program bugs (account_key shorter than 32B) — they will be caught in
// unit tests, not at runtime.
func sealOutFrame(f proto.Frame, accountKey []byte) (proto.Frame, bool) {
	if len(accountKey) < e2eecrypto.SessionKeySize {
		return f, false
	}
	seq, data, err := proto.DecodeOut(f.Payload)
	if err != nil {
		return f, false
	}
	sk, err := e2eecrypto.DeriveSessionKey(accountKey, f.SessionID)
	if err != nil {
		return f, false
	}
	envelope, err := e2eecrypto.SealOut(sk, f.SessionID, byte(proto.TypeOut), seq, data)
	if err != nil {
		return f, false
	}
	return proto.EncodeOut(f.SessionID, seq, envelope), true
}

// openInboundFrame is the M2e counterpart of sealOutFrame for the
// relay→agent direction: when an inbound TypeIn or TypePasteImage frame
// carries an e2eecrypto envelope (cipher_id 0x01 prefix + minimum
// envelope size), unseal it with the per-session key and return the
// plaintext frame. TypeResize is structural metadata (cols/rows) and
// is never encrypted; skipped here.
//
// ok == false means "leave the frame as-is". Cases that hit this path:
//   - accountKey == nil (user not logged in / no remote E2EE relay)
//   - frame is not TypeIn / TypePasteImage
//   - payload does not look like an envelope (plaintext from a legacy
//     client that hasn't migrated yet)
//   - AEAD open fails (tampered, wrong key, replay across sessions)
//
// All four are treated identically: pass the frame through unchanged
// so the legacy plaintext path keeps working. A future hardening pass
// can promote AEAD failures into hard drops once every client speaks
// the encrypted dialect.
func openInboundFrame(f proto.Frame, accountKey func() []byte) (proto.Frame, bool) {
	if accountKey == nil {
		return f, false
	}
	if f.Type != proto.TypeIn && f.Type != proto.TypePasteImage {
		return f, false
	}
	if !looksLikeUnsequencedEnvelope(f.Payload) {
		return f, false
	}
	key := accountKey()
	if len(key) < e2eecrypto.SessionKeySize {
		return f, false
	}
	sk, err := e2eecrypto.DeriveSessionKey(key, f.SessionID)
	if err != nil {
		return f, false
	}
	plaintext, err := e2eecrypto.OpenUnsequenced(sk, f.SessionID, byte(f.Type), f.Payload)
	if err != nil {
		return f, false
	}
	return proto.Frame{
		Type:      f.Type,
		SessionID: f.SessionID,
		Payload:   plaintext,
	}, true
}

// stripMetaContentFields parses an outbound TypeMeta payload, drops the
// content fields the relay is not entitled to read under the E2EE
// threat model, and re-marshals. Used on the agent's outbound path so
// live META updates do not leak text the OUT-byte stream is hiding.
//
// Stripped here:
//   - Summary       (M3a)
//   - CurrentCommand (M3c)
//   - Cwd            (M5)
//   - Title          (M5)
//
// Caller MUST run sealMetaContentFields on the frame first so the
// sealed envelope is populated before the plaintext fields are
// cleared; otherwise clients have nothing to render.
//
// Returns ok == false when the payload doesn't decode or nothing
// needed stripping (in which case the caller passes the frame through
// unchanged — losing the strip is preferable to dropping META).
func stripMetaContentFields(f proto.Frame) (proto.Frame, bool) {
	var m proto.MetaPayload
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		return f, false
	}
	if m.Summary == nil && m.CurrentCommand == "" && m.Cwd == "" && m.Title == "" {
		return f, false
	}
	m.Summary = nil
	m.CurrentCommand = ""
	m.Cwd = ""
	m.Title = ""
	payload, err := json.Marshal(m)
	if err != nil {
		return f, false
	}
	return proto.Frame{
		Type:      f.Type,
		SessionID: f.SessionID,
		Payload:   payload,
	}, true
}

// sealedMetaFields is the JSON document we encrypt into MetaPayload.Sealed.
// Mirrors sealedSessionFields (M3b-agent) but for the live TypeMeta path —
// the relay sees this as opaque ciphertext, clients with the matching
// account_key decrypt it and overlay the fields back onto MetaPayload.
type sealedMetaFields struct {
	Cwd            string `json:"cwd,omitempty"`
	Title          string `json:"title,omitempty"`
	CurrentCommand string `json:"current_command,omitempty"`
}

// sealMetaContentFields rewrites a TypeMeta frame's payload so the
// MetaPayload.Sealed field carries an AEAD envelope over the content-
// bearing fields. The plaintext fields stay populated; the caller is
// expected to invoke stripMetaContentFields immediately after to zero
// them out. Two-step split keeps the per-test surface tidy.
//
// Returns ok == false when there is nothing sensitive to seal, the
// session id can't be parsed, or any cipher operation fails — in any
// of those cases the caller forwards the frame unchanged.
func sealMetaContentFields(f proto.Frame, accountKey []byte) (proto.Frame, bool) {
	if len(accountKey) < e2eecrypto.SessionKeySize {
		return f, false
	}
	var m proto.MetaPayload
	if err := json.Unmarshal(f.Payload, &m); err != nil {
		return f, false
	}
	if m.Cwd == "" && m.Title == "" && m.CurrentCommand == "" {
		return f, false
	}
	sk, err := e2eecrypto.DeriveSessionKey(accountKey, f.SessionID)
	if err != nil {
		return f, false
	}
	body, err := json.Marshal(sealedMetaFields{
		Cwd:            m.Cwd,
		Title:          m.Title,
		CurrentCommand: m.CurrentCommand,
	})
	if err != nil {
		return f, false
	}
	env, err := e2eecrypto.SealUnsequenced(sk, f.SessionID, byte(proto.TypeMeta), body)
	if err != nil {
		return f, false
	}
	m.Sealed = env
	payload, err := json.Marshal(m)
	if err != nil {
		return f, false
	}
	return proto.Frame{
		Type:      f.Type,
		SessionID: f.SessionID,
		Payload:   payload,
	}, true
}

// looksLikeUnsequencedEnvelope is an alias for e2eecrypto.LooksLikeSealed,
// kept as a package-local name so the call sites in this file read the way
// they always did. Same heuristic on both the relay (inbound OUT) and the
// desktop (unsequenced IN / PASTE_IMAGE) paths.
var looksLikeUnsequencedEnvelope = e2eecrypto.LooksLikeSealed

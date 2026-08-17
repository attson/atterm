package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/google/uuid"
)

// profilesSyncSessionID is the fixed virtual session UUID used to derive the
// key and bind the AAD for session-profile sync. It is NOT a real session —
// just a stable namespace so the sealed blob is bound to this purpose. Do not
// change it or previously-synced blobs won't open. Generated fresh for this
// feature; deliberately distinct from sshHostsSyncSessionID (ssh_sync.go) so
// the two sealed namespaces can never collide.
var profilesSyncSessionID = uuid.MustParse("bbbac178-5e9b-45b4-8b79-3257d9af7ca5")

// SessionProfile is a named launch configuration for a new tab/split: shell,
// working directory, startup command, and environment variables. See design
// §4 (docs/superpowers/specs/2026-08-17-session-profiles-design.md).
type SessionProfile struct {
	ID         string            `json:"id"`                    // uuid, assigned at creation; sync and references key off it
	Name       string            `json:"name"`
	Shell      string            `json:"shell,omitempty"`       // empty = fall back to the global default_shell
	Cwd        string            `json:"cwd,omitempty"`         // empty = existing behavior (HOME)
	StartupCmd string            `json:"startup_cmd,omitempty"` // injected into the PTY after the first prompt
	Env        map[string]string `json:"env,omitempty"`
	SyncEnv    bool              `json:"sync_env,omitempty"` // default false = Env never leaves this machine
}

// stripUnsyncedEnv returns a copy of profiles where every profile with
// SyncEnv == false has its Env cleared. It never mutates the input slice or
// its profiles in place: the caller (the config store's live Profiles field)
// owns that Env map, and clearing it there would erase the user's own
// machine-local environment variables — the same data loss the merge rule in
// mergeProfiles exists to prevent, just triggered from the sending side
// instead of the receiving side.
//
// This is a separate step from sealProfiles rather than folded into it:
// sealProfiles seals exactly what it is handed, so a caller that already has
// a set of profiles it knows should travel as-is (e.g. a round trip in a
// test, or a future local-export path) is not forced through the SyncEnv
// gate. The outbound sync path (wired in Task 3's prefssync adapter) is
// expected to call stripUnsyncedEnv on its snapshot before sealProfiles.
func stripUnsyncedEnv(profiles []SessionProfile) []SessionProfile {
	out := make([]SessionProfile, len(profiles))
	for i, p := range profiles {
		if !p.SyncEnv {
			p.Env = nil
		}
		out[i] = p
	}
	return out
}

// sealProfiles packs the profile list and seals it with the account key.
// Callers that must honor SyncEnv (the real sync path) are expected to pass
// the result of stripUnsyncedEnv, not the raw config-store slice; sealProfiles
// itself does not second-guess what it is given. Returns (nil, nil) when
// accountKey is empty — the caller treats that as "skip sync" (local-only,
// never send plaintext to the relay).
func sealProfiles(accountKey []byte, profiles []SessionProfile) (json.RawMessage, error) {
	if len(accountKey) == 0 {
		return nil, nil
	}
	plain, err := json.Marshal(profiles)
	if err != nil {
		return nil, err
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, profilesSyncSessionID)
	if err != nil {
		return nil, err
	}
	ct, err := e2eecrypto.SealUnsequenced(sessionKey, profilesSyncSessionID, e2eecrypto.AADTagProfiles, plain)
	if err != nil {
		return nil, err
	}
	// Store as a JSON string of base64(ciphertext) so it round-trips as a
	// prefssync value.
	b64, err := json.Marshal(base64.StdEncoding.EncodeToString(ct))
	if err != nil {
		return nil, err
	}
	return b64, nil
}

// openProfiles decrypts a synced blob back into a profile list.
func openProfiles(accountKey []byte, value json.RawMessage) ([]SessionProfile, error) {
	var b64 string
	if err := json.Unmarshal(value, &b64); err != nil {
		return nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, profilesSyncSessionID)
	if err != nil {
		return nil, err
	}
	plain, err := e2eecrypto.OpenUnsequenced(sessionKey, profilesSyncSessionID, e2eecrypto.AADTagProfiles, ct)
	if err != nil {
		return nil, err
	}
	var profiles []SessionProfile
	if err := json.Unmarshal(plain, &profiles); err != nil {
		return nil, err
	}
	return profiles, nil
}

// mergeProfiles combines the local profile list with an incoming (pulled)
// one, per profile ID, applying design §5.1's env rule:
//
//   - incoming has Env       -> take incoming wholesale (source machine opted
//     into SyncEnv for this profile).
//   - incoming has no Env,
//     local has Env          -> keep local's Env, take every other field from
//     incoming. Without this branch, a profile
//     with SyncEnv == false looks identical on
//     the wire to one with no env at all, and a
//     single pull would silently wipe env this
//     machine set locally.
//   - ID absent locally      -> add it.
//   - ID absent from incoming -> the profile was deleted on the other
//     machine; delete it here too.
//
// Output order follows the incoming list (deletions are simply not emitted).
func mergeProfiles(local, incoming []SessionProfile) []SessionProfile {
	localByID := make(map[string]SessionProfile, len(local))
	for _, p := range local {
		localByID[p.ID] = p
	}
	out := make([]SessionProfile, 0, len(incoming))
	for _, in := range incoming {
		if len(in.Env) == 0 {
			if l, ok := localByID[in.ID]; ok && len(l.Env) > 0 {
				in.Env = l.Env
			}
		}
		out = append(out, in)
	}
	return out
}

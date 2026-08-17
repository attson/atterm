package main

import (
	"encoding/base64"
	"encoding/json"
	"strings"

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
// Called unconditionally from inside sealProfiles (see its comment) so that
// "env does not sync by default" is a guarantee no caller can accidentally
// skip, rather than a step every future caller has to remember to perform
// first. Kept as a named helper because it is independently useful to test
// and to reason about, not because any caller is expected to invoke it on
// its own.
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

// profilesSyncPayload is the wire shape sealed into the profiles_encrypted
// blob. DefaultProfileID rides alongside the profile list rather than
// getting a sealed key of its own: SetDefaultProfileID marks the very same
// "profiles_encrypted" key dirty as SetProfiles (see app.go), because from
// prefssync's point of view "which profiles exist" and "which one is the
// default" are one user-facing preference, not two. Splitting them would
// mean a second sealed key and a second AAD tag just to carry one string.
type profilesSyncPayload struct {
	Profiles         []SessionProfile `json:"profiles"`
	DefaultProfileID string           `json:"default_profile_id,omitempty"`
}

// sealProfiles packs the profile list and the default-profile selection and
// seals them with the account key.
//
// Strips the Env of every profile with SyncEnv == false before sealing,
// unconditionally — this is the one enforcement point for "env does not
// leave the machine unless the user opts in per profile" (design §5.1). It
// deliberately does not trust a caller to have stripped already: a relay
// that never sees plaintext env is only as strong as the one function on
// the path to it, and putting the guarantee here instead of in every future
// caller is the same reasoning as composeFontFamily always appending the
// CJK fallback chain rather than relying on call sites to remember it.
//
// stripUnsyncedEnv already returns a copy, so this never mutates the
// caller's profiles slice or its Env maps — matters more here than it would
// in a conditional helper, because every seal now goes through it.
//
// Returns (nil, nil) when accountKey is empty — the caller treats that as
// "skip sync" (local-only, never send plaintext to the relay).
func sealProfiles(accountKey []byte, profiles []SessionProfile, defaultProfileID string) (json.RawMessage, error) {
	if len(accountKey) == 0 {
		return nil, nil
	}
	payload := profilesSyncPayload{
		Profiles:         stripUnsyncedEnv(profiles),
		DefaultProfileID: defaultProfileID,
	}
	plain, err := json.Marshal(payload)
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

// openProfiles decrypts a synced blob back into a profile list and the
// default-profile selection that traveled with it.
func openProfiles(accountKey []byte, value json.RawMessage) ([]SessionProfile, string, error) {
	var b64 string
	if err := json.Unmarshal(value, &b64); err != nil {
		return nil, "", err
	}
	ct, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, "", err
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, profilesSyncSessionID)
	if err != nil {
		return nil, "", err
	}
	plain, err := e2eecrypto.OpenUnsequenced(sessionKey, profilesSyncSessionID, e2eecrypto.AADTagProfiles, ct)
	if err != nil {
		return nil, "", err
	}
	var payload profilesSyncPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, "", err
	}
	return payload.Profiles, payload.DefaultProfileID, nil
}

// filterValidProfiles drops entries that would corrupt this machine's
// profile list: empty ID, empty Name, or a duplicate ID within the same
// payload (first occurrence wins). SetProfiles enforces these same
// invariants for local edits (see app.go), but nothing validated an inbound
// (pulled) payload before this — a malformed or buggy remote client could
// put duplicate/empty-id profiles on the wire and WriteValue would store
// them as-is.
//
// Drops the offending entries and keeps the rest, rather than rejecting the
// whole payload: item 21 shipped broken through four review rounds because
// one missing relay-whitelist entry 400'd — and therefore silently
// dropped — every key in the same PUT batch. A malformed profile among ten
// good ones should cost the user that one profile, not stop the other nine
// (and every other synced key riding in the same pull) from working.
func filterValidProfiles(profiles []SessionProfile) []SessionProfile {
	seen := make(map[string]bool, len(profiles))
	out := make([]SessionProfile, 0, len(profiles))
	for _, p := range profiles {
		id := strings.TrimSpace(p.ID)
		name := strings.TrimSpace(p.Name)
		if id == "" || name == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, p)
	}
	return out
}

// resolveDefaultProfileID returns id if it names a profile present in
// profiles, or "" otherwise. Applied to an inbound default-profile
// selection so a dangling reference (e.g. the referenced profile was one of
// the entries filterValidProfiles dropped) never lingers in config.
func resolveDefaultProfileID(id string, profiles []SessionProfile) string {
	if id == "" {
		return ""
	}
	for _, p := range profiles {
		if p.ID == id {
			return id
		}
	}
	return ""
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
				// Copy, don't alias: in.Env = l.Env would give the merged
				// output the same map object the local slice holds. That was
				// harmless while every persist path happened to run through
				// configStore.Set() -> detachMaps() (which deep-copies again),
				// but a caller that inspects or mutates the merged result
				// before storing it — the pull handler in
				// prefssync_adapter.go is exactly that caller — could reuse
				// or corrupt `local`'s own env map through this alias.
				env := make(map[string]string, len(l.Env))
				for k, v := range l.Env {
					env[k] = v
				}
				in.Env = env
			}
		}
		out = append(out, in)
	}
	return out
}

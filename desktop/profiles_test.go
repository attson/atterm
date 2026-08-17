package main

import (
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/e2eecrypto"
)

func TestMergeProfilesEnvRules(t *testing.T) {
	env := map[string]string{"FOO": "bar"}

	t.Run("incoming env wins when present", func(t *testing.T) {
		local := []SessionProfile{{ID: "a", Name: "A", Env: map[string]string{"OLD": "1"}}}
		incoming := []SessionProfile{{ID: "a", Name: "A", Env: env}}
		got := mergeProfiles(local, incoming)
		if got[0].Env["FOO"] != "bar" || got[0].Env["OLD"] != "" {
			t.Errorf("incoming env must replace local: %v", got[0].Env)
		}
	})

	t.Run("local env survives when incoming has none", func(t *testing.T) {
		local := []SessionProfile{{ID: "a", Name: "A", Env: env}}
		incoming := []SessionProfile{{ID: "a", Name: "A-renamed"}}
		got := mergeProfiles(local, incoming)
		if got[0].Env["FOO"] != "bar" {
			t.Error("an unsynced env must not be cleared by a pull that carries none")
		}
		if got[0].Name != "A-renamed" {
			t.Error("non-env fields must still take the incoming value")
		}
	})

	t.Run("profile absent locally is added", func(t *testing.T) {
		got := mergeProfiles(nil, []SessionProfile{{ID: "b", Name: "B"}})
		if len(got) != 1 || got[0].ID != "b" {
			t.Errorf("got %v", got)
		}
	})

	t.Run("profile absent from incoming is deleted", func(t *testing.T) {
		local := []SessionProfile{{ID: "a"}, {ID: "b"}}
		got := mergeProfiles(local, []SessionProfile{{ID: "a"}})
		if len(got) != 1 || got[0].ID != "a" {
			t.Errorf("a profile deleted on the other machine must go away here too: %v", got)
		}
	})

	// TestMergeProfilesDoesNotAliasLocalEnv guards the Task 2 review finding
	// carried into Task 3: mergeProfiles used to assign in.Env = l.Env,
	// handing the merged output the exact map object `local` holds. Mutating
	// the merged result's Env must never be visible through `local` — a
	// caller (WriteValue's pull handler) that reads `local` again after
	// calling mergeProfiles and before storing the result must see its own
	// untouched map.
	t.Run("does not alias local's env map", func(t *testing.T) {
		local := []SessionProfile{{ID: "a", Name: "A", Env: map[string]string{"FOO": "bar"}}}
		incoming := []SessionProfile{{ID: "a", Name: "A-renamed"}}
		got := mergeProfiles(local, incoming)
		got[0].Env["FOO"] = "mutated"
		if local[0].Env["FOO"] != "bar" {
			t.Errorf("mergeProfiles aliased local's Env map: mutating the merged output changed local to %v", local[0].Env)
		}
	})
}

func TestSealProfilesSkipsWithoutAccountKey(t *testing.T) {
	blob, err := sealProfiles(nil, []SessionProfile{{ID: "a"}}, "")
	if err != nil || blob != nil {
		t.Fatalf("no account key must mean skip-sync, never plaintext: blob=%v err=%v", blob, err)
	}
}

func TestSealProfilesRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	// SyncEnv: true — this is the only case where Env is expected to survive
	// sealing. Without it, sealProfiles strips Env before it ever reaches the
	// wire (see TestSealProfilesStripsEnvWhenSyncEnvFalse below).
	in := []SessionProfile{{ID: "a", Name: "Work", Shell: "/bin/zsh", Env: map[string]string{"K": "V"}, SyncEnv: true}}
	blob, err := sealProfiles(key, in, "a")
	if err != nil || blob == nil {
		t.Fatalf("seal: %v", err)
	}
	out, defaultID, err := openProfiles(key, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Work" || out[0].Env["K"] != "V" {
		t.Errorf("round trip lost data: %+v", out)
	}
	if defaultID != "a" {
		t.Errorf("default profile id lost in round trip: got %q, want %q", defaultID, "a")
	}
}

// TestSealProfilesStripsEnvWhenSyncEnvFalse is the mirror of the round trip
// above and the whole point of the SyncEnv flag: a profile that has not
// opted in must never have its Env reach the sealed blob, let alone the
// relay. sealProfiles enforces this itself rather than trusting a caller to
// strip first — see its doc comment.
func TestSealProfilesStripsEnvWhenSyncEnvFalse(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	in := []SessionProfile{{ID: "a", Name: "Work", Env: map[string]string{"SECRET": "token"}, SyncEnv: false}}
	blob, err := sealProfiles(key, in, "")
	if err != nil || blob == nil {
		t.Fatalf("seal: %v", err)
	}
	out, _, err := openProfiles(key, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Work" {
		t.Fatalf("unexpected profiles: %+v", out)
	}
	if len(out[0].Env) != 0 {
		t.Errorf("SyncEnv:false must never let Env reach the sealed blob, got %v", out[0].Env)
	}
}

// TestSealProfilesDoesNotMutateCaller guards the same data-loss shape as
// TestStripUnsyncedEnvDoesNotMutateCaller, but at the sealProfiles boundary:
// now that every seal strips unconditionally, an in-place mutation here would
// wipe the user's own machine-local env on the sending side.
func TestSealProfilesDoesNotMutateCaller(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	env := map[string]string{"FOO": "bar"}
	in := []SessionProfile{{ID: "a", Name: "Work", Env: env, SyncEnv: false}}
	if _, err := sealProfiles(key, in, ""); err != nil {
		t.Fatalf("seal: %v", err)
	}
	if in[0].Env == nil || in[0].Env["FOO"] != "bar" {
		t.Errorf("sealProfiles must not clear Env on the caller's own slice/profile: %v", in[0].Env)
	}
}

func TestStripUnsyncedEnvDoesNotMutateCaller(t *testing.T) {
	env := map[string]string{"FOO": "bar"}
	in := []SessionProfile{{ID: "a", Name: "A", Env: env, SyncEnv: false}}
	out := stripUnsyncedEnv(in)
	if out[0].Env != nil {
		t.Errorf("expected stripped Env in the output copy, got %v", out[0].Env)
	}
	if in[0].Env == nil || in[0].Env["FOO"] != "bar" {
		t.Errorf("stripUnsyncedEnv must not clear Env on the caller's own slice/profile: %v", in[0].Env)
	}
}

// TestOpenProfilesRejectsWrongAADTag isolates the AAD-tag discriminator as
// the only variable: same account key, same profilesSyncSessionID (so
// DeriveSessionKey yields exactly the session key openProfiles itself will
// derive), only the AAD tag differs (AADTagSSHHosts instead of
// AADTagProfiles). This is the direct check of the control redline #22
// exists to enforce.
//
// The previous version sealed under sshHostsSyncSessionID and opened under
// profilesSyncSessionID — two different UUIDs — so DeriveSessionKey already
// produced two different session keys before the AAD tag ever entered the
// picture. That version would still have passed even if AADTagSSHHosts and
// AADTagProfiles shared the same byte value, because the key mismatch alone
// is enough to make OpenUnsequenced fail; it tested "different session ⇒
// different key" (true but uninteresting here), not "same session, wrong
// tag ⇒ rejected" (the actual guarantee the discriminator byte provides).
func TestOpenProfilesRejectsWrongAADTag(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(key, profilesSyncSessionID)
	if err != nil {
		t.Fatalf("DeriveSessionKey: %v", err)
	}
	payload := profilesSyncPayload{Profiles: []SessionProfile{{ID: "a", Name: "A"}}}
	plain, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	// Seal under the *profiles* session key/id, but with the *ssh_hosts* AAD
	// tag — everything openProfiles will derive matches except the one byte
	// under test.
	ct, err := e2eecrypto.SealUnsequenced(sessionKey, profilesSyncSessionID, e2eecrypto.AADTagSSHHosts, plain)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	b64, err := json.Marshal(base64.StdEncoding.EncodeToString(ct))
	if err != nil {
		t.Fatalf("marshal b64: %v", err)
	}
	if _, _, err := openProfiles(key, b64); err == nil {
		t.Error("an envelope sealed with AADTagSSHHosts must not open with openProfiles' AADTagProfiles, even under the identical session key/id")
	}
}

func TestFilterValidProfilesDropsMalformedEntriesKeepsRest(t *testing.T) {
	in := []SessionProfile{
		{ID: "a", Name: "Good A"},
		{ID: "", Name: "No id"},
		{ID: "b", Name: ""},
		{ID: "a", Name: "Duplicate of a"},
		{ID: "c", Name: "Good C"},
	}
	got := filterValidProfiles(in)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "c" {
		t.Errorf("expected only the two valid, first-occurrence entries to survive, got %+v", got)
	}
}

func TestResolveDefaultProfileID(t *testing.T) {
	profiles := []SessionProfile{{ID: "a"}, {ID: "b"}}
	if got := resolveDefaultProfileID("a", profiles); got != "a" {
		t.Errorf("known id must survive: got %q", got)
	}
	if got := resolveDefaultProfileID("missing", profiles); got != "" {
		t.Errorf("dangling id must be dropped: got %q", got)
	}
	if got := resolveDefaultProfileID("", profiles); got != "" {
		t.Errorf("empty id must stay empty: got %q", got)
	}
}

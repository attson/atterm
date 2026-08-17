package main

import "testing"

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
}

func TestSealProfilesSkipsWithoutAccountKey(t *testing.T) {
	blob, err := sealProfiles(nil, []SessionProfile{{ID: "a"}})
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
	blob, err := sealProfiles(key, in)
	if err != nil || blob == nil {
		t.Fatalf("seal: %v", err)
	}
	out, err := openProfiles(key, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(out) != 1 || out[0].Name != "Work" || out[0].Env["K"] != "V" {
		t.Errorf("round trip lost data: %+v", out)
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
	blob, err := sealProfiles(key, in)
	if err != nil || blob == nil {
		t.Fatalf("seal: %v", err)
	}
	out, err := openProfiles(key, blob)
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
	if _, err := sealProfiles(key, in); err != nil {
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

func TestOpenProfilesRejectsWrongAADTag(t *testing.T) {
	// Sealing under the SSH namespace and opening under the profiles one must
	// fail — that is the whole point of a per-namespace discriminator byte.
	key := make([]byte, 32)
	sealed, err := sealSSHHosts(key, nil, nil, nil, nil)
	if err != nil || sealed == nil {
		t.Skip("ssh seal unavailable in this shape")
	}
	if _, err := openProfiles(key, sealed); err == nil {
		t.Error("an ssh_hosts envelope must not open as a profiles envelope")
	}
}

package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// fixtureAccountKey is a FIXED (not random) 32-byte key, byte i == i, so the
// TS side can hardcode the exact same bytes and open what this test seals.
// Same pattern as internal/proto/fs_vectors_test.go's vectorAccountKey.
func fixtureAccountKey() []byte {
	k := make([]byte, 32)
	for i := range k {
		k[i] = byte(i)
	}
	return k
}

// syncedBlobVectorsFixture is the shape written to testdata/synced_blob_vectors.json
// and read by desktop/frontend/src/lib/syncedBlobs.test.ts. Field names are
// snake_case to match every other value on this wire (prefs-sync values,
// profilesSyncPayload, sshSyncPayload).
type syncedBlobVectorsFixture struct {
	AccountKeyB64 string `json:"account_key_b64"`
	ProfilesValue string `json:"profiles_value"`
	SSHHostsValue string `json:"ssh_hosts_value"`
}

// TestGenerateSyncedBlobVectors seals a fixed profiles payload and a fixed
// ssh-hosts payload with a fixed account key using the real sealProfiles /
// sealSSHHosts production code, and writes both resulting prefs-sync values
// (the plain base64 string a mobile client actually receives as
// ServerItem.value — see appConfigAdapter.ReadValue) to
// desktop/testdata/synced_blob_vectors.json.
//
// This file is committed and is the cross-language contract for
// desktop/frontend/src/lib/syncedBlobs.ts: the Go and TS envelope
// implementations are written independently, and only a fixture produced by
// one and opened by the other proves they agree on session-key derivation,
// AAD layout, and envelope framing (see design §2). A hand-written fixture
// would only encode what someone believed the format to be.
//
// Re-run `go test ./desktop/... -run TestGenerateSyncedBlobVectors` and
// commit the resulting testdata/synced_blob_vectors.json after any change to
// sealProfiles, sealSSHHosts, profilesSyncSessionID, sshHostsSyncSessionID,
// AADTagProfiles, or AADTagSSHHosts. If you forget, the TS test fails on its
// next run — that failure is the point, not a bug in the fixture.
func TestGenerateSyncedBlobVectors(t *testing.T) {
	key := fixtureAccountKey()

	// Two profiles: one opted into env sync, one not. The second exercises
	// stripUnsyncedEnv — its Env must NOT appear in the sealed payload at
	// all, which is exactly the fact the TS ProfileView type has to expose
	// to callers (design §2's "not synced from that machine" vs "no env
	// vars set").
	profiles := []SessionProfile{
		{
			ID:         "p-synced",
			Name:       "Synced Profile",
			Shell:      "/bin/zsh",
			Cwd:        "/home/u/work",
			StartupCmd: "tmux attach",
			Env:        map[string]string{"FOO": "bar"},
			SyncEnv:    true,
		},
		{
			ID:      "p-unsynced",
			Name:    "Unsynced Env Profile",
			Shell:   "/bin/bash",
			Env:     map[string]string{"SECRET": "CANARY-env-must-not-leak"},
			SyncEnv: false,
		},
	}
	profilesBlob, err := sealProfiles(key, profiles, "p-synced")
	if err != nil {
		t.Fatalf("sealProfiles: %v", err)
	}
	var profilesValue string
	if err := json.Unmarshal(profilesBlob, &profilesValue); err != nil {
		t.Fatalf("unmarshal profiles blob (expected a JSON string): %v", err)
	}

	// Two hosts (one with a jump chain, referencing a key) and one key with
	// a credential/secret attached, so the fixture exercises the
	// credential-stripping assertion on the TS side: sealSSHHosts bundles
	// sshCredential/sshKeySecret into the sealed payload, and the reader
	// must not surface them.
	hosts := []SSHHost{
		{
			ID:       "h1",
			Alias:    "box1",
			Host:     "box1.example.com",
			Port:     "22",
			User:     "root",
			AuthKind: "password",
			Tags:     []string{"prod"},
			Note:     "primary",
		},
		{
			ID:        "h2",
			Alias:     "jump-target",
			Host:      "10.0.0.5",
			User:      "deploy",
			AuthKind:  "key",
			KeyID:     "k1",
			ProxyJump: "box1",
		},
	}
	creds := map[string]sshCredential{
		"h1": {Password: "CANARY-password-must-not-leak"},
	}
	keys := []SSHKey{
		{ID: "k1", Name: "deploy-key", KeyType: "ED25519"},
	}
	keySecrets := map[string]sshKeySecret{
		"k1": {PrivateKey: "CANARY-private-key-must-not-leak", Passphrase: "CANARY-passphrase-must-not-leak"},
	}
	sshBlob, err := sealSSHHosts(key, hosts, creds, keys, keySecrets)
	if err != nil {
		t.Fatalf("sealSSHHosts: %v", err)
	}
	var sshValue string
	if err := json.Unmarshal(sshBlob, &sshValue); err != nil {
		t.Fatalf("unmarshal ssh hosts blob (expected a JSON string): %v", err)
	}

	fixture := syncedBlobVectorsFixture{
		AccountKeyB64: base64.StdEncoding.EncodeToString(key),
		ProfilesValue: profilesValue,
		SSHHostsValue: sshValue,
	}
	out, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.MkdirAll("testdata", 0o755); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	path := filepath.Join("testdata", "synced_blob_vectors.json")
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

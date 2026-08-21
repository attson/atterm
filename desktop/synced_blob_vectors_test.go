package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// updateSyncedBlobVectors gates regeneration of testdata/synced_blob_vectors.json.
// Default (false): TestSyncedBlobVectors only OPENS the committed fixture and
// asserts its decoded contents — no write, so a plain `go test` never dirties
// the tree. Pass -update to reseal the fixed payloads below with the current
// sealProfiles/sealSSHHosts and overwrite the file; review the diff and
// commit it.
//
//	go test ./desktop/ -run TestSyncedBlobVectors -update
var updateSyncedBlobVectors = flag.Bool("update", false, "regenerate desktop/testdata/synced_blob_vectors.json from the current sealProfiles/sealSSHHosts")

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

// fixtureDefaultProfileID is the default-profile selection sealed alongside
// fixtureProfiles().
const fixtureDefaultProfileID = "p-synced"

// fixtureProfiles is the fixed input sealed into the committed fixture's
// profiles_value. Two profiles: one opted into env sync, one not. The
// second exercises stripUnsyncedEnv — its Env must NOT appear in the sealed
// payload at all, which is exactly the fact the TS ProfileView type has to
// expose to callers (design §2's "not synced from that machine" vs "no env
// vars set"). TestSyncedBlobVectors asserts the decoded result equals
// stripUnsyncedEnv(fixtureProfiles()), not fixtureProfiles() itself.
func fixtureProfiles() []SessionProfile {
	return []SessionProfile{
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
}

// fixtureSSHHosts/fixtureSSHCreds/fixtureSSHKeys/fixtureSSHKeySecrets are the
// fixed input sealed into the committed fixture's ssh_hosts_value. Two hosts
// (one with a jump chain, referencing a key) and one key with a
// credential/secret attached, so the fixture exercises the
// credential-stripping assertion on the TS side: sealSSHHosts bundles
// sshCredential/sshKeySecret into the sealed payload, and the reader must
// not surface them. The CANARY values let syncedBlobs.test.ts assert their
// literal absence from anything the reader returns, not just from the view
// type's declared fields.
func fixtureSSHHosts() []SSHHost {
	return []SSHHost{
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
}

func fixtureSSHCreds() map[string]sshCredential {
	return map[string]sshCredential{
		"h1": {Password: "CANARY-password-must-not-leak"},
	}
}

func fixtureSSHKeys() []SSHKey {
	return []SSHKey{
		{ID: "k1", Name: "deploy-key", KeyType: "ED25519"},
	}
}

func fixtureSSHKeySecrets() map[string]sshKeySecret {
	return map[string]sshKeySecret{
		"k1": {PrivateKey: "CANARY-private-key-must-not-leak", Passphrase: "CANARY-passphrase-must-not-leak"},
	}
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

func syncedBlobVectorsFixturePath() string {
	return filepath.Join("testdata", "synced_blob_vectors.json")
}

// regenerateSyncedBlobVectors seals fixtureProfiles()/fixtureSSHHosts() (etc)
// with fixtureAccountKey() using the real production sealProfiles /
// sealSSHHosts and overwrites testdata/synced_blob_vectors.json. Only runs
// under -update — see updateSyncedBlobVectors.
func regenerateSyncedBlobVectors(t *testing.T, key []byte) {
	t.Helper()

	profilesBlob, err := sealProfiles(key, fixtureProfiles(), fixtureDefaultProfileID)
	if err != nil {
		t.Fatalf("sealProfiles: %v", err)
	}
	var profilesValue string
	if err := json.Unmarshal(profilesBlob, &profilesValue); err != nil {
		t.Fatalf("unmarshal profiles blob (expected a JSON string): %v", err)
	}

	sshBlob, err := sealSSHHosts(key, fixtureSSHHosts(), fixtureSSHCreds(), fixtureSSHKeys(), fixtureSSHKeySecrets())
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
	path := syncedBlobVectorsFixturePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, append(out, '\n'), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	t.Logf("regenerated %s from the current sealProfiles/sealSSHHosts — review the diff and commit it", path)
}

// asJSONStringValue re-wraps a plain string as the json.RawMessage shape
// openProfiles/openSSHHosts expect for their `value` argument: a JSON string
// token, e.g. `"AbCd=="`. Mirrors what appConfigAdapter.ReadValue's
// json.RawMessage already looks like on the wire.
func asJSONStringValue(t *testing.T, s string) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("marshal %q as a JSON string: %v", s, err)
	}
	return b
}

// TestSyncedBlobVectors is the cross-language contract for
// desktop/frontend/src/lib/syncedBlobs.ts: the Go and TS envelope
// implementations are written independently, and only a fixture produced by
// one and opened by the other proves they agree on session-key derivation,
// AAD layout, and envelope framing (see design §2). A hand-written fixture
// would only encode what someone believed the format to be.
//
// By default this test does NOT write anything — it opens the committed
// testdata/synced_blob_vectors.json with the current openProfiles /
// openSSHHosts and asserts the decoded contents match fixtureProfiles() /
// fixtureSSHHosts() (etc) exactly. That makes drift fail HERE, on the Go
// side, immediately and deterministically: a changed AAD tag or session
// UUID makes decryption fail outright; a renamed JSON field decrypts fine
// but silently zeroes that field, which the equality check below catches.
// It does not depend on anyone remembering to also run the TS suite.
//
// Run with -update to reseal the fixed payloads and overwrite the fixture
// after an intentional envelope change; review the diff before committing.
func TestSyncedBlobVectors(t *testing.T) {
	key := fixtureAccountKey()

	if *updateSyncedBlobVectors {
		regenerateSyncedBlobVectors(t, key)
	}

	path := syncedBlobVectorsFixturePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v\nRun `go test ./desktop/ -run TestSyncedBlobVectors -update` to create it, then commit the result.", path, err)
	}
	var fixture syncedBlobVectorsFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	if fixture.AccountKeyB64 != base64.StdEncoding.EncodeToString(key) {
		t.Fatalf("fixture account_key_b64 does not match fixtureAccountKey(); regenerate with -update")
	}

	// ---- profiles_encrypted ----
	gotProfiles, gotDefaultID, err := openProfiles(key, asJSONStringValue(t, fixture.ProfilesValue))
	if err != nil {
		t.Fatalf("openProfiles could not open the committed fixture: %v\n"+
			"If you changed sealProfiles, profilesSyncSessionID, or AADTagProfiles, "+
			"regenerate with `go test ./desktop/ -run TestSyncedBlobVectors -update` and commit the result.", err)
	}
	if gotDefaultID != fixtureDefaultProfileID {
		t.Errorf("default_profile_id = %q, want %q", gotDefaultID, fixtureDefaultProfileID)
	}
	wantProfiles := stripUnsyncedEnv(fixtureProfiles())
	if !reflect.DeepEqual(gotProfiles, wantProfiles) {
		t.Errorf("decoded profiles do not match stripUnsyncedEnv(fixtureProfiles()):\n got  %#v\n want %#v", gotProfiles, wantProfiles)
	}

	// ---- ssh_hosts_encrypted ----
	gotHosts, gotCreds, gotKeys, gotSecrets, err := openSSHHosts(key, asJSONStringValue(t, fixture.SSHHostsValue))
	if err != nil {
		t.Fatalf("openSSHHosts could not open the committed fixture: %v\n"+
			"If you changed sealSSHHosts, sshHostsSyncSessionID, or AADTagSSHHosts, "+
			"regenerate with `go test ./desktop/ -run TestSyncedBlobVectors -update` and commit the result.", err)
	}
	if !reflect.DeepEqual(gotHosts, fixtureSSHHosts()) {
		t.Errorf("decoded hosts do not match fixtureSSHHosts():\n got  %#v\n want %#v", gotHosts, fixtureSSHHosts())
	}
	// openSSHHosts populates one creds entry per host (openSSHHosts,
	// desktop/ssh_sync.go), zero-valued for any host fixtureSSHCreds()
	// doesn't mention — mirror that expansion here rather than comparing
	// against fixtureSSHCreds() directly.
	wantCreds := make(map[string]sshCredential, len(fixtureSSHHosts()))
	for _, h := range fixtureSSHHosts() {
		wantCreds[h.ID] = fixtureSSHCreds()[h.ID]
	}
	if !reflect.DeepEqual(gotCreds, wantCreds) {
		t.Errorf("decoded creds do not match the expected per-host map:\n got  %#v\n want %#v", gotCreds, wantCreds)
	}
	if !reflect.DeepEqual(gotKeys, fixtureSSHKeys()) {
		t.Errorf("decoded keys do not match fixtureSSHKeys():\n got  %#v\n want %#v", gotKeys, fixtureSSHKeys())
	}
	if !reflect.DeepEqual(gotSecrets, fixtureSSHKeySecrets()) {
		t.Errorf("decoded key secrets do not match fixtureSSHKeySecrets():\n got  %#v\n want %#v", gotSecrets, fixtureSSHKeySecrets())
	}
}

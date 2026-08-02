package main

import (
	"encoding/base64"
	"encoding/json"

	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/google/uuid"
)

// sshHostsSyncSessionID is the fixed virtual session UUID used to derive the
// key and bind the AAD for host-list sync. It is NOT a real session — just a
// stable namespace so the sealed blob is bound to this purpose. Do not change
// it or previously-synced blobs won't open.
var sshHostsSyncSessionID = uuid.MustParse("55480000-0000-4000-8000-000000000001")

// sshSyncFrameType is an AAD-only tag for host-list sync seals. It never
// appears on the relay wire (not a proto frame type) — it only binds the AAD.
const sshSyncFrameType = 0xF0

type sshSyncPayload struct {
	Hosts []sshSyncHost `json:"hosts"`
}

type sshSyncHost struct {
	Host SSHHost       `json:"host"`
	Cred sshCredential `json:"cred"`
}

// sealSSHHosts packs the host list + credentials and seals it with the account
// key. Returns (nil, nil) when accountKey is empty — the caller treats that as
// "skip sync" (local-only, never send plaintext to the relay).
func sealSSHHosts(accountKey []byte, hosts []SSHHost, creds map[string]sshCredential) (json.RawMessage, error) {
	if len(accountKey) == 0 {
		return nil, nil
	}
	payload := sshSyncPayload{}
	for _, h := range hosts {
		payload.Hosts = append(payload.Hosts, sshSyncHost{Host: h, Cred: creds[h.ID]})
	}
	plain, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, sshHostsSyncSessionID)
	if err != nil {
		return nil, err
	}
	ct, err := e2eecrypto.SealUnsequenced(sessionKey, sshHostsSyncSessionID, sshSyncFrameType, plain)
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

// openSSHHosts decrypts a synced blob back into hosts + credentials keyed by
// host ID.
func openSSHHosts(accountKey []byte, value json.RawMessage) ([]SSHHost, map[string]sshCredential, error) {
	var b64 string
	if err := json.Unmarshal(value, &b64); err != nil {
		return nil, nil, err
	}
	ct, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, nil, err
	}
	sessionKey, err := e2eecrypto.DeriveSessionKey(accountKey, sshHostsSyncSessionID)
	if err != nil {
		return nil, nil, err
	}
	plain, err := e2eecrypto.OpenUnsequenced(sessionKey, sshHostsSyncSessionID, sshSyncFrameType, ct)
	if err != nil {
		return nil, nil, err
	}
	var payload sshSyncPayload
	if err := json.Unmarshal(plain, &payload); err != nil {
		return nil, nil, err
	}
	hosts := make([]SSHHost, 0, len(payload.Hosts))
	creds := make(map[string]sshCredential, len(payload.Hosts))
	for _, sh := range payload.Hosts {
		hosts = append(hosts, sh.Host)
		creds[sh.Host.ID] = sh.Cred
	}
	return hosts, creds, nil
}

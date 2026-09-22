package peertransport

import (
	"bytes"
	"crypto/ecdh"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
)

func fixedKey(t *testing.T, last byte) *ecdh.PrivateKey {
	t.Helper()
	raw := make([]byte, 32)
	raw[len(raw)-1] = last
	key, err := ecdh.P256().NewPrivateKey(raw)
	if err != nil {
		t.Fatalf("NewPrivateKey: %v", err)
	}
	return key
}

func testTranscript(t *testing.T) (Transcript, []byte, *ecdh.PrivateKey, *ecdh.PrivateKey) {
	t.Helper()
	client := fixedKey(t, 1)
	host := fixedKey(t, 2)
	claims := Transcript{
		AttemptID:           uuid.MustParse("11111111-2222-4333-8444-555555555555"),
		Ticket:              bytes.Repeat([]byte{0xa5}, 32),
		SessionID:           uuid.MustParse("aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"),
		UserID:              "user-123",
		HostID:              "host-macbook",
		ClientInstanceID:    "client-iphone",
		Permission:          PermissionControl,
		ExpiresAtUnixMillis: 1_800_000_000_123,
		ClientPublicKey:     client.PublicKey().Bytes(),
		HostPublicKey:       host.PublicKey().Bytes(),
	}
	wire, err := claims.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary: %v", err)
	}
	return claims, wire, client, host
}

func TestTranscriptAndKeySchedule(t *testing.T) {
	_, transcript, client, host := testTranscript(t)
	accountKey := bytes.Repeat([]byte{0x42}, 32)
	auth, err := NewAccountKeyAuthenticator(accountKey)
	if err != nil {
		t.Fatal(err)
	}
	clientProof, err := auth.BuildProof(transcript, RoleClient)
	if err != nil {
		t.Fatal(err)
	}
	hostProof, err := auth.BuildProof(transcript, RoleHost)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(clientProof, hostProof) {
		t.Fatal("role-separated proofs are equal")
	}
	if err := auth.VerifyProof(transcript, RoleClient, clientProof); err != nil {
		t.Fatalf("VerifyProof: %v", err)
	}
	if err := auth.VerifyProof(transcript, RoleHost, clientProof); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("client proof accepted as host proof: %v", err)
	}

	clientKeys, err := DeriveTrafficKeys(client, host.PublicKey().Bytes(), transcript, auth)
	if err != nil {
		t.Fatal(err)
	}
	hostKeys, err := DeriveTrafficKeys(host, client.PublicKey().Bytes(), transcript, auth)
	if err != nil {
		t.Fatal(err)
	}
	if clientKeys != hostKeys {
		t.Fatal("ECDH endpoints derived different traffic keys")
	}
	if clientKeys.ClientToHostKey == clientKeys.HostToClientKey {
		t.Fatal("direction keys are equal")
	}
}

func TestWrongAccountKeyAndTranscriptClaimsFailProof(t *testing.T) {
	claims, transcript, _, _ := testTranscript(t)
	auth, _ := NewAccountKeyAuthenticator(bytes.Repeat([]byte{0x42}, 32))
	proof, _ := auth.BuildProof(transcript, RoleClient)

	wrongAuth, _ := NewAccountKeyAuthenticator(bytes.Repeat([]byte{0x43}, 32))
	if err := wrongAuth.VerifyProof(transcript, RoleClient, proof); !errors.Is(err, ErrAuthentication) {
		t.Fatalf("wrong account key accepted: %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*Transcript)
	}{
		{"session", func(v *Transcript) { v.SessionID = uuid.New() }},
		{"ticket", func(v *Transcript) { v.Ticket = bytes.Repeat([]byte{0xa6}, 32) }},
		{"expiry", func(v *Transcript) { v.ExpiresAtUnixMillis++ }},
		{"public key", func(v *Transcript) { v.HostPublicKey = append([]byte(nil), v.ClientPublicKey...) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changedClaims := claims
			tt.mutate(&changedClaims)
			changed, err := changedClaims.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			if err := auth.VerifyProof(changed, RoleClient, proof); !errors.Is(err, ErrAuthentication) {
				t.Fatalf("changed transcript accepted: %v", err)
			}
		})
	}
}

func TestTranscriptRejectsMalformedInputs(t *testing.T) {
	claims, _, _, _ := testTranscript(t)
	tests := []struct {
		name   string
		mutate func(*Transcript)
	}{
		{"short ticket", func(v *Transcript) { v.Ticket = v.Ticket[:31] }},
		{"nil session", func(v *Transcript) { v.SessionID = uuid.Nil }},
		{"empty host", func(v *Transcript) { v.HostID = "" }},
		{"bad permission", func(v *Transcript) { v.Permission = 9 }},
		{"zero expiry", func(v *Transcript) { v.ExpiresAtUnixMillis = 0 }},
		{"bad public key", func(v *Transcript) { v.HostPublicKey = bytes.Repeat([]byte{1}, 65) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := claims
			tt.mutate(&changed)
			if _, err := changed.MarshalBinary(); !errors.Is(err, ErrInvalidTranscript) {
				t.Fatalf("got %v", err)
			}
		})
	}
}

func TestGoldenValuesAreStable(t *testing.T) {
	type goldenVector struct {
		AccountKeyHex        string `json:"account_key_hex"`
		ClientPrivateKeyHex  string `json:"client_private_key_hex"`
		HostPrivateKeyHex    string `json:"host_private_key_hex"`
		ClientPublicKeyHex   string `json:"client_public_key_hex"`
		HostPublicKeyHex     string `json:"host_public_key_hex"`
		TranscriptHex        string `json:"transcript_hex"`
		ClientProofHex       string `json:"client_proof_hex"`
		HostProofHex         string `json:"host_proof_hex"`
		FinishProofHex       string `json:"finish_proof_hex"`
		ClientToHostKeyHex   string `json:"client_to_host_key_hex"`
		HostToClientKeyHex   string `json:"host_to_client_key_hex"`
		ClientNoncePrefixHex string `json:"client_nonce_prefix_hex"`
		HostNoncePrefixHex   string `json:"host_nonce_prefix_hex"`
		RecordPlaintextHex   string `json:"record_plaintext_hex"`
		RecordHex            string `json:"record_hex"`
	}
	raw, err := os.ReadFile("testdata/direct_crypto_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector goldenVector
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}

	_, transcript, client, host := testTranscript(t)
	auth, _ := NewAccountKeyAuthenticator(bytes.Repeat([]byte{0x42}, 32))
	clientProof, _ := auth.BuildProof(transcript, RoleClient)
	hostProof, _ := auth.BuildProof(transcript, RoleHost)
	finishProof, _ := BuildFinishProof(auth, transcript)
	keys, _ := DeriveTrafficKeys(client, host.PublicKey().Bytes(), transcript, auth)
	sealer, _ := NewRecordSealer(keys.ClientToHostKey[:], keys.ClientToHostNoncePrefix[:], TranscriptHash(transcript))
	recordPlaintext, err := hex.DecodeString(vector.RecordPlaintextHex)
	if err != nil {
		t.Fatal(err)
	}
	record, _ := sealer.Seal(RecordFrame, recordPlaintext)

	vectors := map[string]struct {
		got  []byte
		want string
	}{
		"account_key":         {bytes.Repeat([]byte{0x42}, 32), vector.AccountKeyHex},
		"client_private":      {client.Bytes(), vector.ClientPrivateKeyHex},
		"host_private":        {host.Bytes(), vector.HostPrivateKeyHex},
		"transcript":          {transcript, vector.TranscriptHex},
		"client_public":       {client.PublicKey().Bytes(), vector.ClientPublicKeyHex},
		"host_public":         {host.PublicKey().Bytes(), vector.HostPublicKeyHex},
		"client_proof":        {clientProof, vector.ClientProofHex},
		"host_proof":          {hostProof, vector.HostProofHex},
		"finish_proof":        {finishProof, vector.FinishProofHex},
		"client_to_host_key":  {keys.ClientToHostKey[:], vector.ClientToHostKeyHex},
		"host_to_client_key":  {keys.HostToClientKey[:], vector.HostToClientKeyHex},
		"client_nonce_prefix": {keys.ClientToHostNoncePrefix[:], vector.ClientNoncePrefixHex},
		"host_nonce_prefix":   {keys.HostToClientNoncePrefix[:], vector.HostNoncePrefixHex},
		"record":              {record, vector.RecordHex},
	}
	for name, vector := range vectors {
		if got := hex.EncodeToString(vector.got); got != vector.want {
			t.Errorf("%s = %s", name, got)
		}
	}
}

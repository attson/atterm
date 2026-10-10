package peertransport

import (
	"bytes"
	"crypto/ecdh"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

type peerAuthGoldenVector struct {
	GenesisHash          string `json:"genesis_hash"`
	ClientMembership     string `json:"client_membership"`
	HostMembership       string `json:"host_membership"`
	ClientPrivateKeyHex  string `json:"client_private_key_hex"`
	HostPrivateKeyHex    string `json:"host_private_key_hex"`
	TranscriptHex        string `json:"transcript_hex"`
	BindingHex           string `json:"binding_hex"`
	FinishProofHex       string `json:"finish_proof_hex"`
	ClientToHostKeyHex   string `json:"client_to_host_key_hex"`
	HostToClientKeyHex   string `json:"host_to_client_key_hex"`
	ClientNoncePrefixHex string `json:"client_nonce_prefix_hex"`
	HostNoncePrefixHex   string `json:"host_nonce_prefix_hex"`
}

func TestPeerAuthGoldenValuesAreStable(t *testing.T) {
	raw, err := os.ReadFile("testdata/peer_auth_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var vector peerAuthGoldenVector
	if err := json.Unmarshal(raw, &vector); err != nil {
		t.Fatal(err)
	}
	transcript, err := hex.DecodeString(vector.TranscriptHex)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := peercrypto.ParseIdentity(bytes.Repeat([]byte{3}, peercrypto.PrivateKeySize))
	if err != nil {
		t.Fatal(err)
	}
	auth := &PeerMembershipAuthenticator{
		identity: identity, localRole: RoleClient, genesisHash: vector.GenesisHash,
		clientMembership: peerproto.VerifiedGrant{Token: vector.ClientMembership},
		hostMembership:   peerproto.VerifiedGrant{Token: vector.HostMembership},
	}
	binding, err := auth.KeyBinding(transcript)
	if err != nil {
		t.Fatal(err)
	}
	finish, err := BuildFinishProof(auth, transcript)
	if err != nil {
		t.Fatal(err)
	}
	clientRaw, err := hex.DecodeString(vector.ClientPrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	hostRaw, err := hex.DecodeString(vector.HostPrivateKeyHex)
	if err != nil {
		t.Fatal(err)
	}
	client, err := ecdh.P256().NewPrivateKey(clientRaw)
	if err != nil {
		t.Fatal(err)
	}
	host, err := ecdh.P256().NewPrivateKey(hostRaw)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := DeriveTrafficKeys(client, host.PublicKey().Bytes(), transcript, auth)
	if err != nil {
		t.Fatal(err)
	}
	vectors := map[string]struct {
		got  []byte
		want string
	}{
		"binding":             {binding, vector.BindingHex},
		"finish_proof":        {finish, vector.FinishProofHex},
		"client_to_host_key":  {keys.ClientToHostKey[:], vector.ClientToHostKeyHex},
		"host_to_client_key":  {keys.HostToClientKey[:], vector.HostToClientKeyHex},
		"client_nonce_prefix": {keys.ClientToHostNoncePrefix[:], vector.ClientNoncePrefixHex},
		"host_nonce_prefix":   {keys.HostToClientNoncePrefix[:], vector.HostNoncePrefixHex},
	}
	for name, item := range vectors {
		if got := hex.EncodeToString(item.got); got != item.want {
			t.Errorf("%s = %s", name, got)
		}
	}
}

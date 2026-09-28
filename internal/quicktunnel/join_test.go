package quicktunnel

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
)

func TestJoinRedeemsEncryptedRequestAndResponse(t *testing.T) {
	t.Parallel()
	now := time.Now().Add(-time.Second).UTC()
	hostIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	hostWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	genesisToken, hostMembership, err := peerproto.NewSpace(hostIdentity, hostWrapping.PublicBytes(), now)
	if err != nil {
		t.Fatal(err)
	}
	genesis, err := peerproto.VerifyGenesis(genesisToken)
	if err != nil {
		t.Fatal(err)
	}
	hostGrant, err := peerproto.VerifyGrant(hostMembership, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	invitations, err := peerproto.NewInvitationBatch(hostIdentity, genesis, hostGrant, now, peerproto.InvitationOptions{Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	ticket, _, err := peerproto.VerifyInvitation(invitations[0], genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	pairingSecret, err := base64.RawURLEncoding.Strict().DecodeString(ticket.PairingSecret)
	if err != nil {
		t.Fatal(err)
	}

	clientIdentity, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	clientWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}

	want := JoinBootstrap{
		GenesisToken: genesisToken, MembershipToken: "apm1.membership.signature",
		Memberships: []string{hostMembership}, EpochRotations: []string{"akr1.rotation.signature"},
		EpochEnvelopes: []string{"ake1.envelope"},
	}
	var requestBody []byte
	var redeemed string
	handler, err := NewPeerHandler(HostConfig{
		Authorize: func(context.Context, OpenRequest) (HostAuthorization, error) {
			return HostAuthorization{}, ErrUnauthorized
		},
		Join: &JoinHostConfig{
			LookupSecret: func(_ context.Context, inviteID string) ([]byte, error) {
				if inviteID != ticket.InviteID {
					t.Fatalf("lookup invite id=%q want=%q", inviteID, ticket.InviteID)
				}
				return pairingSecret, nil
			},
			Redeem: func(_ context.Context, joinRequest string) (JoinBootstrap, error) {
				redeemed = joinRequest
				return want, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var readErr error
		requestBody, readErr = io.ReadAll(r.Body)
		if readErr != nil {
			http.Error(w, "read request", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(requestBody))
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()

	bundle, err := peerproto.NewConnectionBundle(hostIdentity, genesis, invitations[0], []peerproto.ConnectionRoute{{
		Kind: peerproto.RouteQuickTunnel, URL: "https://join-test.trycloudflare.com",
	}}, now, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	got, err := Join(ctx, JoinClientConfig{
		URL: server.URL, AllowInsecure: true, BundleToken: bundle,
		Identity: clientIdentity, WrappingPublicKey: clientWrapping.PublicBytes(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if redeemed == "" || !bytes.Equal(mustJSON(t, got), mustJSON(t, want)) {
		t.Fatalf("redeemed=%t bootstrap=%+v want=%+v", redeemed != "", got, want)
	}
	if bytes.Contains(requestBody, []byte(invitations[0])) || bytes.Contains(requestBody, []byte(redeemed)) || bytes.Contains(requestBody, []byte(ticket.PairingSecret)) {
		t.Fatal("join HTTP body exposed invitation, join request, or pairing secret plaintext")
	}
}

func TestJoinRejectsTamperedCiphertextBeforeRedemption(t *testing.T) {
	t.Parallel()
	var redeemed bool
	handler, err := NewPeerHandler(HostConfig{
		Authorize: func(context.Context, OpenRequest) (HostAuthorization, error) {
			return HostAuthorization{}, ErrUnauthorized
		},
		Join: &JoinHostConfig{
			LookupSecret: func(context.Context, string) ([]byte, error) { return bytes.Repeat([]byte{7}, 32), nil },
			Redeem: func(context.Context, string) (JoinBootstrap, error) {
				redeemed = true
				return JoinBootstrap{}, nil
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	req, err := http.NewRequest(http.MethodPost, server.URL+PeerJoinPath, bytes.NewBufferString(`{"v":1,"invite_id":"00000000-0000-4000-8000-000000000000","nonce":"AA","ciphertext":"AA"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d want=%d", response.StatusCode, http.StatusForbidden)
	}
	if redeemed {
		t.Fatal("tampered join request reached redemption callback")
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	blob, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return blob
}

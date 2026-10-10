package quicktunnel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/attson/atterm/internal/peerproto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/google/uuid"
	"nhooyr.io/websocket"
)

func TestPeerHandlerAuthenticatesAndEncryptsBidirectionalSignals(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	hostAuthenticated := make(chan *SignalChannel, 1)
	hostSignals := make(chan Signal, 1)
	clientSignals := make(chan Signal, 1)
	handler := newSignalTestHandler(t, peers, HostConfig{
		OnAuthenticated: func(channel *SignalChannel) { hostAuthenticated <- channel },
		OnSignal: func(_ *SignalChannel, signal Signal) error {
			hostSignals <- signal
			return nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Dial(ctx, ClientConfig{
		URL:                   server.URL,
		AllowInsecure:         true,
		Identity:              peers.clientIdentity,
		GenesisToken:          peers.genesisToken,
		ClientMembershipToken: peers.clientMembership,
		HostMembershipToken:   peers.hostMembership,
		SessionID:             peers.sessionID,
		ClientInstanceID:      "test-client",
		OnSignal: func(_ *SignalChannel, signal Signal) error {
			clientSignals <- signal
			return nil
		},
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var host *SignalChannel
	select {
	case host = <-hostAuthenticated:
	case <-ctx.Done():
		t.Fatal("host was not authenticated")
	}
	if client.RemoteMembershipToken() != peers.hostMembership || host.RemoteMembershipToken() != peers.clientMembership {
		t.Fatal("channel did not retain handshake-authenticated remote memberships")
	}
	clientBinding := client.Binding()
	hostBinding := host.Binding()
	if clientBinding.AttemptID == uuid.Nil || clientBinding.AttemptID != hostBinding.AttemptID ||
		clientBinding.SessionID != peers.sessionID || hostBinding.SessionID != peers.sessionID ||
		clientBinding.ClientInstanceID != "test-client" || hostBinding.ClientInstanceID != "test-client" ||
		clientBinding.Permission != peertransport.PermissionControl || hostBinding.Permission != peertransport.PermissionControl {
		t.Fatalf("channel bindings differ: client=%+v host=%+v", clientBinding, hostBinding)
	}

	offerPayload := `{"type":"offer","sdp":"` + strings.Repeat("secret-sdp-", 2400) + `"}`
	if err := client.SendSignal(ctx, Signal{Type: SignalOffer, Payload: offerPayload}); err != nil {
		t.Fatalf("send offer: %v", err)
	}
	select {
	case got := <-hostSignals:
		if got.Type != SignalOffer || got.Payload != offerPayload {
			t.Fatalf("host signal = %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("host did not receive encrypted offer")
	}

	answerPayload := `{"type":"answer","sdp":"private-answer"}`
	if err := host.SendSignal(ctx, Signal{Type: SignalAnswer, Payload: answerPayload}); err != nil {
		t.Fatalf("send answer: %v", err)
	}
	select {
	case got := <-clientSignals:
		if got.Type != SignalAnswer || got.Payload != answerPayload {
			t.Fatalf("client signal = %+v", got)
		}
	case <-ctx.Done():
		t.Fatal("client did not receive encrypted answer")
	}
}

func TestPeerHandlerRejectsUnknownOrWrongIdentity(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	var authenticated atomic.Int32
	handler := newSignalTestHandler(t, peers, HostConfig{
		Authorize: func(_ context.Context, request OpenRequest) (HostAuthorization, error) {
			if request.ClientPeerID != peers.clientIdentity.PeerID() {
				return HostAuthorization{}, ErrUnauthorized
			}
			return peers.hostAuthorization(), nil
		},
		OnAuthenticated: func(*SignalChannel) { authenticated.Add(1) },
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	unknown, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err = Dial(ctx, ClientConfig{
		URL: server.URL, AllowInsecure: true, Identity: unknown,
		GenesisToken: peers.genesisToken, ClientMembershipToken: peers.clientMembership,
		HostMembershipToken: peers.hostMembership, SessionID: peers.sessionID,
		ClientInstanceID: "unknown-client",
	})
	if err == nil {
		t.Fatal("unknown identity established a signaling channel")
	}
	if authenticated.Load() != 0 {
		t.Fatal("host authenticated an unknown identity")
	}
}

func TestPeerHandlerRejectsMembershipOutsideSessionScope(t *testing.T) {
	t.Parallel()

	allowed := uuid.New()
	peers := newSignalTestPeers(t, []string{allowed.String()})
	peers.sessionID = uuid.New()
	handler := newSignalTestHandler(t, peers, HostConfig{})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := Dial(ctx, peers.clientConfig(server.URL))
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Dial error = %v, want ErrUnauthorized", err)
	}
}

func TestPeerHandlerRejectsPermissionAboveMembershipGrant(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	handler := newSignalTestHandler(t, peers, HostConfig{
		Authorize: func(_ context.Context, request OpenRequest) (HostAuthorization, error) {
			if request.ClientPeerID != peers.clientIdentity.PeerID() {
				return HostAuthorization{}, ErrUnauthorized
			}
			authorization := peers.hostAuthorization()
			authorization.Permission = peertransport.PermissionFull
			return authorization, nil
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Dial(ctx, peers.clientConfig(server.URL)); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Dial error = %v, want ErrUnauthorized", err)
	}
}

func TestPeerHandlerLimitsConcurrentConnections(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	handler := newSignalTestHandler(t, peers, HostConfig{MaxConnections: 1})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	first, err := Dial(ctx, peers.clientConfig(server.URL))
	if err != nil {
		t.Fatalf("first Dial: %v", err)
	}
	defer first.Close()
	if _, err := Dial(ctx, peers.clientConfig(server.URL)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("second Dial error = %v, want ErrCapacity", err)
	}
}

func TestPeerHandlerRequiresPeerSubprotocol(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	handler := newSignalTestHandler(t, peers, HostConfig{})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + PeerConnectPath
	conn, response, err := websocket.Dial(ctx, url, nil)
	if conn != nil {
		defer conn.Close(websocket.StatusNormalClosure, "")
	}
	if err == nil || response == nil || response.StatusCode != http.StatusUpgradeRequired {
		t.Fatalf("Dial error=%v status=%v, want HTTP 426", err, responseStatus(response))
	}
}

func TestPeerHandlerClosesOnTamperedEncryptedRecord(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	hostClosed := make(chan error, 1)
	handler := newSignalTestHandler(t, peers, HostConfig{
		OnClosed: func(_ *SignalChannel, err error) { hostClosed <- err },
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Dial(ctx, peers.clientConfig(server.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	payload, err := json.Marshal(Signal{Version: signalVersion, Type: SignalOffer, Payload: "private-sdp"})
	if err != nil {
		t.Fatal(err)
	}
	record, err := client.sealer.Seal(peertransport.RecordSignal, payload)
	if err != nil {
		t.Fatal(err)
	}
	record[len(record)-1] ^= 0x80
	if err := client.conn.Write(ctx, websocket.MessageBinary, record); err != nil {
		t.Fatalf("write tampered record: %v", err)
	}
	select {
	case err := <-hostClosed:
		if !errors.Is(err, peertransport.ErrInvalidRecord) {
			t.Fatalf("host close error = %v, want ErrInvalidRecord", err)
		}
	case <-ctx.Done():
		t.Fatal("host did not close tampered encrypted channel")
	}
}

func TestSignalChannelRejectsOutOfPolicySignalBeforeWrite(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	handler := newSignalTestHandler(t, peers, HostConfig{})
	server := httptest.NewServer(handler)
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := Dial(ctx, peers.clientConfig(server.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	if err := client.SendSignal(ctx, Signal{Type: SignalICEEnd, Payload: "not-empty"}); !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("ICE end error = %v, want ErrInvalidSignal", err)
	}
	if err := client.SendSignal(ctx, Signal{Type: SignalOffer, Payload: strings.Repeat("x", signalOfferLimit+1)}); !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("oversized offer error = %v, want ErrInvalidSignal", err)
	}
	if err := client.SendSignal(ctx, Signal{Type: SignalOffer, Payload: "first"}); err != nil {
		t.Fatalf("first offer: %v", err)
	}
	if err := client.SendSignal(ctx, Signal{Type: SignalOffer, Payload: "duplicate"}); !errors.Is(err, ErrInvalidSignal) {
		t.Fatalf("duplicate offer error = %v, want ErrInvalidSignal", err)
	}
}

func responseStatus(response *http.Response) any {
	if response == nil {
		return nil
	}
	return response.StatusCode
}

type signalTestPeers struct {
	clientIdentity   *peercrypto.Identity
	hostIdentity     *peercrypto.Identity
	genesisToken     string
	clientMembership string
	hostMembership   string
	sessionID        uuid.UUID
}

func newSignalTestPeers(t *testing.T, allowedSessions []string) signalTestPeers {
	t.Helper()
	now := time.Now().Add(-time.Second)
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
	invitations, err := peerproto.NewInvitationBatch(hostIdentity, genesis, hostGrant, now, peerproto.InvitationOptions{
		Count: 1, Permission: peerproto.PermissionControl, AllowedSessionIDs: allowedSessions,
	})
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
	joinToken, err := peerproto.NewJoinRequest(clientIdentity, clientWrapping.PublicBytes(), invitations[0], now)
	if err != nil {
		t.Fatal(err)
	}
	join, err := peerproto.VerifyJoinRequest(joinToken, genesis, now)
	if err != nil {
		t.Fatal(err)
	}
	clientMembership, err := peerproto.IssueMembership(hostIdentity, genesis, hostGrant, join, now)
	if err != nil {
		t.Fatal(err)
	}
	return signalTestPeers{
		clientIdentity: clientIdentity, hostIdentity: hostIdentity,
		genesisToken: genesisToken, clientMembership: clientMembership,
		hostMembership: hostMembership, sessionID: uuid.New(),
	}
}

func newSignalTestHandler(t *testing.T, peers signalTestPeers, overrides HostConfig) *PeerHandler {
	t.Helper()
	if overrides.Authorize == nil {
		overrides.Authorize = func(_ context.Context, request OpenRequest) (HostAuthorization, error) {
			if request.ClientPeerID != peers.clientIdentity.PeerID() {
				return HostAuthorization{}, ErrUnauthorized
			}
			return peers.hostAuthorization(), nil
		}
	}
	handler, err := NewPeerHandler(overrides)
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func (p signalTestPeers) hostAuthorization() HostAuthorization {
	return HostAuthorization{
		Identity: p.hostIdentity, GenesisToken: p.genesisToken,
		ClientMembershipToken: p.clientMembership, HostMembershipToken: p.hostMembership,
		Permission: peertransport.PermissionControl,
	}
}

func (p signalTestPeers) clientConfig(baseURL string) ClientConfig {
	return ClientConfig{
		URL: baseURL, AllowInsecure: true, Identity: p.clientIdentity,
		GenesisToken: p.genesisToken, ClientMembershipToken: p.clientMembership,
		HostMembershipToken: p.hostMembership, SessionID: p.sessionID,
		ClientInstanceID: "test-client",
	}
}

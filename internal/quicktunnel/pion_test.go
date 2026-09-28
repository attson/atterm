package quicktunnel

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/attson/atterm/internal/peertransport"
	"github.com/pion/webrtc/v4"
)

func TestQuickTunnelSignalBridgesPionDataChannel(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	hostAuthenticated := make(chan *peertransport.PionHostChannel, 1)
	clientAuthenticated := make(chan *peertransport.PionClientChannel, 1)
	hostRecords := make(chan []byte, 1)
	clientRecords := make(chan []byte, 1)
	hostConfig := make(chan []byte, 1)
	hostClosed := make(chan error, 1)
	bridgeErrors := make(chan error, 1)

	handler := newSignalTestHandler(t, peers, HostConfig{
		OnAuthenticated: func(signal *SignalChannel) {
			_, err := signal.BridgePionHost(ctx, PionHostBridgeConfig{
				WebRTC: webrtc.Configuration{},
				OnAuthenticated: func(channel *peertransport.PionHostChannel) {
					hostAuthenticated <- channel
				},
				OnRecord: func(kind peertransport.RecordKind, payload []byte) {
					if kind == peertransport.RecordPing {
						hostRecords <- payload
					}
				},
				OnConfigMessage: func(kind peertransport.RecordKind, payload []byte) error {
					if kind == peertransport.RecordConfigInventory {
						hostConfig <- payload
					}
					return nil
				},
				OnClosed: func(err error) { hostClosed <- err },
			})
			if err != nil {
				bridgeErrors <- err
			}
		},
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	signal, err := Dial(ctx, peers.clientConfig(server.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer signal.Close()
	clientAttempt, err := signal.BridgePionClient(ctx, PionClientBridgeConfig{
		WebRTC: webrtc.Configuration{},
		OnAuthenticated: func(channel *peertransport.PionClientChannel) {
			clientAuthenticated <- channel
		},
		OnRecord: func(kind peertransport.RecordKind, payload []byte) {
			if kind == peertransport.RecordPong {
				clientRecords <- payload
			}
		},
	})
	if err != nil {
		t.Fatalf("BridgePionClient: %v", err)
	}
	defer clientAttempt.Close()

	var hostChannel *peertransport.PionHostChannel
	var clientChannel *peertransport.PionClientChannel
	for hostChannel == nil || clientChannel == nil {
		select {
		case err := <-bridgeErrors:
			t.Fatalf("host bridge: %v", err)
		case hostChannel = <-hostAuthenticated:
		case clientChannel = <-clientAuthenticated:
		case <-ctx.Done():
			t.Fatal("Pion DataChannel did not authenticate")
		}
	}

	if token, ok := hostChannel.RemoteMembershipToken(); !ok || token != peers.clientMembership {
		t.Fatal("host Pion channel lost authenticated client membership")
	}
	if token, ok := clientChannel.RemoteMembershipToken(); !ok || token != peers.hostMembership {
		t.Fatal("client Pion channel lost authenticated host membership")
	}
	if err := clientChannel.SendRecord(ctx, peertransport.RecordSignal, []byte("not-data")); !errors.Is(err, peertransport.ErrDirectTransport) {
		t.Fatalf("send signal record over DataChannel error = %v, want ErrDirectTransport", err)
	}
	if err := clientChannel.SendRecord(ctx, peertransport.RecordPing, []byte("client-to-host")); err != nil {
		t.Fatalf("send client record: %v", err)
	}
	if got := receiveBytes(t, ctx, hostRecords); !bytes.Equal(got, []byte("client-to-host")) {
		t.Fatalf("host record = %q", got)
	}
	if err := hostChannel.SendRecord(ctx, peertransport.RecordPong, []byte("host-to-client")); err != nil {
		t.Fatalf("send host record: %v", err)
	}
	if got := receiveBytes(t, ctx, clientRecords); !bytes.Equal(got, []byte("host-to-client")) {
		t.Fatalf("client record = %q", got)
	}
	configPayload := bytes.Repeat([]byte("inventory"), 4096)
	if err := clientChannel.SendConfigMessage(ctx, peertransport.RecordConfigInventory, configPayload); err != nil {
		t.Fatalf("send config message: %v", err)
	}
	if got := receiveBytes(t, ctx, hostConfig); !bytes.Equal(got, configPayload) {
		t.Fatal("host config message differs")
	}

	if err := signal.Close(); err != nil {
		t.Fatalf("close signal channel: %v", err)
	}
	select {
	case <-hostClosed:
	case <-ctx.Done():
		t.Fatal("closing signaling did not close host Pion attempt")
	}
}

func TestPionBridgeRejectsWrongRoleAndDuplicateWithoutClosingSignal(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hostAuthenticated := make(chan *SignalChannel, 1)
	handler := newSignalTestHandler(t, peers, HostConfig{
		OnAuthenticated: func(signal *SignalChannel) { hostAuthenticated <- signal },
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := Dial(ctx, peers.clientConfig(server.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	var host *SignalChannel
	select {
	case host = <-hostAuthenticated:
	case <-ctx.Done():
		t.Fatal("host signal channel was not authenticated")
	}

	if _, err := host.BridgePionClient(ctx, PionClientBridgeConfig{}); !errors.Is(err, peertransport.ErrDirectTransport) {
		t.Fatalf("host accepted client bridge: %v", err)
	}
	if _, err := client.BridgePionHost(ctx, PionHostBridgeConfig{}); !errors.Is(err, peertransport.ErrDirectTransport) {
		t.Fatalf("client accepted host bridge: %v", err)
	}

	first, err := host.BridgePionHost(ctx, PionHostBridgeConfig{})
	if err != nil {
		t.Fatalf("first host bridge: %v", err)
	}
	defer first.Close()
	if _, err := host.BridgePionHost(ctx, PionHostBridgeConfig{}); err == nil {
		t.Fatal("duplicate host bridge succeeded")
	}
	select {
	case <-host.done:
		t.Fatal("duplicate bridge closed the existing signal channel")
	case <-time.After(150 * time.Millisecond):
	}
}

func TestSignalChannelConcurrentCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var closedCalls atomic.Int32
	handler := newSignalTestHandler(t, peers, HostConfig{})
	server := httptest.NewServer(handler)
	defer server.Close()
	cfg := peers.clientConfig(server.URL)
	cfg.OnClosed = func(*SignalChannel, error) { closedCalls.Add(1) }
	client, err := Dial(ctx, cfg)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	const callers = 8
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			_ = client.Close()
		}()
	}
	wg.Wait()
	if got := closedCalls.Load(); got != 1 {
		t.Fatalf("OnClosed calls = %d, want 1", got)
	}
}

func TestSignalChannelWithoutHandlerFailsClosed(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hostClosed := make(chan error, 1)
	handler := newSignalTestHandler(t, peers, HostConfig{
		OnClosed: func(_ *SignalChannel, err error) { hostClosed <- err },
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := Dial(ctx, peers.clientConfig(server.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	if err := client.SendSignal(ctx, Signal{Type: SignalOffer, Payload: "unsolicited"}); err != nil {
		t.Fatalf("SendSignal: %v", err)
	}
	select {
	case err := <-hostClosed:
		if !errors.Is(err, ErrInvalidSignal) {
			t.Fatalf("host close error = %v, want ErrInvalidSignal", err)
		}
	case <-ctx.Done():
		t.Fatal("signal without a handler did not close the channel")
	}
}

func TestPionClosureDoesNotWaitForApplicationCallbackToCloseSignal(t *testing.T) {
	t.Parallel()

	peers := newSignalTestPeers(t, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hostAuthenticated := make(chan *SignalChannel, 1)
	handler := newSignalTestHandler(t, peers, HostConfig{
		OnAuthenticated: func(signal *SignalChannel) { hostAuthenticated <- signal },
	})
	server := httptest.NewServer(handler)
	defer server.Close()
	client, err := Dial(ctx, peers.clientConfig(server.URL))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	var host *SignalChannel
	select {
	case host = <-hostAuthenticated:
	case <-ctx.Done():
		t.Fatal("host signal channel was not authenticated")
	}

	callbackStarted := make(chan struct{})
	releaseCallback := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseCallback) }) }
	t.Cleanup(release)
	attempt, err := host.BridgePionHost(ctx, PionHostBridgeConfig{
		OnClosed: func(error) {
			close(callbackStarted)
			<-releaseCallback
		},
	})
	if err != nil {
		t.Fatalf("BridgePionHost: %v", err)
	}
	attemptClosed := make(chan struct{})
	go func() {
		_ = attempt.Close()
		close(attemptClosed)
	}()
	select {
	case <-callbackStarted:
	case <-ctx.Done():
		t.Fatal("Pion close callback did not start")
	}
	select {
	case <-host.done:
	case <-ctx.Done():
		t.Fatal("blocked Pion close callback prevented signal cleanup")
	}
	release()
	select {
	case <-attemptClosed:
	case <-ctx.Done():
		t.Fatal("Pion close did not finish after callback returned")
	}
}

func receiveBytes(t *testing.T, ctx context.Context, values <-chan []byte) []byte {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-ctx.Done():
		t.Fatal("timed out waiting for Peer transport payload")
		return nil
	}
}

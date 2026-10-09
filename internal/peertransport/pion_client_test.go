package peertransport

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

func TestPionConfigCallbackFailureFailsClosed(t *testing.T) {
	sealer, opener := recordPair(t)
	wantErr := errors.New("reject config")
	terminalDelivered := false
	channel := &PionClientChannel{
		attempt: &PionClientAttempt{cfg: PionClientConfig{
			OnRecord: func(RecordKind, []byte) { terminalDelivered = true },
			OnConfigMessage: func(RecordKind, []byte) error {
				return wantErr
			},
		}},
		opener: opener,
	}
	record, err := sealer.Seal(RecordConfigInventory, []byte(`{"v":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := channel.receive(record); !errors.Is(err, wantErr) || !errors.Is(err, ErrDirectTransport) {
		t.Fatalf("config callback error=%v", err)
	}
	if terminalDelivered {
		t.Fatal("rejected config message reached terminal callback")
	}
}

func TestSelectedCandidateTypeReportsEitherRelayedSide(t *testing.T) {
	tests := []struct {
		name   string
		local  *webrtc.ICECandidate
		remote *webrtc.ICECandidate
		want   string
	}{
		{name: "local relay", local: &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeRelay}, remote: &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost}, want: "relay"},
		{name: "remote relay", local: &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost}, remote: &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeRelay}, want: "relay"},
		{name: "direct keeps local type", local: &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeSrflx}, remote: &webrtc.ICECandidate{Typ: webrtc.ICECandidateTypeHost}, want: "srflx"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := selectedCandidateType(&webrtc.ICECandidatePair{Local: tt.local, Remote: tt.remote}); got != tt.want {
				t.Fatalf("selectedCandidateType()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestPionClientAndHostCarryEncryptedRecords(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	auth, err := NewAccountKeyAuthenticator(bytes.Repeat([]byte{0x72}, accountKeySize))
	if err != nil {
		t.Fatal(err)
	}
	authorization := testAuthorization(time.Now().Add(time.Minute))
	errCh := make(chan error, 8)
	hostRecord := make(chan []byte, 1)
	clientFrame := make(chan []byte, 1)
	hostConfig := make(chan []byte, 1)
	clientConfig := make(chan []byte, 1)
	hostService := make(chan []byte, 1)
	clientService := make(chan []byte, 1)
	clientReady := make(chan uint64, 1)
	clientAuthenticated := make(chan struct{}, 1)
	hostAuthenticated := make(chan *PionHostChannel, 1)
	largeConfig := bytes.Repeat([]byte("config"), 4096)
	serviceID := uuid.New()
	clientServiceMessage, err := EncodeServiceMessage(ServiceMessage{
		ServiceID: serviceID, Kind: ServiceData, Connection: 1, Data: []byte("client service"),
	})
	if err != nil {
		t.Fatal(err)
	}
	hostServiceMessage, err := EncodeServiceMessage(ServiceMessage{
		ServiceID: serviceID, Kind: ServiceData, Connection: 1, Data: []byte("host service"),
	})
	if err != nil {
		t.Fatal(err)
	}

	var client *PionClientAttempt
	host, err := NewPionHostAttempt(ctx, PionHostConfig{
		Authorization: authorization,
		Authenticator: auth,
		WebRTC:        webrtc.Configuration{},
		SendSignal: func(signalType, payload string) error {
			if client == nil {
				return fmt.Errorf("client not ready")
			}
			return client.HandleSignal(signalType, payload)
		},
		OnAuthenticated: func(channel *PionHostChannel) {
			hostAuthenticated <- channel
			ready := make([]byte, 8)
			binary.BigEndian.PutUint64(ready, 41)
			if sendErr := channel.SendRecord(ctx, RecordDirectReady, ready); sendErr != nil {
				nonBlockingTestError(errCh, sendErr)
			}
			if sendErr := channel.SendFrame(ctx, []byte("host output")); sendErr != nil {
				nonBlockingTestError(errCh, sendErr)
			}
		},
		OnRecord: func(kind RecordKind, payload []byte) {
			switch kind {
			case RecordFrame:
				hostRecord <- payload
			case RecordService:
				hostService <- payload
			default:
				nonBlockingTestError(errCh, fmt.Errorf("host record kind=%d", kind))
			}
		},
		OnConfigMessage: func(kind RecordKind, payload []byte) error {
			if kind != RecordConfigBatch {
				nonBlockingTestError(errCh, fmt.Errorf("host config kind=%d", kind))
				return nil
			}
			hostConfig <- payload
			return nil
		},
		OnClosed: func(closeErr error) {
			if closeErr != nil && ctx.Err() == nil {
				nonBlockingTestError(errCh, closeErr)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()

	client, err = NewPionClientAttempt(ctx, PionClientConfig{
		Authorization: authorization,
		Authenticator: auth,
		WebRTC:        webrtc.Configuration{},
		SendSignal: func(signalType, payload string) error {
			return host.HandleSignal(signalType, payload)
		},
		OnAuthenticated: func(channel *PionClientChannel) {
			clientAuthenticated <- struct{}{}
			if sendErr := channel.SendFrame(ctx, []byte("client input")); sendErr != nil {
				nonBlockingTestError(errCh, sendErr)
			}
			if sendErr := channel.SendConfigMessage(ctx, RecordConfigBatch, largeConfig); sendErr != nil {
				nonBlockingTestError(errCh, sendErr)
			}
			if sendErr := channel.SendServiceMessage(ctx, clientServiceMessage); sendErr != nil {
				nonBlockingTestError(errCh, sendErr)
			}
		},
		OnRecord: func(kind RecordKind, payload []byte) {
			switch kind {
			case RecordFrame:
				clientFrame <- payload
			case RecordDirectReady:
				if len(payload) != 8 {
					nonBlockingTestError(errCh, fmt.Errorf("ready payload length=%d", len(payload)))
					return
				}
				clientReady <- binary.BigEndian.Uint64(payload)
			case RecordService:
				clientService <- payload
			default:
				nonBlockingTestError(errCh, fmt.Errorf("client record kind=%d", kind))
			}
		},
		OnConfigMessage: func(kind RecordKind, payload []byte) error {
			if kind != RecordConfigInventory {
				nonBlockingTestError(errCh, fmt.Errorf("client config kind=%d", kind))
				return nil
			}
			clientConfig <- payload
			return nil
		},
		OnClosed: func(closeErr error) {
			if closeErr != nil && ctx.Err() == nil {
				nonBlockingTestError(errCh, closeErr)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	if err := client.Start(); err != nil {
		t.Fatal(err)
	}

	var hostChannel *PionHostChannel
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case err := <-errCh:
		t.Fatal(err)
	case hostChannel = <-hostAuthenticated:
	}
	if err := hostChannel.SendConfigMessage(ctx, RecordConfigInventory, largeConfig); err != nil {
		t.Fatal(err)
	}
	if err := hostChannel.SendServiceMessage(ctx, hostServiceMessage); err != nil {
		t.Fatal(err)
	}

	for authenticated, gotHost, gotFrame, gotReady, gotHostConfig, gotClientConfig, gotHostService, gotClientService := false, false, false, false, false, false, false, false; !authenticated || !gotHost || !gotFrame || !gotReady || !gotHostConfig || !gotClientConfig || !gotHostService || !gotClientService; {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case err := <-errCh:
			t.Fatal(err)
		case <-clientAuthenticated:
			authenticated = true
		case payload := <-hostRecord:
			if string(payload) != "client input" {
				t.Fatalf("host payload=%q", payload)
			}
			gotHost = true
		case payload := <-clientFrame:
			if string(payload) != "host output" {
				t.Fatalf("client payload=%q", payload)
			}
			gotFrame = true
		case seq := <-clientReady:
			if seq != 41 {
				t.Fatalf("ready seq=%d", seq)
			}
			gotReady = true
		case payload := <-hostConfig:
			if !bytes.Equal(payload, largeConfig) {
				t.Fatalf("host config bytes=%d", len(payload))
			}
			gotHostConfig = true
		case payload := <-clientConfig:
			if !bytes.Equal(payload, largeConfig) {
				t.Fatalf("client config bytes=%d", len(payload))
			}
			gotClientConfig = true
		case payload := <-hostService:
			if !bytes.Equal(payload, clientServiceMessage) {
				t.Fatalf("host service payload=%x", payload)
			}
			gotHostService = true
		case payload := <-clientService:
			if !bytes.Equal(payload, hostServiceMessage) {
				t.Fatalf("client service payload=%x", payload)
			}
			gotClientService = true
		}
	}
}

package peertransport

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

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
	clientReady := make(chan uint64, 1)
	clientAuthenticated := make(chan struct{}, 1)

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
			if kind != RecordFrame {
				nonBlockingTestError(errCh, fmt.Errorf("host record kind=%d", kind))
				return
			}
			hostRecord <- payload
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
			default:
				nonBlockingTestError(errCh, fmt.Errorf("client record kind=%d", kind))
			}
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

	for authenticated, gotHost, gotFrame, gotReady := false, false, false, false; !authenticated || !gotHost || !gotFrame || !gotReady; {
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
		}
	}
}

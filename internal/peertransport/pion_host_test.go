package peertransport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestPionHostAttemptAuthenticatesAndCarriesEncryptedRecords(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	accountKey := bytes.Repeat([]byte{0x63}, accountKeySize)
	auth, err := NewAccountKeyAuthenticator(accountKey)
	if err != nil {
		t.Fatal(err)
	}
	authorization := testAuthorization(time.Now().Add(time.Minute))
	answerCh := make(chan string, 1)
	hostAuthenticated := make(chan *PionHostChannel, 1)
	hostRecord := make(chan []byte, 1)
	clientRecord := make(chan []byte, 1)
	errCh := make(chan error, 8)

	hostAttempt, err := NewPionHostAttempt(ctx, PionHostConfig{
		Authorization: authorization,
		Authenticator: auth,
		SendSignal: func(signalType, payload string) error {
			switch signalType {
			case "answer":
				answerCh <- payload
			case "ice_end":
			default:
				return fmt.Errorf("unexpected host signal %q", signalType)
			}
			return nil
		},
		OnAuthenticated: func(channel *PionHostChannel) {
			hostAuthenticated <- channel
			if sendErr := channel.SendRecord(ctx, RecordDirectReady, []byte("ready")); sendErr != nil {
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
	defer hostAttempt.Close()

	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer clientPC.Close()
	dc, err := clientPC.CreateDataChannel(TerminalDataChannelLabel, nil)
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate, err := GenerateEphemeralKey()
	if err != nil {
		t.Fatal(err)
	}
	clientHello, err := EncodeClientHello(authorization.AttemptID, authorization.Ticket, clientPrivate.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}

	var (
		clientMu     sync.Mutex
		clientSealer *RecordSealer
		clientOpener *RecordOpener
	)
	dc.OnOpen(func() {
		if sendErr := dc.Send(clientHello); sendErr != nil {
			nonBlockingTestError(errCh, sendErr)
		}
	})
	dc.OnMessage(func(message webrtc.DataChannelMessage) {
		clientMu.Lock()
		defer clientMu.Unlock()
		if IsAuthOK(message.Data) {
			if clientSealer == nil {
				nonBlockingTestError(errCh, fmt.Errorf("AUTH_OK before host hello"))
				return
			}
			record, sealErr := clientSealer.Seal(RecordFrame, []byte("input"))
			if sealErr != nil {
				nonBlockingTestError(errCh, sealErr)
				return
			}
			if sendErr := dc.Send(record); sendErr != nil {
				nonBlockingTestError(errCh, sendErr)
			}
			return
		}
		if clientOpener != nil {
			kind, plaintext, openErr := clientOpener.Open(message.Data)
			if openErr != nil {
				nonBlockingTestError(errCh, openErr)
				return
			}
			if kind != RecordDirectReady {
				nonBlockingTestError(errCh, fmt.Errorf("client record kind=%d", kind))
				return
			}
			clientRecord <- plaintext
			return
		}
		hostPublic, hostProof, decodeErr := DecodeHostHello(message.Data)
		if decodeErr != nil {
			nonBlockingTestError(errCh, decodeErr)
			return
		}
		transcript, transcriptErr := buildHandshakeTestTranscript(authorization, clientPrivate.PublicKey().Bytes(), hostPublic)
		if transcriptErr != nil {
			nonBlockingTestError(errCh, transcriptErr)
			return
		}
		if verifyErr := auth.VerifyProof(transcript, RoleHost, hostProof); verifyErr != nil {
			nonBlockingTestError(errCh, verifyErr)
			return
		}
		keys, keyErr := DeriveTrafficKeys(clientPrivate, hostPublic, transcript, auth)
		if keyErr != nil {
			nonBlockingTestError(errCh, keyErr)
			return
		}
		hash := TranscriptHash(transcript)
		clientSealer, keyErr = NewRecordSealer(keys.ClientToHostKey[:], keys.ClientToHostNoncePrefix[:], hash)
		if keyErr == nil {
			clientOpener, keyErr = NewRecordOpener(keys.HostToClientKey[:], keys.HostToClientNoncePrefix[:], hash)
		}
		if keyErr != nil {
			nonBlockingTestError(errCh, keyErr)
			return
		}
		clientProof, _ := auth.BuildProof(transcript, RoleClient)
		finishProof, _ := BuildFinishProof(auth, transcript)
		finish, _ := EncodeClientFinish(clientProof, finishProof)
		if sendErr := dc.Send(finish); sendErr != nil {
			nonBlockingTestError(errCh, sendErr)
		}
	})

	offer, err := clientPC.CreateOffer(nil)
	if err != nil {
		t.Fatal(err)
	}
	gathered := webrtc.GatheringCompletePromise(clientPC)
	if err := clientPC.SetLocalDescription(offer); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case <-gathered:
	}
	offerJSON, _ := json.Marshal(clientPC.LocalDescription())
	go func() {
		if handleErr := hostAttempt.HandleSignal("offer", string(offerJSON)); handleErr != nil {
			nonBlockingTestError(errCh, handleErr)
		}
	}()

	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	case err := <-errCh:
		t.Fatal(err)
	case answerJSON := <-answerCh:
		var answer webrtc.SessionDescription
		if err := json.Unmarshal([]byte(answerJSON), &answer); err != nil {
			t.Fatal(err)
		}
		if err := clientPC.SetRemoteDescription(answer); err != nil {
			t.Fatal(err)
		}
	}

	for authenticated, gotHost, gotClient := false, false, false; !authenticated || !gotHost || !gotClient; {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case err := <-errCh:
			t.Fatal(err)
		case <-hostAuthenticated:
			authenticated = true
		case payload := <-hostRecord:
			if string(payload) != "input" {
				t.Fatalf("host payload=%q", payload)
			}
			gotHost = true
		case payload := <-clientRecord:
			if string(payload) != "ready" {
				t.Fatalf("client payload=%q", payload)
			}
			gotClient = true
		}
	}
}

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/attson/atterm/internal/directsignal"
	"github.com/attson/atterm/internal/e2eecrypto"
	"github.com/attson/atterm/internal/peertransport"
	"github.com/attson/atterm/internal/proto"
	"github.com/attson/atterm/internal/session"
	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"nhooyr.io/websocket"
)

func addDirectTestSession(t *testing.T, host *relayHost, id uuid.UUID) *session.Session {
	t.Helper()
	sess := session.New(id, proto.SessionInfo{HostID: host.hostID, RemotePermission: proto.RemotePermissionFull})
	if _, err := host.server.Registry().Add(sess); err != nil {
		t.Fatalf("Registry.Add: %v", err)
	}
	t.Cleanup(func() { host.server.Registry().Remove(id) })
	return sess
}

func TestDirectSignalHostHelloAndRegistration(t *testing.T) {
	host := newTestRelayHost(t)
	first := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	addDirectTestSession(t, host, first)
	addDirectTestSession(t, host, second)

	requestChange := make(chan struct{})
	serverErr := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer desktop-token" {
			serverErr <- fmt.Errorf("Authorization=%q", got)
			return
		}
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close(websocket.StatusNormalClosure, "done")
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		hello, err := readDirectSignalHost(ctx, conn)
		if err != nil {
			serverErr <- err
			return
		}
		wantIDs := []string{second.String(), first.String()}
		if hello.Kind != "hello" || hello.Role != "host" || hello.HostID != host.hostID || !reflect.DeepEqual(hello.SessionIDs, wantIDs) {
			serverErr <- fmt.Errorf("unexpected hello: %+v", hello)
			return
		}
		if err := writeDirectSignalHost(ctx, conn, directsignal.Message{Version: directsignal.Version, Kind: "hello_ok"}); err != nil {
			serverErr <- err
			return
		}
		close(requestChange)
		registration, err := readDirectSignalHost(ctx, conn)
		if err != nil {
			serverErr <- err
			return
		}
		if registration.Kind != "host_register" || registration.HostID != host.hostID || len(registration.SessionIDs) != 3 {
			serverErr <- fmt.Errorf("unexpected registration: %+v", registration)
			return
		}
		serverErr <- nil
	}))
	defer server.Close()

	directHost := newDirectSignalHost("ws"+strings.TrimPrefix(server.URL, "http"), "desktop-token", proto.RemotePermissionFull, host, nil, false)
	runErr := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { runErr <- directHost.runOnce(ctx) }()

	select {
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for host hello")
	case <-requestChange:
	}
	third := uuid.MustParse("00000000-0000-0000-0000-000000000003")
	addDirectTestSession(t, host, third)
	for i := 0; i < 10; i++ {
		host.server.Registry().NotifyChange()
		select {
		case err := <-serverErr:
			if err != nil {
				t.Fatal(err)
			}
			cancel()
			<-runErr
			return
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatal("timed out waiting for host registration")
}

func TestDirectSignalHostRejectsInvalidClaimsAndLockedKey(t *testing.T) {
	host := newTestRelayHost(t)
	sessionID := uuid.New()
	addDirectTestSession(t, host, sessionID)
	attemptID := uuid.New()
	ticket := bytes.Repeat([]byte{0x41}, 32)
	valid := directsignal.Message{
		Version:          directsignal.Version,
		Kind:             "direct_attempt",
		AttemptID:        attemptID.String(),
		Ticket:           base64.RawURLEncoding.EncodeToString(ticket),
		SessionID:        sessionID.String(),
		UserID:           "user-1",
		HostID:           host.hostID,
		ClientInstanceID: "client-1",
		Permission:       proto.RemotePermissionFull,
		ExpiresAtUnixM:   time.Now().Add(time.Minute).UnixMilli(),
	}

	tests := []struct {
		name   string
		mutate func(*directsignal.Message)
		key    []byte
	}{
		{name: "attempt id", mutate: func(m *directsignal.Message) { m.AttemptID = "bad" }, key: ticket},
		{name: "session id", mutate: func(m *directsignal.Message) { m.SessionID = "bad" }, key: ticket},
		{name: "ticket", mutate: func(m *directsignal.Message) { m.Ticket = "bad" }, key: ticket},
		{name: "host", mutate: func(m *directsignal.Message) { m.HostID = "other" }, key: ticket},
		{name: "permission", mutate: func(m *directsignal.Message) { m.Permission = proto.RemotePermissionView }, key: ticket},
		{name: "expiry", mutate: func(m *directsignal.Message) { m.ExpiresAtUnixM = time.Now().Add(-time.Second).UnixMilli() }, key: ticket},
		{name: "missing session", mutate: func(m *directsignal.Message) { m.SessionID = uuid.NewString() }, key: ticket},
		{name: "locked key", mutate: func(*directsignal.Message) {}, key: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := valid
			tt.mutate(&message)
			directHost := newDirectSignalHost("ws://unused", "", proto.RemotePermissionFull, host, func() []byte { return tt.key }, false)
			if err := directHost.startAttempt(context.Background(), message, func(directsignal.Message) error { return nil }); err == nil {
				t.Fatal("invalid direct attempt accepted")
			}
			if len(directHost.attempts) != 0 {
				t.Fatal("rejected direct attempt leaked into registry")
			}
		})
	}
}

func TestDirectSignalHostDuplicateAttemptKeepsExistingEntry(t *testing.T) {
	host := newTestRelayHost(t)
	sessionID := uuid.New()
	addDirectTestSession(t, host, sessionID)
	attemptID := uuid.New()
	accountKey := bytes.Repeat([]byte{0x29}, e2eecrypto.SessionKeySize)
	directHost := newDirectSignalHost("ws://unused", "", proto.RemotePermissionFull, host, func() []byte { return accountKey }, false)
	existing := &directHostAttempt{id: attemptID, sessionID: sessionID, permission: proto.RemotePermissionFull, host: directHost}
	directHost.attempts[attemptID] = existing

	message := directsignal.Message{
		AttemptID:        attemptID.String(),
		Ticket:           base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0x31}, 32)),
		SessionID:        sessionID.String(),
		UserID:           "user-1",
		HostID:           host.hostID,
		ClientInstanceID: "client-1",
		Permission:       proto.RemotePermissionFull,
		ExpiresAtUnixM:   time.Now().Add(time.Minute).UnixMilli(),
	}
	if err := directHost.startAttempt(context.Background(), message, func(directsignal.Message) error { return nil }); err == nil {
		t.Fatal("duplicate attempt accepted")
	}
	if directHost.attempts[attemptID] != existing {
		t.Fatal("duplicate attempt replaced or removed the existing entry")
	}
}

func TestDirectHostPermissionGateDefaultsDeny(t *testing.T) {
	host := newTestRelayHost(t)
	sessionID := uuid.New()
	sess := addDirectTestSession(t, host, sessionID)
	accountKey := bytes.Repeat([]byte{0x44}, e2eecrypto.SessionKeySize)
	directHost := newDirectSignalHost("ws://unused", "", proto.RemotePermissionFull, host, func() []byte { return accountKey }, false)

	view := &directHostAttempt{id: uuid.New(), sessionID: sessionID, permission: proto.RemotePermissionView, host: directHost}
	input := proto.Frame{Type: proto.TypeIn, SessionID: sessionID, Payload: []byte("x")}
	if err := view.handleRecord(context.Background(), peertransport.RecordFrame, proto.Marshal(input)); err == nil {
		t.Fatal("view permission accepted input")
	}

	sub, _ := sess.Subscribe(0, "direct:client", "client", session.WithoutAutoDrive())
	t.Cleanup(func() { sess.Unsubscribe(sub) })
	control := &directHostAttempt{id: uuid.New(), sessionID: sessionID, permission: proto.RemotePermissionControl, host: directHost, sub: sub, subscribedSession: sess}
	if err := control.handleRecord(context.Background(), peertransport.RecordFrame, proto.Marshal(input)); err == nil {
		t.Fatal("non-driver direct subscriber accepted input")
	}
	claimPayload, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "client", ClientName: "client"})
	claim := proto.Frame{Type: proto.TypeClaimDriver, SessionID: sessionID, Payload: claimPayload}
	if err := control.handleRecord(context.Background(), peertransport.RecordFrame, proto.Marshal(claim)); err != nil {
		t.Fatalf("control driver claim rejected: %v", err)
	}
	if err := control.handleRecord(context.Background(), peertransport.RecordFrame, proto.Marshal(input)); err != nil {
		t.Fatalf("control input rejected: %v", err)
	}
	select {
	case got := <-sess.Inbound():
		if got.Type != proto.TypeIn || !bytes.Equal(got.Payload, []byte("x")) {
			t.Fatalf("unexpected inbound frame: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("control input was not delivered")
	}

	for _, typ := range []proto.Type{proto.TypePasteImage, proto.TypePasteFile, proto.TypeFSRequest, proto.TypeServiceOpen, 0xff} {
		frame := proto.Frame{Type: typ, SessionID: sessionID, Payload: []byte("payload")}
		if err := control.handleRecord(context.Background(), peertransport.RecordFrame, proto.Marshal(frame)); err == nil {
			t.Fatalf("frame type 0x%02x accepted", typ)
		}
	}
	wrongSession := proto.Frame{Type: proto.TypeIn, SessionID: uuid.New(), Payload: []byte("x")}
	if err := control.handleRecord(context.Background(), peertransport.RecordFrame, proto.Marshal(wrongSession)); err == nil {
		t.Fatal("wrong-session frame accepted")
	}
}

func TestDirectSignalDisconnectClosesOnlyPendingAttempts(t *testing.T) {
	host := newTestRelayHost(t)
	sessionID := uuid.New()
	sess := addDirectTestSession(t, host, sessionID)
	directHost := newDirectSignalHost("ws://unused", "", proto.RemotePermissionFull, host, nil, false)

	newAttempt := func(consumed bool) *directHostAttempt {
		sub, _ := sess.Subscribe(0, "direct:test", "test", session.WithoutAutoDrive())
		attempt := &directHostAttempt{id: uuid.New(), sessionID: sessionID, permission: proto.RemotePermissionFull, host: directHost, consumed: consumed, sub: sub, subscribedSession: sess}
		directHost.attempts[attempt.id] = attempt
		return attempt
	}
	pending := newAttempt(false)
	active := newAttempt(true)
	if got := sess.SubscriberCount(); got != 2 {
		t.Fatalf("SubscriberCount=%d, want 2", got)
	}

	directHost.closePendingAttempts()
	if got := sess.SubscriberCount(); got != 1 {
		t.Fatalf("SubscriberCount after signaling close=%d, want 1", got)
	}
	if _, ok := directHost.attempts[pending.id]; ok {
		t.Fatal("pending attempt survived signaling close")
	}
	if directHost.attempts[active.id] != active {
		t.Fatal("authenticated attempt was closed with signaling connection")
	}

	directHost.closeAllAttempts()
	if got := sess.SubscriberCount(); got != 0 {
		t.Fatalf("SubscriberCount after host close=%d, want 0", got)
	}
}

func TestDirectHostAttemptReportsDirectionalBytes(t *testing.T) {
	var gotSent, gotReceived uint64
	attempt := &directHostAttempt{reportBytes: func(sent, received uint64) error {
		gotSent += sent
		gotReceived += received
		return nil
	}}
	attempt.addDirectBytes(200, 30, false)
	if gotSent != 0 || gotReceived != 0 {
		t.Fatal("reported before threshold")
	}
	attempt.addDirectBytes(directStatsReportThreshold-230, 0, false)
	if gotSent != directStatsReportThreshold-30 || gotReceived != 30 {
		t.Fatalf("threshold report = sent %d received %d", gotSent, gotReceived)
	}
	attempt.addDirectBytes(5, 7, true)
	if gotSent != directStatsReportThreshold-25 || gotReceived != 37 {
		t.Fatalf("forced report = sent %d received %d", gotSent, gotReceived)
	}
}

func TestDirectHostAttemptPeriodicallyReportsSmallDeltas(t *testing.T) {
	reports := make(chan [2]uint64, 2)
	attempt := &directHostAttempt{
		statsReportInterval: 5 * time.Millisecond,
		reportBytes: func(sent, received uint64) error {
			reports <- [2]uint64{sent, received}
			return nil
		},
	}
	attempt.addDirectBytes(17, 9, false)
	select {
	case report := <-reports:
		t.Fatalf("reported before interval: %v", report)
	default:
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		attempt.reportDirectStats(ctx)
	}()
	select {
	case report := <-reports:
		if report != [2]uint64{17, 9} {
			t.Fatalf("periodic report = %v, want [17 9]", report)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for periodic report")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("periodic reporter did not stop after cancellation")
	}
	attempt.addDirectBytes(0, 0, true)
	select {
	case report := <-reports:
		t.Fatalf("reported the same bytes twice: %v", report)
	default:
	}
}

func TestDirectHostAttemptCarriesSealedOutputAndInput(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	host := newTestRelayHost(t)
	sessionID := uuid.New()
	sess := addDirectTestSession(t, host, sessionID)
	accountKey := bytes.Repeat([]byte{0x52}, e2eecrypto.SessionKeySize)
	sess.PushOut(1, []byte("secret output"))

	directHost := newDirectSignalHost("ws://unused", "", proto.RemotePermissionFull, host, func() []byte { return accountKey }, false)
	directHost.webrtcConfig = webrtc.Configuration{}
	attemptID := uuid.New()
	ticket := bytes.Repeat([]byte{0x73}, 32)
	expires := time.Now().Add(time.Minute).UnixMilli()
	message := directsignal.Message{
		Version:          directsignal.Version,
		Kind:             "direct_attempt",
		AttemptID:        attemptID.String(),
		Ticket:           base64.RawURLEncoding.EncodeToString(ticket),
		SessionID:        sessionID.String(),
		UserID:           "user-1",
		HostID:           host.hostID,
		ClientInstanceID: "client-1",
		Permission:       proto.RemotePermissionFull,
		ExpiresAtUnixM:   expires,
	}
	signals := make(chan directsignal.Message, 8)
	if err := directHost.startAttempt(ctx, message, func(message directsignal.Message) error {
		signals <- message
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	defer directHost.closeAllAttempts()

	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatal(err)
	}
	defer clientPC.Close()
	dc, err := clientPC.CreateDataChannel(peertransport.TerminalDataChannelLabel, nil)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := peertransport.NewAccountKeyAuthenticator(accountKey)
	if err != nil {
		t.Fatal(err)
	}
	clientPrivate, err := peertransport.GenerateEphemeralKey()
	if err != nil {
		t.Fatal(err)
	}
	clientHello, err := peertransport.EncodeClientHello(attemptID, ticket, clientPrivate.PublicKey().Bytes())
	if err != nil {
		t.Fatal(err)
	}

	errCh := make(chan error, 8)
	records := make(chan directClientRecord, 8)
	var (
		clientMu     sync.Mutex
		clientSealer *peertransport.RecordSealer
		clientOpener *peertransport.RecordOpener
	)
	dc.OnOpen(func() {
		if err := dc.Send(clientHello); err != nil {
			nonBlockingDirectTestError(errCh, err)
		}
	})
	dc.OnMessage(func(message webrtc.DataChannelMessage) {
		clientMu.Lock()
		defer clientMu.Unlock()
		if peertransport.IsAuthOK(message.Data) {
			if clientSealer == nil {
				nonBlockingDirectTestError(errCh, errors.New("AUTH_OK before HOST_HELLO"))
				return
			}
			claimPayload, _ := json.Marshal(proto.ClaimDriverPayload{ClientID: "client-1", ClientName: "client"})
			frames := []proto.Frame{
				{Type: proto.TypeClaimDriver, SessionID: sessionID, Payload: claimPayload},
				{Type: proto.TypeIn, SessionID: sessionID, Payload: []byte("direct input")},
			}
			for _, frame := range frames {
				record, sealErr := clientSealer.Seal(peertransport.RecordFrame, proto.Marshal(frame))
				if sealErr != nil {
					nonBlockingDirectTestError(errCh, sealErr)
					return
				}
				if sendErr := dc.Send(record); sendErr != nil {
					nonBlockingDirectTestError(errCh, sendErr)
					return
				}
			}
			return
		}
		if clientOpener != nil {
			kind, payload, err := clientOpener.Open(message.Data)
			if err != nil {
				nonBlockingDirectTestError(errCh, err)
				return
			}
			records <- directClientRecord{kind: kind, payload: payload}
			return
		}
		hostPublic, hostProof, err := peertransport.DecodeHostHello(message.Data)
		if err != nil {
			nonBlockingDirectTestError(errCh, err)
			return
		}
		transcript, err := (peertransport.Transcript{
			AttemptID:           attemptID,
			Ticket:              ticket,
			SessionID:           sessionID,
			UserID:              "user-1",
			HostID:              host.hostID,
			ClientInstanceID:    "client-1",
			Permission:          peertransport.PermissionFull,
			ExpiresAtUnixMillis: uint64(expires),
			ClientPublicKey:     clientPrivate.PublicKey().Bytes(),
			HostPublicKey:       hostPublic,
		}).MarshalBinary()
		if err == nil {
			err = auth.VerifyProof(transcript, peertransport.RoleHost, hostProof)
		}
		keys, keyErr := peertransport.DeriveTrafficKeys(clientPrivate, hostPublic, transcript, auth)
		if err == nil {
			err = keyErr
		}
		if err != nil {
			nonBlockingDirectTestError(errCh, err)
			return
		}
		hash := peertransport.TranscriptHash(transcript)
		clientSealer, err = peertransport.NewRecordSealer(keys.ClientToHostKey[:], keys.ClientToHostNoncePrefix[:], hash)
		if err == nil {
			clientOpener, err = peertransport.NewRecordOpener(keys.HostToClientKey[:], keys.HostToClientNoncePrefix[:], hash)
		}
		if err != nil {
			nonBlockingDirectTestError(errCh, err)
			return
		}
		clientProof, _ := auth.BuildProof(transcript, peertransport.RoleClient)
		finishProof, _ := peertransport.BuildFinishProof(auth, transcript)
		finish, _ := peertransport.EncodeClientFinish(clientProof, finishProof)
		if err := dc.Send(finish); err != nil {
			nonBlockingDirectTestError(errCh, err)
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
		if err := directHost.handleSignal(directsignal.Message{AttemptID: attemptID.String(), SignalType: "offer", Payload: string(offerJSON)}); err != nil {
			nonBlockingDirectTestError(errCh, err)
		}
	}()

	for answerSet := false; !answerSet; {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case err := <-errCh:
			t.Fatal(err)
		case signal := <-signals:
			if signal.Kind != "signal" || signal.SignalType != "answer" {
				continue
			}
			var answer webrtc.SessionDescription
			if err := json.Unmarshal([]byte(signal.Payload), &answer); err != nil {
				t.Fatal(err)
			}
			if err := clientPC.SetRemoteDescription(answer); err != nil {
				t.Fatal(err)
			}
			answerSet = true
		}
	}

	var gotOutput, gotReady, gotInput, gotConsumed bool
	for !gotOutput || !gotReady || !gotInput || !gotConsumed {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case err := <-errCh:
			t.Fatal(err)
		case signal := <-signals:
			if signal.Kind == "consumed" && signal.AttemptID == attemptID.String() {
				gotConsumed = true
			}
		case record := <-records:
			switch record.kind {
			case peertransport.RecordDirectReady:
				if len(record.payload) != 8 || binary.BigEndian.Uint64(record.payload) != 1 {
					t.Fatalf("DIRECT_READY payload=%x", record.payload)
				}
				gotReady = true
			case peertransport.RecordFrame:
				frame, err := proto.Unmarshal(record.payload)
				if err != nil {
					t.Fatal(err)
				}
				if frame.Type != proto.TypeOut {
					continue
				}
				seq, sealed, err := proto.DecodeOut(frame.Payload)
				if err != nil || seq != 1 || bytes.Equal(sealed, []byte("secret output")) {
					t.Fatalf("unexpected sealed OUT seq=%d payload=%x err=%v", seq, sealed, err)
				}
				sessionKey, _ := e2eecrypto.DeriveSessionKey(accountKey, sessionID)
				plaintext, err := e2eecrypto.OpenOut(sessionKey, sessionID, byte(proto.TypeOut), seq, sealed)
				if err != nil || !bytes.Equal(plaintext, []byte("secret output")) {
					t.Fatalf("open sealed OUT: plaintext=%q err=%v", plaintext, err)
				}
				gotOutput = true
			}
		case frame := <-sess.Inbound():
			if frame.Type == proto.TypeIn && bytes.Equal(frame.Payload, []byte("direct input")) {
				gotInput = true
			}
		}
	}

	directHost.removeAttempt(attemptID)
	deadline := time.Now().Add(time.Second)
	for sess.SubscriberCount() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := sess.SubscriberCount(); got != 0 {
		t.Fatalf("direct subscriber leaked after close: %d", got)
	}
}

type directClientRecord struct {
	kind    peertransport.RecordKind
	payload []byte
}

func nonBlockingDirectTestError(ch chan<- error, err error) {
	select {
	case ch <- err:
	default:
	}
}

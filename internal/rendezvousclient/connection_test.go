package rendezvousclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/rendezvous"
	"nhooyr.io/websocket"
)

func TestPresenceConnectionPublishesOpaquePayloadAndReceivesAck(t *testing.T) {
	handler, err := rendezvous.New(rendezvous.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	defer handler.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	topic := encodedID(20, 32)
	recipientID := encodedID(21, 32)
	senderID := encodedID(22, 32)
	recipient, _, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: topic, PresenceID: recipientID, Role: rendezvous.RoleHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer recipient.CloseNow()
	sender, _, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: topic, PresenceID: senderID, Role: rendezvous.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sender.CloseNow()

	messageID, err := sender.Publish(ctx, recipientID, []byte("ciphertext-only"))
	if err != nil {
		t.Fatal(err)
	}
	signal, err := recipient.ReadEvent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if signal.Kind == rendezvous.KindPresence {
		signal, err = recipient.ReadEvent(ctx)
	}
	if err != nil || signal.Kind != rendezvous.KindSignal || signal.MessageID != messageID || signal.From != senderID {
		t.Fatalf("signal=%+v err=%v", signal, err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(signal.Payload)
	if err != nil || string(decoded) != "ciphertext-only" || strings.Contains(signal.Payload, "ciphertext-only") {
		t.Fatalf("opaque payload=%q decoded=%q err=%v", signal.Payload, decoded, err)
	}
	ack, err := sender.ReadEvent(ctx)
	if err != nil || ack.Kind != rendezvous.KindAck || ack.MessageID != messageID || ack.State != rendezvous.DeliveryDelivered {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
}

func TestDialPresenceObservesSnapshotAndOnlineOfflineEvents(t *testing.T) {
	handler, err := rendezvous.New(rendezvous.Config{})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	defer handler.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	topic := encodedID(1, 32)
	firstPresence := encodedID(2, 32)
	secondPresence := encodedID(3, 32)
	first, snapshot, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: topic, PresenceID: firstPresence, Role: rendezvous.RoleHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer first.CloseNow()
	if len(snapshot) != 0 {
		t.Fatalf("first snapshot=%+v", snapshot)
	}

	second, snapshot, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: server.URL, AllowInsecureLoopback: true,
		Topic: topic, PresenceID: secondPresence, Role: rendezvous.RoleMember,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot) != 1 || snapshot[0].PresenceID != firstPresence || snapshot[0].Role != rendezvous.RoleHost {
		t.Fatalf("second snapshot=%+v", snapshot)
	}
	online, err := first.ReadEvent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if online.Kind != rendezvous.KindPresence || online.Event != rendezvous.PresenceOnline || online.PresenceID != secondPresence {
		t.Fatalf("online event=%+v", online)
	}
	second.CloseNow()
	offline, err := first.ReadEvent(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if offline.Kind != rendezvous.KindPresence || offline.Event != rendezvous.PresenceOffline || offline.PresenceID != secondPresence {
		t.Fatalf("offline event=%+v", offline)
	}
}

func TestDialPresenceRejectsInvalidCoordinatesBeforeConnecting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, _, err := DialPresence(ctx, PresenceConfig{
		ServiceURL: "http://127.0.0.1:1", AllowInsecureLoopback: true,
		Topic: "space-id-must-not-be-sent", PresenceID: encodedID(1, 32), Role: rendezvous.RoleMember,
	}); err == nil {
		t.Fatal("accepted non-opaque Rendezvous topic")
	}
}

func TestDialPresenceUsesFreshChallengeIdentityForEveryConnection(t *testing.T) {
	publicKeys := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		conn, err := websocket.Accept(w, request, &websocket.AcceptOptions{Subprotocols: []string{rendezvous.Subprotocol}})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		ctx := request.Context()
		challenge := rendezvous.EventMessage{
			Version: rendezvous.Version, Kind: rendezvous.KindChallenge,
			Challenge: encodedID(9, 32), ExpiresAt: time.Now().Add(time.Minute).Unix(),
		}
		if payload, err := json.Marshal(challenge); err != nil || conn.Write(ctx, websocket.MessageText, payload) != nil {
			return
		}
		_, payload, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var registration rendezvous.RegisterMessage
		if json.Unmarshal(payload, &registration) != nil {
			return
		}
		publicKeys <- registration.PublicKey
		registered, _ := json.Marshal(rendezvous.EventMessage{Version: rendezvous.Version, Kind: rendezvous.KindRegistered})
		_ = conn.Write(ctx, websocket.MessageText, registered)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for index := 0; index < 2; index++ {
		connection, _, err := DialPresence(ctx, PresenceConfig{
			ServiceURL: server.URL, AllowInsecureLoopback: true,
			Topic: encodedID(1, 32), PresenceID: encodedID(byte(index+2), 32), Role: rendezvous.RoleMember,
		})
		if err != nil {
			t.Fatal(err)
		}
		connection.CloseNow()
	}
	got := []string{<-publicKeys, <-publicKeys}
	if got[0] == "" || got[1] == "" || reflect.DeepEqual(got[0], got[1]) {
		t.Fatalf("challenge identities were reused: %v", got)
	}
}

func TestDialPresenceClassifiesRegistrationRejections(t *testing.T) {
	for _, test := range []struct {
		name string
		code string
		want error
	}{
		{name: "authentication", code: rendezvous.CodeUnauthorized, want: ErrAuthentication},
		{name: "capacity", code: rendezvous.CodeServerCapacity, want: ErrServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
				conn, err := websocket.Accept(w, request, &websocket.AcceptOptions{Subprotocols: []string{rendezvous.Subprotocol}})
				if err != nil {
					return
				}
				defer conn.CloseNow()
				ctx := request.Context()
				challenge, _ := json.Marshal(rendezvous.EventMessage{
					Version: rendezvous.Version, Kind: rendezvous.KindChallenge,
					Challenge: encodedID(9, 32), ExpiresAt: time.Now().Add(time.Minute).Unix(),
				})
				if conn.Write(ctx, websocket.MessageText, challenge) != nil {
					return
				}
				if _, _, err := conn.Read(ctx); err != nil {
					return
				}
				rejected, _ := json.Marshal(rendezvous.EventMessage{
					Version: rendezvous.Version, Kind: rendezvous.KindError, Code: test.code,
				})
				_ = conn.Write(ctx, websocket.MessageText, rejected)
			}))
			defer server.Close()

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _, err := DialPresence(ctx, PresenceConfig{
				ServiceURL: server.URL, AllowInsecureLoopback: true,
				Topic: encodedID(1, 32), PresenceID: encodedID(2, 32), Role: rendezvous.RoleMember,
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("registration error=%v want category %v", err, test.want)
			}
		})
	}
}

func encodedID(fill byte, size int) string {
	raw := make([]byte, size)
	for index := range raw {
		raw[index] = fill
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

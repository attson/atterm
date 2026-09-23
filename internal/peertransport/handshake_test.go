package peertransport

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestHostHandshakeAuthenticatesClientAndDerivesMatchingKeys(t *testing.T) {
	accountKey := bytes.Repeat([]byte{0x42}, accountKeySize)
	auth, err := NewAccountKeyAuthenticator(accountKey)
	if err != nil {
		t.Fatal(err)
	}
	authorization := testAuthorization(time.Now().Add(time.Minute))
	host, err := NewHostHandshake(auth, authorization)
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
	result, err := host.Handle(clientHello)
	if err != nil {
		t.Fatal(err)
	}
	hostPublic, hostProof, err := DecodeHostHello(result.Response)
	if err != nil {
		t.Fatal(err)
	}
	transcript, err := buildHandshakeTestTranscript(authorization, clientPrivate.PublicKey().Bytes(), hostPublic)
	if err != nil {
		t.Fatal(err)
	}
	if err := auth.VerifyProof(transcript, RoleHost, hostProof); err != nil {
		t.Fatalf("verify host proof: %v", err)
	}
	clientProof, _ := auth.BuildProof(transcript, RoleClient)
	finishProof, _ := BuildFinishProof(auth, transcript)
	clientFinish, _ := EncodeClientFinish(clientProof, finishProof)
	result, err = host.Handle(clientFinish)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Authenticated || !IsAuthOK(result.Response) {
		t.Fatalf("final result = %+v", result)
	}
	wantKeys, err := DeriveTrafficKeys(clientPrivate, hostPublic, transcript, auth)
	if err != nil {
		t.Fatal(err)
	}
	if result.TrafficKeys != wantKeys {
		t.Fatal("host and client traffic keys differ")
	}
	if _, err := host.Handle(clientFinish); !errors.Is(err, ErrInvalidHandshake) {
		t.Fatalf("repeated finish err=%v", err)
	}
}

func TestHostHandshakeRejectsExpiredTicketAndMutatedProofs(t *testing.T) {
	accountKey := bytes.Repeat([]byte{0x51}, accountKeySize)
	auth, _ := NewAccountKeyAuthenticator(accountKey)
	authorization := testAuthorization(time.Now().Add(time.Minute))
	clientPrivate, _ := GenerateEphemeralKey()

	expired, _ := NewHostHandshake(auth, testAuthorization(time.Now().Add(-time.Millisecond)))
	expired.now = time.Now
	hello, _ := EncodeClientHello(expired.authorization.AttemptID, expired.authorization.Ticket, clientPrivate.PublicKey().Bytes())
	if _, err := expired.Handle(hello); !errors.Is(err, ErrInvalidHandshake) {
		t.Fatalf("expired err=%v", err)
	}

	wrongTicketHost, _ := NewHostHandshake(auth, authorization)
	wrongTicket := append([]byte(nil), authorization.Ticket...)
	wrongTicket[0] ^= 0xff
	hello, _ = EncodeClientHello(authorization.AttemptID, wrongTicket, clientPrivate.PublicKey().Bytes())
	if _, err := wrongTicketHost.Handle(hello); !errors.Is(err, ErrInvalidHandshake) {
		t.Fatalf("wrong ticket err=%v", err)
	}

	mutatedHost, _ := NewHostHandshake(auth, authorization)
	hello, _ = EncodeClientHello(authorization.AttemptID, authorization.Ticket, clientPrivate.PublicKey().Bytes())
	first, err := mutatedHost.Handle(hello)
	if err != nil {
		t.Fatal(err)
	}
	hostPublic, _, _ := DecodeHostHello(first.Response)
	transcript, _ := buildHandshakeTestTranscript(authorization, clientPrivate.PublicKey().Bytes(), hostPublic)
	clientProof, _ := auth.BuildProof(transcript, RoleClient)
	finishProof, _ := BuildFinishProof(auth, transcript)
	clientProof[0] ^= 0xff
	finish, _ := EncodeClientFinish(clientProof, finishProof)
	if _, err := mutatedHost.Handle(finish); !errors.Is(err, ErrInvalidHandshake) {
		t.Fatalf("mutated proof err=%v", err)
	}
}

func testAuthorization(expiresAt time.Time) Authorization {
	return Authorization{
		AttemptID:           uuid.New(),
		Ticket:              bytes.Repeat([]byte{0x31}, directTicketSize),
		SessionID:           uuid.New(),
		UserID:              "user-01",
		HostID:              "host-01",
		ClientInstanceID:    "client-01",
		Permission:          PermissionControl,
		ExpiresAtUnixMillis: uint64(expiresAt.UnixMilli()),
	}
}

func buildHandshakeTestTranscript(authorization Authorization, clientPublic, hostPublic []byte) ([]byte, error) {
	return (Transcript{
		AttemptID:           authorization.AttemptID,
		Ticket:              authorization.Ticket,
		SessionID:           authorization.SessionID,
		UserID:              authorization.UserID,
		HostID:              authorization.HostID,
		ClientInstanceID:    authorization.ClientInstanceID,
		Permission:          authorization.Permission,
		ExpiresAtUnixMillis: authorization.ExpiresAtUnixMillis,
		ClientPublicKey:     clientPublic,
		HostPublicKey:       hostPublic,
	}).MarshalBinary()
}

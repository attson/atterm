package configsync

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

func testIdentity(t *testing.T) *peercrypto.Identity {
	t.Helper()
	id, err := peercrypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testToken(t *testing.T, id *peercrypto.Identity, spaceID string) string {
	t.Helper()
	token, err := SignOp(id, spaceID, 1, Timestamp{PhysicalMS: 1_800_000_000_000}, nil, Mutation{
		SchemaVersion: 1,
		Collection:    "preferences",
		RecordID:      "terminal_theme",
		Kind:          KindSet,
		Payload:       []byte(`{"name":"dark"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	return token
}

func TestSignedOpRoundTripAndMutation(t *testing.T) {
	id := testIdentity(t)
	spaceID := uuid.NewString()
	token := testToken(t, id, spaceID)
	verified, err := VerifyOp(token)
	if err != nil {
		t.Fatal(err)
	}
	if verified.Document.OpID != OpIDFor(id.PeerID(), 1) || verified.Document.SpaceID != spaceID {
		t.Fatalf("unexpected verified operation: %+v", verified.Document)
	}

	parts := strings.Split(token, ".")
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	raw = []byte(strings.Replace(string(raw), "terminal_theme", "terminal_thfme", 1))
	parts[1] = encode(raw)
	if _, err := VerifyOp(strings.Join(parts, ".")); err == nil {
		t.Fatal("operation with mutated signed bytes verified")
	}
}

func TestSignedOpRejectsActorKeyMismatchAndUnknownFields(t *testing.T) {
	id := testIdentity(t)
	other := testIdentity(t)
	token := testToken(t, id, uuid.NewString())
	verified, err := VerifyOp(token)
	if err != nil {
		t.Fatal(err)
	}

	doc := verified.Document
	doc.ActorPublicKey = encode(other.PublicBytes())
	if _, err := VerifyOp(resignTestDocument(t, id, doc)); err == nil {
		t.Fatal("actor id/public-key mismatch verified")
	}

	parts := strings.Split(token, ".")
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	raw = append(raw[:len(raw)-1], []byte(`,"unknown":true}`)...)
	signature, err := id.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	unknownToken := opTokenPrefix + "." + encode(raw) + "." + encode(signature)
	if _, err := VerifyOp(unknownToken); err == nil {
		t.Fatal("unknown JSON field was accepted")
	}
}

func TestSignedOpRejectsBrokenCounterChain(t *testing.T) {
	id := testIdentity(t)
	_, err := SignOp(id, uuid.NewString(), 2, Timestamp{PhysicalMS: 1}, nil, Mutation{
		SchemaVersion: 1, Collection: "preferences", RecordID: "locale", Kind: KindSet, Payload: []byte("zh-CN"),
	})
	if err == nil {
		t.Fatal("counter 2 without causal counter 1 was accepted")
	}
}

func resignTestDocument(t *testing.T, id *peercrypto.Identity, doc SyncOp) string {
	t.Helper()
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	signature, err := id.Sign(raw)
	if err != nil {
		t.Fatal(err)
	}
	return opTokenPrefix + "." + encode(raw) + "." + encode(signature)
}

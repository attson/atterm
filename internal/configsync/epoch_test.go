package configsync

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/peercrypto"
	"github.com/google/uuid"
)

func TestEpochEnvelopeRoundTripAndBinding(t *testing.T) {
	spaceID := uuid.NewString()
	signingIdentity := testIdentity(t)
	wrappingIdentity, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	key, err := GenerateEpochKey(KeyClassSync, 7)
	if err != nil {
		t.Fatal(err)
	}
	recipient := EpochRecipient{
		PeerID: signingIdentity.PeerID(), WrappingPublicKey: wrappingIdentity.PublicBytes(),
	}
	token, err := SealEpochKey(spaceID, key, recipient)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenEpochKey(token, spaceID, signingIdentity.PeerID(), wrappingIdentity)
	if err != nil {
		t.Fatal(err)
	}
	if opened.Class != key.Class || opened.Epoch != key.Epoch || !bytes.Equal(opened.Bytes(), key.Bytes()) {
		t.Fatalf("opened key=%+v want class=%s epoch=%d", opened, key.Class, key.Epoch)
	}

	otherWrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenEpochKey(token, spaceID, signingIdentity.PeerID(), otherWrapping); !errors.Is(err, ErrInvalidEpochEnvelope) {
		t.Fatalf("wrong wrapping key error=%v", err)
	}
	if _, err := OpenEpochKey(token, uuid.NewString(), signingIdentity.PeerID(), wrappingIdentity); !errors.Is(err, ErrInvalidEpochEnvelope) {
		t.Fatalf("wrong space error=%v", err)
	}
	if _, err := OpenEpochKey(tamperEpochEnvelope(t, token), spaceID, signingIdentity.PeerID(), wrappingIdentity); !errors.Is(err, ErrInvalidEpochEnvelope) {
		t.Fatalf("tampered envelope error=%v", err)
	}
}

func TestVaultEnvelopeRequiresCapability(t *testing.T) {
	identity := testIdentity(t)
	wrapping, err := peercrypto.GenerateWrappingIdentity()
	if err != nil {
		t.Fatal(err)
	}
	key, err := GenerateEpochKey(KeyClassVault, 1)
	if err != nil {
		t.Fatal(err)
	}
	recipient := EpochRecipient{PeerID: identity.PeerID(), WrappingPublicKey: wrapping.PublicBytes()}
	if _, err := SealEpochKey(uuid.NewString(), key, recipient); !errors.Is(err, ErrSecretSyncDenied) {
		t.Fatalf("vault capability error=%v", err)
	}
	recipient.CanSyncSecrets = true
	if _, err := SealEpochKey(uuid.NewString(), key, recipient); err != nil {
		t.Fatal(err)
	}
}

func TestConfigPayloadBindsEveryContextField(t *testing.T) {
	identity := testIdentity(t)
	key, err := GenerateEpochKey(KeyClassSync, 3)
	if err != nil {
		t.Fatal(err)
	}
	context := PayloadContext{
		SpaceID: uuid.NewString(), Collection: "preferences", RecordID: "theme",
		OpID: OpIDFor(identity.PeerID(), 9), KeyClass: KeyClassSync, Epoch: 3,
	}
	plaintext := []byte(`{"name":"dark"}`)
	sealed, err := SealConfigPayload(key, context, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := OpenConfigPayload(key, context, sealed)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("open payload=%q err=%v", opened, err)
	}

	mutations := []PayloadContext{
		{SpaceID: uuid.NewString(), Collection: context.Collection, RecordID: context.RecordID, OpID: context.OpID, KeyClass: context.KeyClass, Epoch: context.Epoch},
		{SpaceID: context.SpaceID, Collection: "profiles", RecordID: context.RecordID, OpID: context.OpID, KeyClass: context.KeyClass, Epoch: context.Epoch},
		{SpaceID: context.SpaceID, Collection: context.Collection, RecordID: "locale", OpID: context.OpID, KeyClass: context.KeyClass, Epoch: context.Epoch},
		{SpaceID: context.SpaceID, Collection: context.Collection, RecordID: context.RecordID, OpID: OpIDFor(identity.PeerID(), 10), KeyClass: context.KeyClass, Epoch: context.Epoch},
	}
	for index, changed := range mutations {
		if _, err := OpenConfigPayload(key, changed, sealed); !errors.Is(err, ErrPayloadAuth) {
			t.Fatalf("context mutation %d error=%v", index, err)
		}
	}

	wrongEpoch, _ := ParseEpochKey(KeyClassSync, 4, key.Bytes())
	changedEpoch := context
	changedEpoch.Epoch = 4
	if _, err := OpenConfigPayload(wrongEpoch, changedEpoch, sealed); !errors.Is(err, ErrPayloadAuth) {
		t.Fatalf("epoch replay error=%v", err)
	}
	wrongClass, _ := ParseEpochKey(KeyClassVault, 3, key.Bytes())
	changedClass := context
	changedClass.KeyClass = KeyClassVault
	if _, err := OpenConfigPayload(wrongClass, changedClass, sealed); !errors.Is(err, ErrPayloadAuth) {
		t.Fatalf("class replay error=%v", err)
	}
	otherKey, _ := GenerateEpochKey(KeyClassSync, 3)
	if _, err := OpenConfigPayload(otherKey, context, sealed); !errors.Is(err, ErrPayloadAuth) {
		t.Fatalf("wrong key error=%v", err)
	}
}

func TestSignEncryptedOpAndOpenRecord(t *testing.T) {
	spaceID := uuid.NewString()
	identity := testIdentity(t)
	key, err := GenerateEpochKey(KeyClassSync, 2)
	if err != nil {
		t.Fatal(err)
	}
	replica := newTestReplica(t, spaceID, 1)
	plaintext := `{"font_size":14}`
	op, err := replica.AppendEncrypted(identity, key, Mutation{
		Collection: "preferences", RecordID: "terminal_font", Kind: KindSet, Payload: []byte(plaintext),
	})
	if err != nil {
		t.Fatal(err)
	}
	if op.Document.KeyClass != KeyClassSync || op.Document.KeyEpoch != 2 || bytes.Equal(op.Document.Payload, []byte(plaintext)) {
		t.Fatalf("operation payload was not sealed: %+v", op.Document)
	}
	opened, err := OpenOpPayload(key, op)
	if err != nil || string(opened) != plaintext {
		t.Fatalf("opened operation=%q err=%v", opened, err)
	}
	record, ok := replica.Get("preferences", "terminal_font")
	if !ok {
		t.Fatal("encrypted record missing")
	}
	opened, err = OpenRecordPayload(key, record)
	if err != nil || string(opened) != plaintext {
		t.Fatalf("opened record=%q err=%v", opened, err)
	}
}

func tamperEpochEnvelope(t *testing.T, token string) string {
	t.Helper()
	parts := strings.Split(token, ".")
	raw, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var doc epochEnvelope
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	ciphertext, err := base64.RawURLEncoding.Strict().DecodeString(doc.Ciphertext)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext[len(ciphertext)-1] ^= 1
	doc.Ciphertext = encode(ciphertext)
	raw, err = json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return epochEnvelopePrefix + "." + encode(raw)
}

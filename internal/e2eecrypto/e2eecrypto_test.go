package e2eecrypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/google/uuid"
)

const testFrameType = 0x03 // matches proto.TypeOut

func mustKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return k
}

// TestDeriveSessionKey_DeterministicAndSessionScoped covers the load-
// bearing properties of HKDF derivation: same inputs → same key, and
// changing the session_uuid yields a totally different key. Without
// these two, replay across sessions or non-determinism between agent
// and client would surface only at runtime as silent decrypt failures.
func TestDeriveSessionKey_DeterministicAndSessionScoped(t *testing.T) {
	accountKey := mustKey(t)
	sid1 := uuid.New()
	sid2 := uuid.New()

	k1a, err := DeriveSessionKey(accountKey, sid1)
	if err != nil {
		t.Fatalf("derive sid1 attempt 1: %v", err)
	}
	k1b, err := DeriveSessionKey(accountKey, sid1)
	if err != nil {
		t.Fatalf("derive sid1 attempt 2: %v", err)
	}
	if !bytes.Equal(k1a, k1b) {
		t.Fatalf("derivation not deterministic: %x vs %x", k1a, k1b)
	}
	if len(k1a) != SessionKeySize {
		t.Fatalf("session_key length = %d, want %d", len(k1a), SessionKeySize)
	}

	k2, _ := DeriveSessionKey(accountKey, sid2)
	if bytes.Equal(k1a, k2) {
		t.Fatalf("different session_uuid produced same key")
	}
}

func TestDeriveSessionKey_RejectsShortAccountKey(t *testing.T) {
	if _, err := DeriveSessionKey(make([]byte, 16), uuid.New()); !errors.Is(err, ErrAccountKeyShort) {
		t.Fatalf("expected ErrAccountKeyShort, got %v", err)
	}
}

func TestSealOpenOut_RoundTrip(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	payload := []byte("\x1b[31mhello relay\x1b[0m\n")
	env, err := SealOut(key, sid, testFrameType, 42, payload)
	if err != nil {
		t.Fatalf("SealOut: %v", err)
	}
	if len(env) < EnvelopePrefixSize+16 {
		t.Fatalf("envelope too short: %d", len(env))
	}
	if env[0] != byte(CipherXChaCha20Poly1305) {
		t.Fatalf("cipher_id = 0x%02x, want 0x01", env[0])
	}
	got, err := OpenOut(key, sid, testFrameType, 42, env)
	if err != nil {
		t.Fatalf("OpenOut: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch")
	}
}

func TestOpen_TamperedCiphertextFails(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	env, _ := SealOut(key, sid, testFrameType, 1, []byte("hello"))
	// Flip the last byte of the tag.
	env[len(env)-1] ^= 0x01
	if _, err := OpenOut(key, sid, testFrameType, 1, env); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed, got %v", err)
	}
}

// AAD binding is the load-bearing property: the same ciphertext must NOT
// decrypt under a different session, frame type, or seq. Without these
// bindings the relay could replay a chunk from session A into session B,
// or a client could reuse a recorded OUT chunk under a forged seq.
func TestOpen_AADBinding_SessionID(t *testing.T) {
	key := mustKey(t)
	sidA := uuid.New()
	sidB := uuid.New()
	env, _ := SealOut(key, sidA, testFrameType, 1, []byte("hello"))
	if _, err := OpenOut(key, sidB, testFrameType, 1, env); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed under wrong sessionID, got %v", err)
	}
}

func TestOpen_AADBinding_FrameType(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	env, _ := SealOut(key, sid, testFrameType, 1, []byte("hello"))
	if _, err := OpenOut(key, sid, 0x02 /* TypeIn */, 1, env); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed under wrong frame_type, got %v", err)
	}
}

func TestOpen_AADBinding_Seq(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	env, _ := SealOut(key, sid, testFrameType, 1, []byte("hello"))
	if _, err := OpenOut(key, sid, testFrameType, 2, env); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed under wrong seq, got %v", err)
	}
}

func TestOpen_WrongKeyFails(t *testing.T) {
	keyA := mustKey(t)
	keyB := mustKey(t)
	sid := uuid.New()
	env, _ := SealOut(keyA, sid, testFrameType, 1, []byte("hello"))
	if _, err := OpenOut(keyB, sid, testFrameType, 1, env); !errors.Is(err, ErrAuthFailed) {
		t.Fatalf("expected ErrAuthFailed under wrong key, got %v", err)
	}
}

func TestOpen_UnknownCipherIDFails(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	env, _ := SealOut(key, sid, testFrameType, 1, []byte("hello"))
	env[0] = 0xff // unknown cipher_id
	if _, err := OpenOut(key, sid, testFrameType, 1, env); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestOpen_TruncatedEnvelopeFails(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	short := make([]byte, EnvelopePrefixSize-1)
	if _, err := OpenOut(key, sid, testFrameType, 1, short); !errors.Is(err, ErrInvalidEnvelope) {
		t.Fatalf("expected ErrInvalidEnvelope, got %v", err)
	}
}

func TestSealOpenUnsequenced_RoundTrip(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	payload := []byte("paste-image-bytes")
	env, err := SealUnsequenced(key, sid, 0x33 /* TypePasteImage */, payload)
	if err != nil {
		t.Fatalf("SealUnsequenced: %v", err)
	}
	got, err := OpenUnsequenced(key, sid, 0x33, env)
	if err != nil {
		t.Fatalf("OpenUnsequenced: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("round-trip mismatch")
	}
}

// TestUnsequenced_NoSeqBinding intentionally documents a non-property:
// because unsequenced frames don't bind seq into the AAD, an attacker
// who has captured one TypeIn envelope can replay it (relay → agent)
// and the agent will accept it. M2's threat model treats this as out of
// scope (relay is the attacker we're hiding content from, not a
// confused agent); a later sequence binding for TypeIn would close the
// gap if needed.
func TestUnsequenced_NoSeqBinding(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	env, _ := SealUnsequenced(key, sid, 0x02 /* TypeIn */, []byte("up arrow"))
	got, err := OpenUnsequenced(key, sid, 0x02, env)
	if err != nil {
		t.Fatalf("OpenUnsequenced: %v", err)
	}
	if !bytes.Equal(got, []byte("up arrow")) {
		t.Fatalf("mismatch")
	}
	// Second open with the same envelope still succeeds — replay is not
	// blocked at the codec layer.
	if _, err := OpenUnsequenced(key, sid, 0x02, env); err != nil {
		t.Fatalf("replay opens: %v (test expects replay to succeed at codec layer)", err)
	}
}

// TestUnsequenced_PasteFileAAD proves the AAD binds the PASTE_FILE frame
// type: sealing with 0x37 and opening with 0x33 (PasteImage) must fail,
// otherwise a compromised relay could re-tag a paste-file envelope as a
// paste-image and slip content past mime-typed frontend paths.
func TestUnsequenced_PasteFileAAD(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	pt := []byte(`{"filename":"foo.pdf","content_type":"application/pdf","data":"aGk="}`)
	env, err := SealUnsequenced(key, sid, 0x37 /* TypePasteFile */, pt)
	if err != nil {
		t.Fatalf("SealUnsequenced: %v", err)
	}
	got, err := OpenUnsequenced(key, sid, 0x37, env)
	if err != nil {
		t.Fatalf("OpenUnsequenced matching AAD: %v", err)
	}
	if !bytes.Equal(got, pt) {
		t.Fatalf("plaintext mismatch")
	}
	if _, err := OpenUnsequenced(key, sid, 0x33 /* TypePasteImage */, env); err == nil {
		t.Fatalf("open with mismatched frame_type AAD should fail")
	}
}

// TestNonceUniquenessProbability is a sanity check that two consecutive
// seals of the same plaintext produce different envelopes (different
// nonce bytes). Without this, a buggy rand source would silently break
// security; with it, a basic regression test catches "seed=0" mistakes.
func TestNonceUniquenessProbability(t *testing.T) {
	key := mustKey(t)
	sid := uuid.New()
	env1, _ := SealOut(key, sid, testFrameType, 1, []byte("repeat"))
	env2, _ := SealOut(key, sid, testFrameType, 1, []byte("repeat"))
	if bytes.Equal(env1, env2) {
		t.Fatalf("two seals of the same plaintext+seq produced identical envelopes; rand source broken")
	}
}

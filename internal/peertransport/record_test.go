package peertransport

import (
	"bytes"
	"errors"
	"testing"
)

func recordPair(t *testing.T) (*RecordSealer, *RecordOpener) {
	t.Helper()
	key := bytes.Repeat([]byte{0x71}, 32)
	prefix := bytes.Repeat([]byte{0x19}, 16)
	hash := TranscriptHash([]byte("test transcript"))
	sealer, err := NewRecordSealer(key, prefix, hash)
	if err != nil {
		t.Fatal(err)
	}
	opener, err := NewRecordOpener(key, prefix, hash)
	if err != nil {
		t.Fatal(err)
	}
	return sealer, opener
}

func TestRecordRoundTripAndSequence(t *testing.T) {
	sealer, opener := recordPair(t)
	first, err := sealer.Seal(RecordFrame, []byte("frame-one"))
	if err != nil {
		t.Fatal(err)
	}
	kind, plaintext, err := opener.Open(first)
	if err != nil {
		t.Fatal(err)
	}
	if kind != RecordFrame || !bytes.Equal(plaintext, []byte("frame-one")) {
		t.Fatalf("opened %d %q", kind, plaintext)
	}
	if _, _, err := opener.Open(first); !errors.Is(err, ErrRecordSequence) {
		t.Fatalf("replay accepted: %v", err)
	}
	second, _ := sealer.Seal(RecordPing, bytes.Repeat([]byte{1}, 8))
	if _, _, err := opener.Open(second); err != nil {
		t.Fatalf("second record after rejected replay: %v", err)
	}
}

func TestRecordRejectsTamperDirectionAndBounds(t *testing.T) {
	sealer, _ := recordPair(t)
	record, _ := sealer.Seal(RecordFrame, []byte("secret"))

	_, opener := recordPair(t)
	tampered := append([]byte(nil), record...)
	tampered[len(tampered)-1] ^= 1
	if _, _, err := opener.Open(tampered); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("tamper accepted: %v", err)
	}
	if _, _, err := opener.Open(record); err != nil {
		t.Fatalf("failed authentication advanced counter: %v", err)
	}

	_, gapOpener := recordPair(t)
	gap := append([]byte(nil), record...)
	gap[9] = 1
	if _, _, err := gapOpener.Open(gap); !errors.Is(err, ErrRecordSequence) {
		t.Fatalf("counter gap accepted: %v", err)
	}

	wrongKey, _ := NewRecordOpener(bytes.Repeat([]byte{0x72}, 32), bytes.Repeat([]byte{0x19}, 16), TranscriptHash([]byte("test transcript")))
	if _, _, err := wrongKey.Open(record); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("wrong direction key accepted: %v", err)
	}

	if _, err := sealer.Seal(RecordFrame, make([]byte, MaxRecordPlaintext+1)); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("oversize accepted: %v", err)
	}
	if _, err := sealer.Seal(99, nil); !errors.Is(err, ErrInvalidRecord) {
		t.Fatalf("unknown kind accepted: %v", err)
	}
}

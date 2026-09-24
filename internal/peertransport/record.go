package peertransport

import (
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"golang.org/x/crypto/chacha20poly1305"
)

const (
	recordVersion    = 1
	recordHeaderSize = 1 + 1 + 8 + 4
	recordTagSize    = 16
	// MaxRecordPlaintext keeps one DataChannel message well below browser
	// implementation limits; larger terminal frames use FRAGMENT records.
	MaxRecordPlaintext = 16 * 1024
)

var (
	// ErrInvalidRecord covers structure, kind, size and AEAD failures. Callers
	// close the direct route and fall back instead of trying to recover in-band.
	ErrInvalidRecord = errors.New("peertransport: invalid record")
	// ErrRecordSequence means a duplicate, gap or exhausted counter.
	ErrRecordSequence = errors.New("peertransport: invalid record sequence")
)

// RecordKind identifies the plaintext carried inside a direct record.
type RecordKind byte

const (
	RecordFrame       RecordKind = 1
	RecordFragment    RecordKind = 2
	RecordDirectReady RecordKind = 3
	RecordPing        RecordKind = 4
	RecordPong        RecordKind = 5
	RecordClose       RecordKind = 6
)

func (k RecordKind) valid() bool {
	return k >= RecordFrame && k <= RecordClose
}

// RecordSealer emits strictly increasing records for one direction.
type RecordSealer struct {
	aead           cipher.AEAD
	noncePrefix    [directionalNonceSize]byte
	transcriptHash [32]byte
	next           uint64
	exhausted      bool
}

// NewRecordSealer constructs one direction of the direct record layer.
func NewRecordSealer(key, noncePrefix []byte, transcriptHash [32]byte) (*RecordSealer, error) {
	aead, prefix, err := newRecordCipher(key, noncePrefix)
	if err != nil {
		return nil, err
	}
	return &RecordSealer{aead: aead, noncePrefix: prefix, transcriptHash: transcriptHash}, nil
}

// Seal encrypts plaintext at the next record counter.
func (s *RecordSealer) Seal(kind RecordKind, plaintext []byte) ([]byte, error) {
	if s == nil || s.aead == nil {
		return nil, fmt.Errorf("%w: nil sealer", ErrInvalidRecord)
	}
	if s.exhausted {
		return nil, ErrRecordSequence
	}
	if !kind.valid() {
		return nil, fmt.Errorf("%w: kind %d", ErrInvalidRecord, kind)
	}
	if len(plaintext) > MaxRecordPlaintext {
		return nil, fmt.Errorf("%w: plaintext is %d bytes", ErrInvalidRecord, len(plaintext))
	}
	counter := s.next
	header := makeRecordHeader(kind, counter, len(plaintext))
	nonce := makeRecordNonce(s.noncePrefix, counter)
	aad := makeRecordAAD(s.transcriptHash, header)
	record := s.aead.Seal(header, nonce, plaintext, aad)
	if counter == math.MaxUint64 {
		s.exhausted = true
	} else {
		s.next++
	}
	return record, nil
}

// RecordOpener accepts only the next record counter for one direction.
type RecordOpener struct {
	aead           cipher.AEAD
	noncePrefix    [directionalNonceSize]byte
	transcriptHash [32]byte
	next           uint64
	exhausted      bool
}

// NewRecordOpener constructs one direction of the direct record layer.
func NewRecordOpener(key, noncePrefix []byte, transcriptHash [32]byte) (*RecordOpener, error) {
	aead, prefix, err := newRecordCipher(key, noncePrefix)
	if err != nil {
		return nil, err
	}
	return &RecordOpener{aead: aead, noncePrefix: prefix, transcriptHash: transcriptHash}, nil
}

// Open validates structure, sequence and AEAD before advancing the counter.
func (o *RecordOpener) Open(record []byte) (RecordKind, []byte, error) {
	if o == nil || o.aead == nil {
		return 0, nil, fmt.Errorf("%w: nil opener", ErrInvalidRecord)
	}
	if o.exhausted {
		return 0, nil, ErrRecordSequence
	}
	if len(record) < recordHeaderSize+recordTagSize {
		return 0, nil, fmt.Errorf("%w: truncated", ErrInvalidRecord)
	}
	if record[0] != recordVersion {
		return 0, nil, fmt.Errorf("%w: version %d", ErrInvalidRecord, record[0])
	}
	kind := RecordKind(record[1])
	if !kind.valid() {
		return 0, nil, fmt.Errorf("%w: kind %d", ErrInvalidRecord, kind)
	}
	counter := binary.BigEndian.Uint64(record[2:10])
	if counter != o.next {
		return 0, nil, fmt.Errorf("%w: got %d want %d", ErrRecordSequence, counter, o.next)
	}
	plainLen := binary.BigEndian.Uint32(record[10:14])
	if plainLen > MaxRecordPlaintext || len(record) != recordHeaderSize+int(plainLen)+recordTagSize {
		return 0, nil, fmt.Errorf("%w: length %d", ErrInvalidRecord, plainLen)
	}
	header := record[:recordHeaderSize]
	nonce := makeRecordNonce(o.noncePrefix, counter)
	aad := makeRecordAAD(o.transcriptHash, header)
	plaintext, err := o.aead.Open(nil, nonce, record[recordHeaderSize:], aad)
	if err != nil {
		return 0, nil, fmt.Errorf("%w: authentication", ErrInvalidRecord)
	}
	if counter == math.MaxUint64 {
		o.exhausted = true
	} else {
		o.next++
	}
	return kind, plaintext, nil
}

func newRecordCipher(key, noncePrefix []byte) (cipher.AEAD, [directionalNonceSize]byte, error) {
	var prefix [directionalNonceSize]byte
	if len(key) != chacha20poly1305.KeySize || len(noncePrefix) != directionalNonceSize {
		return nil, prefix, fmt.Errorf("%w: key/prefix size %d/%d", ErrInvalidKey, len(key), len(noncePrefix))
	}
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, prefix, fmt.Errorf("XChaCha20-Poly1305: %w", err)
	}
	copy(prefix[:], noncePrefix)
	return aead, prefix, nil
}

func makeRecordHeader(kind RecordKind, counter uint64, plainLen int) []byte {
	header := make([]byte, recordHeaderSize)
	header[0] = recordVersion
	header[1] = byte(kind)
	binary.BigEndian.PutUint64(header[2:10], counter)
	binary.BigEndian.PutUint32(header[10:14], uint32(plainLen))
	return header
}

func makeRecordNonce(prefix [directionalNonceSize]byte, counter uint64) []byte {
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	copy(nonce, prefix[:])
	binary.BigEndian.PutUint64(nonce[directionalNonceSize:], counter)
	return nonce
}

func makeRecordAAD(transcriptHash [32]byte, header []byte) []byte {
	aad := make([]byte, 0, len(transcriptHash)+len(header))
	aad = append(aad, transcriptHash[:]...)
	return append(aad, header...)
}

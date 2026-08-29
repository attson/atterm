// Package serviceproxy implements the end-to-end encrypted multiplex codec
// used by Remote Web Preview. The relay validates only the fixed header and
// forwards the ciphertext; owner/client endpoints are the only AEAD holders.
package serviceproxy

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/google/uuid"
)

const (
	Version          byte = 1
	HeaderSize            = 24
	MaxPlaintextSize      = 64 * 1024
	GCMTagSize            = 16
	MaxPacketSize         = HeaderSize + MaxPlaintextSize + GCMTagSize
)

var magic = [4]byte{'A', 'T', 'S', 'P'}

type Kind byte

const (
	KindOpen  Kind = 1
	KindData  Kind = 2
	KindClose Kind = 3
)

var (
	ErrInvalidPacket = errors.New("serviceproxy: invalid packet")
	ErrReplay        = errors.New("serviceproxy: replayed or reordered packet")
)

type Header struct {
	Kind       Kind
	Connection uint32
	Sequence   uint64
	SealedLen  uint32
}

// Codec seals one direction and opens the opposite direction. Seal is safe for
// concurrent TCP reader goroutines; it serializes sequence allocation so AES-
// GCM nonces are never reused.
type Codec struct {
	serviceID uuid.UUID
	tx        cipher.AEAD
	rx        cipher.AEAD
	txMu      sync.Mutex
	txSeq     uint64
	rxMu      sync.Mutex
	rxSeq     uint64
}

func NewCodec(serviceID uuid.UUID, txKey, rxKey []byte) (*Codec, error) {
	tx, err := newGCM(txKey)
	if err != nil {
		return nil, fmt.Errorf("tx: %w", err)
	}
	rx, err := newGCM(rxKey)
	if err != nil {
		return nil, fmt.Errorf("rx: %w", err)
	}
	return &Codec{serviceID: serviceID, tx: tx, rx: rx}, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (c *Codec) Seal(kind Kind, connection uint32, plaintext []byte) ([]byte, error) {
	if !validKind(kind) || connection == 0 || len(plaintext) > MaxPlaintextSize {
		return nil, ErrInvalidPacket
	}
	c.txMu.Lock()
	defer c.txMu.Unlock()
	if c.txSeq == ^uint64(0) {
		return nil, ErrInvalidPacket
	}
	c.txSeq++
	sealedLen := len(plaintext) + c.tx.Overhead()
	packet := make([]byte, HeaderSize, HeaderSize+sealedLen)
	writeHeader(packet, Header{Kind: kind, Connection: connection, Sequence: c.txSeq, SealedLen: uint32(sealedLen)})
	nonce := sequenceNonce(c.txSeq)
	aad := c.aad(packet)
	return c.tx.Seal(packet, nonce[:], plaintext, aad), nil
}

func (c *Codec) Open(packet []byte) (Header, []byte, error) {
	h, err := ParseHeader(packet)
	if err != nil {
		return Header{}, nil, err
	}
	c.rxMu.Lock()
	defer c.rxMu.Unlock()
	if h.Sequence <= c.rxSeq {
		return Header{}, nil, ErrReplay
	}
	nonce := sequenceNonce(h.Sequence)
	plaintext, err := c.rx.Open(nil, nonce[:], packet[HeaderSize:], c.aad(packet[:HeaderSize]))
	if err != nil {
		return Header{}, nil, fmt.Errorf("%w: authentication failed", ErrInvalidPacket)
	}
	c.rxSeq = h.Sequence
	return h, plaintext, nil
}

// ParseHeader is the relay-visible structural validator. It does not decrypt
// or authenticate content; endpoints must still call Codec.Open.
func ParseHeader(packet []byte) (Header, error) {
	if len(packet) < HeaderSize+GCMTagSize || len(packet) > MaxPacketSize {
		return Header{}, ErrInvalidPacket
	}
	if string(packet[:4]) != string(magic[:]) || packet[4] != Version || packet[6] != 0 || packet[7] != 0 {
		return Header{}, ErrInvalidPacket
	}
	h := Header{
		Kind:       Kind(packet[5]),
		Connection: binary.BigEndian.Uint32(packet[8:12]),
		Sequence:   binary.BigEndian.Uint64(packet[12:20]),
		SealedLen:  binary.BigEndian.Uint32(packet[20:24]),
	}
	if !validKind(h.Kind) || h.Connection == 0 || h.Sequence == 0 || int(h.SealedLen) != len(packet)-HeaderSize {
		return Header{}, ErrInvalidPacket
	}
	return h, nil
}

func writeHeader(dst []byte, h Header) {
	copy(dst[:4], magic[:])
	dst[4] = Version
	dst[5] = byte(h.Kind)
	dst[6], dst[7] = 0, 0
	binary.BigEndian.PutUint32(dst[8:12], h.Connection)
	binary.BigEndian.PutUint64(dst[12:20], h.Sequence)
	binary.BigEndian.PutUint32(dst[20:24], h.SealedLen)
}

func validKind(kind Kind) bool {
	return kind == KindOpen || kind == KindData || kind == KindClose
}

func sequenceNonce(seq uint64) [12]byte {
	var nonce [12]byte
	binary.BigEndian.PutUint64(nonce[4:], seq)
	return nonce
}

func (c *Codec) aad(header []byte) []byte {
	aad := make([]byte, 16+HeaderSize)
	copy(aad[:16], c.serviceID[:])
	copy(aad[16:], header[:HeaderSize])
	return aad
}

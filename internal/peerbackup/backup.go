// Package peerbackup seals accountless Peer Space recovery material with a
// user-supplied passphrase. Callers decide which validated trust state belongs
// in the plaintext; this package only owns the bounded, versioned file format.
package peerbackup

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

const (
	Version        = 1
	MaxPackageSize = 8 << 20

	argonMemoryKiB = 64 * 1024
	argonTime      = 3
	argonThreads   = 1
	minPassphrase  = 12
	maxPassphrase  = 1024
)

var (
	backupAAD        = []byte("atterm-peer-trust-backup-v1")
	ErrInvalidFile   = errors.New("peer backup: invalid file")
	ErrBadPassphrase = errors.New("peer backup: invalid passphrase")
)

type packageFile struct {
	Version    int       `json:"atterm_peer_trust_backup"`
	KDF        kdfParams `json:"kdf"`
	Cipher     string    `json:"cipher"`
	Nonce      []byte    `json:"nonce"`
	Ciphertext []byte    `json:"ciphertext"`
}

type kdfParams struct {
	Algorithm string `json:"alg"`
	MemoryKiB uint32 `json:"m"`
	Time      uint32 `json:"t"`
	Threads   uint8  `json:"p"`
	Salt      []byte `json:"salt"`
}

func defaultKDF(salt []byte) kdfParams {
	return kdfParams{
		Algorithm: "argon2id",
		MemoryKiB: argonMemoryKiB,
		Time:      argonTime,
		Threads:   argonThreads,
		Salt:      salt,
	}
}

// Seal encrypts plaintext into the complete JSON recovery-file bytes.
func Seal(passphrase string, plaintext []byte) ([]byte, error) {
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	if len(plaintext) == 0 || len(plaintext) > MaxPackageSize/2 {
		return nil, fmt.Errorf("%w: plaintext size", ErrInvalidFile)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("peer backup: generate salt: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("peer backup: generate nonce: %w", err)
	}
	params := defaultKDF(salt)
	key := deriveKey(passphrase, params)
	defer clear(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("peer backup: create cipher: %w", err)
	}
	file := packageFile{
		Version: Version, KDF: params, Cipher: "xchacha20-poly1305", Nonce: nonce,
		Ciphertext: aead.Seal(nil, nonce, plaintext, backupAAD),
	}
	encoded, err := json.Marshal(file)
	if err != nil {
		return nil, fmt.Errorf("peer backup: encode file: %w", err)
	}
	if len(encoded) > MaxPackageSize {
		return nil, fmt.Errorf("%w: package size", ErrInvalidFile)
	}
	return encoded, nil
}

// Open authenticates and decrypts one complete JSON recovery file.
func Open(passphrase string, encoded []byte) ([]byte, error) {
	if err := validatePassphrase(passphrase); err != nil {
		return nil, err
	}
	if len(encoded) == 0 || len(encoded) > MaxPackageSize {
		return nil, fmt.Errorf("%w: package size", ErrInvalidFile)
	}
	var file packageFile
	if err := strictJSON(encoded, &file); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidFile, err)
	}
	if file.Version != Version || file.Cipher != "xchacha20-poly1305" ||
		file.KDF.Algorithm != "argon2id" || file.KDF.MemoryKiB != argonMemoryKiB ||
		file.KDF.Time != argonTime || file.KDF.Threads != argonThreads ||
		len(file.KDF.Salt) != 16 || len(file.Nonce) != chacha20poly1305.NonceSizeX ||
		len(file.Ciphertext) < chacha20poly1305.Overhead {
		return nil, ErrInvalidFile
	}
	key := deriveKey(passphrase, file.KDF)
	defer clear(key)
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return nil, fmt.Errorf("peer backup: create cipher: %w", err)
	}
	plaintext, err := aead.Open(nil, file.Nonce, file.Ciphertext, backupAAD)
	if err != nil {
		return nil, ErrBadPassphrase
	}
	if len(plaintext) == 0 || len(plaintext) > MaxPackageSize/2 {
		clear(plaintext)
		return nil, ErrInvalidFile
	}
	return plaintext, nil
}

func deriveKey(passphrase string, params kdfParams) []byte {
	return argon2.IDKey([]byte(passphrase), params.Salt, params.Time, params.MemoryKiB, params.Threads, chacha20poly1305.KeySize)
}

func validatePassphrase(passphrase string) error {
	count := utf8.RuneCountInString(passphrase)
	if count < minPassphrase || count > maxPassphrase {
		return fmt.Errorf("%w: use %d to %d characters", ErrBadPassphrase, minPassphrase, maxPassphrase)
	}
	return nil
}

func strictJSON(data []byte, value any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == nil {
		return errors.New("trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

package e2eecrypto

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attson/atterm/internal/opaquesuite"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/chacha20poly1305"
)

// accountKeyAAD is the additional-data string bound into the AEAD seal
// so a wrap envelope from a future protocol (e.g. an account_key_v2)
// cannot be opened by v1 code, even if the ciphertext bytes happen to
// line up. Any bump here is a one-way migration.
const accountKeyAAD = "atterm-account-key-v1"

// KDFParams tunes Argon2id when deriving a wrap_key from the user's
// password. Tuned for laptop CPUs; mobile may want lower memory. The
// relay echoes these back in kdf_params at login so a future rotation
// of parameters survives a password change on a single device.
type KDFParams struct {
	Alg     string `json:"alg"` // always "argon2id" in v1
	MemKiB  uint32 `json:"m"`   // memory in KiB
	Time    uint32 `json:"t"`   // iterations
	Threads uint8  `json:"p"`   // parallelism
}

// DefaultKDFParams returns the v1 baseline parameters: 64 MiB memory,
// 3 iterations, 1 thread.
func DefaultKDFParams() KDFParams {
	return KDFParams{
		Alg:     "argon2id",
		MemKiB:  64 * 1024,
		Time:    3,
		Threads: 1,
	}
}

// Marshal renders kp as the JSON string the relay stores in
// user_account_key_wraps.kdf_params.
func (kp KDFParams) Marshal() string {
	b, _ := json.Marshal(kp)
	return string(b)
}

// WrapAccountKey derives wrap_key = Argon2id(password, salt, kp),
// generates a fresh 24-byte nonce, and seals accountKey into an
// AccountKeyWrap envelope using XChaCha20-Poly1305 with a versioned AAD.
//
// The account_key bytes never touch the relay in plaintext form; the
// relay only ever sees this envelope + the kdf_params echo needed for
// the next login on a fresh device.
func WrapAccountKey(password string, accountKey []byte, kp KDFParams) (opaquesuite.AccountKeyWrap, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return opaquesuite.AccountKeyWrap{}, fmt.Errorf("rand salt: %w", err)
	}
	wrapKey := argon2.IDKey([]byte(password), salt, kp.Time, kp.MemKiB, kp.Threads, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(wrapKey)
	if err != nil {
		return opaquesuite.AccountKeyWrap{}, fmt.Errorf("aead: %w", err)
	}
	nonce := make([]byte, chacha20poly1305.NonceSizeX)
	if _, err := rand.Read(nonce); err != nil {
		return opaquesuite.AccountKeyWrap{}, fmt.Errorf("rand nonce: %w", err)
	}
	ciphertext := aead.Seal(nil, nonce, accountKey, []byte(accountKeyAAD))
	return opaquesuite.AccountKeyWrap{
		Method:    "password",
		Wrapped:   ciphertext,
		Nonce:     nonce,
		Salt:      salt,
		KDFParams: kp.Marshal(),
	}, nil
}

// UnwrapAccountKey recovers the raw account_key from a wrap envelope
// using the user's password. Returns the sentinel error message
// "e2eecrypto: invalid password" on AEAD open failure — callers rely on
// this to distinguish a wrong password from a transport-level fault.
func UnwrapAccountKey(password string, w opaquesuite.AccountKeyWrap) ([]byte, error) {
	var kp KDFParams
	if err := json.Unmarshal([]byte(w.KDFParams), &kp); err != nil {
		return nil, fmt.Errorf("kdf_params: %w", err)
	}
	if kp.Alg != "argon2id" {
		return nil, fmt.Errorf("unsupported kdf alg: %q", kp.Alg)
	}
	wrapKey := argon2.IDKey([]byte(password), w.Salt, kp.Time, kp.MemKiB, kp.Threads, chacha20poly1305.KeySize)
	aead, err := chacha20poly1305.NewX(wrapKey)
	if err != nil {
		return nil, fmt.Errorf("aead: %w", err)
	}
	plaintext, err := aead.Open(nil, w.Nonce, w.Wrapped, []byte(accountKeyAAD))
	if err != nil {
		return nil, errors.New("e2eecrypto: invalid password")
	}
	return plaintext, nil
}

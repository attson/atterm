package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attson/atterm/internal/safekeyring"
)

// keychainSlot addresses one entry in the OS keychain.
//
//   - `service` is the versioned atterm namespace, e.g.
//     "com.atterm.account-key.v1" + appdir.KeychainSuffix(); shared by every
//     entry of that kind so a future version bump migrates all rows together.
//   - `account` identifies the specific entry within that namespace and is
//     usually a composite of stable per-row identifiers (realmID + userID,
//     relayOrigin + email, host ID, ...).
//
// The codec knows how to serialize T to the string the keychain stores; T
// stays typed in Go so callers don't juggle []byte / json.Marshal by hand.
//
// The zero value for `account` is treated as "don't persist" so callers that
// build the slot from partially-initialized inputs can short-circuit without
// sprinkling guard clauses everywhere.
type keychainSlot[T any] struct {
	service string
	account string
	codec   slotCodec[T]
}

// slotCodec is the T↔string conversion used by a keychainSlot. isZero decides
// when a Save call should turn into a Clear (so nil / empty inputs consistently
// remove the entry instead of writing a "zero" ciphertext).
type slotCodec[T any] struct {
	encode func(T) (string, error)
	decode func(string) (T, error)
	isZero func(T) bool
}

// Load returns the stored value, or the zero T + nil when the slot is missing
// or `account` is empty. Non-NotFound keychain errors surface verbatim so the
// caller can log them and fall back to whatever in-memory state it has.
func (s keychainSlot[T]) Load() (T, error) {
	var zero T
	if s.account == "" {
		return zero, nil
	}
	raw, err := safekeyring.Get(s.service, s.account)
	if err != nil {
		if errors.Is(err, safekeyring.ErrNotFound) {
			return zero, nil
		}
		return zero, fmt.Errorf("keychain get: %w", err)
	}
	v, err := s.codec.decode(raw)
	if err != nil {
		return zero, fmt.Errorf("keychain decode: %w", err)
	}
	return v, nil
}

// Save writes v. A zero-valued v (per codec.isZero) is treated as Clear so
// callers can pipe the same setter through without a separate branch.
func (s keychainSlot[T]) Save(v T) error {
	if s.account == "" {
		return nil
	}
	if s.codec.isZero(v) {
		return s.Clear()
	}
	raw, err := s.codec.encode(v)
	if err != nil {
		return fmt.Errorf("keychain encode: %w", err)
	}
	if err := safekeyring.Set(s.service, s.account, raw); err != nil {
		return fmt.Errorf("keychain set: %w", err)
	}
	return nil
}

// Clear removes the entry. A missing entry is not an error (idempotent).
func (s keychainSlot[T]) Clear() error {
	if s.account == "" {
		return nil
	}
	if err := safekeyring.Delete(s.service, s.account); err != nil {
		if errors.Is(err, safekeyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("keychain delete: %w", err)
	}
	return nil
}

// ---- built-in codecs ----

// bytesCodec stores []byte as base64.RawStdEncoding so the on-disk value is
// printable ASCII (some backends log secret values on certain code paths and
// control bytes are more likely to be misinterpreted).
var bytesCodec = slotCodec[[]byte]{
	encode: func(b []byte) (string, error) {
		return base64.RawStdEncoding.EncodeToString(b), nil
	},
	decode: func(s string) ([]byte, error) {
		return base64.RawStdEncoding.DecodeString(s)
	},
	isZero: func(b []byte) bool { return len(b) == 0 },
}

// stringCodec stores a string verbatim.
var stringCodec = slotCodec[string]{
	encode: func(s string) (string, error) { return s, nil },
	decode: func(s string) (string, error) { return s, nil },
	isZero: func(s string) bool { return s == "" },
}

// jsonCodec[T] stores T as its JSON encoding — used by slots that bundle
// several fields into a single keychain entry (SSH credentials, key secrets).
// isZero is caller-supplied because Go can't derive "empty" for arbitrary
// structs.
func jsonCodec[T any](isZero func(T) bool) slotCodec[T] {
	return slotCodec[T]{
		encode: func(v T) (string, error) {
			b, err := json.Marshal(v)
			if err != nil {
				return "", err
			}
			return string(b), nil
		},
		decode: func(s string) (T, error) {
			var v T
			if err := json.Unmarshal([]byte(s), &v); err != nil {
				return v, err
			}
			return v, nil
		},
		isZero: isZero,
	}
}

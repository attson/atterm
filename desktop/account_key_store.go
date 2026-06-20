package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/safekeyring"
)

// accountKeyService is the OS-keychain service name under which atterm
// stores the unlocked E2EE account key. macOS Keychain renders this as
// the "Where" column; Windows wincred uses it as the credential target
// prefix; Linux libsecret uses it as one of the schema attributes.
//
// The trailing version suffix lets us migrate to a new wrap format
// later without colliding with old entries — drop the v1 read once a
// future "v2" path is reading + writing cleanly.
func accountKeyService() string {
	return "com.atterm.account-key.v1" + appdir.KeychainSuffix()
}

// accountKeyAccount derives the keychain "account" name from the relay
// origin and user ID. Multiple relays on the same desktop must not
// share storage — e.g. testing against staging while production stays
// logged in — so the origin is part of the key. user_id is included so
// a future multi-account-per-relay flow doesn't require schema work.
//
// Returns empty string when either input is empty so callers can treat
// that as "don't persist" without sprinkling guard clauses everywhere.
func accountKeyAccount(relayOrigin, userID string) string {
	relayOrigin = strings.TrimRight(strings.TrimSpace(relayOrigin), "/")
	userID = strings.TrimSpace(userID)
	if relayOrigin == "" || userID == "" {
		return ""
	}
	return relayOrigin + "|" + userID
}

// errKeychainNotConfigured is the keyring-unconfigured error we surface
// to callers as "no key persisted" rather than as a real failure.
var errKeychainNotConfigured = safekeyring.ErrNotFound

// loadAccountKey reads the persisted E2EE account key for (relayOrigin,
// userID), or returns nil if nothing was stored. Any keychain-level
// error other than "not found" surfaces verbatim so the caller can log
// it and fall back to the in-memory state.
//
// The on-disk format is base64.RawStdEncoding so the OS keychain stores
// a printable ASCII string (some backends — notably macOS' security CLI
// — log secret values on certain code paths; printable bytes are less
// likely to be misinterpreted as control sequences).
func loadAccountKey(relayOrigin, userID string) ([]byte, error) {
	account := accountKeyAccount(relayOrigin, userID)
	if account == "" {
		return nil, nil
	}
	encoded, err := safekeyring.Get(accountKeyService(), account)
	if err != nil {
		if errors.Is(err, errKeychainNotConfigured) {
			return nil, nil
		}
		return nil, fmt.Errorf("keychain get: %w", err)
	}
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("keychain decode: %w", err)
	}
	return raw, nil
}

// saveAccountKey persists key for (relayOrigin, userID). A nil or empty
// key is treated as "delete" — same code path as clearAccountKey — so
// callers can pipe the same setter through without a separate branch.
func saveAccountKey(relayOrigin, userID string, key []byte) error {
	account := accountKeyAccount(relayOrigin, userID)
	if account == "" {
		return nil
	}
	if len(key) == 0 {
		return clearAccountKeyFor(relayOrigin, userID)
	}
	encoded := base64.RawStdEncoding.EncodeToString(key)
	if err := safekeyring.Set(accountKeyService(), account, encoded); err != nil {
		return fmt.Errorf("keychain set: %w", err)
	}
	return nil
}

// clearAccountKeyFor removes the persisted key for (relayOrigin,
// userID). Returns nil when the entry was already absent.
func clearAccountKeyFor(relayOrigin, userID string) error {
	account := accountKeyAccount(relayOrigin, userID)
	if account == "" {
		return nil
	}
	if err := safekeyring.Delete(accountKeyService(), account); err != nil {
		if errors.Is(err, safekeyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("keychain delete: %w", err)
	}
	return nil
}

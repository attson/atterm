package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/safekeyring"
)

// relayPasswordService is the OS-keychain service name under which atterm
// caches the user's OPAQUE relay password so SettingsRelay can prefill the
// password field on subsequent launches.
//
// The trailing .v1 lets us migrate to a different storage format later
// without colliding with old entries.
func relayPasswordService() string {
	return "com.atterm.relay-password.v1" + appdir.KeychainSuffix()
}

// relayPasswordAccount derives the keychain "account" name from the relay
// origin and the user's email. Two relays on the same desktop must not share
// storage (staging vs production), and the same desktop may end up with
// different accounts per relay — so both inputs are part of the key.
//
// Returns "" when either input is empty so callers can treat that as
// "don't persist" without sprinkling guard clauses everywhere.
func relayPasswordAccount(relayOrigin, email string) string {
	relayOrigin = strings.TrimRight(strings.TrimSpace(relayOrigin), "/")
	email = strings.TrimSpace(email)
	if relayOrigin == "" || email == "" {
		return ""
	}
	return relayOrigin + "|" + email
}

// loadRelayPassword reads the persisted relay password for (relayOrigin,
// email), or returns "" if nothing is stored. Any keychain-level error
// other than "not found" surfaces verbatim so the caller can log it.
func loadRelayPassword(relayOrigin, email string) (string, error) {
	account := relayPasswordAccount(relayOrigin, email)
	if account == "" {
		return "", nil
	}
	v, err := safekeyring.Get(relayPasswordService(), account)
	if err != nil {
		if errors.Is(err, safekeyring.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("keychain get: %w", err)
	}
	return v, nil
}

// saveRelayPassword persists password for (relayOrigin, email). An empty
// password is treated as "delete" — same code path as clearRelayPasswordFor
// — so callers can pipe the same setter through without a separate branch.
func saveRelayPassword(relayOrigin, email, password string) error {
	account := relayPasswordAccount(relayOrigin, email)
	if account == "" {
		return nil
	}
	if password == "" {
		return clearRelayPasswordFor(relayOrigin, email)
	}
	if err := safekeyring.Set(relayPasswordService(), account, password); err != nil {
		return fmt.Errorf("keychain set: %w", err)
	}
	return nil
}

// clearRelayPasswordFor removes the persisted password for (relayOrigin,
// email). Returns nil when the entry was already absent.
func clearRelayPasswordFor(relayOrigin, email string) error {
	account := relayPasswordAccount(relayOrigin, email)
	if account == "" {
		return nil
	}
	if err := safekeyring.Delete(relayPasswordService(), account); err != nil {
		if errors.Is(err, safekeyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("keychain delete: %w", err)
	}
	return nil
}

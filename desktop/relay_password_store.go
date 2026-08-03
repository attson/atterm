package main

import (
	"strings"

	"github.com/attson/atterm/internal/appdir"
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
// Returns "" when either input is empty so the resulting slot short-circuits
// to no-ops.
func relayPasswordAccount(relayOrigin, email string) string {
	relayOrigin = strings.TrimRight(strings.TrimSpace(relayOrigin), "/")
	email = strings.TrimSpace(email)
	if relayOrigin == "" || email == "" {
		return ""
	}
	return relayOrigin + "|" + email
}

func relayPasswordSlot(relayOrigin, email string) keychainSlot[string] {
	return keychainSlot[string]{
		service: relayPasswordService(),
		account: relayPasswordAccount(relayOrigin, email),
		codec:   stringCodec,
	}
}

// loadRelayPassword reads the persisted relay password for (relayOrigin,
// email), or returns "" if nothing is stored.
func loadRelayPassword(relayOrigin, email string) (string, error) {
	return relayPasswordSlot(relayOrigin, email).Load()
}

// saveRelayPassword persists password for (relayOrigin, email). An empty
// password is treated as delete.
func saveRelayPassword(relayOrigin, email, password string) error {
	return relayPasswordSlot(relayOrigin, email).Save(password)
}

// clearRelayPasswordFor removes the persisted password for (relayOrigin,
// email).
func clearRelayPasswordFor(relayOrigin, email string) error {
	return relayPasswordSlot(relayOrigin, email).Clear()
}

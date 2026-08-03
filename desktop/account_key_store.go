package main

import (
	"strings"

	"github.com/attson/atterm/internal/appdir"
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

// accountKeyAccount derives the keychain "account" name from the cluster
// realm id and user id. Anchoring on realm (not the physical relay origin)
// lets the account_key survive node/domain switches. Multiple realms on the
// same desktop stay isolated via the realm prefix.
//
// Returns empty string when either input is empty so the resulting slot
// short-circuits Load/Save/Clear to no-ops.
func accountKeyAccount(realmID, userID string) string {
	realmID = strings.TrimSpace(realmID)
	userID = strings.TrimSpace(userID)
	if realmID == "" || userID == "" {
		return ""
	}
	return realmID + "|" + userID
}

func accountKeySlot(realmID, userID string) keychainSlot[[]byte] {
	return keychainSlot[[]byte]{
		service: accountKeyService(),
		account: accountKeyAccount(realmID, userID),
		codec:   bytesCodec,
	}
}

// loadAccountKey reads the persisted E2EE account key for (realmID,
// userID), or returns nil if nothing was stored.
func loadAccountKey(realmID, userID string) ([]byte, error) {
	return accountKeySlot(realmID, userID).Load()
}

// saveAccountKey persists key for (realmID, userID). A nil or empty
// key is treated as delete.
func saveAccountKey(realmID, userID string, key []byte) error {
	return accountKeySlot(realmID, userID).Save(key)
}

// clearAccountKeyFor removes the persisted key for (realmID, userID).
func clearAccountKeyFor(realmID, userID string) error {
	return accountKeySlot(realmID, userID).Clear()
}

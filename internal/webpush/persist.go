package webpush

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const stateFilename = "web-push.json"

// persistedState is the on-disk JSON shape.
type persistedState struct {
	PrivateKey    string                    `json:"private_key"`
	PublicKey     string                    `json:"public_key"`
	Subscriptions map[string][]Subscription `json:"subscriptions"`
}

// loadOrInitState reads <dir>/web-push.json. If missing, generates a fresh
// VAPID keypair and persists. If corrupt, renames the bad file with a
// .corrupt-<unix> suffix, then regenerates.
func loadOrInitState(dir string) (persistedState, error) {
	if dir == "" {
		return persistedState{}, errors.New("config dir is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return persistedState{}, fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, stateFilename)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return regenerateAndPersist(dir)
	}
	if err != nil {
		return persistedState{}, fmt.Errorf("read %s: %w", path, err)
	}
	var state persistedState
	if err := json.Unmarshal(data, &state); err != nil {
		backup := fmt.Sprintf("%s.corrupt-%d", path, time.Now().Unix())
		if renameErr := os.Rename(path, backup); renameErr != nil {
			log.Printf("webpush: rename corrupt state %s -> %s failed: %v", path, backup, renameErr)
		} else {
			log.Printf("webpush: state file corrupt; backed up to %s and regenerating", backup)
		}
		return regenerateAndPersist(dir)
	}
	// Legacy schema detection: tokenHash keys are 64-char lowercase hex;
	// userID keys are 26-char ULIDs. If we detect any legacy key, rename the
	// file and start fresh so the user re-subscribes with their account.
	if isLegacySubscriptionSchema(state.Subscriptions) {
		legacyPath := fmt.Sprintf("%s.legacy-%d", path, time.Now().Unix())
		if renameErr := os.Rename(path, legacyPath); renameErr != nil {
			return persistedState{}, fmt.Errorf("rename legacy schema: %w", renameErr)
		}
		log.Printf("webpush: legacy tokenHash schema detected; renamed to %s — users must re-enable notifications", legacyPath)
		return regenerateAndPersist(dir)
	}
	if state.PrivateKey == "" || state.PublicKey == "" {
		// Partial state (no keys); regenerate but keep loaded subs.
		priv, pub, err := generateVAPIDKeypair()
		if err != nil {
			return persistedState{}, fmt.Errorf("generate vapid: %w", err)
		}
		state.PrivateKey = priv
		state.PublicKey = pub
		if err := saveState(dir, state); err != nil {
			log.Printf("webpush: persist regenerated state failed: %v", err)
		}
	}
	if state.Subscriptions == nil {
		state.Subscriptions = make(map[string][]Subscription)
	}
	return state, nil
}

func regenerateAndPersist(dir string) (persistedState, error) {
	priv, pub, err := generateVAPIDKeypair()
	if err != nil {
		return persistedState{}, fmt.Errorf("generate vapid: %w", err)
	}
	state := persistedState{
		PrivateKey:    priv,
		PublicKey:     pub,
		Subscriptions: make(map[string][]Subscription),
	}
	if err := saveState(dir, state); err != nil {
		log.Printf("webpush: persist fresh state failed: %v", err)
	}
	return state, nil
}

// isLegacySubscriptionSchema returns true when the subscriptions map contains
// at least one key that looks like a 64-char lowercase hex tokenHash (the old
// schema) rather than a ULID-shaped user_id (26 chars, Crockford base32).
func isLegacySubscriptionSchema(subs map[string][]Subscription) bool {
	for key := range subs {
		if len(key) == 64 && isAllHex(key) {
			return true
		}
	}
	return false
}

// isAllHex returns true when every byte of s is a lowercase or uppercase hex digit.
func isAllHex(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// CleanupLegacy removes web-push.json.legacy-* files in dir whose modification
// time is older than 30 days. Intended to be called once per day.
func CleanupLegacy(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "web-push.json.legacy-") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			full := filepath.Join(dir, e.Name())
			if rmErr := os.Remove(full); rmErr != nil {
				log.Printf("webpush: CleanupLegacy: remove %s: %v", full, rmErr)
			}
		}
	}
	return nil
}

// saveState writes state to <dir>/web-push.json atomically (write-temp-rename).
// Failure logs a WARN; the in-memory state is the source of truth at runtime.
func saveState(dir string, state persistedState) error {
	if dir == "" {
		return errors.New("config dir is empty")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, stateFilename)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, path, err)
	}
	return nil
}

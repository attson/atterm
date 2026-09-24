// Package peerstore persists Peer Space membership and invitation state. The
// complete file is encrypted because unredeemed tickets contain pairing
// secrets; the encryption key is supplied by platform secure storage.
package peerstore

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	Version      = 1
	maxStoreSize = 4 << 20
	lockTimeout  = 5 * time.Second
	staleLockAge = 30 * time.Second
)

var (
	ErrNotInitialized = errors.New("peerstore: not initialized")
	ErrAlreadyExists  = errors.New("peerstore: space already exists")
	ErrInviteInvalid  = errors.New("peerstore: invitation invalid")
	ErrInviteConsumed = errors.New("peerstore: invitation already consumed")
	ErrInviteRevoked  = errors.New("peerstore: invitation revoked")
)

var storeAAD = []byte("atterm-peer-store-v1")

// KeySource returns the 32-byte store key from platform secure storage.
type KeySource func() ([]byte, error)

type Invitation struct {
	InviteID         string `json:"invite_id"`
	BatchID          string `json:"batch_id"`
	Token            string `json:"token"`
	ExpiresAt        int64  `json:"expires_at"`
	ConsumedAt       int64  `json:"consumed_at,omitempty"`
	ConsumedByPeerID string `json:"consumed_by_peer_id,omitempty"`
	RevokedAt        int64  `json:"revoked_at,omitempty"`
}

// State contains the local replica needed before config replication exists.
// Revocation maps use timestamps so later signed governance operations can be
// imported without changing the disk schema.
type State struct {
	Version             int              `json:"version"`
	GenesisToken        string           `json:"genesis_token"`
	LocalMembership     string           `json:"local_membership"`
	Invitations         []Invitation     `json:"invitations"`
	RevokedMembers      map[string]int64 `json:"revoked_members,omitempty"`
	RevokedGrantSerials map[string]int64 `json:"revoked_grant_serials,omitempty"`
	CreatedAt           int64            `json:"created_at"`
	UpdatedAt           int64            `json:"updated_at"`
}

type envelope struct {
	Version    int    `json:"version"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// Store serializes read-modify-write operations so invitation consumption is
// atomic within the redemption device process.
type Store struct {
	mu   sync.Mutex
	path string
	key  KeySource
}

func New(path string, key KeySource) *Store {
	return &Store{path: path, key: key}
}

// Initialize creates a new store and refuses to replace an existing Space.
func (s *Store) Initialize(state State) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withFileLock(func() error {
		if _, err := os.Stat(s.path); err == nil {
			return ErrAlreadyExists
		} else if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("stat peer store: %w", err)
		}
		if state.GenesisToken == "" || state.LocalMembership == "" {
			return fmt.Errorf("peerstore: missing space documents")
		}
		state.Version = Version
		if state.CreatedAt == 0 {
			state.CreatedAt = time.Now().Unix()
		}
		state.UpdatedAt = state.CreatedAt
		state.Invitations = append([]Invitation(nil), state.Invitations...)
		return s.writeLocked(state)
	})
}

// Load returns a detached snapshot.
func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// AddInvitations persists a newly signed batch, rejecting duplicate ids.
func (s *Store) AddInvitations(invitations []Invitation, now time.Time) error {
	return s.mutate(func(state *State) error {
		seen := make(map[string]struct{}, len(state.Invitations)+len(invitations))
		for _, invite := range state.Invitations {
			seen[invite.InviteID] = struct{}{}
		}
		for _, invite := range invitations {
			if invite.InviteID == "" || invite.BatchID == "" || invite.Token == "" || invite.ExpiresAt <= now.Unix() {
				return ErrInviteInvalid
			}
			if _, exists := seen[invite.InviteID]; exists {
				return ErrInviteInvalid
			}
			seen[invite.InviteID] = struct{}{}
			state.Invitations = append(state.Invitations, invite)
		}
		state.UpdatedAt = now.Unix()
		return nil
	})
}

// ConsumeInvitation records the only successful redemption for inviteID.
func (s *Store) ConsumeInvitation(inviteID, consumerPeerID string, now time.Time) error {
	if inviteID == "" || consumerPeerID == "" {
		return ErrInviteInvalid
	}
	return s.mutate(func(state *State) error {
		for index := range state.Invitations {
			invite := &state.Invitations[index]
			if invite.InviteID != inviteID {
				continue
			}
			if invite.RevokedAt != 0 {
				return ErrInviteRevoked
			}
			if invite.ConsumedAt != 0 {
				return ErrInviteConsumed
			}
			if now.Unix() >= invite.ExpiresAt {
				return ErrInviteInvalid
			}
			invite.ConsumedAt = now.Unix()
			invite.ConsumedByPeerID = consumerPeerID
			state.UpdatedAt = now.Unix()
			return nil
		}
		return ErrInviteInvalid
	})
}

// RevokeInvitation and RevokeBatch are idempotent deny-wins updates.
func (s *Store) RevokeInvitation(inviteID string, now time.Time) error {
	return s.revoke(func(invite Invitation) bool { return invite.InviteID == inviteID }, now)
}

func (s *Store) RevokeBatch(batchID string, now time.Time) error {
	return s.revoke(func(invite Invitation) bool { return invite.BatchID == batchID }, now)
}

func (s *Store) revoke(match func(Invitation) bool, now time.Time) error {
	return s.mutate(func(state *State) error {
		found := false
		for index := range state.Invitations {
			if match(state.Invitations[index]) {
				found = true
				if state.Invitations[index].RevokedAt == 0 {
					state.Invitations[index].RevokedAt = now.Unix()
				}
			}
		}
		if !found {
			return ErrInviteInvalid
		}
		state.UpdatedAt = now.Unix()
		return nil
	})
}

func (s *Store) mutate(apply func(*State) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.withFileLock(func() error {
		state, err := s.loadLocked()
		if err != nil {
			return err
		}
		if err := apply(&state); err != nil {
			return err
		}
		return s.writeLocked(state)
	})
}

func (s *Store) loadLocked() (State, error) {
	blob, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, ErrNotInitialized
		}
		return State{}, fmt.Errorf("read peer store: %w", err)
	}
	if len(blob) > maxStoreSize {
		return State{}, fmt.Errorf("peerstore: file exceeds size limit")
	}
	var wrapped envelope
	if err := strictJSON(blob, &wrapped); err != nil || wrapped.Version != Version {
		return State{}, fmt.Errorf("peerstore: invalid envelope")
	}
	nonce, err := base64.RawURLEncoding.Strict().DecodeString(wrapped.Nonce)
	if err != nil {
		return State{}, fmt.Errorf("peerstore: invalid nonce")
	}
	ciphertext, err := base64.RawURLEncoding.Strict().DecodeString(wrapped.Ciphertext)
	if err != nil {
		return State{}, fmt.Errorf("peerstore: invalid ciphertext")
	}
	aead, err := s.aead()
	if err != nil {
		return State{}, err
	}
	if len(nonce) != aead.NonceSize() {
		return State{}, fmt.Errorf("peerstore: invalid nonce size")
	}
	plaintext, err := aead.Open(nil, nonce, ciphertext, storeAAD)
	if err != nil {
		return State{}, fmt.Errorf("peerstore: decrypt: %w", err)
	}
	var state State
	if err := strictJSON(plaintext, &state); err != nil || state.Version != Version || state.GenesisToken == "" || state.LocalMembership == "" {
		return State{}, fmt.Errorf("peerstore: invalid state")
	}
	state.Invitations = append([]Invitation(nil), state.Invitations...)
	state.RevokedMembers = cloneMap(state.RevokedMembers)
	state.RevokedGrantSerials = cloneMap(state.RevokedGrantSerials)
	return state, nil
}

func (s *Store) writeLocked(state State) error {
	plaintext, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal peer store: %w", err)
	}
	if len(plaintext) > maxStoreSize {
		return fmt.Errorf("peerstore: state exceeds size limit")
	}
	aead, err := s.aead()
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("peerstore: nonce: %w", err)
	}
	wrapped, err := json.Marshal(envelope{
		Version:    Version,
		Nonce:      base64.RawURLEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawURLEncoding.EncodeToString(aead.Seal(nil, nonce, plaintext, storeAAD)),
	})
	if err != nil {
		return fmt.Errorf("marshal peer envelope: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create peer store directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".peer-space-*.tmp")
	if err != nil {
		return fmt.Errorf("create peer store temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod peer store temp: %w", err)
	}
	if _, err := tmp.Write(wrapped); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write peer store temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync peer store temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close peer store temp: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace peer store: %w", err)
	}
	return nil
}

func (s *Store) aead() (cipher.AEAD, error) {
	if s.key == nil {
		return nil, errors.New("peerstore: missing key source")
	}
	key, err := s.key()
	if err != nil {
		return nil, fmt.Errorf("peerstore: load key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("peerstore: key length %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("peerstore: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func strictJSON(blob []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(blob))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return errors.New("trailing JSON")
	} else if !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// withFileLock uses atomic directory creation rather than an OS-specific
// advisory lock, so two desktop instances sharing one identity cannot both
// consume the same ticket. A crashed writer is recoverable after a bounded
// stale interval; ordinary operations hold the directory for milliseconds.
func (s *Store) withFileLock(run func() error) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create peer store directory: %w", err)
	}
	lockPath := s.path + ".lock"
	deadline := time.Now().Add(lockTimeout)
	for {
		err := os.Mkdir(lockPath, 0o700)
		if err == nil {
			defer os.Remove(lockPath)
			return run()
		}
		if !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("acquire peer store lock: %w", err)
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > staleLockAge {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return errors.New("peerstore: timed out waiting for store lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func cloneMap(in map[string]int64) map[string]int64 {
	if in == nil {
		return nil
	}
	out := make(map[string]int64, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

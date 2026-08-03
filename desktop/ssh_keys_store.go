package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/safekeyring"
	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/ssh"
)

// SSHKey is one key in the vault. Non-secret fields (id/name/key_type) live in
// config.json; the private key + passphrase live in the keyring keyed by ID.
type SSHKey struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	KeyType string `json:"key_type,omitempty"` // parsed from PEM (read-only)
}

// sshKeySecret is JSON-encoded into a single keyring entry keyed by key ID.
type sshKeySecret struct {
	PrivateKey string `json:"private_key"`
	Passphrase string `json:"passphrase,omitempty"`
}

func sshKeyService() string {
	return "com.atterm.ssh-key.v1" + appdir.KeychainSuffix()
}

// parseKeyType parses the PEM (optionally with passphrase) and returns a
// normalized algorithm name. An unparsable key is an error.
func parseKeyType(pemStr, passphrase string) (string, error) {
	var signer ssh.Signer
	var err error
	if passphrase != "" {
		signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(pemStr), []byte(passphrase))
	} else {
		signer, err = ssh.ParsePrivateKey([]byte(pemStr))
	}
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	t := signer.PublicKey().Type()
	switch {
	case t == "ssh-rsa":
		return "RSA", nil
	case t == "ssh-ed25519":
		return "ED25519", nil
	case strings.HasPrefix(t, "ecdsa-"):
		return "ECDSA", nil
	default:
		return t, nil
	}
}

// ListSSHKeys returns the saved keys (non-secret fields only).
func (a *App) ListSSHKeys() []SSHKey {
	if a.cfgStore == nil {
		return []SSHKey{}
	}
	ks := a.cfgStore.Get().SSHKeys
	if ks == nil {
		return []SSHKey{}
	}
	return ks
}

// AddSSHKey validates the PEM, stores non-secret fields in config and the
// private key in the keyring. On config failure it rolls back the keyring.
func (a *App) AddSSHKey(name, privateKeyPEM, passphrase string) (SSHKey, error) {
	if a.cfgStore == nil {
		return SSHKey{}, fmt.Errorf("config store not ready")
	}
	kt, err := parseKeyType(privateKeyPEM, passphrase)
	if err != nil {
		return SSHKey{}, err
	}
	k := SSHKey{ID: ulid.Make().String(), Name: name, KeyType: kt}
	if err := storeSSHKeySecret(k.ID, sshKeySecret{PrivateKey: privateKeyPEM, Passphrase: passphrase}); err != nil {
		return SSHKey{}, fmt.Errorf("store key: %w", err)
	}
	cfg := a.cfgStore.Get()
	cfg.SSHKeys = append(cfg.SSHKeys, k)
	if err := a.cfgStore.Set(cfg); err != nil {
		_ = safekeyring.Delete(sshKeyService(), k.ID)
		return SSHKey{}, err
	}
	a.markSSHHostsDirty()
	return k, nil
}

// UpdateSSHKey renames the key and, when a new PEM is supplied, replaces the
// private key. A blank PEM leaves the stored private key untouched.
func (a *App) UpdateSSHKey(id, name, privateKeyPEM, passphrase string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	idx := -1
	for i := range cfg.SSHKeys {
		if cfg.SSHKeys[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no such key: %s", id)
	}
	cfg.SSHKeys[idx].Name = name
	if privateKeyPEM != "" {
		kt, err := parseKeyType(privateKeyPEM, passphrase)
		if err != nil {
			return err
		}
		cfg.SSHKeys[idx].KeyType = kt
		if err := storeSSHKeySecret(id, sshKeySecret{PrivateKey: privateKeyPEM, Passphrase: passphrase}); err != nil {
			return fmt.Errorf("store key: %w", err)
		}
	}
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markSSHHostsDirty()
	return nil
}

// DeleteSSHKey removes the key and its secret. It refuses when any host
// references the key, returning the referencing host names.
func (a *App) DeleteSSHKey(id string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	var users []string
	for _, h := range cfg.SSHHosts {
		if h.AuthKind == "key" && h.KeyID == id {
			name := h.Alias
			if name == "" {
				name = h.User + "@" + h.Host
			}
			users = append(users, name)
		}
	}
	if len(users) > 0 {
		return fmt.Errorf("key in use by: %s", strings.Join(users, ", "))
	}
	out := cfg.SSHKeys[:0:0]
	found := false
	for _, k := range cfg.SSHKeys {
		if k.ID == id {
			found = true
			continue
		}
		out = append(out, k)
	}
	if !found {
		return fmt.Errorf("no such key: %s", id)
	}
	cfg.SSHKeys = out
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if err := safekeyring.Delete(sshKeyService(), id); err != nil && err != safekeyring.ErrNotFound {
		return fmt.Errorf("delete key secret: %w", err)
	}
	a.markSSHHostsDirty()
	return nil
}

func storeSSHKeySecret(id string, sec sshKeySecret) error {
	blob, err := json.Marshal(sec)
	if err != nil {
		return err
	}
	return safekeyring.Set(sshKeyService(), id, string(blob))
}

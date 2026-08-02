package main

import (
	"encoding/json"
	"fmt"

	"github.com/attson/atterm/internal/appdir"
	"github.com/attson/atterm/internal/safekeyring"
	"github.com/oklog/ulid/v2"
)

// SSHHost is the non-secret part of a saved host, persisted in config.json.
// Credentials (password / private key / passphrase) live in the keyring keyed
// by ID, never here.
type SSHHost struct {
	ID       string `json:"id"`
	Alias    string `json:"alias,omitempty"`
	Host     string `json:"host"`
	Port     string `json:"port,omitempty"`
	User     string `json:"user"`
	AuthKind string `json:"auth_kind"` // "password" | "privateKey"
	Group    string `json:"group,omitempty"`
	Note     string `json:"note,omitempty"`
}

// sshCredential is JSON-encoded into a single keyring entry keyed by host ID.
// Only the fields relevant to the host's AuthKind are populated.
type sshCredential struct {
	Password   string `json:"password,omitempty"`
	PrivateKey string `json:"private_key,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

// sshCredentialService is the OS-keychain service name under which SSH host
// credentials are stored, one entry per host ID. Versioned so a future wrap
// format can migrate without colliding with v1 entries.
func sshCredentialService() string {
	return "com.atterm.ssh-credential.v1" + appdir.KeychainSuffix()
}

// ListSSHHosts returns the saved hosts (non-secret fields only).
func (a *App) ListSSHHosts() []SSHHost {
	if a.cfgStore == nil {
		return []SSHHost{}
	}
	hosts := a.cfgStore.Get().SSHHosts
	if hosts == nil {
		return []SSHHost{}
	}
	return hosts
}

// AddSSHHost generates an ID, stores non-secret fields in config and the
// credential in the keyring. On keyring failure it does not touch config; on
// config failure it rolls back the keyring entry.
func (a *App) AddSSHHost(h SSHHost, cred sshCredential) (SSHHost, error) {
	if a.cfgStore == nil {
		return SSHHost{}, fmt.Errorf("config store not ready")
	}
	h.ID = ulid.Make().String()

	if err := storeSSHCredential(h.ID, cred); err != nil {
		return SSHHost{}, fmt.Errorf("store credential: %w", err)
	}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = append(cfg.SSHHosts, h)
	if err := a.cfgStore.Set(cfg); err != nil {
		_ = safekeyring.Delete(sshCredentialService(), h.ID) // roll back
		return SSHHost{}, err
	}
	return h, nil
}

// UpdateSSHHost replaces the non-secret fields of the host with matching ID.
// If cred is non-nil the credential is replaced too; nil leaves it untouched.
func (a *App) UpdateSSHHost(h SSHHost, cred *sshCredential) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	idx := -1
	for i := range cfg.SSHHosts {
		if cfg.SSHHosts[i].ID == h.ID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("no such host: %s", h.ID)
	}
	if cred != nil {
		if err := storeSSHCredential(h.ID, *cred); err != nil {
			return fmt.Errorf("store credential: %w", err)
		}
	}
	cfg.SSHHosts[idx] = h
	return a.cfgStore.Set(cfg)
}

// DeleteSSHHost removes the host and its credential. A missing credential is
// treated as already deleted (idempotent).
func (a *App) DeleteSSHHost(id string) error {
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	out := cfg.SSHHosts[:0:0]
	found := false
	for _, hh := range cfg.SSHHosts {
		if hh.ID == id {
			found = true
			continue
		}
		out = append(out, hh)
	}
	if !found {
		return fmt.Errorf("no such host: %s", id)
	}
	cfg.SSHHosts = out
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	if err := safekeyring.Delete(sshCredentialService(), id); err != nil && err != safekeyring.ErrNotFound {
		return fmt.Errorf("delete credential: %w", err)
	}
	return nil
}

func storeSSHCredential(id string, cred sshCredential) error {
	blob, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	return safekeyring.Set(sshCredentialService(), id, string(blob))
}

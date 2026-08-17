package main

import (
	"fmt"
	"time"

	"github.com/attson/atterm/internal/appdir"
	"github.com/oklog/ulid/v2"
)

// markSSHHostsDirty flags the synced host-list value dirty in config (so a
// later Push uploads it) and, when a sync engine is wired, kicks a push. It
// writes meta directly so it works even before the engine exists (tests).
func (a *App) markSSHHostsDirty() {
	if a.cfgStore == nil {
		return
	}
	cfg := a.cfgStore.Get()
	if cfg.PrefsMeta == nil {
		cfg.PrefsMeta = map[string]prefsMetaEntry{}
	}
	m := cfg.PrefsMeta["ssh_hosts_encrypted"]
	m.Dirty = true
	m.UpdatedAtLocal = time.Now().UnixMilli()
	cfg.PrefsMeta["ssh_hosts_encrypted"] = m
	_ = a.cfgStore.Set(cfg)

	if a.prefsSync != nil {
		a.markPrefDirtyAndPush("ssh_hosts_encrypted")
	}
}

// SSHHost is the non-secret part of a saved host, persisted in config.json.
// Credentials (password / private key / passphrase) live in the keyring keyed
// by ID, never here.
type SSHHost struct {
	ID       string   `json:"id"`
	Alias    string   `json:"alias,omitempty"`
	Host     string   `json:"host"`
	Port     string   `json:"port,omitempty"`
	User     string   `json:"user"`
	AuthKind string   `json:"auth_kind"`        // "password" | "key"
	KeyID    string   `json:"key_id,omitempty"` // referenced SSHKey when auth_kind=="key"
	Tags     []string `json:"tags,omitempty"`
	Note     string   `json:"note,omitempty"`

	// IdentityFile, ProxyJump and ProxyCommand are populated by ssh_config
	// import (see ssh_config_import.go) and are otherwise empty for
	// manually-added hosts.
	//
	// IdentityFile is the private key *path* only — atterm never reads the
	// file as part of import; AuthKind is set to "key" but KeyID is left
	// empty until the user explicitly imports the key via the existing
	// key-import flow.
	//
	// ProxyJump / ProxyCommand mark a host that NewSshSessionByID must
	// refuse to dial directly (roadmap item 27 adds jump-host support).
	IdentityFile string `json:"identity_file,omitempty"`
	ProxyJump    string `json:"proxy_jump,omitempty"`
	ProxyCommand string `json:"proxy_command,omitempty"`
}

// sshCredential is JSON-encoded into a single keyring entry keyed by host ID.
// Private keys are no longer inlined here — key auth references an SSHKey by ID.
type sshCredential struct {
	Password string `json:"password,omitempty"`
}

// sshCredentialService is the OS-keychain service name under which SSH host
// credentials are stored, one entry per host ID. Versioned so a future wrap
// format can migrate without colliding with v1 entries.
func sshCredentialService() string {
	return "com.atterm.ssh-credential.v1" + appdir.KeychainSuffix()
}

// sshCredentialSlot addresses one host's credential in the keychain.
func sshCredentialSlot(id string) keychainSlot[sshCredential] {
	return keychainSlot[sshCredential]{
		service: sshCredentialService(),
		account: id,
		codec:   jsonCodec[sshCredential](func(c sshCredential) bool { return c == sshCredential{} }),
	}
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

	if err := sshCredentialSlot(h.ID).Save(cred); err != nil {
		return SSHHost{}, fmt.Errorf("store credential: %w", err)
	}

	cfg := a.cfgStore.Get()
	cfg.SSHHosts = append(cfg.SSHHosts, h)
	if err := a.cfgStore.Set(cfg); err != nil {
		_ = sshCredentialSlot(h.ID).Clear() // roll back
		return SSHHost{}, err
	}
	a.markSSHHostsDirty()
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
		if err := sshCredentialSlot(h.ID).Save(*cred); err != nil {
			return fmt.Errorf("store credential: %w", err)
		}
	}
	cfg.SSHHosts[idx] = h
	if err := a.cfgStore.Set(cfg); err != nil {
		return err
	}
	a.markSSHHostsDirty()
	return nil
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
	if err := sshCredentialSlot(id).Clear(); err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	a.markSSHHostsDirty()
	return nil
}

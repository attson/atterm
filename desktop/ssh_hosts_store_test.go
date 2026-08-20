package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

func newHostsTestApp(t *testing.T) *App {
	t.Helper()
	useIsolatedKeyring(t)
	return &App{cfgStore: newTestConfigStore(t)}
}

func TestAddSSHHostPersistsAndReadsBack(t *testing.T) {
	a := newHostsTestApp(t)
	h, err := a.AddSSHHost(SSHHost{
		Alias: "box", Host: "h", Port: "22", User: "u", AuthKind: "password",
	}, sshCredential{Password: "pw"})
	if err != nil {
		t.Fatalf("AddSSHHost: %v", err)
	}
	if h.ID == "" {
		t.Fatal("expected generated ID")
	}

	list := a.ListSSHHosts()
	if len(list) != 1 || list[0].ID != h.ID || list[0].Host != "h" {
		t.Fatalf("unexpected list: %+v", list)
	}
	if list[0].AuthKind != "password" {
		t.Fatalf("auth kind lost: %+v", list[0])
	}

	raw, err := safekeyring.Get(sshCredentialService(), h.ID)
	if err != nil {
		t.Fatalf("keyring get: %v", err)
	}
	var cred sshCredential
	if err := json.Unmarshal([]byte(raw), &cred); err != nil {
		t.Fatal(err)
	}
	if cred.Password != "pw" {
		t.Fatalf("password mismatch: %q", cred.Password)
	}
}

func TestUpdateSSHHostKeepsCredentialWhenNil(t *testing.T) {
	a := newHostsTestApp(t)
	h, _ := a.AddSSHHost(SSHHost{Host: "h", User: "u", AuthKind: "password"}, sshCredential{Password: "pw"})

	h.User = "u2"
	if err := a.UpdateSSHHost(h, nil); err != nil {
		t.Fatalf("UpdateSSHHost: %v", err)
	}
	if a.ListSSHHosts()[0].User != "u2" {
		t.Fatal("user not updated")
	}
	raw, err := safekeyring.Get(sshCredentialService(), h.ID)
	if err != nil || raw == "" {
		t.Fatalf("credential should remain: %v", err)
	}
}

// TestUpdateSSHHostPreservesConfigDerivedFields pins the second layer of the
// gate fix. The traced scenario: import a ProxyJump host from ~/.ssh/config
// (import writes no credential by design), then do the one thing the user is
// *required* to do next — open the host drawer and attach a credential. That
// save must not blank ProxyJump, or NewSshSessionByID's refusal disappears
// and atterm dials a bastion-only machine directly (and syncs the ungated
// record to every other device).
func TestUpdateSSHHostPreservesConfigDerivedFields(t *testing.T) {
	a := newHostsTestApp(t)
	stored, err := a.AddSSHHost(SSHHost{
		Alias: "inner", Host: "10.0.0.9", User: "root", AuthKind: "password",
		IdentityFile: "~/.ssh/id_ed25519",
		ProxyJump:    "bastion",
		ProxyCommand: "ssh -W %h:%p bastion",
	}, sshCredential{})
	if err != nil {
		t.Fatalf("AddSSHHost: %v", err)
	}

	// Exactly what a form-built payload looks like: the fields the drawer
	// owns, and zero values for the three it has no control for.
	if err := a.UpdateSSHHost(SSHHost{
		ID: stored.ID, Alias: "inner", Host: "10.0.0.9", User: "root",
		AuthKind: "password",
	}, &sshCredential{Password: "pw"}); err != nil {
		t.Fatalf("UpdateSSHHost: %v", err)
	}

	got := a.ListSSHHosts()[0]
	if got.ProxyJump != "bastion" {
		t.Errorf("ProxyJump must survive an edit, got %q", got.ProxyJump)
	}
	if got.ProxyCommand != "ssh -W %h:%p bastion" {
		t.Errorf("ProxyCommand must survive an edit, got %q", got.ProxyCommand)
	}
	if got.IdentityFile != "~/.ssh/id_ed25519" {
		t.Errorf("IdentityFile must survive an edit, got %q", got.IdentityFile)
	}
}

// TestUpdateSSHHostStillClearsUIOwnedFields is the other half of the split:
// the guard above must not turn UpdateSSHHost into "never clears anything".
// Alias, Tags and Note are the user's own data, editable in (or carried
// through) the drawer, so emptying them is a legitimate save, not a payload
// that forgot a field.
func TestUpdateSSHHostStillClearsUIOwnedFields(t *testing.T) {
	a := newHostsTestApp(t)
	stored, err := a.AddSSHHost(SSHHost{
		Alias: "box", Host: "h", User: "u", AuthKind: "password",
		Tags: []string{"prod"}, Note: "old note",
	}, sshCredential{})
	if err != nil {
		t.Fatalf("AddSSHHost: %v", err)
	}

	if err := a.UpdateSSHHost(SSHHost{
		ID: stored.ID, Host: "h", User: "u", AuthKind: "password",
	}, nil); err != nil {
		t.Fatalf("UpdateSSHHost: %v", err)
	}

	got := a.ListSSHHosts()[0]
	if got.Alias != "" {
		t.Errorf("Alias must be clearable, got %q", got.Alias)
	}
	if len(got.Tags) != 0 {
		t.Errorf("Tags must be clearable, got %+v", got.Tags)
	}
	if got.Note != "" {
		t.Errorf("Note must be clearable, got %q", got.Note)
	}
}

func TestDeleteSSHHostClearsCredential(t *testing.T) {
	a := newHostsTestApp(t)
	h, _ := a.AddSSHHost(SSHHost{Host: "h", User: "u", AuthKind: "password"}, sshCredential{Password: "pw"})

	if err := a.DeleteSSHHost(h.ID); err != nil {
		t.Fatalf("DeleteSSHHost: %v", err)
	}
	if len(a.ListSSHHosts()) != 0 {
		t.Fatal("host not removed")
	}
	if _, err := safekeyring.Get(sshCredentialService(), h.ID); err == nil {
		t.Fatal("credential should be gone")
	}
}

func TestAdapterSSHHostsEncryptedRoundTrip(t *testing.T) {
	useIsolatedKeyring(t)
	cs := newTestConfigStore(t)
	a := &App{cfgStore: cs}

	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	a.accountKey = key

	// canaryPW is a long, unmistakable secret. A short value like "pw" would
	// collide with random base64 output, making the leakage assertion flaky.
	const canaryPW = "CANARY-secret-password-do-not-leak-0123456789"
	h, err := a.AddSSHHost(SSHHost{Host: "h", User: "u", AuthKind: "password"}, sshCredential{Password: canaryPW})
	if err != nil {
		t.Fatal(err)
	}

	adapter := newAppConfigAdapter(cs, a.accountKeyForSync)

	val, ok := adapter.ReadValue("ssh_hosts_encrypted")
	if !ok {
		t.Fatal("expected ReadValue ok with account key")
	}
	// The sealed value the relay would see must not contain the plaintext
	// secret — that is the whole point of E2EE sync.
	if strings.Contains(string(val), canaryPW) {
		t.Fatalf("plaintext secret leaked into sealed value: %s", val)
	}

	cfg := cs.Get()
	cfg.SSHHosts = nil
	_ = cs.Set(cfg)
	_ = safekeyring.Delete(sshCredentialService(), h.ID)

	if err := adapter.WriteValue("ssh_hosts_encrypted", val); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}
	if got := cs.Get().SSHHosts; len(got) != 1 || got[0].ID != h.ID {
		t.Fatalf("hosts not restored: %+v", got)
	}
	raw, err := safekeyring.Get(sshCredentialService(), h.ID)
	if err != nil || !strings.Contains(raw, canaryPW) {
		t.Fatalf("credential not restored: %v %q", err, raw)
	}
}

func TestAdapterSSHHostsNoAccountKeySkips(t *testing.T) {
	cs := newTestConfigStore(t)
	a := &App{cfgStore: cs} // accountKey nil
	adapter := newAppConfigAdapter(cs, a.accountKeyForSync)

	if _, ok := adapter.ReadValue("ssh_hosts_encrypted"); ok {
		t.Fatal("no account key must skip ReadValue (ok=false)")
	}
	if err := adapter.WriteValue("ssh_hosts_encrypted", json.RawMessage(`"x"`)); err != nil {
		t.Fatalf("WriteValue no-op expected, got %v", err)
	}
}

func TestCRUDMarksSSHSyncDirty(t *testing.T) {
	a := newHostsTestApp(t)
	h, err := a.AddSSHHost(SSHHost{Host: "h", User: "u", AuthKind: "password"}, sshCredential{Password: "pw"})
	if err != nil {
		t.Fatal(err)
	}
	if !a.cfgStore.Get().PrefsMeta["ssh_hosts_encrypted"].Dirty {
		t.Fatal("Add should mark ssh_hosts_encrypted dirty")
	}

	cfg := a.cfgStore.Get()
	m := cfg.PrefsMeta["ssh_hosts_encrypted"]
	m.Dirty = false
	cfg.PrefsMeta["ssh_hosts_encrypted"] = m
	_ = a.cfgStore.Set(cfg)

	if err := a.DeleteSSHHost(h.ID); err != nil {
		t.Fatal(err)
	}
	if !a.cfgStore.Get().PrefsMeta["ssh_hosts_encrypted"].Dirty {
		t.Fatal("Delete should mark ssh_hosts_encrypted dirty")
	}
}

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

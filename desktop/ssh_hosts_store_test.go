package main

import (
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

func newHostsTestApp(t *testing.T) *App {
	t.Helper()
	safekeyring.UseFileStore()
	safekeyring.SetFileDirForTest(t.TempDir())
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

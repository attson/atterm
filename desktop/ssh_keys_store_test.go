package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"

	"github.com/attson/atterm/internal/safekeyring"
)

func testKeyPEM(t *testing.T) string {
	t.Helper()
	k, _ := rsa.GenerateKey(rand.Reader, 2048)
	der := x509.MarshalPKCS1PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}

func newKeysTestApp(t *testing.T) *App {
	t.Helper()
	useIsolatedKeyring(t)
	return &App{cfgStore: newTestConfigStore(t)}
}

func TestAddSSHKeyParsesTypeAndStores(t *testing.T) {
	a := newKeysTestApp(t)
	k, err := a.AddSSHKey("aws", testKeyPEM(t), "")
	if err != nil {
		t.Fatalf("AddSSHKey: %v", err)
	}
	if k.ID == "" || k.Name != "aws" || k.KeyType != "RSA" {
		t.Fatalf("unexpected key: %+v", k)
	}
	list := a.ListSSHKeys()
	if len(list) != 1 || list[0].ID != k.ID {
		t.Fatalf("list mismatch: %+v", list)
	}
	raw, err := safekeyring.Get(sshKeyService(), k.ID)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	var sec sshKeySecret
	_ = json.Unmarshal([]byte(raw), &sec)
	if !strings.Contains(sec.PrivateKey, "PRIVATE KEY") {
		t.Fatalf("private key not stored: %q", sec.PrivateKey)
	}
}

func TestAddSSHKeyRejectsInvalidPEM(t *testing.T) {
	a := newKeysTestApp(t)
	if _, err := a.AddSSHKey("bad", "not a key", ""); err == nil {
		t.Fatal("expected invalid key error")
	}
}

func TestDeleteSSHKeyInUseRejected(t *testing.T) {
	a := newKeysTestApp(t)
	k, _ := a.AddSSHKey("aws", testKeyPEM(t), "")
	cfg := a.cfgStore.Get()
	cfg.SSHHosts = []SSHHost{{ID: "h1", Host: "h", User: "u", AuthKind: "key", KeyID: k.ID, Alias: "box"}}
	_ = a.cfgStore.Set(cfg)

	err := a.DeleteSSHKey(k.ID)
	if err == nil || !strings.Contains(err.Error(), "box") {
		t.Fatalf("expected in-use error naming host, got %v", err)
	}
	if len(a.ListSSHKeys()) != 1 {
		t.Fatal("key should not be deleted while in use")
	}
}

func TestDeleteSSHKeyUnreferenced(t *testing.T) {
	a := newKeysTestApp(t)
	k, _ := a.AddSSHKey("aws", testKeyPEM(t), "")
	if err := a.DeleteSSHKey(k.ID); err != nil {
		t.Fatalf("DeleteSSHKey: %v", err)
	}
	if len(a.ListSSHKeys()) != 0 {
		t.Fatal("key not removed")
	}
	if _, err := safekeyring.Get(sshKeyService(), k.ID); err == nil {
		t.Fatal("secret should be gone")
	}
}

func TestUpdateSSHKeyKeepsPrivateKeyWhenBlank(t *testing.T) {
	a := newKeysTestApp(t)
	k, _ := a.AddSSHKey("aws", testKeyPEM(t), "")
	if err := a.UpdateSSHKey(k.ID, "aws-renamed", "", ""); err != nil {
		t.Fatalf("UpdateSSHKey: %v", err)
	}
	if a.ListSSHKeys()[0].Name != "aws-renamed" {
		t.Fatal("name not updated")
	}
	if _, err := safekeyring.Get(sshKeyService(), k.ID); err != nil {
		t.Fatal("private key should be kept")
	}
}

package main

import (
	"crypto/rand"
	"strings"
	"testing"
)

func testAccountKey(t *testing.T) []byte {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return k
}

func TestSealOpenSSHHostsRoundTrip(t *testing.T) {
	key := testAccountKey(t)
	// Long, unmistakable canaries so the leakage assertions can't collide
	// with random base64 output (a short value like "box" would be flaky).
	const canaryAlias = "CANARY-alias-do-not-leak-abcdef"
	const canaryPW = "CANARY-secret-password-do-not-leak-0123456789"
	hosts := []SSHHost{{ID: "1", Host: "h", User: "u", AuthKind: "password", Alias: canaryAlias}}
	creds := map[string]sshCredential{"1": {Password: canaryPW}}

	blob, err := sealSSHHosts(key, hosts, creds, nil, nil)
	if err != nil {
		t.Fatalf("sealSSHHosts: %v", err)
	}
	if blob == nil {
		t.Fatal("expected ciphertext, got nil")
	}
	if strings.Contains(string(blob), canaryPW) || strings.Contains(string(blob), canaryAlias) {
		t.Fatalf("plaintext leaked into sealed blob: %s", blob)
	}

	gotHosts, gotCreds, _, _, err := openSSHHosts(key, blob)
	if err != nil {
		t.Fatalf("openSSHHosts: %v", err)
	}
	if len(gotHosts) != 1 || gotHosts[0].ID != "1" || gotHosts[0].Alias != canaryAlias {
		t.Fatalf("hosts mismatch: %+v", gotHosts)
	}
	if gotCreds["1"].Password != canaryPW {
		t.Fatalf("cred mismatch: %+v", gotCreds)
	}
}

func TestSealSSHHostsEmptyAccountKeySkips(t *testing.T) {
	blob, err := sealSSHHosts(nil, []SSHHost{{ID: "1"}}, map[string]sshCredential{}, nil, nil)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if blob != nil {
		t.Fatalf("empty account key must skip (nil blob), got %s", blob)
	}
}

func TestOpenSSHHostsWrongKeyFails(t *testing.T) {
	blob, err := sealSSHHosts(testAccountKey(t), []SSHHost{{ID: "1"}}, map[string]sshCredential{}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := openSSHHosts(testAccountKey(t), blob); err == nil {
		t.Fatal("open with wrong key must fail")
	}
}

func TestSealOpenWithKeys(t *testing.T) {
	key := testAccountKey(t)
	const canaryPK = "CANARY-PRIVATE-KEY-do-not-leak-0123456789"
	hosts := []SSHHost{{ID: "h1", Host: "h", User: "u", AuthKind: "key", KeyID: "k1"}}
	keys := []SSHKey{{ID: "k1", Name: "aws", KeyType: "RSA"}}
	keySecrets := map[string]sshKeySecret{"k1": {PrivateKey: canaryPK}}

	blob, err := sealSSHHosts(key, hosts, map[string]sshCredential{}, keys, keySecrets)
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if strings.Contains(string(blob), canaryPK) {
		t.Fatalf("private key leaked: %s", blob)
	}
	gotHosts, _, gotKeys, gotSecrets, err := openSSHHosts(key, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(gotHosts) != 1 || gotHosts[0].KeyID != "k1" {
		t.Fatalf("hosts: %+v", gotHosts)
	}
	if len(gotKeys) != 1 || gotKeys[0].Name != "aws" {
		t.Fatalf("keys: %+v", gotKeys)
	}
	if gotSecrets["k1"].PrivateKey != canaryPK {
		t.Fatalf("secret: %+v", gotSecrets)
	}
}

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
	hosts := []SSHHost{{ID: "1", Host: "h", User: "u", AuthKind: "password", Alias: "box"}}
	creds := map[string]sshCredential{"1": {Password: "secret-pw"}}

	blob, err := sealSSHHosts(key, hosts, creds)
	if err != nil {
		t.Fatalf("sealSSHHosts: %v", err)
	}
	if blob == nil {
		t.Fatal("expected ciphertext, got nil")
	}
	if strings.Contains(string(blob), "secret-pw") || strings.Contains(string(blob), "box") {
		t.Fatalf("plaintext leaked into sealed blob: %s", blob)
	}

	gotHosts, gotCreds, err := openSSHHosts(key, blob)
	if err != nil {
		t.Fatalf("openSSHHosts: %v", err)
	}
	if len(gotHosts) != 1 || gotHosts[0].ID != "1" || gotHosts[0].Alias != "box" {
		t.Fatalf("hosts mismatch: %+v", gotHosts)
	}
	if gotCreds["1"].Password != "secret-pw" {
		t.Fatalf("cred mismatch: %+v", gotCreds)
	}
}

func TestSealSSHHostsEmptyAccountKeySkips(t *testing.T) {
	blob, err := sealSSHHosts(nil, []SSHHost{{ID: "1"}}, map[string]sshCredential{})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if blob != nil {
		t.Fatalf("empty account key must skip (nil blob), got %s", blob)
	}
}

func TestOpenSSHHostsWrongKeyFails(t *testing.T) {
	blob, err := sealSSHHosts(testAccountKey(t), []SSHHost{{ID: "1"}}, map[string]sshCredential{})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := openSSHHosts(testAccountKey(t), blob); err == nil {
		t.Fatal("open with wrong key must fail")
	}
}

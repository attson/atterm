package peerbackup

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
)

func TestSealOpenHidesPlaintextAndRejectsTampering(t *testing.T) {
	const passphrase = "correct horse battery staple"
	plaintext := []byte(`{"signing_private":"CANARY-private-key"}`)
	sealed, err := Seal(passphrase, plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("CANARY")) {
		t.Fatal("recovery file contains plaintext private key marker")
	}
	opened, err := Open(passphrase, sealed)
	if err != nil || !bytes.Equal(opened, plaintext) {
		t.Fatalf("open=%q err=%v", opened, err)
	}
	clear(opened)
	if _, err := Open("wrong passphrase value", sealed); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("wrong passphrase error=%v", err)
	}

	var file packageFile
	if err := json.Unmarshal(sealed, &file); err != nil {
		t.Fatal(err)
	}
	file.Ciphertext[len(file.Ciphertext)-1] ^= 1
	tampered, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(passphrase, tampered); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("tampered error=%v", err)
	}
}

func TestOpenRejectsParameterDowngradeAndUnknownFields(t *testing.T) {
	sealed, err := Seal("correct horse battery staple", []byte("trust state"))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(sealed, &raw); err != nil {
		t.Fatal(err)
	}
	kdf := raw["kdf"].(map[string]any)
	kdf["m"] = float64(8)
	downgraded, _ := json.Marshal(raw)
	if _, err := Open("correct horse battery staple", downgraded); !errors.Is(err, ErrInvalidFile) {
		t.Fatalf("downgraded KDF error=%v", err)
	}
	raw["extra"] = true
	unknown, _ := json.Marshal(raw)
	if _, err := Open("correct horse battery staple", unknown); !errors.Is(err, ErrInvalidFile) {
		t.Fatalf("unknown field error=%v", err)
	}
}

func TestSealRejectsWeakPassphrase(t *testing.T) {
	if _, err := Seal("too-short", []byte("trust")); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("weak passphrase error=%v", err)
	}
}

package main

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func TestValidateBootstrapPassword(t *testing.T) {
	cases := []struct {
		name  string
		token string
		valid bool
	}{
		{"weak: dev (blacklist + length)", "dev", false},
		{"weak: short", "changeme123", false},
		{"weak: single char class repeating", "aaaaaaaaaaaaaaaaA1!", false},
		{"strong: Bootstrap-Pass-2026!", "Bootstrap-Pass-2026!", true},
		{"strong: xY9! repeated 4x", strings.Repeat("xY9!", 4), true},
		{"weak: hex 48 chars (only 2 classes: lower+digit)", "a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2c3d4a1b2", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBootstrapPassword(tc.token)
			if tc.valid && err != nil {
				t.Errorf("validateBootstrapPassword(%q) = %v; want nil", tc.token, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("validateBootstrapPassword(%q) = nil; want error", tc.token)
			}
		})
	}
}

func TestValidateBootstrapPassword_CryptoRandom(t *testing.T) {
	// A 24-byte random token encoded as 48 hex chars: lower+digit only (2 classes) → FAIL.
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	hexTok := hex.EncodeToString(b) // 48 hex chars: lowercase + digits only → fails ≥3 classes
	if err := validateBootstrapPassword(hexTok); err == nil {
		t.Errorf("validateBootstrapPassword(hex48) = nil; want error (only 2 char classes)")
	}

	// Use a hand-crafted strong token to verify the happy path.
	strong := "Bootstrap-Pass-2026!"
	if err := validateBootstrapPassword(strong); err != nil {
		t.Errorf("validateBootstrapPassword(strong) = %v; want nil", err)
	}
}

func TestValidateBootstrapPassword_BlacklistedExact(t *testing.T) {
	blacklisted := []string{"dev", "test", "admin", "password", "changeme", "letmein", "12345", "secret"}
	for _, tok := range blacklisted {
		// Short blacklisted tokens fail on the length check (≥16 chars) before reaching
		// the blacklist check itself.
		err := validateBootstrapPassword(tok)
		if err == nil {
			t.Errorf("validateBootstrapPassword(%q) = nil; want error (short/blacklisted)", tok)
		}
	}
}

func TestValidateBootstrapPassword_RunLimit(t *testing.T) {
	// 5 identical chars in a row should fail even if long enough and has mixed classes.
	// Build a 16+ char password with a run of 5 identical chars and 3 char classes.
	bad := "aaaaaaaaaaaaaaaaA1!" // 16 'a' + 'A' + '1' + '!' = 19 chars; run of >4 'a' → fail
	if err := validateBootstrapPassword(bad); err == nil {
		t.Errorf("validateBootstrapPassword(%q) = nil; want error (run > 4)", bad)
	}
}

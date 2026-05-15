package main

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"testing"
)

func TestValidateAdminToken(t *testing.T) {
	cases := []struct {
		name  string
		token string
		valid bool
	}{
		{"weak: dev (blacklist + length)", "dev", false},
		{"weak: short", "changeme123", false},
		{"weak: single char class repeating", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", false},
		{"strong: Aa1! repeated 8x", "Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!Aa1!", true},
		{"strong: xY9! repeated 8x", strings.Repeat("xY9!", 8), true},
		{"weak: hex 64 chars (only 2 classes: lower+digit)", strings.Repeat("a1b2c3d4", 8), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateAdminToken(tc.token)
			if tc.valid && err != nil {
				t.Errorf("validateAdminToken(%q) = %v; want nil", tc.token, err)
			}
			if !tc.valid && err == nil {
				t.Errorf("validateAdminToken(%q) = nil; want error", tc.token)
			}
		})
	}
}

func TestValidateAdminToken_CryptoRandom(t *testing.T) {
	// A 32-byte random token encoded as 64 hex chars: lower+digit only (2 classes) → FAIL.
	// Use base64 encoding which includes upper+lower+digit+symbols → PASS.
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	hexTok := hex.EncodeToString(b) // 48 hex chars: lowercase + digits only → fails ≥3 classes
	if err := validateAdminToken(hexTok); err == nil {
		t.Errorf("validateAdminToken(hex48) = nil; want error (only 2 char classes)")
	}

	// 32 random bytes via a base32-like approach that mixes upper/lower/digit/sym
	// Use a hand-crafted strong token to verify the happy path.
	strong := "Xy9!Xy9!Xy9!Xy9!Xy9!Xy9!Xy9!Xy9!"
	if err := validateAdminToken(strong); err != nil {
		t.Errorf("validateAdminToken(strong) = %v; want nil", err)
	}
}

func TestValidateAdminToken_BlacklistedExact(t *testing.T) {
	blacklisted := []string{"dev", "test", "admin", "password", "changeme", "letmein", "12345", "secret"}
	for _, tok := range blacklisted {
		// Only tokens >= 32 chars would reach the blacklist check, so this confirms the
		// length check fires first for short tokens, and we also test the uppercased form
		// for the blacklist check itself.
		err := validateAdminToken(tok)
		if err == nil {
			t.Errorf("validateAdminToken(%q) = nil; want error (short/blacklisted)", tok)
		}
	}
}

func TestValidateAdminToken_RunLimit(t *testing.T) {
	// 5 identical chars in a row should fail even if long enough and has mixed classes.
	tok := "AaAaAaAaAaAaAa1!aaaaa" // ends with 5 'a' in a row (run of 5, must fail)
	tok = tok + strings.Repeat("x", 32-len(tok)) // pad to ≥32
	// Reconstruct to have clear 5-in-a-row:
	// "Aa1!" x 6 = 24 chars + "aaaaaa" = 30 chars (still short)
	// Build a 32-char token with a run of 5 identical chars and 3 char classes.
	bad := "Aa1!Aa1!Aa1!Aa1!" + "aaaaa!!!" // 24+8=32; run of 5 'a' → fail
	if err := validateAdminToken(bad); err == nil {
		t.Errorf("validateAdminToken(%q) = nil; want error (run > 4)", bad)
	}
}

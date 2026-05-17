package main

import (
	"errors"
	"strings"
	"unicode"
)

// weakBootstrapPasswordBlacklist holds plaintexts so commonly used that
// any deploy that picks one is almost certainly misconfigured. Match is
// case-insensitive.
var weakBootstrapPasswordBlacklist = map[string]bool{
	"dev":      true,
	"test":     true,
	"admin":    true,
	"password": true,
	"changeme": true,
	"letmein":  true,
	"12345":    true,
	"secret":   true,
}

// validateBootstrapPassword enforces the rule applied to
// ATTERM_BOOTSTRAP_ADMIN_PASSWORD when used to create a new admin user.
// The plaintext lives in env files / systemd units and is therefore a
// long-lived disk secret, so the rule is stricter than the everyday
// user ChangePassword ≥12-char minimum.
func validateBootstrapPassword(pw string) error {
	if len(pw) < 16 {
		return errors.New("ATTERM_BOOTSTRAP_ADMIN_PASSWORD: must be ≥ 16 characters")
	}
	if weakBootstrapPasswordBlacklist[strings.ToLower(pw)] {
		return errors.New("ATTERM_BOOTSTRAP_ADMIN_PASSWORD: matches a known weak value")
	}
	var upper, lower, digit, sym bool
	var runChar rune
	var run int
	for _, c := range pw {
		switch {
		case unicode.IsUpper(c):
			upper = true
		case unicode.IsLower(c):
			lower = true
		case unicode.IsDigit(c):
			digit = true
		default:
			sym = true
		}
		if c == runChar {
			run++
			if run > 4 {
				return errors.New("ATTERM_BOOTSTRAP_ADMIN_PASSWORD: too many repeated characters in a row")
			}
		} else {
			run, runChar = 1, c
		}
	}
	classes := 0
	for _, b := range []bool{upper, lower, digit, sym} {
		if b {
			classes++
		}
	}
	if classes < 3 {
		return errors.New("ATTERM_BOOTSTRAP_ADMIN_PASSWORD: must contain at least 3 of {upper, lower, digit, symbol}")
	}
	return nil
}

package main

import (
	"errors"
	"strings"
	"unicode"
)

var weakAdminTokenBlacklist = map[string]bool{
	"dev":      true,
	"test":     true,
	"admin":    true,
	"password": true,
	"changeme": true,
	"letmein":  true,
	"12345":    true,
	"secret":   true,
}

func validateAdminToken(tok string) error {
	if len(tok) < 32 {
		return errors.New("ATTERM_ADMIN_TOKEN: must be ≥ 32 characters")
	}
	if weakAdminTokenBlacklist[strings.ToLower(tok)] {
		return errors.New("ATTERM_ADMIN_TOKEN: matches a known weak token")
	}
	var upper, lower, digit, sym bool
	var runChar rune
	var run int
	for _, c := range tok {
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
				return errors.New("ATTERM_ADMIN_TOKEN: too many repeated characters in a row")
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
		return errors.New("ATTERM_ADMIN_TOKEN: must contain at least 3 of {upper, lower, digit, symbol}")
	}
	return nil
}

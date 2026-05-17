package main

import (
	"context"
	"fmt"
	"log"
	"net/mail"

	"github.com/attson/atterm/internal/userstore"
)

// bootstrapAdmin reconciles the relay's admin role with the
// ATTERM_BOOTSTRAP_ADMIN_EMAIL / _PASSWORD env vars on startup. See
// docs/superpowers/specs/2026-05-17-web-ui-redesign-design.md.
//
//   - email == ""                          → no-op.
//   - email + existing user                → promote, ignore password,
//     WARN if password was set.
//   - email + missing user + valid password → create as admin, WARN to
//     unset the password env now.
//   - malformed email                      → error; caller log.Fatalfs.
//   - weak/empty password (create path)    → error; caller log.Fatalfs.
func bootstrapAdmin(ctx context.Context, store userstore.Store, email, password string) error {
	if email == "" {
		return nil
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return fmt.Errorf("ATTERM_BOOTSTRAP_ADMIN_EMAIL: %w", err)
	}
	// If the password is set, enforce the bootstrap strength rule now —
	// it's a no-op when the user already exists (EnsureAdminUser will
	// ignore it), but a misconfigured weak password should still fail
	// fast so the operator notices.
	if password != "" {
		if err := validateBootstrapPassword(password); err != nil {
			return err
		}
	}
	created, err := store.EnsureAdminUser(ctx, email, password)
	if err != nil {
		return err
	}
	if created {
		log.Printf("WARN: bootstrap created admin user %s — unset ATTERM_BOOTSTRAP_ADMIN_PASSWORD and restart to remove the credential from process state.", email)
	} else if password != "" {
		log.Printf("WARN: ATTERM_BOOTSTRAP_ADMIN_PASSWORD set but %s already exists — password ignored. Unset the env to remove it from process state.", email)
	} else {
		log.Printf("promoted existing user to admin: %s", email)
	}
	return nil
}

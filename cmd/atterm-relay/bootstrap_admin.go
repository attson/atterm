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
//   - email == ""                          → no-op (returns "", nil, nil).
//   - email + existing user                → promote, ignore password,
//     WARN if password was set, mint session, return token.
//   - email + missing user + valid password → create as admin, WARN to
//     unset the password env now, mint session, return token.
//   - malformed email                      → error; caller log.Fatalfs.
//   - weak/empty password (create path)    → error; caller log.Fatalfs.
func bootstrapAdmin(ctx context.Context, store userstore.Store, email, password string) (string, *userstore.User, error) {
	if email == "" {
		return "", nil, nil
	}
	if _, err := mail.ParseAddress(email); err != nil {
		return "", nil, fmt.Errorf("ATTERM_BOOTSTRAP_ADMIN_EMAIL: %w", err)
	}
	// If the password is set, enforce the bootstrap strength rule now —
	// it's a no-op when the user already exists (EnsureAdminUser will
	// ignore it), but a misconfigured weak password should still fail
	// fast so the operator notices.
	if password != "" {
		if err := validateBootstrapPassword(password); err != nil {
			return "", nil, err
		}
	}
	created, err := store.EnsureAdminUser(ctx, email, password)
	if err != nil {
		return "", nil, err
	}

	// Look up the user to get its ID for session creation
	// For a newly created user with a password, VerifyPassword will succeed.
	// For an existing user that was promoted, VerifyPassword might fail if the
	// password env var is empty or wrong, so fall back to ListUsers.
	var user *userstore.User
	if created && password != "" {
		// Newly created user with password - VerifyPassword should work
		user, err = store.VerifyPassword(ctx, email, password)
		if err != nil {
			return "", nil, fmt.Errorf("verify password for new user: %w", err)
		}
	} else {
		// Existing user (created=false) - look up by listing users since
		// password may not be known or may have changed
		users, err := store.ListUsers(ctx)
		if err != nil {
			return "", nil, fmt.Errorf("list users: %w", err)
		}
		for i := range users {
			if users[i].Email == email {
				user = &users[i]
				break
			}
		}
		if user == nil {
			return "", nil, fmt.Errorf("user not found after ensure: %s", email)
		}
	}

	if created {
		log.Printf("WARN: bootstrap created admin user %s — unset ATTERM_BOOTSTRAP_ADMIN_PASSWORD and restart to remove the credential from process state.", email)
	} else if password != "" {
		log.Printf("WARN: ATTERM_BOOTSTRAP_ADMIN_PASSWORD set but %s already exists — password ignored. Unset the env to remove it from process state.", email)
	} else {
		log.Printf("promoted existing user to admin: %s", email)
	}

	// Mint a session token for the newly created/promoted admin user
	tok, _, err := store.CreateSession(ctx, user.ID, "bootstrap", "", userstore.DefaultSessionTTL)
	if err != nil {
		return "", nil, fmt.Errorf("bootstrap: create session: %w", err)
	}

	return tok, user, nil
}

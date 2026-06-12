package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"

	"github.com/attson/atterm/internal/userstore"
)

// localAdminEmail identifies the desktop-process-owned admin user. The
// in-process mini-relay validates session tokens against userstore; the
// desktop holds a session for this user so its own webview can talk to
// the mini-relay over the same /api/* + WS contract as remote relays.
const localAdminEmail = "local@atterm.local"

// bootstrapLocalAdmin ensures a local admin user exists with the given
// email + password, then mints a fresh session token. Safe to call on
// every launch: when the user already exists (and password matches), it
// just creates a new session and returns the new token; the old session
// is left in place to expire on its own.
//
// On first run pw must be non-empty (used to create the user). On
// subsequent runs the same pw is required to re-authenticate; if pw is
// wrong (e.g. the config file was edited or copied between machines) we
// fall back to creating a new user, which will fail with a unique-email
// constraint and surface the misconfiguration.
func bootstrapLocalAdmin(ctx context.Context, store userstore.Store, email, pw string) (string, *userstore.User, error) {
	if pw == "" {
		return "", nil, fmt.Errorf("local admin password is empty")
	}
	user, err := store.VerifyPassword(ctx, email, pw)
	if err != nil {
		return "", nil, fmt.Errorf("verify local admin password: %w", err)
	}
	if user == nil {
		// VerifyPassword returns nil for both "user missing" and "password
		// mismatch". Try CreateUser first — succeeds when fresh. On
		// ErrEmailTaken (the user exists but the config-stored password
		// drifted from the DB hash), force-reset the password so the
		// desktop can always log into its own mini relay. Only ever runs
		// on local-process-owned identities; never exposed to the relay
		// HTTP surface.
		user, err = store.CreateUser(ctx, email, pw)
		if errors.Is(err, userstore.ErrEmailTaken) {
			user, err = store.ResetPasswordByEmail(ctx, email, pw)
			if err != nil {
				return "", nil, fmt.Errorf("reset local admin password: %w", err)
			}
		} else if err != nil {
			return "", nil, fmt.Errorf("create local admin: %w", err)
		}
		if !user.IsAdmin {
			if err := store.SetUserAdmin(ctx, user.ID, true); err != nil {
				return "", nil, fmt.Errorf("promote local admin: %w", err)
			}
		}
	}
	tok, _, err := store.CreateSession(ctx, user.ID, "desktop-local", "", userstore.DefaultSessionTTL)
	if err != nil {
		return "", nil, fmt.Errorf("create local session: %w", err)
	}
	return tok, user, nil
}

// randomPassword returns a base64url-encoded random string of n bytes.
// Used to seed appConfig.LocalAdminPassword on first launch. Returns the
// empty string on a (hypothetical) rand.Read failure — caller treats
// empty as fatal.
func randomPassword(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

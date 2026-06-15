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

// bootstrapLocalAdmin ensures a local admin user exists in store and
// mints a fresh session token. The relay is loopback-only and the only
// way to talk to it is via the returned token, so the user has no
// password / OPAQUE record — sessions are minted by this function and
// nobody else.
//
// Behaviour:
//   - On first run: create a fresh OPAQUE-shaped user (auth_mode='opaque'
//     but no registration record persisted), mark admin, mint a session.
//   - On subsequent runs: look up the existing user by email, mint a
//     fresh session. The previous session is left in place to expire on
//     its own (DefaultSessionTTL governs it).
//
// The pw argument is no longer meaningful — password auth was removed in
// M1a T12 — but the signature is preserved so callers in app.go don't
// have to change. It is accepted, validated as non-empty (to keep
// callers honest about supplying a config-stored seed), and otherwise
// ignored at the userstore boundary.
func bootstrapLocalAdmin(ctx context.Context, store userstore.Store, email, pw string) (string, *userstore.User, error) {
	if pw == "" {
		return "", nil, fmt.Errorf("local admin seed is empty")
	}
	// Look up existing user; if missing, create.
	user, err := store.GetUserByEmail(ctx, email)
	if errors.Is(err, userstore.ErrUserNotFound) {
		user, err = store.CreateOpaqueUser(ctx, email)
		if err != nil {
			return "", nil, fmt.Errorf("create local admin: %w", err)
		}
	} else if err != nil {
		return "", nil, fmt.Errorf("lookup local admin: %w", err)
	}
	if !user.IsAdmin {
		if err := store.SetUserAdmin(ctx, user.ID, true); err != nil {
			return "", nil, fmt.Errorf("promote local admin: %w", err)
		}
		// Re-fetch to reflect the flip.
		if u2, err := store.GetUser(ctx, user.ID); err == nil {
			user = u2
		}
	}
	tok, _, err := store.CreateSession(ctx, user.ID, "desktop-local", "", userstore.DefaultSessionTTL)
	if err != nil {
		return "", nil, fmt.Errorf("create local session: %w", err)
	}
	return tok, user, nil
}

// randomPassword returns a base64url-encoded random string of n bytes.
// Retained because desktop appConfig still seeds LocalAdminPassword to
// distinguish first-run from re-launch (non-empty value means "we have
// initialized once before"). The bytes are no longer used as actual
// credential material; bootstrapLocalAdmin only checks they are
// non-empty before creating the no-credential local admin row.
func randomPassword(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

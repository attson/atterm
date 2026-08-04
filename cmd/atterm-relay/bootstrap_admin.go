package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// bootstrapAdminTokenTTL is how long the operator has to complete OPAQUE
// registration with the printed claim token. One week is generous enough
// for a relay that boots, prints the token, and is left waiting for the
// operator to come back to it — and short enough that a forgotten token
// rotates out on its own.
const bootstrapAdminTokenTTL = 7 * 24 * time.Hour

// bootstrapAdmin mints a single-use claim token that the operator uses to
// complete OPAQUE registration as the named admin email. The plaintext
// token is printed once; it cannot be recovered after this run.
//
// OPAQUE requires a client-side protocol round (the user types their
// password into a client that runs RegistrationInit / RegistrationFinalize),
// so the server can no longer accept a password on boot. The claim token
// is the rendezvous: the operator pastes it into the registration flow,
// which validates the token, creates the user, consumes the token, and
// promotes the new user to admin via SetUserAdmin.
//
// Empty email is treated as "no bootstrap requested" and returns nil so
// that local-only relays without ATTERM_BOOTSTRAP_ADMIN_EMAIL set still
// boot cleanly.
func bootstrapAdmin(ctx context.Context, store userstore.Store, email string) error {
	if email == "" {
		return nil // no admin bootstrap requested
	}
	sqliteStore, ok := store.(*userstore.DBStore)
	if !ok {
		return fmt.Errorf("bootstrap admin: store is not SQLite (got %T)", store)
	}
	plaintext, err := sqliteStore.CreateClaimToken(ctx, email, "admin", bootstrapAdminTokenTTL)
	if err != nil {
		return fmt.Errorf("bootstrap admin: %w", err)
	}
	log.Printf("bootstrap-admin: claim token for %s (expires in 7 days):", email)
	log.Printf("    %s", plaintext)
	log.Printf("bootstrap-admin: complete OPAQUE registration via POST /api/auth/register/finalize with this token in claim_token to receive admin role")
	return nil
}

package relay

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/attson/atterm/internal/userstore"
)

// newRealmID generates a fresh opaque realm identifier.
func newRealmID() string { return uuid.NewString() }

// LoadOrInitRealm returns the stable cluster realm_id, generating and
// persisting one on first boot. envRealmID (ATTERM_RELAY_REALM_ID) is an
// optional operator pin: if the DB already holds a different realm, this is a
// hard error to avoid silently orphaning every client's account_key.
// Concurrent first-boot nodes converge on one realm via EnsureRealmState.
func LoadOrInitRealm(ctx context.Context, store userstore.Store, envRealmID string) (string, error) {
	envRealmID = strings.TrimSpace(envRealmID)

	existing, err := store.GetRealmState(ctx)
	switch {
	case err == nil:
		if envRealmID != "" && envRealmID != existing.RealmID {
			return "", fmt.Errorf("ATTERM_RELAY_REALM_ID %q conflicts with persisted realm %q", envRealmID, existing.RealmID)
		}
		return existing.RealmID, nil
	case errors.Is(err, userstore.ErrRealmStateMissing):
		candidate := envRealmID
		if candidate == "" {
			candidate = newRealmID()
		}
		rs, err := store.EnsureRealmState(ctx, candidate)
		if err != nil {
			return "", err
		}
		if envRealmID != "" && rs.RealmID != envRealmID {
			return "", fmt.Errorf("ATTERM_RELAY_REALM_ID %q conflicts with concurrently-initialized realm %q", envRealmID, rs.RealmID)
		}
		return rs.RealmID, nil
	default:
		return "", err
	}
}

package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrRealmStateMissing is returned by GetRealmState when the singleton row
// has not been initialized yet (fresh relay).
var ErrRealmStateMissing = errors.New("userstore: realm state not initialized")

// RealmState is the stable cluster-wide realm identity (relay_realm_state,
// id=1). It is generated once and never changed; clients anchor their E2EE
// account_key to RealmID instead of the physical relay origin.
type RealmState struct {
	RealmID   string
	CreatedAt time.Time
}

// GetRealmState reads the singleton realm row, or ErrRealmStateMissing if absent.
func (s *DBStore) GetRealmState(ctx context.Context) (RealmState, error) {
	var (
		rs        RealmState
		createdAt int64
	)
	err := s.db.QueryRowContext(ctx, s.dia.Rebind(
		`SELECT realm_id, created_at FROM relay_realm_state WHERE id = 1`)).
		Scan(&rs.RealmID, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RealmState{}, ErrRealmStateMissing
	}
	if err != nil {
		return RealmState{}, fmt.Errorf("get realm state: %w", err)
	}
	rs.CreatedAt = time.Unix(createdAt, 0)
	return rs, nil
}

// EnsureRealmState inserts the realm row (id=1) with candidateRealmID if no
// row exists yet, then returns the effective state. First writer wins: if a
// row already exists (or a concurrent node inserted first), candidateRealmID
// is ignored and the existing realm is returned. Concurrent first-boot nodes
// converge on a single realm_id.
func (s *DBStore) EnsureRealmState(ctx context.Context, candidateRealmID string) (RealmState, error) {
	if _, err := s.db.ExecContext(ctx, s.dia.Rebind(
		`INSERT INTO relay_realm_state(id, realm_id, created_at)
		 VALUES (1, ?, ?)
		 ON CONFLICT(id) DO NOTHING`),
		candidateRealmID, time.Now().Unix()); err != nil {
		return RealmState{}, fmt.Errorf("ensure realm state: %w", err)
	}
	return s.GetRealmState(ctx)
}

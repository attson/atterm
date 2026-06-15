package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var (
	// ErrOpaqueStateMissing is returned by GetOpaqueServerState when the
	// singleton row in opaque_server_state has not been initialized yet.
	// Callers (the OPAQUE server bootstrap) interpret this as a signal to
	// generate fresh OPRF seed + AKE keypair and persist them.
	ErrOpaqueStateMissing = errors.New("userstore: opaque server state not initialized")
	// ErrOpaqueRecordMissing is returned by GetOpaqueRecord when no record
	// exists for the given user. Used by login-init to surface a generic
	// "credentials invalid" without leaking account existence.
	ErrOpaqueRecordMissing = errors.New("userstore: opaque record not found")
)

// OpaqueServerState is the persisted per-relay OPAQUE server material:
// the OPRF seed (used to derive per-user OPRF keys) and the long-term
// server AKE keypair. Generated once on first boot, never rotated in v1.
type OpaqueServerState struct {
	OPRFSeed        []byte
	AKEServerSecret []byte
	AKEServerPublic []byte
	Suite           string
	CreatedAt       time.Time
}

// GetOpaqueServerState loads the singleton opaque_server_state row.
// Returns ErrOpaqueStateMissing when the table is empty (first boot).
func (s *SQLiteStore) GetOpaqueServerState(ctx context.Context) (OpaqueServerState, error) {
	var (
		seed, sk, pk []byte
		suite        string
		createdAt    int64
	)
	err := s.db.QueryRowContext(ctx,
		`SELECT oprf_seed, server_ake_sk, server_ake_pk, suite, created_at
		 FROM opaque_server_state WHERE id = 1`).
		Scan(&seed, &sk, &pk, &suite, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return OpaqueServerState{}, ErrOpaqueStateMissing
	}
	if err != nil {
		return OpaqueServerState{}, fmt.Errorf("query opaque_server_state: %w", err)
	}
	return OpaqueServerState{
		OPRFSeed:        seed,
		AKEServerSecret: sk,
		AKEServerPublic: pk,
		Suite:           suite,
		CreatedAt:       time.Unix(createdAt, 0).UTC(),
	}, nil
}

// StoreOpaqueServerState upserts the singleton opaque_server_state row.
// Subsequent calls overwrite the previous values — there is no migration
// path for OPRF/AKE rotation in v1 (see feedback_no_backward_compat).
func (s *SQLiteStore) StoreOpaqueServerState(ctx context.Context, st OpaqueServerState) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO opaque_server_state(id, oprf_seed, server_ake_sk, server_ake_pk, suite, created_at)
		 VALUES (1, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     oprf_seed     = excluded.oprf_seed,
		     server_ake_sk = excluded.server_ake_sk,
		     server_ake_pk = excluded.server_ake_pk,
		     suite         = excluded.suite,
		     created_at    = excluded.created_at`,
		st.OPRFSeed, st.AKEServerSecret, st.AKEServerPublic, st.Suite, st.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("upsert opaque_server_state: %w", err)
	}
	return nil
}

// GetOpaqueRecord loads the per-user OPAQUE envelope stored at registration
// time. Returns ErrOpaqueRecordMissing when no row exists. Callers MUST
// keep error reporting opaque to clients (login flow returns a generic
// credentials-invalid response either way).
func (s *SQLiteStore) GetOpaqueRecord(ctx context.Context, userID string) ([]byte, error) {
	var rec []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT record FROM user_opaque_records WHERE user_id = ?`, userID).Scan(&rec)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrOpaqueRecordMissing
	}
	if err != nil {
		return nil, fmt.Errorf("query opaque record: %w", err)
	}
	return rec, nil
}

// StoreOpaqueRecord upserts the OPAQUE envelope for userID. Called from
// RegisterFinalize on first registration, or on a password-change flow
// that rotates the envelope (re-registration semantics in v1).
func (s *SQLiteStore) StoreOpaqueRecord(ctx context.Context, userID string, record []byte) error {
	now := time.Now().Unix()
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_opaque_records(user_id, record, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		     record     = excluded.record,
		     created_at = excluded.created_at`,
		userID, record, now)
	if err != nil {
		return fmt.Errorf("upsert opaque record: %w", err)
	}
	return nil
}

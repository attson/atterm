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
	// ErrAccountKeyWrapMissing is returned by GetAccountKeyWrap when no
	// wrap blob exists for the (user, method) pair. The relay treats the
	// blob as opaque ciphertext — see spec §4.5.
	ErrAccountKeyWrapMissing = errors.New("userstore: account key wrap not found")
)

// AccountKeyWrap is the per-user, per-method wrapped account key blob
// stored opaquely by the relay. Wrapped/Nonce/Salt are AEAD ciphertext
// + nonce + KDF salt (bytes are uninterpreted server-side); KDFParams
// is the client-chosen KDF parameter JSON the client needs to derive
// the wrap key on the next unlock.
type AccountKeyWrap struct {
	UserID    string
	Method    string
	Wrapped   []byte
	Nonce     []byte
	Salt      []byte
	KDFParams string
	CreatedAt time.Time
}

// GetAccountKeyWrap loads the wrap blob for (userID, method). Returns
// ErrAccountKeyWrapMissing when the row does not exist; callers surface
// this as a 404 to the client.
func (s *SQLiteStore) GetAccountKeyWrap(ctx context.Context, userID, method string) (AccountKeyWrap, error) {
	var (
		w         AccountKeyWrap
		createdAt int64
	)
	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`SELECT user_id, method, wrapped, nonce, salt, kdf_params, created_at
		 FROM user_account_key_wraps WHERE user_id = ? AND method = ?`),
		userID, method).Scan(&w.UserID, &w.Method, &w.Wrapped, &w.Nonce, &w.Salt, &w.KDFParams, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountKeyWrap{}, ErrAccountKeyWrapMissing
	}
	if err != nil {
		return AccountKeyWrap{}, fmt.Errorf("query account key wrap: %w", err)
	}
	w.CreatedAt = time.Unix(createdAt, 0).UTC()
	return w, nil
}

// StoreAccountKeyWrap upserts the wrap blob for (userID, method). The
// relay does not validate KDFParams or blob contents — see spec §4.5.
func (s *SQLiteStore) StoreAccountKeyWrap(ctx context.Context, w AccountKeyWrap) error {
	if w.CreatedAt.IsZero() {
		w.CreatedAt = time.Now()
	}
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO user_account_key_wraps(user_id, method, wrapped, nonce, salt, kdf_params, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(user_id, method) DO UPDATE SET
		     wrapped    = excluded.wrapped,
		     nonce      = excluded.nonce,
		     salt       = excluded.salt,
		     kdf_params = excluded.kdf_params,
		     created_at = excluded.created_at`),
		w.UserID, w.Method, w.Wrapped, w.Nonce, w.Salt, w.KDFParams, w.CreatedAt.Unix())
	if err != nil {
		return fmt.Errorf("upsert account key wrap: %w", err)
	}
	return nil
}

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
		s.dia.Rebind(`SELECT oprf_seed, server_ake_sk, server_ake_pk, suite, created_at
		 FROM opaque_server_state WHERE id = 1`)).
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
		s.dia.Rebind(`INSERT INTO opaque_server_state(id, oprf_seed, server_ake_sk, server_ake_pk, suite, created_at)
		 VALUES (1, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     oprf_seed     = excluded.oprf_seed,
		     server_ake_sk = excluded.server_ake_sk,
		     server_ake_pk = excluded.server_ake_pk,
		     suite         = excluded.suite,
		     created_at    = excluded.created_at`),
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
		s.dia.Rebind(`SELECT record FROM user_opaque_records WHERE user_id = ?`), userID).Scan(&rec)
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
		s.dia.Rebind(`INSERT INTO user_opaque_records(user_id, record, created_at)
		 VALUES (?, ?, ?)
		 ON CONFLICT(user_id) DO UPDATE SET
		     record     = excluded.record,
		     created_at = excluded.created_at`),
		userID, record, now)
	if err != nil {
		return fmt.Errorf("upsert opaque record: %w", err)
	}
	return nil
}

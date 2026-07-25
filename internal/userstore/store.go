// Package userstore is the relay's account / token / session database layer.
// It is the only package that imports a SQLite driver or writes SQL. Other
// packages depend on the Store interface, which lets tests substitute an
// in-memory implementation without touching SQLite directly.
package userstore

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// DBStore is the production Store backed by database/sql. It serves both
// SQLite (single file) and Postgres, distinguished by its dialect.
type DBStore struct {
	db  *sql.DB
	dia dialect
	// cipher is the AEAD cipher for field-level secret encryption (Feishu
	// app_secret etc.). It is settable after Open so the relay can enable
	// Feishu at runtime without a restart; a nil pointer means Feishu CRUD
	// errors. Use an atomic pointer because SetSecretCipher can race with
	// in-flight Feishu reads/writes — those load it once into a local.
	cipher atomic.Pointer[SecretCipher]
}

// SQLiteStore is the historical name for DBStore, kept as an alias so the
// many callers that reference *userstore.SQLiteStore keep compiling.
// TODO(cleanup): migrate callers to DBStore / the Store interface.
type SQLiteStore = DBStore

// OpenOption is a functional option for Open.
type OpenOption func(*DBStore)

// WithSecretCipher sets the AEAD cipher used for field-level secret encryption
// (e.g. Feishu app_secret). Must be set with a non-nil cipher before any
// Feishu CRUD methods are used.
func WithSecretCipher(c *SecretCipher) OpenOption {
	return func(s *SQLiteStore) { s.cipher.Store(c) }
}

// SetSecretCipher swaps the field-encryption cipher at runtime. Pass a
// non-nil cipher to enable Feishu CRUD, or nil to disable it. Safe to call
// concurrently with Feishu reads/writes (they snapshot the pointer once).
func (s *SQLiteStore) SetSecretCipher(c *SecretCipher) { s.cipher.Store(c) }

// HasSecretCipher reports whether a field-encryption cipher is configured.
func (s *SQLiteStore) HasSecretCipher() bool { return s.cipher.Load() != nil }

// Open opens (or creates) the SQLite database at path and runs any pending
// migrations. Pass ":memory:" for tests. WAL mode is enabled on file-backed
// databases; tests against ":memory:" fall back to the default journal.
func Open(ctx context.Context, path string, opts ...OpenOption) (*SQLiteStore, error) {
	dsn := path
	if path != ":memory:" {
		dsn = path + "?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)"
	} else {
		dsn = path + "?_pragma=foreign_keys(on)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	s := &SQLiteStore{db: db, dia: dialectSQLite}
	for _, o := range opts {
		o(s)
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// OpenPostgres opens a Postgres-backed store at dsn (e.g.
// "postgres://user:pass@host:5432/db?sslmode=disable") and runs pending
// postgres migrations. Unlike SQLite, Postgres handles concurrent
// connections, so the pool is not pinned to a single connection.
func OpenPostgres(ctx context.Context, dsn string, opts ...OpenOption) (*DBStore, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open pgx: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxIdleTime(5 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	s := &DBStore{db: db, dia: dialectPostgres}
	for _, o := range opts {
		o(s)
	}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

// OpenFromDSN opens a store from a scheme-tagged DSN:
//   - "postgres://..." or "postgresql://..."  → Postgres
//   - "sqlite:<path>"                          → SQLite at <path>
//   - anything else                            → SQLite, treating the DSN
//     as a bare file path
//
// Used by the migrate subcommand for --from/--to.
func OpenFromDSN(ctx context.Context, dsn string, opts ...OpenOption) (*DBStore, error) {
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return OpenPostgres(ctx, dsn, opts...)
	case strings.HasPrefix(dsn, "sqlite:"):
		path := strings.TrimPrefix(dsn, "sqlite:")
		path = strings.TrimPrefix(path, "//")
		return Open(ctx, path, opts...)
	default:
		return Open(ctx, dsn, opts...)
	}
}

// DB returns the underlying sql.DB. Only used in tests that need direct
// SQL access to verify internal state. Not part of the Store interface.
func (s *SQLiteStore) DB() *sql.DB { return s.db }

// Store is the dependency-inversion seam between internal/relay and the
// concrete SQLite implementation. Tests in internal/relay can substitute
// a memory implementation that satisfies this interface.
type Store interface {
	// Users
	// CreateOpaqueUser inserts a row tagged auth_mode='opaque' with no
	// credential material. Used by the OPAQUE register/finalize flow
	// (and by tests that just need a user row).
	CreateOpaqueUser(ctx context.Context, email string) (*User, error)
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByEmail(ctx context.Context, email string) (*User, error)
	DisableUser(ctx context.Context, id string) error
	ListUsers(ctx context.Context) ([]User, error)
	// SetUserAdmin sets the is_admin flag for userID. Idempotent.
	SetUserAdmin(ctx context.Context, userID string, admin bool) error
	// AdminExists reports whether any enabled admin user exists. Gates the
	// first-run setup flow's email-matched auto-promotion.
	AdminExists(ctx context.Context) (bool, error)
	// DeleteUser hard-deletes userID. sessions and pairing_tokens cascade
	// via the existing FK. invitations.consumed_by is REFERENCES users(id)
	// without cascade (history field), so this method first sets that
	// column to NULL for every invitation consumed by the user, then
	// deletes the users row, in one transaction.
	DeleteUser(ctx context.Context, userID string) error

	// Invitations
	CreateInvitation(ctx context.Context, expiresAt *time.Time, note string) (Secret, *Invitation, error)
	ConsumeInvitation(ctx context.Context, plaintext, userID string) error
	ListInvitations(ctx context.Context) ([]Invitation, error)

	// Pairing tokens (mobile QR code). Consuming a code returns the owning
	// user; the caller is responsible for minting a session token.
	// wrap may be nil; when non-nil, stored verbatim in wrapped_account_key.
	CreatePairingToken(ctx context.Context, userID string, ttl time.Duration, wrap []byte) (Secret, *PairingToken, error)
	ConsumePairingToken(ctx context.Context, plaintext string) (*User, error)

	// Sessions (single bearer token; cookie OR Authorization header).
	CreateSession(ctx context.Context, userID, userAgent, ipPrefix string, ttl time.Duration) (plaintext string, sess *Session, err error)
	LookupSession(ctx context.Context, plaintext string) (*Session, *User, error)
	DeleteSession(ctx context.Context, plaintext string) error
	PurgeExpiredSessions(ctx context.Context) (int64, error)
	// ListSessions returns all non-expired sessions for userID, ordered by
	// created_at DESC. Used by the Settings → Signed-in devices panel.
	ListSessions(ctx context.Context, userID string) ([]Session, error)

	// DeleteSessionByIDHash revokes the session with the given id_hash,
	// ONLY IF it belongs to userID. Returns (false, nil) if no such session
	// exists or it belongs to a different user — never reveal cross-user
	// existence.
	DeleteSessionByIDHash(ctx context.Context, userID, idHash string) (deleted bool, err error)

	// DeleteOtherSessionsForUser deletes every session for userID except
	// the one whose id_hash matches exceptIDHash. Returns the number of
	// rows deleted. Used by Settings → Sign out everywhere except this
	// device.
	DeleteOtherSessionsForUser(ctx context.Context, userID, exceptIDHash string) (int64, error)

	// Session seen/unread inbox
	SetSeen(ctx context.Context, userID string, sessionIDs []string, at int64) error
	SeenAt(ctx context.Context, userID string) (map[string]int64, error)
	PruneSeenSession(ctx context.Context, sessionID string) error

	// User preferences (cross-platform settings sync)
	GetUserPreferences(ctx context.Context, userID string) ([]PreferenceItem, error)
	SetUserPreferences(ctx context.Context, userID string, serverNowMs int64, items []PreferenceItem) ([]PreferenceItem, error)

	// Account-key wrap (E2EE). The relay treats the wrap blob as opaque
	// bytes; only the holder of the user's password can unwrap to recover
	// account_key. See docs/superpowers/specs/2026-06-15-relay-e2ee-design.md
	// §4.5. Method is "password" in v1; future methods (recovery_code,
	// passkey) will share the table via the composite (user_id, method) PK.
	GetAccountKeyWrap(ctx context.Context, userID, method string) (AccountKeyWrap, error)
	StoreAccountKeyWrap(ctx context.Context, w AccountKeyWrap) error

	// Cluster realm identity (singleton); anchors E2EE keys across nodes.
	GetRealmState(ctx context.Context) (RealmState, error)
	EnsureRealmState(ctx context.Context, candidateRealmID string) (RealmState, error)

	// Instance registry + account-level home node (phase 2 node selection).
	UpsertInstanceHeartbeat(ctx context.Context, instanceID, publicURL string, nowUnix int64) error
	ListLiveInstances(ctx context.Context, minHeartbeat int64) ([]RelayInstance, error)
	GetUserHome(ctx context.Context, userID string) (string, bool, error)
	SetUserHome(ctx context.Context, userID, instanceID string) error

	// Relay-wide singleton config (DB-backed replacement for relay.json).
	GetRelayConfig(ctx context.Context) (RelayConfig, error)
	SetRelayConfig(ctx context.Context, cfg RelayConfig) (RelayConfig, error)

	// Web Push: VAPID keypair singleton + per-user subscriptions (DB-backed
	// replacement for web-push.json).
	GetVAPIDKeys(ctx context.Context) (VAPIDKeys, bool, error)
	SetVAPIDKeys(ctx context.Context, k VAPIDKeys) error
	AddWebPushSubscription(ctx context.Context, userID string, sub WebPushSubscription) error
	RemoveWebPushSubscription(ctx context.Context, userID, endpoint string) error
	ListWebPushSubscriptions(ctx context.Context, userID string) ([]WebPushSubscription, error)

	Close() error
}

func nowUnix() int64 { return time.Now().Unix() }

func (s *SQLiteStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at BIGINT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	dir := "migrations/" + s.dia.Name()
	entries, err := migrationsFS.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read embedded migrations %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		seen, err := s.migrationSeen(ctx, name)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if seen {
			if err := s.recordMigration(ctx, name); err != nil {
				return fmt.Errorf("record renamed migration %s: %w", name, err)
			}
			continue
		}
		body, err := migrationsFS.ReadFile(dir + "/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx,
			s.dia.Rebind(`INSERT INTO schema_migrations(name, applied_at) VALUES(?, ?)`),
			name, time.Now().Unix()); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}

func (s *SQLiteStore) migrationSeen(ctx context.Context, name string) (bool, error) {
	names := equivalentMigrationNames(name)
	placeholders := make([]string, len(names))
	args := make([]any, len(names))
	for i, n := range names {
		placeholders[i] = "?"
		args[i] = n
	}
	var seen int
	err := s.db.QueryRowContext(ctx,
		s.dia.Rebind(`SELECT count(*) FROM schema_migrations WHERE name IN (`+strings.Join(placeholders, ",")+`)`),
		args...,
	).Scan(&seen)
	return seen > 0, err
}

func (s *SQLiteStore) recordMigration(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx,
		s.dia.Rebind(`INSERT INTO schema_migrations(name, applied_at) VALUES(?, ?) ON CONFLICT(name) DO NOTHING`),
		name, time.Now().Unix())
	return err
}

func equivalentMigrationNames(name string) []string {
	switch name {
	case "0007_feishu_remote_terminal.sql":
		return []string{name, "0010_feishu_remote_terminal.sql"}
	case "0010_feishu_remote_terminal.sql":
		// v0.3 moved this migration after DB-backed relay config migrations.
		// Some desktop SQLite stores already recorded the earlier name while
		// carrying the same columns, so the later name must not rerun ALTER.
		return []string{name, "0007_feishu_remote_terminal.sql"}
	default:
		return []string{name}
	}
}

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
	"time"

	_ "modernc.org/sqlite"
)

// UserWebSession is the public view of a row in web_sessions for the
// owning user. id_hash is opaque (already hashed); plaintext cookies
// are not stored or exposed.
type UserWebSession struct {
	IDHash    string
	UserAgent string
	IPPrefix  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// SQLiteStore is the production Store backed by a single SQLite file.
type SQLiteStore struct {
	db *sql.DB
}

// Open opens (or creates) the SQLite database at path and runs any pending
// migrations. Pass ":memory:" for tests. WAL mode is enabled on file-backed
// databases; tests against ":memory:" fall back to the default journal.
func Open(ctx context.Context, path string) (*SQLiteStore, error) {
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
	s := &SQLiteStore{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) Close() error { return s.db.Close() }

// DB returns the underlying sql.DB. Only used in tests that need direct
// SQL access to verify internal state (e.g. csrf_secret rotation). Not part
// of the Store interface.
func (s *SQLiteStore) DB() *sql.DB { return s.db }

// Store is the dependency-inversion seam between internal/relay and the
// concrete SQLite implementation. Tests in internal/relay can substitute
// a memory implementation that satisfies this interface.
type Store interface {
	// Users
	CreateUser(ctx context.Context, email, password string) (*User, error)
	VerifyPassword(ctx context.Context, email, password string) (*User, error)
	GetUser(ctx context.Context, id string) (*User, error)
	DisableUser(ctx context.Context, id string) error
	ListUsers(ctx context.Context) ([]User, error)
	ResetUserPassword(ctx context.Context, userID string) (Secret, error)
	// SetUserAdmin sets the is_admin flag for userID. Idempotent.
	SetUserAdmin(ctx context.Context, userID string, admin bool) error
	// EnsureAdminUser is idempotent. If a user with this email exists, it
	// is marked is_admin=1 and returns (created=false, nil); password is
	// ignored. Otherwise a new user is created with the given plaintext
	// password and is_admin=1, returning (created=true, nil). Empty
	// plaintext for the create path returns ErrEmptyBootstrapPassword;
	// strength enforcement is the caller's job.
	EnsureAdminUser(ctx context.Context, email, plaintext string) (created bool, err error)

	// Invitations
	CreateInvitation(ctx context.Context, expiresAt *time.Time, note string) (Secret, *Invitation, error)
	ConsumeInvitation(ctx context.Context, plaintext, userID string) error
	ListInvitations(ctx context.Context) ([]Invitation, error)

	// API tokens
	CreateAPIToken(ctx context.Context, userID, name string) (Secret, *APIToken, error)
	LookupAPIToken(ctx context.Context, plaintext string) (tokenID, userID string, err error)
	RevokeAPIToken(ctx context.Context, tokenID, userID string) error
	ListAPITokens(ctx context.Context, userID string) ([]APIToken, error)
	TouchAPIToken(ctx context.Context, tokenID string) error

	// Web sessions (cookie)
	CreateWebSession(ctx context.Context, userID, userAgent, ipPrefix string) (Secret, error)
	LookupWebSession(ctx context.Context, plaintext string) (userID string, csrfSecret []byte, err error)
	DeleteWebSession(ctx context.Context, plaintext string) error
	PurgeExpiredWebSessions(ctx context.Context) (int64, error)
	// ListUserWebSessions returns all non-expired sessions for userID,
	// ordered by created_at DESC. Used by the Settings → Signed-in devices
	// panel.
	ListUserWebSessions(ctx context.Context, userID string) ([]UserWebSession, error)

	// DeleteUserWebSessionByIDHash revokes the session with the given
	// id_hash, ONLY IF it belongs to userID. Returns (false, nil) if no
	// such session exists or it belongs to a different user — never
	// reveal cross-user existence.
	DeleteUserWebSessionByIDHash(ctx context.Context, userID, idHash string) (deleted bool, err error)

	// ChangePassword verifies currentPlaintext against the stored hash for
	// userID, then updates to a new hash and rotates csrf_secret. All existing
	// web sessions for the user are deleted (caller issues a fresh session).
	// Returns ErrUserNotFound or ErrPasswordIncorrect on validation failure.
	ChangePassword(ctx context.Context, userID, currentPlaintext, newPlaintext string) error

	Close() error
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at INTEGER NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var seen int
		if err := s.db.QueryRowContext(ctx,
			`SELECT count(*) FROM schema_migrations WHERE name=?`, name,
		).Scan(&seen); err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if seen > 0 {
			continue
		}
		body, err := migrationsFS.ReadFile("migrations/" + name)
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
			`INSERT INTO schema_migrations(name, applied_at) VALUES(?, strftime('%s','now'))`,
			name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
	}
	return nil
}

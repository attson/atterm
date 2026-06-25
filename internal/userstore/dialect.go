package userstore

import (
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
)

// dialect adapts the small set of SQL differences between SQLite and
// Postgres. DML (the actual queries in users.go, sessions.go, ...) is
// shared; only placeholder syntax and unique-violation error detection
// differ at runtime. DDL differences live in the per-dialect migration
// directories, not here.
type dialect interface {
	// Name is the migration subdirectory name ("sqlite" / "postgres").
	Name() string
	// Rebind converts '?' positional placeholders to the dialect's form.
	// SQLite keeps '?'; Postgres rewrites to $1, $2, ... in order.
	Rebind(query string) string
	// IsUniqueViolation reports whether err is a UNIQUE/primary-key
	// constraint violation. Callers already know which constraint they
	// could have hit (each insert touches a single unique key), so this
	// does not distinguish columns.
	IsUniqueViolation(err error) bool
}

type sqliteDialect struct{}

func (sqliteDialect) Name() string           { return "sqlite" }
func (sqliteDialect) Rebind(q string) string { return q }
func (sqliteDialect) IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite surfaces "UNIQUE constraint failed: <table>.<col>"
	// and "PRIMARY KEY constraint failed" in the error string.
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: PRIMARY KEY") ||
		strings.Contains(msg, "PRIMARY KEY constraint failed")
}

type postgresDialect struct{}

func (postgresDialect) Name() string { return "postgres" }

func (postgresDialect) Rebind(q string) string {
	var b strings.Builder
	b.Grow(len(q) + 8)
	n := 0
	for i := 0; i < len(q); i++ {
		if q[i] == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteByte(q[i])
	}
	return b.String()
}

func (postgresDialect) IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" // unique_violation
	}
	return false
}

var (
	dialectSQLite   dialect = sqliteDialect{}
	dialectPostgres dialect = postgresDialect{}
)

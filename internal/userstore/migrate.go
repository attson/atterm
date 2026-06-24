package userstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// copyTablesInOrder lists every data table in foreign-key-safe order
// (parents before children) for a raw row copy. schema_migrations is
// intentionally excluded — the target builds its own when opened.
var copyTablesInOrder = []string{
	"users",
	"sessions",
	"pairing_tokens",
	"invitations",
	"session_seen",
	"user_preferences",
	"user_opaque_records",
	"user_account_key_wraps",
	"feishu_bindings",
	"feishu_pending_binds",
	"web_push_subscriptions",
	"opaque_server_state",
	"claim_tokens",
	"relay_config",
	"web_push_keys",
}

// isEmptyForCopy reports whether every data table is empty. Copy refuses a
// non-empty target to avoid clobbering or PK conflicts.
func (s *DBStore) isEmptyForCopy(ctx context.Context) (bool, error) {
	for _, table := range copyTablesInOrder {
		var n int
		// Table names are fixed constants from copyTablesInOrder (not user
		// input), so the interpolation is safe.
		if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			return false, fmt.Errorf("count %s: %w", table, err)
		}
		if n > 0 {
			return false, nil
		}
	}
	return true, nil
}

// Copy faithfully copies all data from src to dst (raw rows, preserving
// hashes, encrypted blobs and timestamps). The target MUST be empty. Returns
// table -> rows-copied. Both stores must already be open (schema migrated).
func Copy(ctx context.Context, src, dst *DBStore) (map[string]int64, error) {
	empty, err := dst.isEmptyForCopy(ctx)
	if err != nil {
		return nil, err
	}
	if !empty {
		return nil, errors.New("migrate: target is not empty (refusing to overwrite); point --to at a freshly-created database")
	}

	tx, err := dst.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	counts := make(map[string]int64, len(copyTablesInOrder))
	for _, table := range copyTablesInOrder {
		n, err := copyTable(ctx, src, dst, tx, table)
		if err != nil {
			return nil, fmt.Errorf("copy %s: %w", table, err)
		}
		counts[table] = n
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return counts, nil
}

// copyTable streams every row of one table from src into dst's tx using a
// generic SELECT */INSERT. Values scan into []interface{} as []byte/int64/
// string/nil, which both modernc.org/sqlite and jackc/pgx/v5/stdlib accept
// when re-inserted into the equivalent BLOB/BYTEA, INTEGER/BIGINT, TEXT, or
// NULL columns.
func copyTable(ctx context.Context, src, dst *DBStore, tx *sql.Tx, table string) (int64, error) {
	rows, err := src.db.QueryContext(ctx, "SELECT * FROM "+table)
	if err != nil {
		return 0, fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return 0, fmt.Errorf("columns: %w", err)
	}
	placeholders := make([]string, len(cols))
	for i := range placeholders {
		placeholders[i] = "?"
	}
	insertSQL := dst.dia.Rebind(fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		table, strings.Join(cols, ", "), strings.Join(placeholders, ", ")))

	var n int64
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return n, fmt.Errorf("scan: %w", err)
		}
		if _, err := tx.ExecContext(ctx, insertSQL, vals...); err != nil {
			return n, fmt.Errorf("insert: %w", err)
		}
		n++
	}
	return n, rows.Err()
}

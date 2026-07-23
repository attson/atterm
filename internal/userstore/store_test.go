package userstore

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenInMemory_RunsMigrations(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	var count int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("schema_migrations table missing: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected at least one migration applied, got 0")
	}
}

func TestOpenInMemory_MigrationIdempotent(t *testing.T) {
	ctx := context.Background()
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	var nBefore int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations`).Scan(&nBefore); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(ctx); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	var nAfter int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations`).Scan(&nAfter); err != nil {
		t.Fatal(err)
	}
	if nBefore != nAfter {
		t.Fatalf("expected same migration row count after re-run: before=%d, after=%d", nBefore, nAfter)
	}
}

func TestOpenSkipsRenamedFeishuRemoteTerminalMigration(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "users.db")

	s, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("initial Open: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close initial store: %v", err)
	}

	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(on)")
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		DELETE FROM schema_migrations WHERE name = '0010_feishu_remote_terminal.sql';
		INSERT INTO schema_migrations(name, applied_at) VALUES('0007_feishu_remote_terminal.sql', strftime('%s','now'));
	`); err != nil {
		db.Close()
		t.Fatalf("prepare renamed migration state: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	s, err = Open(ctx, path)
	if err != nil {
		t.Fatalf("Open with renamed feishu remote terminal migration: %v", err)
	}
	defer s.Close()

	var seen int
	if err := s.db.QueryRowContext(ctx,
		`SELECT count(*) FROM schema_migrations WHERE name = '0010_feishu_remote_terminal.sql'`,
	).Scan(&seen); err != nil {
		t.Fatalf("check backfilled migration record: %v", err)
	}
	if seen != 1 {
		t.Fatalf("expected current migration name to be backfilled, got %d rows", seen)
	}
}

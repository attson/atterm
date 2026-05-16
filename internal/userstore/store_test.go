package userstore

import (
	"context"
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

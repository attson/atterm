package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestMigrateCmdSQLiteFileToFile(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "src.db")
	dstPath := filepath.Join(dir, "dst.db")
	ctx := context.Background()

	// Seed the source file DB.
	src, err := userstore.Open(ctx, srcPath)
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	u, err := src.CreateOpaqueUser(ctx, "cli@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, _, err := src.CreateSession(ctx, u.ID, "ua", "127.0.0", time.Hour); err != nil {
		t.Fatalf("session: %v", err)
	}
	src.Close()

	// Run the subcommand.
	code := migrateCmd([]string{"--from", "sqlite:" + srcPath, "--to", "sqlite:" + dstPath})
	if code != 0 {
		t.Fatalf("migrateCmd exit = %d, want 0", code)
	}

	// Verify the target file received the data.
	dst, err := userstore.Open(ctx, dstPath)
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	t.Cleanup(func() { dst.Close() })
	if got, err := dst.GetUser(ctx, u.ID); err != nil || got.Email != "cli@example.com" {
		t.Fatalf("target user: %v %+v", err, got)
	}
	if sess, err := dst.ListSessions(ctx, u.ID); err != nil || len(sess) != 1 {
		t.Fatalf("target sessions: %v %+v", err, sess)
	}
}

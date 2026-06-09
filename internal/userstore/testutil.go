package userstore

import (
	"context"
	"testing"
)

// NewInMemory opens an in-memory sqlite store and runs migrations. Intended
// for tests in other packages (e.g. internal/relay) that need a real store.
func NewInMemory(t *testing.T) *SQLiteStore {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("userstore.NewInMemory: open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

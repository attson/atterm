package userstore

import (
	"context"
	"testing"
)

// testCipher is shared by all tests so that feishu_bindings round-trip
// across goroutines / subtests without each test wiring its own.
var testCipher = mustTestCipher()

func mustTestCipher() *SecretCipher {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i) // deterministic, NOT for production
	}
	c, err := NewSecretCipher(key)
	if err != nil {
		panic(err)
	}
	return c
}

// NewInMemory opens an in-memory sqlite store and runs migrations. Intended
// for tests in other packages (e.g. internal/relay) that need a real store.
func NewInMemory(t *testing.T) *DBStore {
	t.Helper()
	s, err := Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatalf("userstore.NewInMemory: open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

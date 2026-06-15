package userstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mustMakeUser inserts a user row directly via the store's underlying *sql.DB,
// bypassing CreateUser because the latter still references the now-removed
// password_hash column (cleanup is scheduled for T12). Returns the created User.
func mustMakeUser(t *testing.T, store *SQLiteStore, email string) *User {
	t.Helper()
	ctx := context.Background()
	id := defaultIDs.New()
	now := time.Now().Unix()
	email = strings.ToLower(strings.TrimSpace(email))
	if _, err := store.DB().ExecContext(ctx,
		`INSERT INTO users(id, email, is_admin, auth_mode, created_at)
		 VALUES(?, ?, 0, 'opaque', ?)`,
		id, email, now); err != nil {
		t.Fatalf("mustMakeUser insert: %v", err)
	}
	return &User{
		ID:        id,
		Email:     email,
		AuthMode:  "opaque",
		CreatedAt: time.Unix(now, 0),
	}
}

func TestOpaqueServerStateRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	_, err := store.GetOpaqueServerState(ctx)
	if !errors.Is(err, ErrOpaqueStateMissing) {
		t.Fatalf("expected ErrOpaqueStateMissing, got %v", err)
	}

	want := OpaqueServerState{
		OPRFSeed:        []byte("seed-bytes"),
		AKEServerSecret: []byte("sk"),
		AKEServerPublic: []byte("pk"),
		Suite:           "default-v1",
		CreatedAt:       time.Now().UTC().Truncate(time.Second),
	}
	if err := store.StoreOpaqueServerState(ctx, want); err != nil {
		t.Fatalf("store: %v", err)
	}
	got, err := store.GetOpaqueServerState(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.OPRFSeed) != string(want.OPRFSeed) ||
		string(got.AKEServerSecret) != string(want.AKEServerSecret) ||
		string(got.AKEServerPublic) != string(want.AKEServerPublic) ||
		got.Suite != want.Suite {
		t.Fatalf("round-trip mismatch: %+v vs %+v", got, want)
	}
}

func TestOpaqueRecordRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	user := mustMakeUser(t, store, "alice@example.com")

	if _, err := store.GetOpaqueRecord(ctx, user.ID); !errors.Is(err, ErrOpaqueRecordMissing) {
		t.Fatalf("expected missing, got %v", err)
	}
	rec := []byte("opaque-envelope-bytes")
	if err := store.StoreOpaqueRecord(ctx, user.ID, rec); err != nil {
		t.Fatalf("store record: %v", err)
	}
	got, err := store.GetOpaqueRecord(ctx, user.ID)
	if err != nil {
		t.Fatalf("get record: %v", err)
	}
	if string(got) != string(rec) {
		t.Fatalf("record mismatch")
	}
}

package userstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// mustMakeUser inserts a user row directly via the store's underlying *sql.DB.
// We deliberately don't go through CreateOpaqueUser here so the OPAQUE
// record / wrap-blob tests below stay isolated from the user-creation flow
// (which has its own coverage in users_test.go) — a bug in CreateOpaqueUser
// shouldn't cascade into failures of the storage primitives being exercised
// here. Returns the created User.
func mustMakeUser(t *testing.T, store *DBStore, email string) *User {
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

func TestAccountKeyWrapRoundTrip(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	user := mustMakeUser(t, store, "bob@example.com")

	if _, err := store.GetAccountKeyWrap(ctx, user.ID, "password"); !errors.Is(err, ErrAccountKeyWrapMissing) {
		t.Fatalf("expected missing, got %v", err)
	}
	wrap := AccountKeyWrap{
		UserID:    user.ID,
		Method:    "password",
		Wrapped:   []byte("aead-ciphertext"),
		Nonce:     []byte("12345678901234567890abcd"),
		Salt:      []byte("salt-16-bytes-here"),
		KDFParams: `{"alg":"argon2id","m":67108864,"t":3,"p":1}`,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}
	if err := store.StoreAccountKeyWrap(ctx, wrap); err != nil {
		t.Fatalf("store wrap: %v", err)
	}
	got, err := store.GetAccountKeyWrap(ctx, user.ID, "password")
	if err != nil {
		t.Fatalf("get wrap: %v", err)
	}
	if string(got.Wrapped) != string(wrap.Wrapped) ||
		string(got.Nonce) != string(wrap.Nonce) ||
		string(got.Salt) != string(wrap.Salt) ||
		got.KDFParams != wrap.KDFParams {
		t.Fatalf("wrap round-trip mismatch")
	}
}

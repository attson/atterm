package userstore

import (
	"context"
	"os"
	"testing"
	"time"
)

// openSQLite returns a fresh in-memory SQLite store with a test cipher so
// Feishu CRUD works.
func openSQLite(t *testing.T) *DBStore {
	t.Helper()
	c, err := NewSecretCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	st, err := Open(context.Background(), ":memory:", WithSecretCipher(c))
	if err != nil {
		t.Fatalf("Open sqlite: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// openPostgres connects to ATTERM_TEST_PG_DSN. Each run drops and recreates
// the public schema so migrations apply to a clean database.
func openPostgres(t *testing.T) *DBStore {
	t.Helper()
	dsn := os.Getenv("ATTERM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ATTERM_TEST_PG_DSN not set; skipping Postgres contract test")
	}
	c, err := NewSecretCipher(make([]byte, 32))
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	// Clean slate: connect raw, reset schema, then open via OpenPostgres.
	reset, err := OpenPostgres(context.Background(), dsn, WithSecretCipher(c))
	if err != nil {
		t.Fatalf("OpenPostgres(reset): %v", err)
	}
	if _, err := reset.db.ExecContext(context.Background(),
		`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		reset.Close()
		t.Fatalf("reset schema: %v", err)
	}
	reset.Close()
	st, err := OpenPostgres(context.Background(), dsn, WithSecretCipher(c))
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestStoreContract(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) { runStoreContract(t, openSQLite) })
	t.Run("postgres", func(t *testing.T) { runStoreContract(t, openPostgres) })
}

func runStoreContract(t *testing.T, open func(t *testing.T) *DBStore) {
	ctx := context.Background()

	t.Run("user CRUD + unique email", func(t *testing.T) {
		st := open(t)
		u, err := st.CreateOpaqueUser(ctx, "Alice@Example.com")
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if u.Email != "alice@example.com" {
			t.Fatalf("email not lowercased: %q", u.Email)
		}
		got, err := st.GetUser(ctx, u.ID)
		if err != nil || got.Email != "alice@example.com" {
			t.Fatalf("get: %v %+v", err, got)
		}
		if _, err := st.CreateOpaqueUser(ctx, "alice@example.com"); err != ErrEmailTaken {
			t.Fatalf("want ErrEmailTaken, got %v", err)
		}
	})

	t.Run("session lifecycle", func(t *testing.T) {
		st := open(t)
		u, err := st.CreateOpaqueUser(ctx, "bob@example.com")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		tok, sess, err := st.CreateSession(ctx, u.ID, "ua", "127.0.0", time.Hour)
		if err != nil {
			t.Fatalf("create session: %v", err)
		}
		gotSess, gotUser, err := st.LookupSession(ctx, tok)
		if err != nil || gotUser.ID != u.ID || gotSess.IDHash != sess.IDHash {
			t.Fatalf("lookup: %v u=%+v s=%+v", err, gotUser, gotSess)
		}
		if err := st.DeleteSession(ctx, tok); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, _, err := st.LookupSession(ctx, tok); err == nil {
			t.Fatalf("expected lookup to fail after delete")
		}
	})

	t.Run("preferences LWW upsert", func(t *testing.T) {
		st := open(t)
		u, err := st.CreateOpaqueUser(ctx, "carol@example.com")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		items := []PreferenceItem{{Key: "locale_preference", ValueJSON: []byte(`"en"`), UpdatedAt: 100}}
		if _, err := st.SetUserPreferences(ctx, u.ID, 0, items); err != nil {
			t.Fatalf("set: %v", err)
		}
		// Stale write (older UpdatedAt) must not overwrite.
		stale := []PreferenceItem{{Key: "locale_preference", ValueJSON: []byte(`"zh"`), UpdatedAt: 50}}
		out, err := st.SetUserPreferences(ctx, u.ID, 0, stale)
		if err != nil {
			t.Fatalf("set stale: %v", err)
		}
		if len(out) != 1 || string(out[0].ValueJSON) != `"en"` || out[0].UpdatedAt != 100 {
			t.Fatalf("stale write won: %+v (UpdatedAt=%d)", out, out[0].UpdatedAt)
		}

		// Regression: stale write must be rejected even when serverNowMs is a
		// large value (guards the naive guard bug where bumped writeTs would
		// wrongly pass the WHERE clause instead of it.UpdatedAt).
		staleHighServerNow := []PreferenceItem{{Key: "locale_preference", ValueJSON: []byte(`"zh"`), UpdatedAt: 50}}
		out2, err := st.SetUserPreferences(ctx, u.ID, 9_000_000_000_000, staleHighServerNow)
		if err != nil {
			t.Fatalf("set stale+highServerNow: %v", err)
		}
		if len(out2) != 1 || string(out2[0].ValueJSON) != `"en"` || out2[0].UpdatedAt != 100 {
			t.Fatalf("stale write (high serverNowMs) won: %+v (UpdatedAt=%d)", out2, out2[0].UpdatedAt)
		}
	})

	t.Run("account key wrap roundtrip", func(t *testing.T) {
		st := open(t)
		u, err := st.CreateOpaqueUser(ctx, "dave@example.com")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		w := AccountKeyWrap{
			UserID: u.ID, Method: "password",
			Wrapped: []byte("w"), Nonce: []byte("n"), Salt: []byte("s"),
			KDFParams: `{"t":1}`,
		}
		if err := st.StoreAccountKeyWrap(ctx, w); err != nil {
			t.Fatalf("store wrap: %v", err)
		}
		got, err := st.GetAccountKeyWrap(ctx, u.ID, "password")
		if err != nil || string(got.Wrapped) != "w" {
			t.Fatalf("get wrap: %v %+v", err, got)
		}
	})
}

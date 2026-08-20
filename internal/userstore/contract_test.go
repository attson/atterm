package userstore

import (
	"context"
	"fmt"
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

	t.Run("relay_config get-default then set bumps version", func(t *testing.T) {
		st := open(t)
		got, err := st.GetRelayConfig(ctx)
		if err != nil {
			t.Fatalf("get default: %v", err)
		}
		if got.Version != 0 {
			t.Fatalf("unconfigured version = %d, want 0", got.Version)
		}
		in := RelayConfig{
			RateLimitPerMinute: 600, MaxConnectionsPerKey: 64,
			AllowedOrigins: []string{"https://a.example", "https://b.example"},
			VAPIDSubject:   "mailto:x@y.z", Debug: true,
			FeishuEnabled: true, FeishuEncryptKey: "k", FeishuBaseURL: "https://f",
		}
		w1, err := st.SetRelayConfig(ctx, in)
		if err != nil {
			t.Fatalf("set: %v", err)
		}
		if w1.Version != 1 {
			t.Fatalf("first set version = %d, want 1", w1.Version)
		}
		got2, _ := st.GetRelayConfig(ctx)
		if got2.RateLimitPerMinute != 600 || len(got2.AllowedOrigins) != 2 ||
			got2.AllowedOrigins[0] != "https://a.example" || !got2.Debug ||
			!got2.FeishuEnabled || got2.FeishuEncryptKey != "k" || got2.Version != 1 {
			t.Fatalf("readback mismatch: %+v", got2)
		}
		w2, _ := st.SetRelayConfig(ctx, in)
		if w2.Version != 2 {
			t.Fatalf("second set version = %d, want 2", w2.Version)
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

	t.Run("webpush subscription cap", func(t *testing.T) {
		st := open(t)
		u, err := st.CreateOpaqueUser(ctx, "webcap@example.com")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}

		// Add maxWebPushSubsPerUser distinct endpoints.
		for i := 0; i < maxWebPushSubsPerUser; i++ {
			sub := WebPushSubscription{
				Endpoint:  fmt.Sprintf("https://push/ep%d", i),
				P256dh:    fmt.Sprintf("p%d", i),
				Auth:      fmt.Sprintf("a%d", i),
				CreatedAt: int64(i + 1),
			}
			if err := st.AddWebPushSubscription(ctx, u.ID, sub); err != nil {
				t.Fatalf("add sub %d: %v", i, err)
			}
		}

		list, err := st.ListWebPushSubscriptions(ctx, u.ID)
		if err != nil {
			t.Fatalf("list after filling cap: %v", err)
		}
		if len(list) != maxWebPushSubsPerUser {
			t.Fatalf("expected %d subs after fill, got %d", maxWebPushSubsPerUser, len(list))
		}

		// Add a 17th NEW endpoint — must be silently dropped.
		overflow := WebPushSubscription{
			Endpoint:  "https://push/overflow",
			P256dh:    "pX",
			Auth:      "aX",
			CreatedAt: 999,
		}
		if err := st.AddWebPushSubscription(ctx, u.ID, overflow); err != nil {
			t.Fatalf("add overflow sub: %v", err)
		}
		list2, err := st.ListWebPushSubscriptions(ctx, u.ID)
		if err != nil {
			t.Fatalf("list after overflow: %v", err)
		}
		if len(list2) != maxWebPushSubsPerUser {
			t.Fatalf("expected %d subs after overflow drop, got %d", maxWebPushSubsPerUser, len(list2))
		}

		// Update one of the existing 16 endpoints — must succeed even at cap.
		updated := WebPushSubscription{
			Endpoint:  "https://push/ep0",
			P256dh:    "p0-updated",
			Auth:      "a0-updated",
			CreatedAt: 1,
		}
		if err := st.AddWebPushSubscription(ctx, u.ID, updated); err != nil {
			t.Fatalf("update existing sub at cap: %v", err)
		}
		list3, err := st.ListWebPushSubscriptions(ctx, u.ID)
		if err != nil {
			t.Fatalf("list after update: %v", err)
		}
		if len(list3) != maxWebPushSubsPerUser {
			t.Fatalf("expected %d subs after update, got %d", maxWebPushSubsPerUser, len(list3))
		}
		var updatedSub *WebPushSubscription
		for i := range list3 {
			if list3[i].Endpoint == "https://push/ep0" {
				updatedSub = &list3[i]
				break
			}
		}
		if updatedSub == nil {
			t.Fatalf("ep0 not found after update")
		}
		if updatedSub.P256dh != "p0-updated" {
			t.Fatalf("ep0 p256dh not updated: got %q, want %q", updatedSub.P256dh, "p0-updated")
		}
	})

	t.Run("realm state ensure is first-writer-wins", func(t *testing.T) {
		st := open(t)
		if _, err := st.GetRealmState(ctx); err != ErrRealmStateMissing {
			t.Fatalf("fresh realm: want ErrRealmStateMissing, got %v", err)
		}
		rs1, err := st.EnsureRealmState(ctx, "realm-A")
		if err != nil || rs1.RealmID != "realm-A" {
			t.Fatalf("ensure A: %v %+v", err, rs1)
		}
		// Second ensure with a different candidate must NOT overwrite.
		rs2, err := st.EnsureRealmState(ctx, "realm-B")
		if err != nil || rs2.RealmID != "realm-A" {
			t.Fatalf("ensure B must keep A: %v %+v", err, rs2)
		}
		got, err := st.GetRealmState(ctx)
		if err != nil || got.RealmID != "realm-A" {
			t.Fatalf("get: %v %+v", err, got)
		}
	})

	t.Run("instance heartbeat + live list", func(t *testing.T) {
		st := open(t)
		if err := st.UpsertInstanceHeartbeat(ctx, "https://a.example", "https://a.example", 1000); err != nil {
			t.Fatalf("upsert a: %v", err)
		}
		if err := st.UpsertInstanceHeartbeat(ctx, "https://b.example", "https://b.example", 500); err != nil {
			t.Fatalf("upsert b: %v", err)
		}
		// Re-heartbeat a (upsert updates last_heartbeat).
		if err := st.UpsertInstanceHeartbeat(ctx, "https://a.example", "https://a.example", 2000); err != nil {
			t.Fatalf("re-upsert a: %v", err)
		}
		live, err := st.ListLiveInstances(ctx, 1000) // cutoff excludes b (500)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(live) != 1 || live[0].InstanceID != "https://a.example" || live[0].LastHeartbeat != 2000 {
			t.Fatalf("live = %+v", live)
		}
	})

	t.Run("user home get/set", func(t *testing.T) {
		st := open(t)
		u, err := st.CreateOpaqueUser(ctx, "home@example.com")
		if err != nil {
			t.Fatalf("user: %v", err)
		}
		if _, ok, err := st.GetUserHome(ctx, u.ID); err != nil || ok {
			t.Fatalf("fresh home: ok=%v err=%v", ok, err)
		}
		if err := st.SetUserHome(ctx, u.ID, "https://a.example"); err != nil {
			t.Fatalf("set: %v", err)
		}
		id, ok, err := st.GetUserHome(ctx, u.ID)
		if err != nil || !ok || id != "https://a.example" {
			t.Fatalf("get: id=%q ok=%v err=%v", id, ok, err)
		}
		if err := st.SetUserHome(ctx, u.ID, "https://b.example"); err != nil {
			t.Fatalf("reset: %v", err)
		}
		id2, _, _ := st.GetUserHome(ctx, u.ID)
		if id2 != "https://b.example" {
			t.Fatalf("reset home = %q", id2)
		}
	})

	t.Run("vapid keys upsert + subscriptions crud", func(t *testing.T) {
		st := open(t)
		if _, ok, err := st.GetVAPIDKeys(ctx); err != nil || ok {
			t.Fatalf("fresh vapid: ok=%v err=%v, want ok=false", ok, err)
		}
		if err := st.SetVAPIDKeys(ctx, VAPIDKeys{PrivateKey: "priv", PublicKey: "pub"}); err != nil {
			t.Fatalf("set vapid: %v", err)
		}
		k, ok, err := st.GetVAPIDKeys(ctx)
		if err != nil || !ok || k.PrivateKey != "priv" || k.PublicKey != "pub" {
			t.Fatalf("get vapid: ok=%v err=%v k=%+v", ok, err, k)
		}

		u, err := st.CreateOpaqueUser(ctx, "push@example.com")
		if err != nil {
			t.Fatalf("create user: %v", err)
		}
		sub := WebPushSubscription{Endpoint: "https://push/ep1", P256dh: "p", Auth: "a", CreatedAt: 123}
		if err := st.AddWebPushSubscription(ctx, u.ID, sub); err != nil {
			t.Fatalf("add sub: %v", err)
		}
		// Upsert same endpoint is idempotent (no duplicate).
		if err := st.AddWebPushSubscription(ctx, u.ID, sub); err != nil {
			t.Fatalf("re-add sub: %v", err)
		}
		list, err := st.ListWebPushSubscriptions(ctx, u.ID)
		if err != nil || len(list) != 1 || list[0].Endpoint != "https://push/ep1" {
			t.Fatalf("list: err=%v list=%+v", err, list)
		}
		if err := st.RemoveWebPushSubscription(ctx, u.ID, "https://push/ep1"); err != nil {
			t.Fatalf("remove: %v", err)
		}
		list2, _ := st.ListWebPushSubscriptions(ctx, u.ID)
		if len(list2) != 0 {
			t.Fatalf("after remove len = %d, want 0", len(list2))
		}
	})
}

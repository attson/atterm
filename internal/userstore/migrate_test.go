package userstore

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"
)

func seedSource(t *testing.T, st *DBStore) (userID string) {
	t.Helper()
	ctx := context.Background()
	u, err := st.CreateOpaqueUser(ctx, "migrate@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if _, _, err := st.CreateSession(ctx, u.ID, "ua", "127.0.0", time.Hour); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := st.StoreAccountKeyWrap(ctx, AccountKeyWrap{
		UserID: u.ID, Method: "password",
		Wrapped: []byte{1, 2, 3}, Nonce: []byte{4, 5}, Salt: []byte{6, 7},
		KDFParams: `{"t":1}`,
	}); err != nil {
		t.Fatalf("store wrap: %v", err)
	}
	if _, err := st.SetRelayConfig(ctx, RelayConfig{
		RateLimitPerMinute: 600, AllowedOrigins: []string{"https://a.example"},
		FeishuEnabled: true, FeishuEncryptKey: "k",
	}); err != nil {
		t.Fatalf("set relay config: %v", err)
	}
	if err := st.SetVAPIDKeys(ctx, VAPIDKeys{PrivateKey: "priv", PublicKey: "pub"}); err != nil {
		t.Fatalf("set vapid: %v", err)
	}
	if err := st.AddWebPushSubscription(ctx, u.ID, WebPushSubscription{
		Endpoint: "https://push/ep", P256dh: "p", Auth: "a", CreatedAt: 99,
	}); err != nil {
		t.Fatalf("add sub: %v", err)
	}
	// OPAQUE per-user record (0x00 byte ensures no null-truncation in BYTEA round-trip).
	opaqueRec := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}
	if err := st.StoreOpaqueRecord(ctx, u.ID, opaqueRec); err != nil {
		t.Fatalf("store opaque record: %v", err)
	}
	// OPAQUE server state singleton (each field has a distinct byte incl. 0x00).
	if err := st.StoreOpaqueServerState(ctx, OpaqueServerState{
		OPRFSeed:        []byte{0x01, 0x00, 0x02},
		AKEServerSecret: []byte{0x03, 0x00, 0x04},
		AKEServerPublic: []byte{0x05, 0x00, 0x06},
		Suite:           "ristretto255, SHA-512",
		CreatedAt:       time.Unix(1700000000, 0).UTC(),
	}); err != nil {
		t.Fatalf("store opaque server state: %v", err)
	}
	return u.ID
}

func assertTarget(t *testing.T, dst *DBStore, userID string) {
	t.Helper()
	ctx := context.Background()
	if u, err := dst.GetUser(ctx, userID); err != nil || u.Email != "migrate@example.com" {
		t.Fatalf("target user: %v %+v", err, u)
	}
	if sess, err := dst.ListSessions(ctx, userID); err != nil || len(sess) != 1 {
		t.Fatalf("target sessions: %v %+v", err, sess)
	}
	if w, err := dst.GetAccountKeyWrap(ctx, userID, "password"); err != nil || string(w.Wrapped) != string([]byte{1, 2, 3}) {
		t.Fatalf("target wrap: %v %+v", err, w)
	}
	rc, err := dst.GetRelayConfig(ctx)
	if err != nil || rc.RateLimitPerMinute != 600 || !rc.FeishuEnabled || rc.FeishuEncryptKey != "k" || len(rc.AllowedOrigins) != 1 {
		t.Fatalf("target relay_config: %v %+v", err, rc)
	}
	k, ok, err := dst.GetVAPIDKeys(ctx)
	if err != nil || !ok || k.PrivateKey != "priv" {
		t.Fatalf("target vapid: ok=%v err=%v %+v", ok, err, k)
	}
	if subs, err := dst.ListWebPushSubscriptions(ctx, userID); err != nil || len(subs) != 1 || subs[0].Endpoint != "https://push/ep" {
		t.Fatalf("target subs: %v %+v", err, subs)
	}
	// OPAQUE per-user record: exact byte round-trip (incl. embedded 0x00).
	gotRec, err := dst.GetOpaqueRecord(ctx, userID)
	if err != nil {
		t.Fatalf("target opaque record get: %v", err)
	}
	if wantRec := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x01}; !bytes.Equal(gotRec, wantRec) {
		t.Fatalf("target opaque record mismatch: got %x want %x", gotRec, wantRec)
	}
	// OPAQUE server state singleton: each key field must survive the cross-backend copy intact.
	gotSS, err := dst.GetOpaqueServerState(ctx)
	if err != nil {
		t.Fatalf("target opaque server state get: %v", err)
	}
	if !bytes.Equal(gotSS.OPRFSeed, []byte{0x01, 0x00, 0x02}) {
		t.Fatalf("opaque server state OPRFSeed mismatch: got %x", gotSS.OPRFSeed)
	}
	if !bytes.Equal(gotSS.AKEServerSecret, []byte{0x03, 0x00, 0x04}) {
		t.Fatalf("opaque server state AKEServerSecret mismatch: got %x", gotSS.AKEServerSecret)
	}
	if !bytes.Equal(gotSS.AKEServerPublic, []byte{0x05, 0x00, 0x06}) {
		t.Fatalf("opaque server state AKEServerPublic mismatch: got %x", gotSS.AKEServerPublic)
	}
	if gotSS.Suite != "ristretto255, SHA-512" {
		t.Fatalf("opaque server state Suite mismatch: got %q", gotSS.Suite)
	}
}

func TestCopySQLiteToSQLite(t *testing.T) {
	ctx := context.Background()
	src, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	t.Cleanup(func() { src.Close() })
	dst, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	t.Cleanup(func() { dst.Close() })

	userID := seedSource(t, src)
	counts, err := Copy(ctx, src, dst)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	if counts["users"] != 1 || counts["sessions"] != 1 || counts["web_push_subscriptions"] != 1 ||
		counts["relay_config"] != 1 || counts["web_push_keys"] != 1 || counts["user_account_key_wraps"] != 1 ||
		counts["user_opaque_records"] != 1 || counts["opaque_server_state"] != 1 {
		t.Fatalf("counts: %+v", counts)
	}
	assertTarget(t, dst, userID)

	// Second copy into the now-non-empty target must be refused.
	if _, err := Copy(ctx, src, dst); err == nil {
		t.Fatalf("expected refusal copying into non-empty target")
	}
}

func TestCopySQLiteToPostgres(t *testing.T) {
	dsn := os.Getenv("ATTERM_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ATTERM_TEST_PG_DSN not set; skipping cross-backend copy test")
	}
	ctx := context.Background()
	src, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open src: %v", err)
	}
	t.Cleanup(func() { src.Close() })
	// Clean PG schema, then open (runs migrations → empty tables).
	reset, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("open reset: %v", err)
	}
	if _, err := reset.db.ExecContext(ctx, `DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		reset.Close()
		t.Fatalf("reset schema: %v", err)
	}
	reset.Close()
	dst, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("open dst: %v", err)
	}
	t.Cleanup(func() { dst.Close() })

	userID := seedSource(t, src)
	if _, err := Copy(ctx, src, dst); err != nil {
		t.Fatalf("copy: %v", err)
	}
	assertTarget(t, dst, userID)
}

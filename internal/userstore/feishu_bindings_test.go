// internal/userstore/feishu_bindings_test.go
package userstore

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFeishuBinding_UpsertAndGet(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, err := s.CreateOpaqueUser(ctx, "fbu@example.com")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	in := FeishuBindingCredentials{
		AppID:       "cli_aabbccdd",
		AppSecret:   "secret-xyz",
		EncryptKey:  "encryptkey-32-bytes-string-here!",
		VerifyToken: "tok-abcdef",
	}
	if err := s.UpsertFeishuBinding(ctx, u.ID, in); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.GetFeishuBinding(ctx, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.UserID != u.ID || got.OpenID != "" || got.BoundAt != 0 || got.DisabledAt != 0 {
		t.Fatalf("unexpected binding row: %+v", got)
	}
	if got.AppID != in.AppID || got.AppSecret != in.AppSecret || got.EncryptKey != in.EncryptKey || got.VerifyToken != in.VerifyToken {
		t.Fatalf("decrypt mismatch: %+v vs %+v", got.FeishuBindingCredentials, in)
	}
	if got.AppIDHash == "" || len(got.AppIDHash) != 64 {
		t.Fatalf("expected SHA256 hex hash, got %q", got.AppIDHash)
	}
}

func TestFeishuBinding_GetByAppIDHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "fbh@example.com")
	in := FeishuBindingCredentials{AppID: "cli_zzz", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}
	_ = s.UpsertFeishuBinding(ctx, u.ID, in)

	full, err := s.GetFeishuBinding(ctx, u.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	by, err := s.GetFeishuBindingByAppIDHash(ctx, full.AppIDHash)
	if err != nil {
		t.Fatalf("get by hash: %v", err)
	}
	if by.UserID != u.ID {
		t.Fatalf("by-hash returned wrong user: %s vs %s", by.UserID, u.ID)
	}
}

func TestFeishuBinding_GetByAppIDHash_NotFound(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_, err := s.GetFeishuBindingByAppIDHash(ctx, "deadbeef")
	if !errors.Is(err, ErrFeishuBindingNotFound) {
		t.Fatalf("want ErrFeishuBindingNotFound, got %v", err)
	}
}

func TestFeishuBinding_UniqueAppIDHash(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u1, _ := s.CreateOpaqueUser(ctx, "a@example.com")
	u2, _ := s.CreateOpaqueUser(ctx, "b@example.com")
	in := FeishuBindingCredentials{AppID: "cli_shared", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}
	if err := s.UpsertFeishuBinding(ctx, u1.ID, in); err != nil {
		t.Fatalf("u1 upsert: %v", err)
	}
	if err := s.UpsertFeishuBinding(ctx, u2.ID, in); !errors.Is(err, ErrFeishuAppIDConflict) {
		t.Fatalf("want ErrFeishuAppIDConflict, got %v", err)
	}
}

func TestFeishuBinding_MarkBoundAndDisable(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "fbm@example.com")
	in := FeishuBindingCredentials{AppID: "cli_mb", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}
	_ = s.UpsertFeishuBinding(ctx, u.ID, in)

	if err := s.MarkFeishuBindingBound(ctx, u.ID, "ou_open123"); err != nil {
		t.Fatalf("mark bound: %v", err)
	}
	got, _ := s.GetFeishuBinding(ctx, u.ID)
	if got.OpenID != "ou_open123" || got.BoundAt == 0 {
		t.Fatalf("bound state not persisted: %+v", got)
	}

	if err := s.MarkFeishuBindingDisabled(ctx, u.ID); err != nil {
		t.Fatalf("mark disabled: %v", err)
	}
	got, _ = s.GetFeishuBinding(ctx, u.ID)
	if got.DisabledAt == 0 {
		t.Fatalf("disabled_at not set")
	}

	if err := s.ClearFeishuBindingDisabled(ctx, u.ID); err != nil {
		t.Fatalf("clear disabled: %v", err)
	}
	got, _ = s.GetFeishuBinding(ctx, u.ID)
	if got.DisabledAt != 0 {
		t.Fatalf("disabled_at not cleared")
	}
}

func TestFeishuBinding_Delete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "fbd@example.com")
	in := FeishuBindingCredentials{AppID: "cli_d", AppSecret: "s", EncryptKey: "k", VerifyToken: "t"}
	_ = s.UpsertFeishuBinding(ctx, u.ID, in)

	if err := s.DeleteFeishuBinding(ctx, u.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	_, err := s.GetFeishuBinding(ctx, u.ID)
	if !errors.Is(err, ErrFeishuBindingNotFound) {
		t.Fatalf("want not-found after delete, got %v", err)
	}
}

func TestFeishuBinding_RequiresCipher(t *testing.T) {
	ctx := context.Background()
	// Bypass newTestStore — open without WithSecretCipher.
	s, err := Open(ctx, ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	u, _ := s.CreateOpaqueUser(ctx, "noc@example.com")
	err = s.UpsertFeishuBinding(ctx, u.ID, FeishuBindingCredentials{AppID: "x", AppSecret: "y", EncryptKey: "z", VerifyToken: "t"})
	if err == nil || !strings.Contains(err.Error(), "cipher") {
		t.Fatalf("expected cipher-required error, got %v", err)
	}
}

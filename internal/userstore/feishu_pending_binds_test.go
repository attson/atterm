// internal/userstore/feishu_pending_binds_test.go
package userstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPendingBind_PutAndConsume(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "pb1@example.com")

	exp := time.Now().Add(15 * time.Minute).Unix()
	if err := s.PutFeishuPendingBind(ctx, u.ID, "AB3CD7", exp); err != nil {
		t.Fatalf("put: %v", err)
	}

	uid, err := s.ConsumeFeishuPendingBind(ctx, "AB3CD7")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if uid != u.ID {
		t.Fatalf("consume returned wrong user: %s vs %s", uid, u.ID)
	}

	// Second consume must fail.
	_, err = s.ConsumeFeishuPendingBind(ctx, "AB3CD7")
	if !errors.Is(err, ErrFeishuPendingBindNotFound) {
		t.Fatalf("want ErrFeishuPendingBindNotFound on second consume, got %v", err)
	}
}

func TestPendingBind_OverwriteOnRePut(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "pb2@example.com")

	exp := time.Now().Add(time.Hour).Unix()
	_ = s.PutFeishuPendingBind(ctx, u.ID, "OLD123", exp)
	if err := s.PutFeishuPendingBind(ctx, u.ID, "NEW999", exp); err != nil {
		t.Fatalf("re-put: %v", err)
	}

	if _, err := s.ConsumeFeishuPendingBind(ctx, "OLD123"); !errors.Is(err, ErrFeishuPendingBindNotFound) {
		t.Fatalf("OLD code should be gone")
	}
	uid, err := s.ConsumeFeishuPendingBind(ctx, "NEW999")
	if err != nil {
		t.Fatalf("NEW consume: %v", err)
	}
	if uid != u.ID {
		t.Fatalf("wrong user")
	}
}

func TestPendingBind_Expired(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u, _ := s.CreateOpaqueUser(ctx, "pb3@example.com")

	exp := time.Now().Add(-time.Second).Unix()
	_ = s.PutFeishuPendingBind(ctx, u.ID, "EXPIRED", exp)

	_, err := s.ConsumeFeishuPendingBind(ctx, "EXPIRED")
	if !errors.Is(err, ErrFeishuPendingBindNotFound) {
		t.Fatalf("expired code should consume as not-found, got %v", err)
	}
}

func TestPendingBind_Sweep(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	u1, _ := s.CreateOpaqueUser(ctx, "sw1@example.com")
	u2, _ := s.CreateOpaqueUser(ctx, "sw2@example.com")
	_ = s.PutFeishuPendingBind(ctx, u1.ID, "OLD111", time.Now().Add(-time.Minute).Unix())
	_ = s.PutFeishuPendingBind(ctx, u2.ID, "FRSH22", time.Now().Add(time.Hour).Unix())

	n, err := s.SweepExpiredFeishuPendingBinds(ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 swept, got %d", n)
	}
	// FRSH22 still there.
	if _, err := s.ConsumeFeishuPendingBind(ctx, "FRSH22"); err != nil {
		t.Fatalf("FRSH should still work: %v", err)
	}
}

func TestGenerateFeishuPairCode(t *testing.T) {
	for i := 0; i < 50; i++ {
		code := GenerateFeishuPairCode()
		if len(code) != 6 {
			t.Fatalf("code length: %q", code)
		}
		const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
		for _, r := range code {
			if !strings.ContainsRune(alphabet, r) {
				t.Fatalf("code %q has illegal char %q", code, r)
			}
		}
	}
}

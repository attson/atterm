package userstore

import (
	"context"
	"testing"
)

func TestWebhookCRUD(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateOpaqueUser(ctx, "wh@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	uid := user.ID

	wh, err := s.CreateWebhook(ctx, uid, "https://open.feishu.cn/x", "feishu", "phone", false)
	if err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if wh.ID == "" || wh.UserID != uid || wh.URL != "https://open.feishu.cn/x" || wh.Format != "feishu" || wh.Name != "phone" {
		t.Fatalf("unexpected webhook row: %+v", wh)
	}

	list, err := s.ListWebhooks(ctx, uid)
	if err != nil {
		t.Fatalf("ListWebhooks: %v", err)
	}
	if len(list) != 1 || list[0].ID != wh.ID {
		t.Fatalf("expected 1 webhook, got %+v", list)
	}

	if err := s.DeleteWebhook(ctx, wh.ID, "other-user"); err != ErrWebhookNotOwnedOrMissing {
		t.Fatalf("cross-user delete should fail with ErrWebhookNotOwnedOrMissing, got %v", err)
	}
	if err := s.DeleteWebhook(ctx, wh.ID, uid); err != nil {
		t.Fatalf("DeleteWebhook: %v", err)
	}
	list, _ = s.ListWebhooks(ctx, uid)
	if len(list) != 0 {
		t.Fatalf("expected 0 webhooks after delete, got %d", len(list))
	}
}

func TestWebhookCascadeOnUserDelete(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	user, err := s.CreateOpaqueUser(ctx, "wh2@example.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	uid := user.ID

	if _, err := s.CreateWebhook(ctx, uid, "https://r/x", "generic", "n", false); err != nil {
		t.Fatalf("CreateWebhook: %v", err)
	}
	if err := s.DeleteUser(ctx, uid); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	list, err := s.ListWebhooks(ctx, uid)
	if err != nil {
		t.Fatalf("ListWebhooks after user delete: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected cascade-delete to remove webhooks, got %d", len(list))
	}
}

package userstore

import (
	"context"
	"testing"
)

func TestUserPreferences_TableExists(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	// Insert a probe row directly; should succeed if migration ran.
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_preferences(user_id, key, value_json, updated_at)
		 VALUES(?, ?, ?, ?)`,
		"user-x", "locale_preference", `"en"`, 1234567890,
	)
	if err != nil {
		t.Fatalf("insert into user_preferences: %v", err)
	}
}

func TestGetUserPreferences_EmptyByDefault(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, err := s.CreateUser(ctx, "x@y.com", "Correct-Horse-Battery-Staple-1!")
	if err != nil { t.Fatalf("CreateUser: %v", err) }

	items, err := s.GetUserPreferences(ctx, u.ID)
	if err != nil { t.Fatalf("GetUserPreferences: %v", err) }
	if len(items) != 0 {
		t.Fatalf("expected empty, got %d items", len(items))
	}
}

func TestGetUserPreferences_ReturnsStoredRows(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "x@y.com", "Correct-Horse-Battery-Staple-1!")

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_preferences(user_id, key, value_json, updated_at)
		 VALUES(?, ?, ?, ?), (?, ?, ?, ?)`,
		u.ID, "locale_preference", `"zh-CN"`, int64(1000),
		u.ID, "notifications_enabled", `true`, int64(2000),
	)
	if err != nil { t.Fatalf("seed: %v", err) }

	items, err := s.GetUserPreferences(ctx, u.ID)
	if err != nil { t.Fatalf("GetUserPreferences: %v", err) }
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	for _, it := range items {
		switch it.Key {
		case "locale_preference":
			if string(it.ValueJSON) != `"zh-CN"` || it.UpdatedAt != 1000 {
				t.Fatalf("locale: %+v", it)
			}
		case "notifications_enabled":
			if string(it.ValueJSON) != `true` || it.UpdatedAt != 2000 {
				t.Fatalf("notifications: %+v", it)
			}
		default:
			t.Fatalf("unexpected key %q", it.Key)
		}
	}
}

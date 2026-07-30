package userstore

import (
	"context"
	"encoding/json"
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
	u, err := s.CreateOpaqueUser(ctx, "x@y.com")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	items, err := s.GetUserPreferences(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserPreferences: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected empty, got %d items", len(items))
	}
}

func TestGetUserPreferences_ReturnsStoredRows(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateOpaqueUser(ctx, "x@y.com")

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO user_preferences(user_id, key, value_json, updated_at)
		 VALUES(?, ?, ?, ?), (?, ?, ?, ?)`,
		u.ID, "locale_preference", `"zh-CN"`, int64(1000),
		u.ID, "notifications_enabled", `true`, int64(2000),
	)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	items, err := s.GetUserPreferences(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetUserPreferences: %v", err)
	}
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

func TestSetUserPreferences_InsertsNewRows(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateOpaqueUser(ctx, "x@y.com")

	now := int64(1700000000000)
	result, err := s.SetUserPreferences(ctx, u.ID, now, []PreferenceItem{
		{Key: "locale_preference", ValueJSON: json.RawMessage(`"en"`), UpdatedAt: 1000},
	})
	if err != nil {
		t.Fatalf("SetUserPreferences: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d items", len(result))
	}
	if string(result[0].ValueJSON) != `"en"` {
		t.Fatalf("value: %s", result[0].ValueJSON)
	}
	// Server stamps max(client_ts, now) — here now > 1000, so now wins.
	if result[0].UpdatedAt != now {
		t.Fatalf("expected updated_at=%d, got %d", now, result[0].UpdatedAt)
	}
}

func TestSetUserPreferences_AcceptsSyncedArrayAndBoolKeys(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateOpaqueUser(ctx, "x@y.com")

	result, err := s.SetUserPreferences(ctx, u.ID, 1700000000000, []PreferenceItem{
		{Key: "pinned_session_ids", ValueJSON: json.RawMessage(`["sid-web"]`), UpdatedAt: 1000},
		{Key: "ai_notifications_only", ValueJSON: json.RawMessage(`true`), UpdatedAt: 1000},
	})
	if err != nil {
		t.Fatalf("SetUserPreferences: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d items, want 2", len(result))
	}
}

func TestSetUserPreferences_RejectsOlderTimestamp(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateOpaqueUser(ctx, "x@y.com")

	_, _ = s.SetUserPreferences(ctx, u.ID, 5000, []PreferenceItem{
		{Key: "locale_preference", ValueJSON: json.RawMessage(`"zh-CN"`), UpdatedAt: 4000},
	})
	result, err := s.SetUserPreferences(ctx, u.ID, 6000, []PreferenceItem{
		{Key: "locale_preference", ValueJSON: json.RawMessage(`"en"`), UpdatedAt: 3000},
	})
	if err != nil {
		t.Fatalf("SetUserPreferences: %v", err)
	}
	// Rejected (3000 < 5000); server returns existing value, not the rejected one.
	if string(result[0].ValueJSON) != `"zh-CN"` {
		t.Fatalf("expected zh-CN preserved, got %s", result[0].ValueJSON)
	}
	if result[0].UpdatedAt != 5000 {
		t.Fatalf("expected updated_at=5000, got %d", result[0].UpdatedAt)
	}
}

func TestSetUserPreferences_UnknownKeyRejected(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateOpaqueUser(ctx, "x@y.com")

	_, err := s.SetUserPreferences(ctx, u.ID, 1, []PreferenceItem{
		{Key: "evil_key", ValueJSON: json.RawMessage(`"x"`), UpdatedAt: 1},
	})
	if err == nil || !errorsIs(err, ErrUnknownPreferenceKey) {
		t.Fatalf("expected ErrUnknownPreferenceKey, got %v", err)
	}
}

func TestSetUserPreferences_TypeMismatchRejected(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateOpaqueUser(ctx, "x@y.com")

	_, err := s.SetUserPreferences(ctx, u.ID, 1, []PreferenceItem{
		{Key: "locale_preference", ValueJSON: json.RawMessage(`true`), UpdatedAt: 1},
	})
	if err == nil || !errorsIs(err, ErrInvalidPreferenceValue) {
		t.Fatalf("expected ErrInvalidPreferenceValue, got %v", err)
	}
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		type wrapped interface{ Unwrap() error }
		w, ok := err.(wrapped)
		if !ok {
			return false
		}
		err = w.Unwrap()
	}
	return false
}

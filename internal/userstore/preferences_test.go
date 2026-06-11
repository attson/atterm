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

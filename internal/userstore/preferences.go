package userstore

import (
	"context"
	"encoding/json"
	"fmt"
)

// PreferenceItem is a single row in user_preferences. ValueJSON is the
// raw JSON bytes for the value (string, bool, number, or array).
type PreferenceItem struct {
	Key       string          `json:"key"`
	ValueJSON json.RawMessage `json:"value"`
	UpdatedAt int64           `json:"updated_at"`
}

// allowedPreferenceKeys is the whitelist enforced on every PUT. Adding a
// new field requires bumping this list (and matching the spec).
var allowedPreferenceKeys = map[string]preferenceKind{
	"locale_preference":                preferenceKindString,
	"quick_templates":                  preferenceKindArray,
	"notifications_enabled":            preferenceKindBool,
	"command_notify_threshold_seconds": preferenceKindInt,
	"shell_integration_enabled":        preferenceKindBool,
}

type preferenceKind int

const (
	preferenceKindString preferenceKind = iota
	preferenceKindBool
	preferenceKindInt
	preferenceKindArray
)

// ErrUnknownPreferenceKey is returned by SetUserPreferences when a PUT
// includes a key not in allowedPreferenceKeys.
var ErrUnknownPreferenceKey = fmt.Errorf("unknown preference key")

// ErrInvalidPreferenceValue is returned when a value's JSON type does not
// match the key's declared kind.
var ErrInvalidPreferenceValue = fmt.Errorf("invalid preference value")

// GetUserPreferences returns all preference rows for the user. Empty
// slice if the user has never synced. Order is unspecified.
func (s *SQLiteStore) GetUserPreferences(ctx context.Context, userID string) ([]PreferenceItem, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT key, value_json, updated_at FROM user_preferences WHERE user_id = ?`,
		userID,
	)
	if err != nil { return nil, fmt.Errorf("query: %w", err) }
	defer rows.Close()

	var out []PreferenceItem
	for rows.Next() {
		var it PreferenceItem
		var raw string
		if err := rows.Scan(&it.Key, &raw, &it.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan: %w", err)
		}
		it.ValueJSON = json.RawMessage(raw)
		out = append(out, it)
	}
	if err := rows.Err(); err != nil { return nil, fmt.Errorf("rows: %w", err) }
	return out, nil
}

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

// SetUserPreferences applies per-key LWW. serverNowMs is the server's
// current ms epoch. For each item:
//   - rejects keys not in allowedPreferenceKeys (ErrUnknownPreferenceKey)
//   - rejects values whose JSON type doesn't match the key's kind (ErrInvalidPreferenceValue)
//   - if item.UpdatedAt > existing.updated_at, writes with
//     updated_at = max(item.UpdatedAt, serverNowMs)
//   - otherwise leaves existing untouched
// Returns the full current state for every key the user has after the
// operation (including keys not in the input).
func (s *SQLiteStore) SetUserPreferences(
	ctx context.Context,
	userID string,
	serverNowMs int64,
	items []PreferenceItem,
) ([]PreferenceItem, error) {
	for _, it := range items {
		kind, ok := allowedPreferenceKeys[it.Key]
		if !ok { return nil, fmt.Errorf("%w: %s", ErrUnknownPreferenceKey, it.Key) }
		if err := validatePreferenceValue(kind, it.ValueJSON); err != nil {
			return nil, fmt.Errorf("%w: %s: %s", ErrInvalidPreferenceValue, it.Key, err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil { return nil, fmt.Errorf("begin: %w", err) }
	defer tx.Rollback()

	for _, it := range items {
		var existing int64
		err := tx.QueryRowContext(ctx,
			`SELECT updated_at FROM user_preferences WHERE user_id = ? AND key = ?`,
			userID, it.Key,
		).Scan(&existing)
		newerOrEqual := err == nil && existing >= it.UpdatedAt
		if newerOrEqual { continue }

		writeTs := it.UpdatedAt
		if serverNowMs > writeTs { writeTs = serverNowMs }

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO user_preferences(user_id, key, value_json, updated_at)
			 VALUES(?, ?, ?, ?)
			 ON CONFLICT(user_id, key) DO UPDATE SET
			   value_json = excluded.value_json,
			   updated_at = excluded.updated_at`,
			userID, it.Key, string(it.ValueJSON), writeTs,
		); err != nil {
			return nil, fmt.Errorf("upsert %s: %w", it.Key, err)
		}
	}

	if err := tx.Commit(); err != nil { return nil, fmt.Errorf("commit: %w", err) }

	return s.GetUserPreferences(ctx, userID)
}

func validatePreferenceValue(kind preferenceKind, raw json.RawMessage) error {
	switch kind {
	case preferenceKindString:
		var v string
		return json.Unmarshal(raw, &v)
	case preferenceKindBool:
		var v bool
		return json.Unmarshal(raw, &v)
	case preferenceKindInt:
		var v int64
		return json.Unmarshal(raw, &v)
	case preferenceKindArray:
		var v []json.RawMessage
		return json.Unmarshal(raw, &v)
	default:
		return fmt.Errorf("unknown kind %d", kind)
	}
}

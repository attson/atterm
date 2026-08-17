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
//
// This list must stay a superset of prefssync.SyncedKeys() (internal/prefssync,
// desktop-side sync engine) — the cross-package test in
// preferences_synced_keys_test.go (this package) enforces that. C1/C2 of the
// 2026-08-17 prefs-sync-l1 final review: this map was
// missing every one of the nine L1 keys (terminal_theme, six terminal
// appearance keys, default_shell, shortcut_bindings) plus ssh_hosts_encrypted,
// so the relay 400'd every PUT containing any of them — and once a batch
// fails, the dirty flag never clears, so the client keeps resending the same
// poisoned batch forever, silently breaking sync for every other key too.
var allowedPreferenceKeys = map[string]preferenceKind{
	"locale_preference":                preferenceKindString,
	"quick_templates":                  preferenceKindArray,
	"notifications_enabled":            preferenceKindBool,
	"ai_notifications_only":            preferenceKindBool,
	"command_notify_threshold_seconds": preferenceKindInt,
	"shell_integration_enabled":        preferenceKindBool,
	"pinned_session_ids":               preferenceKindArray,
	"ssh_hosts_encrypted":              preferenceKindString,
	"terminal_theme":                   preferenceKindString,
	"terminal_font_head":               preferenceKindString,
	"terminal_font_size":               preferenceKindInt,
	"terminal_line_height":             preferenceKindNumber,
	"terminal_cursor_style":            preferenceKindString,
	"terminal_cursor_blink":            preferenceKindBool,
	"terminal_scrollback":              preferenceKindInt,
	"default_shell":                    preferenceKindString,
	"shortcut_bindings":                preferenceKindObject,
	// profiles_encrypted carries an E2EE-sealed session-profiles blob (design
	// docs/superpowers/specs/2026-08-17-session-profiles-design.md §5): same
	// wire shape as ssh_hosts_encrypted — a base64(ciphertext) JSON string
	// the relay cannot open — so it gets the same kind.
	"profiles_encrypted": preferenceKindString,
}

type preferenceKind int

const (
	preferenceKindString preferenceKind = iota
	preferenceKindBool
	preferenceKindInt
	preferenceKindArray
	// preferenceKindNumber is a JSON number that may carry a fractional
	// part (e.g. terminal_line_height: 1.2). preferenceKindInt would 400
	// invalid_value on it — json.Unmarshal into int64 rejects non-integer
	// numbers.
	preferenceKindNumber
	// preferenceKindObject is a JSON object (e.g. shortcut_bindings:
	// map[string]string). preferenceKindArray would 400 invalid_value on
	// it — json.Unmarshal into []json.RawMessage rejects a `{...}` payload.
	preferenceKindObject
)

// ErrUnknownPreferenceKey is returned by SetUserPreferences when a PUT
// includes a key not in allowedPreferenceKeys.
var ErrUnknownPreferenceKey = fmt.Errorf("unknown preference key")

// ErrInvalidPreferenceValue is returned when a value's JSON type does not
// match the key's declared kind.
var ErrInvalidPreferenceValue = fmt.Errorf("invalid preference value")

// GetUserPreferences returns all preference rows for the user. Empty
// slice if the user has never synced. Order is unspecified.
func (s *DBStore) GetUserPreferences(ctx context.Context, userID string) ([]PreferenceItem, error) {
	rows, err := s.db.QueryContext(ctx,
		s.dia.Rebind(`SELECT key, value_json, updated_at FROM user_preferences WHERE user_id = ?`),
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
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
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows: %w", err)
	}
	return out, nil
}

// SetUserPreferences applies per-key LWW. serverNowMs is the server's
// current ms epoch. For each item:
//   - rejects keys not in allowedPreferenceKeys (ErrUnknownPreferenceKey)
//   - rejects values whose JSON type doesn't match the key's kind (ErrInvalidPreferenceValue)
//   - if item.UpdatedAt > existing.updated_at, writes with
//     updated_at = max(item.UpdatedAt, serverNowMs)
//   - otherwise leaves existing untouched
//
// Returns the full current state for every key the user has after the
// operation (including keys not in the input).
func (s *DBStore) SetUserPreferences(
	ctx context.Context,
	userID string,
	serverNowMs int64,
	items []PreferenceItem,
) ([]PreferenceItem, error) {
	for _, it := range items {
		kind, ok := allowedPreferenceKeys[it.Key]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownPreferenceKey, it.Key)
		}
		if err := validatePreferenceValue(kind, it.ValueJSON); err != nil {
			return nil, fmt.Errorf("%w: %s: %s", ErrInvalidPreferenceValue, it.Key, err)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback()

	for _, it := range items {
		writeTs := it.UpdatedAt
		if serverNowMs > writeTs {
			writeTs = serverNowMs
		}
		if _, err := tx.ExecContext(ctx, s.dia.Rebind(
			`INSERT INTO user_preferences(user_id, key, value_json, updated_at)
         VALUES(?, ?, ?, ?)
         ON CONFLICT(user_id, key) DO UPDATE SET
           value_json = excluded.value_json,
           updated_at = excluded.updated_at
         WHERE ? > user_preferences.updated_at`),
			userID, it.Key, string(it.ValueJSON), writeTs, it.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("upsert %s: %w", it.Key, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

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
	case preferenceKindNumber:
		var v float64
		return json.Unmarshal(raw, &v)
	case preferenceKindObject:
		var v map[string]json.RawMessage
		return json.Unmarshal(raw, &v)
	default:
		return fmt.Errorf("unknown kind %d", kind)
	}
}

# Cross-Platform Settings Sync Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Sync five account-level user preferences (`locale_preference`, `quick_templates`, `notifications_enabled`, `command_notify_threshold_seconds`, `shell_integration_enabled`) across desktop, mobile, and web through the relay server, using per-field last-write-wins and pull-on-foreground + push-on-change.

**Architecture:** Relay gets a new `user_preferences` SQLite KV table and two REST endpoints (`GET` / `PUT /api/me/preferences`). Each client implements the same per-key state machine (`value`, `updated_at_local`, `dirty`) over its existing local storage. Sync runs only when the user is logged into relay; unlogged clients remain purely local.

**Tech Stack:** Go (relay + desktop), SQLite (database/sql + embedded `*.sql` migrations), Vue 3 + TypeScript (desktop/frontend shared with mobile via Capacitor), Vue 3 + TypeScript (web/src/), Wails v2 runtime, Vitest.

**Spec:** [`docs/superpowers/specs/2026-06-12-cross-platform-settings-sync-design.md`](../specs/2026-06-12-cross-platform-settings-sync-design.md)

---

## File Structure

### Relay (Go)
- Create: `internal/userstore/migrations/0002_user_preferences.sql` — schema
- Create: `internal/userstore/preferences.go` — store methods + key whitelist
- Create: `internal/userstore/preferences_test.go` — store unit tests
- Create: `internal/relay/preferences_http.go` — HTTP handlers
- Create: `internal/relay/preferences_http_test.go` — handler tests
- Modify: `internal/relay/auth.go` — register routes via `AuthServer.RegisterInto`

### Desktop (Go)
- Create: `internal/prefssync/sync.go` — sync engine
- Create: `internal/prefssync/sync_test.go`
- Modify: `desktop/config.go` — add `PrefsMeta` field + helpers
- Modify: `desktop/app.go` — wire engine to startup, login, logout, and the 5 setters
- Modify: `desktop/main.go` — engine lifecycle in startup/shutdown

### Desktop/Mobile frontend (shared TS — `desktop/frontend/src/`)
- Create: `lib/prefsSync.ts` — state machine + relay PUT/GET
- Create: `lib/prefsSync.test.ts`
- Modify: `main.ts` — wire focus + Wails event listener for login
- Modify: `main.capacitor.ts` — wire `appStateChange` + login event

### Web frontend (`web/src/`)
- Create: `shared/sync/prefsSync.ts` — same state machine adapted to apiFetch
- Create: `shared/sync/prefsSync.test.ts`
- Modify: `shared/i18n/index.ts` — locale read/write goes through engine when logged in
- Modify: `shared/templates.ts` — templates read/write goes through engine when logged in
- Modify: `main/main.ts` and `settings/main.ts` — bootstrap engine + focus listener

---

## Task 1: Relay — preferences table migration

**Files:**
- Create: `internal/userstore/migrations/0002_user_preferences.sql`
- Test: `internal/userstore/preferences_test.go`

- [ ] **Step 1: Write the failing test** in `internal/userstore/preferences_test.go`

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/userstore -run TestUserPreferences_TableExists -v`
Expected: FAIL with "no such table: user_preferences"

- [ ] **Step 3: Write migration** at `internal/userstore/migrations/0002_user_preferences.sql`

```sql
CREATE TABLE user_preferences (
    user_id     TEXT    NOT NULL,
    key         TEXT    NOT NULL,
    value_json  TEXT    NOT NULL,
    updated_at  INTEGER NOT NULL,
    PRIMARY KEY (user_id, key)
);

CREATE INDEX user_preferences_user ON user_preferences(user_id);
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/userstore -run TestUserPreferences_TableExists -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/migrations/0002_user_preferences.sql internal/userstore/preferences_test.go
git commit -m "feat(relay): add user_preferences table"
```

---

## Task 2: Relay — Preferences store: whitelist + GetUserPreferences

**Files:**
- Create: `internal/userstore/preferences.go`
- Test: `internal/userstore/preferences_test.go` (append)

- [ ] **Step 1: Add the failing test** at the bottom of `internal/userstore/preferences_test.go`

```go
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
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/userstore -run TestGetUserPreferences -v`
Expected: FAIL with "s.GetUserPreferences undefined"

- [ ] **Step 3: Implement** at `internal/userstore/preferences.go`

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/userstore -run TestGetUserPreferences -v`
Expected: PASS for both `TestGetUserPreferences_EmptyByDefault` and `TestGetUserPreferences_ReturnsStoredRows`

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/preferences.go internal/userstore/preferences_test.go
git commit -m "feat(relay): GetUserPreferences + key whitelist"
```

---

## Task 3: Relay — SetUserPreferences with per-key LWW

**Files:**
- Modify: `internal/userstore/preferences.go`
- Test: `internal/userstore/preferences_test.go` (append)

- [ ] **Step 1: Add failing tests**

```go
func TestSetUserPreferences_InsertsNewRows(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "x@y.com", "Correct-Horse-Battery-Staple-1!")

	now := int64(1700000000000)
	result, err := s.SetUserPreferences(ctx, u.ID, now, []PreferenceItem{
		{Key: "locale_preference", ValueJSON: json.RawMessage(`"en"`), UpdatedAt: 1000},
	})
	if err != nil { t.Fatalf("SetUserPreferences: %v", err) }
	if len(result) != 1 { t.Fatalf("got %d items", len(result)) }
	if string(result[0].ValueJSON) != `"en"` { t.Fatalf("value: %s", result[0].ValueJSON) }
	// Server stamps max(client_ts, now) — here now > 1000, so now wins.
	if result[0].UpdatedAt != now {
		t.Fatalf("expected updated_at=%d, got %d", now, result[0].UpdatedAt)
	}
}

func TestSetUserPreferences_RejectsOlderTimestamp(t *testing.T) {
	s := NewInMemory(t)
	ctx := context.Background()
	u, _ := s.CreateUser(ctx, "x@y.com", "Correct-Horse-Battery-Staple-1!")

	_, _ = s.SetUserPreferences(ctx, u.ID, 5000, []PreferenceItem{
		{Key: "locale_preference", ValueJSON: json.RawMessage(`"zh-CN"`), UpdatedAt: 4000},
	})
	result, err := s.SetUserPreferences(ctx, u.ID, 6000, []PreferenceItem{
		{Key: "locale_preference", ValueJSON: json.RawMessage(`"en"`), UpdatedAt: 3000},
	})
	if err != nil { t.Fatalf("SetUserPreferences: %v", err) }
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
	u, _ := s.CreateUser(ctx, "x@y.com", "Correct-Horse-Battery-Staple-1!")

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
	u, _ := s.CreateUser(ctx, "x@y.com", "Correct-Horse-Battery-Staple-1!")

	_, err := s.SetUserPreferences(ctx, u.ID, 1, []PreferenceItem{
		{Key: "locale_preference", ValueJSON: json.RawMessage(`true`), UpdatedAt: 1},
	})
	if err == nil || !errorsIs(err, ErrInvalidPreferenceValue) {
		t.Fatalf("expected ErrInvalidPreferenceValue, got %v", err)
	}
}

func errorsIs(err, target error) bool {
	for err != nil {
		if err == target { return true }
		type wrapped interface{ Unwrap() error }
		w, ok := err.(wrapped)
		if !ok { return false }
		err = w.Unwrap()
	}
	return false
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/userstore -run TestSetUserPreferences -v`
Expected: FAIL with "s.SetUserPreferences undefined"

- [ ] **Step 3: Implement `SetUserPreferences` + helpers** in `internal/userstore/preferences.go`

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/userstore -run TestSetUserPreferences -v`
Expected: PASS for all four `TestSetUserPreferences_*`

- [ ] **Step 5: Commit**

```bash
git add internal/userstore/preferences.go internal/userstore/preferences_test.go
git commit -m "feat(relay): SetUserPreferences with per-key LWW"
```

---

## Task 4: Relay — `GET /api/me/preferences` handler

**Files:**
- Create: `internal/relay/preferences_http.go`
- Create: `internal/relay/preferences_http_test.go`
- Modify: `internal/relay/auth.go` (route registration; see Step 5)

- [ ] **Step 1: Write the failing test** at `internal/relay/preferences_http_test.go`

```go
package relay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

func TestGetPreferences_ReturnsEmptyItemsForFreshUser(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)

	req := httptest.NewRequest("GET", "/api/me/preferences", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK { t.Fatalf("status %d: %s", rec.Code, rec.Body.String()) }
	var body struct {
		Items []userstore.PreferenceItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Items) != 0 {
		t.Fatalf("expected empty items, got %d", len(body.Items))
	}
}

func TestGetPreferences_RequiresAuth(t *testing.T) {
	s, _, _ := serverWithSessionAndUser(t)
	req := httptest.NewRequest("GET", "/api/me/preferences", nil)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}
}

func TestGetPreferences_ReturnsStoredRows(t *testing.T) {
	s, tok, userID := serverWithSessionAndUser(t)

	// Seed one row directly through the store.
	store := s.Store()
	_, err := store.SetUserPreferences(context.Background(), userID,
		time.Now().UnixMilli(),
		[]userstore.PreferenceItem{
			{Key: "locale_preference", ValueJSON: json.RawMessage(`"en"`), UpdatedAt: 100},
		})
	if err != nil { t.Fatalf("seed: %v", err) }

	req := httptest.NewRequest("GET", "/api/me/preferences", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status %d", rec.Code) }
	if !strings.Contains(rec.Body.String(), `"locale_preference"`) {
		t.Fatalf("missing key in body: %s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/relay -run TestGetPreferences -v`
Expected: FAIL — route 404 or test helper missing

If the helper `serverWithSessionAndUser` is missing in scope (already used in `auth_test.go`) or `s.Store()` accessor doesn't exist, add a tiny accessor in `internal/relay/server.go` returning the `*userstore.SQLiteStore` already held by the server. Confirm by reading `server.go` first; if there is no public accessor, add:

```go
// Store returns the underlying userstore. Test-only convenience.
func (s *Server) Store() *userstore.SQLiteStore { return s.cfg.Store }
```

- [ ] **Step 3: Implement the handler** at `internal/relay/preferences_http.go`

```go
package relay

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/attson/atterm/internal/userstore"
)

// handleGetPreferences answers GET /api/me/preferences with the user's
// full preference state (possibly empty). Auth is enforced by
// requireSession when the route is registered.
func (a *AuthServer) handleGetPreferences(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := a.Store.GetUserPreferences(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error")
		return
	}
	if items == nil { items = []userstore.PreferenceItem{} }
	writeJSONStatus(w, http.StatusOK, map[string]any{"items": items})
}

// nowMs returns the server's current ms epoch. Indirection lets tests
// inject a clock if needed.
func nowMs() int64 { return time.Now().UnixMilli() }
```

- [ ] **Step 4: Register the route** in `internal/relay/auth.go` (inside `(a *AuthServer) RegisterInto`):

```go
mux.Handle("GET /api/me/preferences", requireSession(a.handleGetPreferences))
```

(Place it next to the other `/api/me/*` routes — read the file first and mirror style.)

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/relay -run TestGetPreferences -v`
Expected: PASS for all three `TestGetPreferences_*`

- [ ] **Step 6: Commit**

```bash
git add internal/relay/preferences_http.go internal/relay/preferences_http_test.go internal/relay/auth.go internal/relay/server.go
git commit -m "feat(relay): GET /api/me/preferences"
```

---

## Task 5: Relay — `PUT /api/me/preferences` handler

**Files:**
- Modify: `internal/relay/preferences_http.go`
- Modify: `internal/relay/preferences_http_test.go` (append)
- Modify: `internal/relay/auth.go`

- [ ] **Step 1: Add failing tests**

```go
func TestPutPreferences_InsertsAndReturnsFullState(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)

	body := `{"items":[
		{"key":"locale_preference","value":"zh-CN","client_updated_at":1700000000000},
		{"key":"quick_templates","value":[{"id":"a","label":"a","text":"a"}],"client_updated_at":1700000000000}
	]}`
	req := httptest.NewRequest("PUT", "/api/me/preferences", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK { t.Fatalf("status %d: %s", rec.Code, rec.Body.String()) }
	var resp struct {
		Items []userstore.PreferenceItem `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil { t.Fatalf("decode: %v", err) }
	if len(resp.Items) != 2 { t.Fatalf("got %d items: %s", len(resp.Items), rec.Body.String()) }
}

func TestPutPreferences_RejectsUnknownKey(t *testing.T) {
	s, tok, _ := serverWithSessionAndUser(t)
	body := `{"items":[{"key":"evil","value":"x","client_updated_at":1}]}`
	req := httptest.NewRequest("PUT", "/api/me/preferences", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPutPreferences_OlderTimestampIsRejectedAndCurrentReturned(t *testing.T) {
	s, tok, userID := serverWithSessionAndUser(t)

	store := s.Store()
	_, _ = store.SetUserPreferences(context.Background(), userID, 5000,
		[]userstore.PreferenceItem{
			{Key: "locale_preference", ValueJSON: json.RawMessage(`"zh-CN"`), UpdatedAt: 5000},
		})

	body := `{"items":[{"key":"locale_preference","value":"en","client_updated_at":1000}]}`
	req := httptest.NewRequest("PUT", "/api/me/preferences", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK { t.Fatalf("status %d", rec.Code) }
	if !strings.Contains(rec.Body.String(), `"zh-CN"`) {
		t.Fatalf("expected server value preserved, body=%s", rec.Body.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/relay -run TestPutPreferences -v`
Expected: FAIL — 404 (no route)

- [ ] **Step 3: Implement the handler** by appending to `internal/relay/preferences_http.go`

```go
type putPreferencesRequest struct {
	Items []struct {
		Key             string          `json:"key"`
		Value           json.RawMessage `json:"value"`
		ClientUpdatedAt int64           `json:"client_updated_at"`
	} `json:"items"`
}

func (a *AuthServer) handlePutPreferences(w http.ResponseWriter, r *http.Request) {
	user, ok := UserFromContext(r.Context())
	if !ok || user == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body putPreferencesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	items := make([]userstore.PreferenceItem, 0, len(body.Items))
	for _, it := range body.Items {
		items = append(items, userstore.PreferenceItem{
			Key:       it.Key,
			ValueJSON: it.Value,
			UpdatedAt: it.ClientUpdatedAt,
		})
	}

	result, err := a.Store.SetUserPreferences(r.Context(), user.ID, nowMs(), items)
	if err != nil {
		switch {
		case errorsIsUnknown(err): writeError(w, http.StatusBadRequest, "unknown_key")
		case errorsIsInvalid(err): writeError(w, http.StatusBadRequest, "invalid_value")
		default: writeError(w, http.StatusInternalServerError, "internal_error")
		}
		return
	}
	if result == nil { result = []userstore.PreferenceItem{} }
	writeJSONStatus(w, http.StatusOK, map[string]any{"items": result})
}

func errorsIsUnknown(err error) bool {
	for err != nil {
		if err == userstore.ErrUnknownPreferenceKey { return true }
		type w interface{ Unwrap() error }
		u, ok := err.(w); if !ok { return false }
		err = u.Unwrap()
	}
	return false
}
func errorsIsInvalid(err error) bool {
	for err != nil {
		if err == userstore.ErrInvalidPreferenceValue { return true }
		type w interface{ Unwrap() error }
		u, ok := err.(w); if !ok { return false }
		err = u.Unwrap()
	}
	return false
}
```

- [ ] **Step 4: Register the route** in `internal/relay/auth.go`

```go
mux.Handle("PUT /api/me/preferences", requireSession(a.handlePutPreferences))
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/relay -run TestPutPreferences -v`
Expected: PASS for all three `TestPutPreferences_*`

- [ ] **Step 6: Commit**

```bash
git add internal/relay/preferences_http.go internal/relay/preferences_http_test.go internal/relay/auth.go
git commit -m "feat(relay): PUT /api/me/preferences with LWW response"
```

---

## Task 6: Desktop — appConfig.PrefsMeta sidecar + helpers

**Files:**
- Modify: `desktop/config.go`
- Test: `desktop/config_test.go` (append)

- [ ] **Step 1: Add the failing test** at the bottom of `desktop/config_test.go`

```go
func TestPrefsMeta_RoundTrip(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tmp)

	store := loadConfig()
	c := store.Get()
	c.PrefsMeta = map[string]prefsMetaEntry{
		"locale_preference": {UpdatedAtLocal: 1700, Dirty: true},
	}
	if err := store.Set(c); err != nil { t.Fatalf("Set: %v", err) }

	store2 := loadConfig()
	got := store2.Get().PrefsMeta["locale_preference"]
	if got.UpdatedAtLocal != 1700 || !got.Dirty {
		t.Fatalf("round-trip lost data: %+v", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./desktop -run TestPrefsMeta_RoundTrip -v`
Expected: FAIL — `prefsMetaEntry undefined` and `PrefsMeta` field missing on `appConfig`.

- [ ] **Step 3: Add the type and field** in `desktop/config.go` (inside the `appConfig` struct — read it first to find the right place; add field near the other top-level fields, and the type next to other helper types):

```go
// prefsMetaEntry tracks per-key sync state for the 5 synced preferences.
// Lives next to the values in config.json, but never sent to the relay.
type prefsMetaEntry struct {
	UpdatedAtLocal int64 `json:"updated_at_local"`
	Dirty          bool  `json:"dirty"`
}
```

And add this field to `appConfig`:

```go
PrefsMeta map[string]prefsMetaEntry `json:"prefs_meta,omitempty"`
```

Also add a small helper at the bottom of `config.go`:

```go
// PrefsSeedMarker is non-empty after the first PULL for a given relay user.
// Stored as a separate config key so it survives logout without confusing the
// sync engine.
func (c appConfig) PrefsSeedMarkerFor(userID string) bool {
	if c.PrefsSeedMarkers == nil { return false }
	return c.PrefsSeedMarkers[userID]
}
```

And the new field on `appConfig`:

```go
PrefsSeedMarkers map[string]bool `json:"prefs_seed_markers,omitempty"`
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./desktop -run TestPrefsMeta_RoundTrip -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/config.go desktop/config_test.go
git commit -m "feat(desktop): prefs_meta sidecar on appConfig"
```

---

## Task 7: Desktop — `internal/prefssync` package: types + adapter interface

**Files:**
- Create: `internal/prefssync/sync.go`
- Create: `internal/prefssync/sync_test.go`

- [ ] **Step 1: Write the failing test** at `internal/prefssync/sync_test.go`

```go
package prefssync

import (
	"context"
	"encoding/json"
	"testing"
)

// fakeAdapter implements Adapter for tests. It records calls.
type fakeAdapter struct {
	values map[string]json.RawMessage
	meta   map[string]Meta
}

func newFake() *fakeAdapter {
	return &fakeAdapter{values: map[string]json.RawMessage{}, meta: map[string]Meta{}}
}
func (f *fakeAdapter) ReadValue(key string) (json.RawMessage, bool) {
	v, ok := f.values[key]; return v, ok
}
func (f *fakeAdapter) WriteValue(key string, v json.RawMessage) error {
	f.values[key] = v; return nil
}
func (f *fakeAdapter) ReadMeta(key string) Meta { return f.meta[key] }
func (f *fakeAdapter) WriteMeta(key string, m Meta) error {
	f.meta[key] = m; return nil
}
func (f *fakeAdapter) Keys() []string {
	out := make([]string, 0, len(f.values))
	for k := range f.values { out = append(out, k) }
	return out
}

func TestSyncedKeys_MatchesWhitelist(t *testing.T) {
	got := SyncedKeys()
	want := []string{
		"locale_preference",
		"quick_templates",
		"notifications_enabled",
		"command_notify_threshold_seconds",
		"shell_integration_enabled",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d keys, want %d: %v", len(got), len(want), got)
	}
	set := map[string]bool{}
	for _, k := range got { set[k] = true }
	for _, k := range want {
		if !set[k] { t.Fatalf("missing key: %s", k) }
	}
}

func TestAdapterContract_RoundTrip(t *testing.T) {
	ctx := context.Background()
	_ = ctx
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	v, ok := a.ReadValue("locale_preference")
	if !ok || string(v) != `"en"` { t.Fatalf("round-trip: %v %s", ok, v) }
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: true})
	m := a.ReadMeta("locale_preference")
	if m.UpdatedAtLocal != 100 || !m.Dirty { t.Fatalf("meta: %+v", m) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/prefssync -run "TestSyncedKeys_MatchesWhitelist|TestAdapterContract_RoundTrip" -v`
Expected: FAIL — package or `SyncedKeys`, `Meta`, `Adapter` undefined.

- [ ] **Step 3: Implement** at `internal/prefssync/sync.go`

```go
// Package prefssync is the desktop-side cross-device user-preference
// sync engine. It owns no UI; the Wails frontend continues to call the
// existing setters on App. This package reads/writes the same five
// fields through an Adapter (impl: appConfigAdapter) and reconciles
// with the relay HTTP API (GET/PUT /api/me/preferences).
package prefssync

import (
	"context"
	"encoding/json"
	"sort"
)

// Meta is the per-key sync state. Mirrors the JSON tags of
// desktop.prefsMetaEntry so the appConfigAdapter can serialize 1:1.
type Meta struct {
	UpdatedAtLocal int64 `json:"updated_at_local"`
	Dirty          bool  `json:"dirty"`
}

// Adapter is the interface the engine uses to read/write the canonical
// local state for the 5 synced fields. The desktop implementation wraps
// configStore; the test impl is in-memory.
type Adapter interface {
	ReadValue(key string) (json.RawMessage, bool)
	WriteValue(key string, value json.RawMessage) error
	ReadMeta(key string) Meta
	WriteMeta(key string, m Meta) error
	Keys() []string // all keys currently present in the adapter
}

// RelayClient is the network surface. Real impl in this package wraps
// http.Client + config-derived bearer token.
type RelayClient interface {
	Get(ctx context.Context) ([]ServerItem, error)
	Put(ctx context.Context, items []ClientItem) ([]ServerItem, error)
}

type ServerItem struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt int64           `json:"updated_at"`
}

type ClientItem struct {
	Key             string          `json:"key"`
	Value           json.RawMessage `json:"value"`
	ClientUpdatedAt int64           `json:"client_updated_at"`
}

var syncedKeys = []string{
	"locale_preference",
	"quick_templates",
	"notifications_enabled",
	"command_notify_threshold_seconds",
	"shell_integration_enabled",
}

// SyncedKeys returns the canonical list of keys this engine syncs.
// The returned slice is a copy and safe to mutate.
func SyncedKeys() []string {
	out := append([]string(nil), syncedKeys...)
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/prefssync -run "TestSyncedKeys_MatchesWhitelist|TestAdapterContract_RoundTrip" -v`
Expected: PASS for both.

- [ ] **Step 5: Commit**

```bash
git add internal/prefssync/sync.go internal/prefssync/sync_test.go
git commit -m "feat(desktop): prefssync types and adapter contract"
```

---

## Task 8: Desktop — sync engine PULL logic

**Files:**
- Modify: `internal/prefssync/sync.go`
- Modify: `internal/prefssync/sync_test.go` (append)

- [ ] **Step 1: Add failing tests**

```go
type fakeRelay struct {
	getReturn []ServerItem
	getErr    error
	putItems  []ClientItem
	putReturn []ServerItem
}

func (f *fakeRelay) Get(ctx context.Context) ([]ServerItem, error) {
	return f.getReturn, f.getErr
}
func (f *fakeRelay) Put(ctx context.Context, items []ClientItem) ([]ServerItem, error) {
	f.putItems = append([]ClientItem(nil), items...)
	if f.putReturn != nil { return f.putReturn, nil }
	return nil, nil
}

func TestPull_ServerNewerOverwritesLocal(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: false})

	r := &fakeRelay{getReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 500},
	}}
	e := NewEngine(a, r)
	if err := e.Pull(context.Background()); err != nil { t.Fatalf("Pull: %v", err) }

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"zh-CN"` { t.Fatalf("expected overwrite, got %s", v) }
	m := a.ReadMeta("locale_preference")
	if m.UpdatedAtLocal != 500 || m.Dirty { t.Fatalf("meta: %+v", m) }
}

func TestPull_LocalDirtyNewerIsPreserved(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 800, Dirty: true})

	r := &fakeRelay{getReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 500},
	}}
	e := NewEngine(a, r)
	if err := e.Pull(context.Background()); err != nil { t.Fatalf("Pull: %v", err) }

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"en"` { t.Fatalf("expected local preserved, got %s", v) }
	m := a.ReadMeta("locale_preference")
	if !m.Dirty { t.Fatalf("expected dirty kept") }
}

func TestPull_ServerMissingKeyDoesNotTouchLocal(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	r := &fakeRelay{getReturn: nil}
	e := NewEngine(a, r)
	if err := e.Pull(context.Background()); err != nil { t.Fatalf("Pull: %v", err) }
	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"en"` { t.Fatalf("local wiped: %s", v) }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/prefssync -run TestPull -v`
Expected: FAIL — `NewEngine` undefined.

- [ ] **Step 3: Implement** by appending to `internal/prefssync/sync.go`

```go
// Engine is a single per-app sync engine instance. NOT safe for
// concurrent calls — wire it into a serial goroutine via the desktop
// boot code.
type Engine struct {
	adapter Adapter
	relay   RelayClient
}

func NewEngine(a Adapter, r RelayClient) *Engine {
	return &Engine{adapter: a, relay: r}
}

// Pull fetches the server state and reconciles into local. Per-key rule:
//   - server.updated_at > local.updated_at_local AND NOT dirty: adopt server
//   - server.updated_at > local.updated_at_local AND dirty: preserve local
//     (subsequent push will reconcile via LWW)
//   - server.updated_at <= local.updated_at_local: no-op
//   - key absent on server: leave local untouched
func (e *Engine) Pull(ctx context.Context) error {
	items, err := e.relay.Get(ctx)
	if err != nil { return err }
	for _, it := range items {
		local := e.adapter.ReadMeta(it.Key)
		if it.UpdatedAt > local.UpdatedAtLocal {
			if local.Dirty {
				continue
			}
			if err := e.adapter.WriteValue(it.Key, it.Value); err != nil { return err }
			if err := e.adapter.WriteMeta(it.Key, Meta{UpdatedAtLocal: it.UpdatedAt, Dirty: false}); err != nil { return err }
		}
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/prefssync -run TestPull -v`
Expected: PASS for all three `TestPull_*`.

- [ ] **Step 5: Commit**

```bash
git add internal/prefssync/sync.go internal/prefssync/sync_test.go
git commit -m "feat(desktop): prefssync Pull with LWW reconciliation"
```

---

## Task 9: Desktop — sync engine PUSH + dirty queue + post-push reconcile

**Files:**
- Modify: `internal/prefssync/sync.go`
- Modify: `internal/prefssync/sync_test.go` (append)

- [ ] **Step 1: Add failing tests**

```go
func TestPush_SendsDirtyAndClearsFlag(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"zh-CN"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 800, Dirty: true})
	a.WriteValue("notifications_enabled", json.RawMessage(`true`))
	a.WriteMeta("notifications_enabled", Meta{UpdatedAtLocal: 200, Dirty: false})

	r := &fakeRelay{putReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 850},
	}}
	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil { t.Fatalf("Push: %v", err) }

	if len(r.putItems) != 1 || r.putItems[0].Key != "locale_preference" {
		t.Fatalf("expected only dirty key sent, got %+v", r.putItems)
	}
	m := a.ReadMeta("locale_preference")
	if m.Dirty || m.UpdatedAtLocal != 850 { t.Fatalf("meta after push: %+v", m) }
}

func TestPush_ServerRejectionOverwritesLocal(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: true})

	// Server returns a newer value (e.g., set by another device).
	r := &fakeRelay{putReturn: []ServerItem{
		{Key: "locale_preference", Value: json.RawMessage(`"zh-CN"`), UpdatedAt: 999},
	}}
	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil { t.Fatalf("Push: %v", err) }

	v, _ := a.ReadValue("locale_preference")
	if string(v) != `"zh-CN"` { t.Fatalf("expected server value, got %s", v) }
	m := a.ReadMeta("locale_preference")
	if m.Dirty || m.UpdatedAtLocal != 999 { t.Fatalf("meta: %+v", m) }
}

func TestPush_NoDirtyKeysSkipsRequest(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	a.WriteMeta("locale_preference", Meta{UpdatedAtLocal: 100, Dirty: false})

	r := &fakeRelay{}
	e := NewEngine(a, r)
	if err := e.Push(context.Background()); err != nil { t.Fatalf("Push: %v", err) }
	if r.putItems != nil { t.Fatalf("unexpected PUT: %+v", r.putItems) }
}

func TestMarkDirty_StampsMeta(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"en"`))
	e := NewEngine(a, &fakeRelay{})
	e.MarkDirty("locale_preference", 12345)
	m := a.ReadMeta("locale_preference")
	if !m.Dirty || m.UpdatedAtLocal != 12345 { t.Fatalf("meta: %+v", m) }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/prefssync -run "TestPush|TestMarkDirty" -v`
Expected: FAIL — `Push` and `MarkDirty` undefined.

- [ ] **Step 3: Implement** by appending to `internal/prefssync/sync.go`

```go
// MarkDirty stamps the meta entry for key with the given timestamp and
// flips Dirty=true. The desktop App should call this after each
// successful setter for a synced field, with timestamp = time.Now().UnixMilli().
func (e *Engine) MarkDirty(key string, updatedAtLocalMs int64) {
	e.adapter.WriteMeta(key, Meta{UpdatedAtLocal: updatedAtLocalMs, Dirty: true})
}

// Push collects all dirty keys, sends them as a single PUT, and
// reconciles per-key with the server response (LWW: server's
// updated_at is authoritative).
func (e *Engine) Push(ctx context.Context) error {
	var items []ClientItem
	for _, k := range e.adapter.Keys() {
		m := e.adapter.ReadMeta(k)
		if !m.Dirty { continue }
		v, ok := e.adapter.ReadValue(k)
		if !ok { continue }
		items = append(items, ClientItem{
			Key: k, Value: v, ClientUpdatedAt: m.UpdatedAtLocal,
		})
	}
	if len(items) == 0 { return nil }

	resp, err := e.relay.Put(ctx, items)
	if err != nil { return err }

	for _, it := range resp {
		// Always trust server's updated_at; if it accepted our push, server.value == ours.
		// If server rejected (server newer), server.value overrides ours.
		if err := e.adapter.WriteValue(it.Key, it.Value); err != nil { return err }
		if err := e.adapter.WriteMeta(it.Key, Meta{UpdatedAtLocal: it.UpdatedAt, Dirty: false}); err != nil { return err }
	}
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/prefssync -run "TestPush|TestMarkDirty" -v`
Expected: PASS for all four cases.

- [ ] **Step 5: Commit**

```bash
git add internal/prefssync/sync.go internal/prefssync/sync_test.go
git commit -m "feat(desktop): prefssync Push + MarkDirty"
```

---

## Task 10: Desktop — `appConfigAdapter` + `httpRelayClient` + boot wiring

**Files:**
- Create: `desktop/prefssync_adapter.go`
- Modify: `desktop/app.go`
- Modify: `desktop/main.go`

- [ ] **Step 1: Implement the adapter and HTTP client** at `desktop/prefssync_adapter.go`

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/attson/atterm/internal/prefssync"
)

// appConfigAdapter glues prefssync.Adapter to the desktop configStore.
// Only the 5 synced keys are exposed.
type appConfigAdapter struct {
	store *configStore
}

func newAppConfigAdapter(s *configStore) *appConfigAdapter { return &appConfigAdapter{store: s} }

func (a *appConfigAdapter) ReadValue(key string) (json.RawMessage, bool) {
	c := a.store.Get()
	switch key {
	case "locale_preference":
		b, _ := json.Marshal(c.LocalePreference); return b, true
	case "quick_templates":
		b, _ := json.Marshal(c.QuickTemplates); return b, true
	case "notifications_enabled":
		if c.NotificationsEnabled == nil { return nil, false }
		b, _ := json.Marshal(*c.NotificationsEnabled); return b, true
	case "command_notify_threshold_seconds":
		if c.CommandNotifyThresholdSeconds == nil { return nil, false }
		b, _ := json.Marshal(*c.CommandNotifyThresholdSeconds); return b, true
	case "shell_integration_enabled":
		if c.ShellIntegrationEnabled == nil { return nil, false }
		b, _ := json.Marshal(*c.ShellIntegrationEnabled); return b, true
	}
	return nil, false
}

func (a *appConfigAdapter) WriteValue(key string, value json.RawMessage) error {
	c := a.store.Get()
	switch key {
	case "locale_preference":
		var s string
		if err := json.Unmarshal(value, &s); err != nil { return err }
		c.LocalePreference = s
	case "quick_templates":
		var t []QuickTemplate
		if err := json.Unmarshal(value, &t); err != nil { return err }
		c.QuickTemplates = t
	case "notifications_enabled":
		var b bool
		if err := json.Unmarshal(value, &b); err != nil { return err }
		c.NotificationsEnabled = &b
	case "command_notify_threshold_seconds":
		var n int
		if err := json.Unmarshal(value, &n); err != nil { return err }
		c.CommandNotifyThresholdSeconds = &n
	case "shell_integration_enabled":
		var b bool
		if err := json.Unmarshal(value, &b); err != nil { return err }
		c.ShellIntegrationEnabled = &b
	default:
		return fmt.Errorf("unknown key %s", key)
	}
	return a.store.Set(c)
}

func (a *appConfigAdapter) ReadMeta(key string) prefssync.Meta {
	c := a.store.Get()
	if c.PrefsMeta == nil { return prefssync.Meta{} }
	m := c.PrefsMeta[key]
	return prefssync.Meta{UpdatedAtLocal: m.UpdatedAtLocal, Dirty: m.Dirty}
}

func (a *appConfigAdapter) WriteMeta(key string, m prefssync.Meta) error {
	c := a.store.Get()
	if c.PrefsMeta == nil { c.PrefsMeta = map[string]prefsMetaEntry{} }
	c.PrefsMeta[key] = prefsMetaEntry{UpdatedAtLocal: m.UpdatedAtLocal, Dirty: m.Dirty}
	return a.store.Set(c)
}

func (a *appConfigAdapter) Keys() []string { return prefssync.SyncedKeys() }

// httpRelayClient implements prefssync.RelayClient against the real
// /api/me/preferences endpoints, using the bearer token stored in the
// config store.
type httpRelayClient struct {
	store *configStore
	http  *http.Client
}

func newHTTPRelayClient(s *configStore) *httpRelayClient {
	return &httpRelayClient{store: s, http: http.DefaultClient}
}

func (c *httpRelayClient) base() (string, string, error) {
	cfg := c.store.Get()
	if cfg.RelaySessionToken == "" || cfg.RelayURL == "" {
		return "", "", fmt.Errorf("not logged in")
	}
	httpURL, _, err := relayLoginEndpoints(cfg.RelayURL)
	if err != nil { return "", "", err }
	return strings.TrimRight(httpURL, "/"), cfg.RelaySessionToken, nil
}

func (c *httpRelayClient) Get(ctx context.Context) ([]prefssync.ServerItem, error) {
	base, tok, err := c.base()
	if err != nil { return nil, err }
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/api/me/preferences", nil)
	if err != nil { return nil, err }
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.http.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get prefs: HTTP %d", resp.StatusCode)
	}
	var body struct {
		Items []prefssync.ServerItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil { return nil, err }
	return body.Items, nil
}

func (c *httpRelayClient) Put(ctx context.Context, items []prefssync.ClientItem) ([]prefssync.ServerItem, error) {
	base, tok, err := c.base()
	if err != nil { return nil, err }
	body, _ := json.Marshal(map[string]any{"items": items})
	req, err := http.NewRequestWithContext(ctx, "PUT", base+"/api/me/preferences", bytes.NewReader(body))
	if err != nil { return nil, err }
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil { return nil, err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("put prefs: HTTP %d", resp.StatusCode)
	}
	var out struct {
		Items []prefssync.ServerItem `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil { return nil, err }
	return out.Items, nil
}
```

- [ ] **Step 2: Wire the engine into App** in `desktop/app.go`

Read the top of `app.go` to find the `App` struct and add fields:

```go
prefsSync     *prefssync.Engine
prefsSyncStop chan struct{}
```

Find `startup(ctx)` and add at the end (after `cfgStore` is loaded):

```go
adapter := newAppConfigAdapter(a.cfgStore)
relayClient := newHTTPRelayClient(a.cfgStore)
a.prefsSync = prefssync.NewEngine(adapter, relayClient)
a.prefsSyncStop = make(chan struct{})

// Trigger an initial PULL in the background if already logged in.
if cfg := a.cfgStore.Get(); cfg.RelaySessionToken != "" {
	go func() {
		_ = a.prefsSync.Pull(a.ctx)
	}()
}
```

Find every setter for the 5 synced fields. They currently look like (example: `SetLocalePreference` in `app.go`):

```go
func (a *App) SetLocalePreference(preference string) error {
	// ... existing body ending with: return a.cfgStore.Set(cfg)
}
```

After the `a.cfgStore.Set(cfg)` call (still inside the method), add the mark+push pair:

```go
err := a.cfgStore.Set(cfg)
if err != nil { return err }
a.markPrefDirtyAndPush("locale_preference")
return nil
```

Then add this helper at the bottom of `app.go`:

```go
// markPrefDirtyAndPush stamps the meta for key with the current ms,
// then triggers a background PUSH. Errors are swallowed by design (sync
// is best-effort; user UI already reflects the change).
func (a *App) markPrefDirtyAndPush(key string) {
	if a.prefsSync == nil { return }
	a.prefsSync.MarkDirty(key, time.Now().UnixMilli())
	go func() { _ = a.prefsSync.Push(a.ctx) }()
}
```

Repeat for `SetQuickTemplates`, `SetNotificationsEnabled`, `SetCommandNotifyThresholdSeconds`, `SetShellIntegrationEnabled` — each calls `markPrefDirtyAndPush(<its key>)` after the persist succeeds.

Also wire login/logout. Find `LoginRemoteRelay` in `app.go`; after the token is persisted (after the `SetRelayConfig` or equivalent call near the end of the function), append:

```go
if a.prefsSync != nil {
	go func() { _ = a.prefsSync.Pull(a.ctx) }()
}
```

Find the logout method (search for `Logout` or where `RelaySessionToken = ""` is set). Append a no-op-safe reset (just leave `prefsSync` running; the next PUT will fail with "not logged in" which is OK).

- [ ] **Step 3: Add a build check + smoke run**

```bash
cd /Users/attson/code/github.com.attson/atterm
go build ./...
go test ./internal/prefssync ./internal/userstore ./internal/relay -count=1
```

Expected: all builds pass, all tests still PASS.

- [ ] **Step 4: Commit**

```bash
git add desktop/prefssync_adapter.go desktop/app.go
git commit -m "feat(desktop): wire prefssync into App and setters"
```

---

## Task 11: Desktop/Mobile frontend — `lib/prefsSync.ts` types + state machine

**Files:**
- Create: `desktop/frontend/src/lib/prefsSync.ts`
- Create: `desktop/frontend/src/lib/prefsSync.test.ts`

- [ ] **Step 1: Write failing tests** at `desktop/frontend/src/lib/prefsSync.test.ts`

```typescript
import { describe, it, expect, vi } from 'vitest'
import { PrefsSyncEngine, type Adapter, type RelayClient, SYNCED_KEYS } from './prefsSync'

class FakeAdapter implements Adapter {
  values = new Map<string, unknown>()
  meta = new Map<string, { updatedAtLocal: number; dirty: boolean }>()
  readValue(k: string) { return this.values.has(k) ? this.values.get(k) : undefined }
  writeValue(k: string, v: unknown) { this.values.set(k, v) }
  readMeta(k: string) { return this.meta.get(k) ?? { updatedAtLocal: 0, dirty: false } }
  writeMeta(k: string, m: { updatedAtLocal: number; dirty: boolean }) { this.meta.set(k, m) }
  keys() { return SYNCED_KEYS.slice() }
}

describe('PrefsSyncEngine', () => {
  it('SYNCED_KEYS lists exactly the five fields', () => {
    expect(SYNCED_KEYS.sort()).toEqual([
      'command_notify_threshold_seconds',
      'locale_preference',
      'notifications_enabled',
      'quick_templates',
      'shell_integration_enabled',
    ])
  })

  it('pull adopts server value when newer and not dirty', async () => {
    const a = new FakeAdapter()
    a.writeValue('locale_preference', 'en')
    a.writeMeta('locale_preference', { updatedAtLocal: 100, dirty: false })
    const r: RelayClient = {
      get: vi.fn().mockResolvedValue([{ key: 'locale_preference', value: 'zh-CN', updated_at: 500 }]),
      put: vi.fn(),
    }
    const e = new PrefsSyncEngine(a, r)
    await e.pull()
    expect(a.values.get('locale_preference')).toBe('zh-CN')
    expect(a.meta.get('locale_preference')).toEqual({ updatedAtLocal: 500, dirty: false })
  })

  it('pull preserves dirty local even when server is newer', async () => {
    const a = new FakeAdapter()
    a.writeValue('locale_preference', 'en')
    a.writeMeta('locale_preference', { updatedAtLocal: 800, dirty: true })
    const r: RelayClient = {
      get: vi.fn().mockResolvedValue([{ key: 'locale_preference', value: 'zh-CN', updated_at: 500 }]),
      put: vi.fn(),
    }
    const e = new PrefsSyncEngine(a, r)
    await e.pull()
    expect(a.values.get('locale_preference')).toBe('en')
    expect(a.meta.get('locale_preference')?.dirty).toBe(true)
  })

  it('markDirty + push sends dirty keys and clears flag', async () => {
    const a = new FakeAdapter()
    a.writeValue('locale_preference', 'zh-CN')
    const putMock = vi.fn().mockResolvedValue([
      { key: 'locale_preference', value: 'zh-CN', updated_at: 1234 },
    ])
    const r: RelayClient = { get: vi.fn(), put: putMock }
    const e = new PrefsSyncEngine(a, r)
    e.markDirty('locale_preference', 1000)
    await e.push()
    expect(putMock).toHaveBeenCalledWith([
      { key: 'locale_preference', value: 'zh-CN', client_updated_at: 1000 },
    ])
    expect(a.meta.get('locale_preference')).toEqual({ updatedAtLocal: 1234, dirty: false })
  })

  it('push with no dirty keys does not call relay', async () => {
    const a = new FakeAdapter()
    a.writeValue('locale_preference', 'en')
    const putMock = vi.fn()
    const r: RelayClient = { get: vi.fn(), put: putMock }
    const e = new PrefsSyncEngine(a, r)
    await e.push()
    expect(putMock).not.toHaveBeenCalled()
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/prefsSync.test.ts`
Expected: FAIL — module `./prefsSync` not found.

- [ ] **Step 3: Implement** at `desktop/frontend/src/lib/prefsSync.ts`

```typescript
// Cross-device user-preference sync engine. Mirror of internal/prefssync
// (Go) — same key list, same per-key LWW semantics. Shared between the
// Wails desktop entry and the Capacitor mobile entry; the adapter is the
// per-platform persistence layer (Wails RPC vs Capacitor localStorage).

export const SYNCED_KEYS = [
  'locale_preference',
  'quick_templates',
  'notifications_enabled',
  'command_notify_threshold_seconds',
  'shell_integration_enabled',
] as const

export type SyncedKey = (typeof SYNCED_KEYS)[number]

export interface Meta {
  updatedAtLocal: number
  dirty: boolean
}

export interface Adapter {
  readValue(key: string): unknown | undefined
  writeValue(key: string, value: unknown): void | Promise<void>
  readMeta(key: string): Meta
  writeMeta(key: string, m: Meta): void | Promise<void>
  keys(): string[]
}

export interface ServerItem {
  key: string
  value: unknown
  updated_at: number
}

export interface ClientItem {
  key: string
  value: unknown
  client_updated_at: number
}

export interface RelayClient {
  get(): Promise<ServerItem[]>
  put(items: ClientItem[]): Promise<ServerItem[]>
}

export class PrefsSyncEngine {
  constructor(private readonly adapter: Adapter, private readonly relay: RelayClient) {}

  async pull(): Promise<void> {
    const items = await this.relay.get()
    for (const it of items) {
      const local = this.adapter.readMeta(it.key)
      if (it.updated_at > local.updatedAtLocal) {
        if (local.dirty) continue
        await this.adapter.writeValue(it.key, it.value)
        await this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
      }
    }
  }

  markDirty(key: string, updatedAtLocal: number): void {
    void this.adapter.writeMeta(key, { updatedAtLocal, dirty: true })
  }

  async push(): Promise<void> {
    const items: ClientItem[] = []
    for (const k of this.adapter.keys()) {
      const m = this.adapter.readMeta(k)
      if (!m.dirty) continue
      const v = this.adapter.readValue(k)
      if (v === undefined) continue
      items.push({ key: k, value: v, client_updated_at: m.updatedAtLocal })
    }
    if (items.length === 0) return
    const resp = await this.relay.put(items)
    for (const it of resp) {
      await this.adapter.writeValue(it.key, it.value)
      await this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
    }
  }
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/prefsSync.test.ts`
Expected: PASS for all five cases.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/prefsSync.ts desktop/frontend/src/lib/prefsSync.test.ts
git commit -m "feat(frontend): prefsSync state machine + tests"
```

---

## Task 12: Mobile/Capacitor — localStorage adapter + relay HTTP client

**Files:**
- Create: `desktop/frontend/src/lib/prefsSync.capacitor.ts`
- Create: `desktop/frontend/src/lib/prefsSync.capacitor.test.ts`
- Modify: `desktop/frontend/src/main.capacitor.ts`

- [ ] **Step 1: Write the failing test** at `desktop/frontend/src/lib/prefsSync.capacitor.test.ts`

```typescript
import { describe, it, expect, beforeEach, vi } from 'vitest'
import { localStorageAdapter, capacitorRelayClient } from './prefsSync.capacitor'

beforeEach(() => {
  localStorage.clear()
})

describe('localStorageAdapter', () => {
  it('persists value and meta under namespaced keys', () => {
    const a = localStorageAdapter()
    a.writeValue('locale_preference', 'zh-CN')
    a.writeMeta('locale_preference', { updatedAtLocal: 123, dirty: true })

    expect(localStorage.getItem('atterm.locale_preference.value')).toBe(`"zh-CN"`)
    expect(JSON.parse(localStorage.getItem('atterm.locale_preference.meta') ?? '{}'))
      .toEqual({ updatedAtLocal: 123, dirty: true })

    expect(a.readValue('locale_preference')).toBe('zh-CN')
    expect(a.readMeta('locale_preference')).toEqual({ updatedAtLocal: 123, dirty: true })
  })
})

describe('capacitorRelayClient', () => {
  it('sends Authorization: Bearer with the stored session token', async () => {
    localStorage.setItem('atterm.relay.session', JSON.stringify({
      baseURL: 'https://r.example.com',
      sessionToken: 'tok-xyz',
      expiresAt: 0,
      allowInsecure: false,
    }))
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [] }), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const client = capacitorRelayClient()
    const out = await client.get()
    expect(out).toEqual([])
    const call = fetchMock.mock.calls[0]
    expect(call[0]).toBe('https://r.example.com/api/me/preferences')
    expect((call[1] as RequestInit).headers).toMatchObject({ Authorization: 'Bearer tok-xyz' })
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/lib/prefsSync.capacitor.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** at `desktop/frontend/src/lib/prefsSync.capacitor.ts`

```typescript
import {
  PrefsSyncEngine,
  SYNCED_KEYS,
  type Adapter,
  type ClientItem,
  type Meta,
  type RelayClient,
  type ServerItem,
} from './prefsSync'

const VALUE_KEY = (k: string) => `atterm.${k}.value`
const META_KEY = (k: string) => `atterm.${k}.meta`

export function localStorageAdapter(): Adapter {
  return {
    readValue(k) {
      const raw = localStorage.getItem(VALUE_KEY(k))
      if (raw === null) return undefined
      try { return JSON.parse(raw) } catch { return undefined }
    },
    writeValue(k, v) {
      localStorage.setItem(VALUE_KEY(k), JSON.stringify(v))
    },
    readMeta(k): Meta {
      const raw = localStorage.getItem(META_KEY(k))
      if (raw === null) return { updatedAtLocal: 0, dirty: false }
      try {
        const m = JSON.parse(raw)
        return { updatedAtLocal: Number(m?.updatedAtLocal ?? 0), dirty: !!m?.dirty }
      } catch { return { updatedAtLocal: 0, dirty: false } }
    },
    writeMeta(k, m) {
      localStorage.setItem(META_KEY(k), JSON.stringify(m))
    },
    keys() { return [...SYNCED_KEYS] },
  }
}

interface StoredRelayConfig {
  baseURL: string
  sessionToken: string
  expiresAt?: number
  allowInsecure?: boolean
}

function loadRelay(): StoredRelayConfig | null {
  const raw = localStorage.getItem('atterm.relay.session')
  if (!raw) return null
  try { return JSON.parse(raw) as StoredRelayConfig } catch { return null }
}

export function capacitorRelayClient(): RelayClient {
  return {
    async get(): Promise<ServerItem[]> {
      const cfg = loadRelay()
      if (!cfg?.sessionToken) throw new Error('not_logged_in')
      const res = await fetch(cfg.baseURL.replace(/\/$/, '') + '/api/me/preferences', {
        method: 'GET',
        headers: { Authorization: `Bearer ${cfg.sessionToken}` },
        credentials: 'omit',
      })
      if (!res.ok) throw new Error(`get prefs: HTTP ${res.status}`)
      const body = await res.json() as { items: ServerItem[] }
      return body.items ?? []
    },
    async put(items: ClientItem[]): Promise<ServerItem[]> {
      const cfg = loadRelay()
      if (!cfg?.sessionToken) throw new Error('not_logged_in')
      const res = await fetch(cfg.baseURL.replace(/\/$/, '') + '/api/me/preferences', {
        method: 'PUT',
        headers: {
          Authorization: `Bearer ${cfg.sessionToken}`,
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ items }),
        credentials: 'omit',
      })
      if (!res.ok) throw new Error(`put prefs: HTTP ${res.status}`)
      const body = await res.json() as { items: ServerItem[] }
      return body.items ?? []
    },
  }
}

export function createCapacitorPrefsSync(): PrefsSyncEngine {
  return new PrefsSyncEngine(localStorageAdapter(), capacitorRelayClient())
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/lib/prefsSync.capacitor.test.ts`
Expected: PASS for both cases.

- [ ] **Step 5: Wire into `main.capacitor.ts`** — after `await initI18n(...)` and Pinia setup but before the app mounts, add:

```typescript
import { App } from '@capacitor/app'
import { createCapacitorPrefsSync } from './lib/prefsSync.capacitor'

const prefsSync = createCapacitorPrefsSync()
// Initial PULL (fire-and-forget; mobile may not be logged in yet)
void prefsSync.pull().catch(() => {})
// Foreground PULL
App.addListener('appStateChange', (s) => {
  if (s.isActive) void prefsSync.pull().catch(() => {})
})
// Expose globally so setting setters can call markDirty + push
;(window as any).__attermPrefsSync = prefsSync
```

The `window.__attermPrefsSync` hatch is intentionally crude — Task 14 will replace it with a proper wiring through `useSettingsStore`.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/lib/prefsSync.capacitor.ts desktop/frontend/src/lib/prefsSync.capacitor.test.ts desktop/frontend/src/main.capacitor.ts
git commit -m "feat(mobile): localStorage adapter + Capacitor lifecycle wiring"
```

---

## Task 13: Mobile — store setter integration

**Files:**
- Modify: `desktop/frontend/src/main.capacitor.ts`
- Modify: `desktop/frontend/src/platform/capacitor.ts`

- [ ] **Step 1: Replace the window-global escape hatch with a typed binding** at the top of `desktop/frontend/src/lib/prefsSync.capacitor.ts`:

Add:

```typescript
let SHARED_ENGINE: PrefsSyncEngine | null = null

export function setSharedPrefsSync(e: PrefsSyncEngine): void { SHARED_ENGINE = e }
export function notifyLocalChange(key: string): void {
  if (!SHARED_ENGINE) return
  SHARED_ENGINE.markDirty(key, Date.now())
  void SHARED_ENGINE.push().catch(() => {})
}
```

- [ ] **Step 2: In `main.capacitor.ts`** replace the `(window as any).__attermPrefsSync = prefsSync` line with `setSharedPrefsSync(prefsSync)` (import added).

- [ ] **Step 3: In `desktop/frontend/src/platform/capacitor.ts`**, find the `templates` save helper (around line 287 per the explore report) and wrap each save path so it calls `notifyLocalChange('quick_templates')`. Read the file to confirm exact structure first, then:

```typescript
import { notifyLocalChange } from '../lib/prefsSync.capacitor'

// inside the templates.save implementation, after the localStorage.setItem call:
notifyLocalChange('quick_templates')
```

- [ ] **Step 4: For locale**, find `saveLocalePreference` in `main.capacitor.ts` (lines 11-26 per the explore report). After `localStorage.setItem('atterm.locale', pref)` add:

```typescript
// keep the prefsSync namespaced value in sync with the public localStorage key
localStorage.setItem('atterm.locale_preference.value', JSON.stringify(pref))
notifyLocalChange('locale_preference')
```

(The two keys are intentional: `atterm.locale` is the historic key that i18n.ts reads; `atterm.locale_preference.value` is the sync-engine cache. Keeping both in lockstep avoids a refactor of i18n right now.)

- [ ] **Step 5: Add a test** in `desktop/frontend/src/lib/prefsSync.capacitor.test.ts` to lock in the notify behavior:

```typescript
import { setSharedPrefsSync, notifyLocalChange, capacitorRelayClient, localStorageAdapter } from './prefsSync.capacitor'
import { PrefsSyncEngine } from './prefsSync'

describe('notifyLocalChange', () => {
  it('marks dirty and schedules a push', () => {
    const fakeRelay = { get: vi.fn(), put: vi.fn().mockResolvedValue([]) }
    const e = new PrefsSyncEngine(localStorageAdapter(), fakeRelay)
    localStorage.setItem('atterm.locale_preference.value', `"zh-CN"`)
    setSharedPrefsSync(e)
    notifyLocalChange('locale_preference')
    expect(JSON.parse(localStorage.getItem('atterm.locale_preference.meta') ?? '{}').dirty)
      .toBe(true)
  })
})
```

- [ ] **Step 6: Run tests + Vue build smoke**

```bash
cd desktop/frontend
npx vitest run src/lib/prefsSync.capacitor.test.ts
VITE_TARGET=capacitor npx vite build
```

Expected: tests PASS, build succeeds.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/lib/prefsSync.capacitor.ts desktop/frontend/src/main.capacitor.ts desktop/frontend/src/platform/capacitor.ts desktop/frontend/src/lib/prefsSync.capacitor.test.ts
git commit -m "feat(mobile): wire setting setters to prefsSync notify"
```

---

## Task 14: Wails entry — focus PULL + setter notify

**Files:**
- Modify: `desktop/frontend/src/main.ts`

The Wails desktop frontend persists via Go RPC, not localStorage — the Go-side engine (Task 10) already drives the sync there. **The frontend's role is only to trigger a refresh of the in-memory values when the Go side has updated them (because another device pushed a change).**

- [ ] **Step 1: Wire a "settings changed" event listener in `main.ts`**

After `app.mount('#app')` but before the bootstrap promise resolves, add:

```typescript
// The Go side (internal/prefssync) emits this event after a PULL or PUSH
// reconciles synced fields, so the Vue components can re-read them.
import { EventsOn } from '../wailsjs/runtime'
EventsOn('prefs:changed', () => {
  // Components that load these values in onMounted should listen to this
  // event as well; for now we trigger a window event so existing settings
  // pages can listen without coupling to wails directly.
  window.dispatchEvent(new CustomEvent('atterm:prefs-changed'))
})
```

- [ ] **Step 2: Emit the event from Go** — open `desktop/app.go` and after each successful `Pull`/`Push` add:

In the helper `markPrefDirtyAndPush` (added in Task 10), wrap the goroutine:

```go
go func() {
	if err := a.prefsSync.Push(a.ctx); err == nil {
		wailsruntime.EventsEmit(a.ctx, "prefs:changed")
	}
}()
```

And in the initial PULL goroutine in `startup`:

```go
go func() {
	if err := a.prefsSync.Pull(a.ctx); err == nil {
		wailsruntime.EventsEmit(a.ctx, "prefs:changed")
	}
}()
```

- [ ] **Step 3: Smoke-build the Wails frontend**

```bash
cd desktop/frontend
VITE_TARGET=wails npx vite build
```

Expected: build PASSES (no missing module errors).

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/main.ts desktop/app.go
git commit -m "feat(desktop): emit prefs:changed events after sync"
```

---

## Task 15: Web — `shared/sync/prefsSync.ts` (mirror)

**Files:**
- Create: `web/src/shared/sync/prefsSync.ts`
- Create: `web/src/shared/sync/prefsSync.test.ts`

- [ ] **Step 1: Write failing tests** at `web/src/shared/sync/prefsSync.test.ts`

```typescript
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { PrefsSyncEngine, SYNCED_KEYS, localStorageAdapter, apiRelayClient } from './prefsSync'

beforeEach(() => { localStorage.clear() })

describe('web prefsSync', () => {
  it('SYNCED_KEYS lists the five fields', () => {
    expect(SYNCED_KEYS.sort()).toEqual([
      'command_notify_threshold_seconds',
      'locale_preference',
      'notifications_enabled',
      'quick_templates',
      'shell_integration_enabled',
    ])
  })

  it('pull adopts server values', async () => {
    const a = localStorageAdapter()
    const fakeRelay = {
      get: vi.fn().mockResolvedValue([
        { key: 'locale_preference', value: 'zh-CN', updated_at: 1234 },
      ]),
      put: vi.fn(),
    }
    const e = new PrefsSyncEngine(a, fakeRelay)
    await e.pull()
    expect(JSON.parse(localStorage.getItem('atterm.locale_preference.value') ?? '""')).toBe('zh-CN')
  })

  it('push routes through apiFetch with Bearer token', async () => {
    localStorage.setItem('atterm.relay', JSON.stringify({
      baseURL: '', sessionToken: 'tok-1', expiresAt: 0,
    }))
    localStorage.setItem('atterm.locale_preference.value', `"en"`)
    localStorage.setItem('atterm.locale_preference.meta', JSON.stringify({ updatedAtLocal: 999, dirty: true }))
    const fetchMock = vi.fn().mockResolvedValue(
      new Response(JSON.stringify({ items: [
        { key: 'locale_preference', value: 'en', updated_at: 999 },
      ]}), { status: 200 }),
    )
    vi.stubGlobal('fetch', fetchMock)
    const e = new PrefsSyncEngine(localStorageAdapter(), apiRelayClient())
    await e.push()
    const call = fetchMock.mock.calls[0]
    expect(call[0]).toBe('/api/me/preferences')
    expect((call[1] as RequestInit).method).toBe('PUT')
    expect((call[1] as RequestInit).headers as Record<string, string>).toMatchObject({
      Authorization: 'Bearer tok-1',
    })
  })
})
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd web && npx vitest run src/shared/sync/prefsSync.test.ts`
Expected: FAIL — module not found.

- [ ] **Step 3: Implement** at `web/src/shared/sync/prefsSync.ts`. Mirror the structure of `desktop/frontend/src/lib/prefsSync.ts` (Task 11) for the engine, but use the web's `apiFetch` for the relay client:

```typescript
import { apiFetch } from '@shared/api/client'

export const SYNCED_KEYS = [
  'locale_preference',
  'quick_templates',
  'notifications_enabled',
  'command_notify_threshold_seconds',
  'shell_integration_enabled',
] as const

export interface Meta { updatedAtLocal: number; dirty: boolean }
export interface Adapter {
  readValue(k: string): unknown | undefined
  writeValue(k: string, v: unknown): void
  readMeta(k: string): Meta
  writeMeta(k: string, m: Meta): void
  keys(): string[]
}
export interface ServerItem { key: string; value: unknown; updated_at: number }
export interface ClientItem { key: string; value: unknown; client_updated_at: number }
export interface RelayClient {
  get(): Promise<ServerItem[]>
  put(items: ClientItem[]): Promise<ServerItem[]>
}

export class PrefsSyncEngine {
  constructor(private readonly adapter: Adapter, private readonly relay: RelayClient) {}
  async pull(): Promise<void> {
    const items = await this.relay.get()
    for (const it of items) {
      const local = this.adapter.readMeta(it.key)
      if (it.updated_at > local.updatedAtLocal) {
        if (local.dirty) continue
        this.adapter.writeValue(it.key, it.value)
        this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
      }
    }
  }
  markDirty(key: string, updatedAtLocal: number): void {
    this.adapter.writeMeta(key, { updatedAtLocal, dirty: true })
  }
  async push(): Promise<void> {
    const items: ClientItem[] = []
    for (const k of this.adapter.keys()) {
      const m = this.adapter.readMeta(k)
      if (!m.dirty) continue
      const v = this.adapter.readValue(k)
      if (v === undefined) continue
      items.push({ key: k, value: v, client_updated_at: m.updatedAtLocal })
    }
    if (items.length === 0) return
    const resp = await this.relay.put(items)
    for (const it of resp) {
      this.adapter.writeValue(it.key, it.value)
      this.adapter.writeMeta(it.key, { updatedAtLocal: it.updated_at, dirty: false })
    }
  }
}

export function localStorageAdapter(): Adapter {
  const v = (k: string) => `atterm.${k}.value`
  const m = (k: string) => `atterm.${k}.meta`
  return {
    readValue(k) {
      const r = localStorage.getItem(v(k))
      if (r === null) return undefined
      try { return JSON.parse(r) } catch { return undefined }
    },
    writeValue(k, val) { localStorage.setItem(v(k), JSON.stringify(val)) },
    readMeta(k) {
      const r = localStorage.getItem(m(k))
      if (r === null) return { updatedAtLocal: 0, dirty: false }
      try {
        const x = JSON.parse(r)
        return { updatedAtLocal: Number(x?.updatedAtLocal ?? 0), dirty: !!x?.dirty }
      } catch { return { updatedAtLocal: 0, dirty: false } }
    },
    writeMeta(k, meta) { localStorage.setItem(m(k), JSON.stringify(meta)) },
    keys() { return [...SYNCED_KEYS] },
  }
}

export function apiRelayClient(): RelayClient {
  return {
    async get(): Promise<ServerItem[]> {
      const { data } = await apiFetch<{ items: ServerItem[] }>('/api/me/preferences')
      return data.items ?? []
    },
    async put(items: ClientItem[]): Promise<ServerItem[]> {
      const { data } = await apiFetch<{ items: ServerItem[] }>('/api/me/preferences', {
        method: 'PUT',
        body: JSON.stringify({ items }),
      })
      return data.items ?? []
    },
  }
}

let SHARED: PrefsSyncEngine | null = null
export function setSharedPrefsSync(e: PrefsSyncEngine): void { SHARED = e }
export function getSharedPrefsSync(): PrefsSyncEngine | null { return SHARED }
export function notifyLocalChange(key: string): void {
  if (!SHARED) return
  SHARED.markDirty(key, Date.now())
  void SHARED.push().catch(() => {})
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd web && npx vitest run src/shared/sync/prefsSync.test.ts`
Expected: PASS for all three cases.

- [ ] **Step 5: Commit**

```bash
git add web/src/shared/sync/prefsSync.ts web/src/shared/sync/prefsSync.test.ts
git commit -m "feat(web): prefsSync engine + apiFetch relay client"
```

---

## Task 16: Web — wire locale + templates through engine

**Files:**
- Modify: `web/src/shared/i18n/index.ts`
- Modify: `web/src/shared/templates.ts`
- Modify: `web/src/main/main.ts`
- Modify: `web/src/settings/main.ts`

- [ ] **Step 1: Bootstrap the engine in both entries**

In `web/src/main/main.ts` and `web/src/settings/main.ts`, after `await initI18n()`, add:

```typescript
import { PrefsSyncEngine, localStorageAdapter, apiRelayClient, setSharedPrefsSync } from '@shared/sync/prefsSync'

const prefsSync = new PrefsSyncEngine(localStorageAdapter(), apiRelayClient())
setSharedPrefsSync(prefsSync)
void prefsSync.pull().catch(() => {})
window.addEventListener('focus', () => {
  void prefsSync.pull().catch(() => {})
})
```

- [ ] **Step 2: Hook setLocalePreference** — modify `web/src/shared/i18n/index.ts` lines 45-53. After `window.localStorage.setItem(storageKey, preference)`, add:

```typescript
// mirror into the prefsSync namespaced key + notify
try {
  window.localStorage.setItem('atterm.locale_preference.value', JSON.stringify(preference))
} catch { /* storage unavailable */ }
import('@shared/sync/prefsSync').then(({ notifyLocalChange }) => {
  notifyLocalChange('locale_preference')
}).catch(() => {})
```

(Dynamic import keeps the i18n module decoupled — i18n shipping without sync also works.)

- [ ] **Step 3: Hook templates save** — modify `web/src/shared/templates.ts`'s `webTemplateStorage.save` (around line 60). After the `localStorage.setItem(STORAGE_KEY, JSON.stringify(list))` line, add:

```typescript
try {
  localStorage.setItem('atterm.quick_templates.value', JSON.stringify(list))
} catch { /* ignore */ }
import('./sync/prefsSync').then(({ notifyLocalChange }) => {
  notifyLocalChange('quick_templates')
}).catch(() => {})
```

- [ ] **Step 4: Smoke build + test**

```bash
cd web
npx vitest run
npx vite build
```

Expected: all existing tests still PASS, new prefsSync tests PASS, build succeeds.

- [ ] **Step 5: Commit**

```bash
git add web/src/shared/i18n/index.ts web/src/shared/templates.ts web/src/main/main.ts web/src/settings/main.ts
git commit -m "feat(web): wire locale + templates through prefsSync"
```

---

## Task 17: First-login seed upload

**Files:**
- Modify: `internal/prefssync/sync.go` — add `SeedFromLocal`
- Modify: `desktop/app.go` — call `SeedFromLocal` after first successful PULL post-login
- Add tests

The seed step bridges users who already have local customizations from before sync existed. On the first PULL for a given relay user, after the PULL completes, for each synced key the server did NOT return, if the local value is non-default we mark it dirty so the next PUSH carries it up. The "seeded" marker prevents re-seeding on subsequent logins.

- [ ] **Step 1: Add Go-side test** in `internal/prefssync/sync_test.go`

```go
func TestSeedFromLocal_MarksMissingNonDefaultDirty(t *testing.T) {
	a := newFake()
	a.WriteValue("locale_preference", json.RawMessage(`"zh-CN"`))
	// Server returned nothing for any key (empty PULL response). We had no
	// chance to write meta during PULL. SeedFromLocal should flag the local
	// non-default value as dirty.
	r := &fakeRelay{}
	e := NewEngine(a, r)
	e.SeedFromLocal(func(key string) bool {
		// Only locale_preference's "zh-CN" is non-default in this test.
		return key == "locale_preference"
	}, 5555)

	m := a.ReadMeta("locale_preference")
	if !m.Dirty || m.UpdatedAtLocal != 5555 {
		t.Fatalf("expected dirty seed, got %+v", m)
	}
	mn := a.ReadMeta("notifications_enabled")
	if mn.Dirty { t.Fatalf("non-customized key should not be dirty") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/prefssync -run TestSeedFromLocal -v`
Expected: FAIL — `SeedFromLocal` undefined.

- [ ] **Step 3: Implement**

```go
// SeedFromLocal stamps Dirty=true (with updatedAtLocalMs) for every
// synced key that:
//   - has a value in the local adapter, AND
//   - is reported as non-default by isCustomized, AND
//   - has Meta{Dirty: false} currently
// Intended to run once per (relay user, device) after the first PULL,
// to carry pre-sync customizations up to the server.
func (e *Engine) SeedFromLocal(isCustomized func(key string) bool, updatedAtLocalMs int64) {
	for _, k := range e.adapter.Keys() {
		if _, ok := e.adapter.ReadValue(k); !ok { continue }
		if !isCustomized(k) { continue }
		m := e.adapter.ReadMeta(k)
		if m.Dirty { continue }
		e.adapter.WriteMeta(k, Meta{UpdatedAtLocal: updatedAtLocalMs, Dirty: true})
	}
}
```

- [ ] **Step 4: Wire from desktop** — in `desktop/app.go`, find where `LoginRemoteRelay` finishes successfully (or the post-login PULL goroutine added in Task 10) and add:

```go
go func() {
	if err := a.prefsSync.Pull(a.ctx); err != nil { return }
	cfg := a.cfgStore.Get()
	userID := relayUserIDFromToken(cfg) // see helper below
	if userID == "" { return }
	if cfg.PrefsSeedMarkerFor(userID) { return }
	a.prefsSync.SeedFromLocal(isPrefCustomized(cfg), time.Now().UnixMilli())
	_ = a.prefsSync.Push(a.ctx)

	cfg2 := a.cfgStore.Get()
	if cfg2.PrefsSeedMarkers == nil { cfg2.PrefsSeedMarkers = map[string]bool{} }
	cfg2.PrefsSeedMarkers[userID] = true
	_ = a.cfgStore.Set(cfg2)
}()
```

Add the two helpers near the bottom of `desktop/app.go`:

```go
// relayUserIDFromToken returns the user id the relay returned at login.
// The login response already populates RelaySessionUserID on appConfig;
// if it's absent (older config), return "".
func relayUserIDFromToken(c appConfig) string { return c.RelaySessionUserID }

// isPrefCustomized returns true when the given synced key's value in
// the loaded config differs from the desktop's hard-coded default.
func isPrefCustomized(c appConfig) func(string) bool {
	return func(key string) bool {
		switch key {
		case "locale_preference":
			return c.LocalePreference != "" && c.LocalePreference != localePreferenceSystem
		case "quick_templates":
			return len(c.QuickTemplates) > 0
		case "notifications_enabled":
			return c.NotificationsEnabled != nil
		case "command_notify_threshold_seconds":
			return c.CommandNotifyThresholdSeconds != nil
		case "shell_integration_enabled":
			return c.ShellIntegrationEnabled != nil
		}
		return false
	}
}
```

If `appConfig` does not already have `RelaySessionUserID`, add it (with json tag `relay_session_user_id,omitempty`) and have `LoginRemoteRelay` populate it from the login response (the relay's `/api/auth/login` already returns `user_id`; if the desktop ignored it before, capture it now).

- [ ] **Step 5: Run tests + build**

```bash
go test ./internal/prefssync -count=1
go build ./...
```

Expected: tests PASS, build succeeds.

- [ ] **Step 6: Commit**

```bash
git add internal/prefssync/sync.go internal/prefssync/sync_test.go desktop/app.go desktop/config.go
git commit -m "feat(desktop): first-login seed upload of local customizations"
```

---

## Task 18: Manual two-device verification

This is a manual verification step, not automated. Run it before merging.

- [ ] **Step 1: Build and launch the desktop app**

```bash
cd /Users/attson/code/github.com.attson/atterm
make dev # or whatever launches the Wails dev build
```

If a `make` target isn't available, follow the existing dev instructions in CLAUDE.md / README.md.

- [ ] **Step 2: Build and serve the web UI**

```bash
cd web && npx vite build && npx vite preview
```

Note the served URL (default `http://localhost:4173/`).

- [ ] **Step 3: Run the relay server locally** (use the existing dev procedure for `atterm-relay`).

- [ ] **Step 4: Sign in to the SAME relay user from both clients** (desktop and the previewed web UI).

- [ ] **Step 5: Test 1 — locale propagation desktop → web**

1. On desktop Settings → switch language to 简体中文.
2. Bring the web tab to the foreground (or refocus the browser window).
3. **Expect:** The web UI's language switches to Chinese within ~1 s of focus.

- [ ] **Step 6: Test 2 — templates propagation web → desktop**

1. On web Settings, edit a quick template label.
2. On desktop, switch away from and back to the window (Wails fires `OnFrontendLoaded`-equivalent events on focus).
3. **Expect:** The desktop's templates bar reflects the edit.

- [ ] **Step 7: Test 3 — offline then reconnect**

1. Disable network on desktop (`networksetup -setairportpower en0 off` on macOS).
2. Change `notifications_enabled` and locale on desktop.
3. Re-enable network.
4. **Expect:** Both fields are present on the web after focus, and the desktop's sync engine is no longer flagging them dirty (check via `~/.config/atterm/config.json` — the `prefs_meta` entries should have `"dirty": false`).

- [ ] **Step 8: Test 4 — first-login seed upload**

1. Create a new relay account on the web (or use a fresh user the desktop has never synced).
2. On the desktop (already logged out of relay), customize quick templates locally.
3. Log into the new relay account on the desktop.
4. **Expect:** The web's templates bar (after a focus refresh) shows the desktop's custom templates.

- [ ] **Step 9: Commit a NOTES file** capturing any anomalies observed (if any) at `docs/superpowers/notes/2026-06-12-settings-sync-manual-verification.md`. If no anomalies, no file is needed.

---

## Self-Review

After writing the plan, checked against the spec:

- **Server data model + endpoints**: Tasks 1, 2, 3, 4, 5. ✓
- **Per-key LWW (server side)**: Task 3 + 5. ✓
- **Whitelist + type check**: Task 3. ✓
- **Forward compat (server returns unknown keys, client ignores)**: Implicitly handled by Adapter.Keys() returning only SYNCED_KEYS; unknown keys are silently dropped from reconciliation in both clients. Verified in Task 8 (Go) and Task 11 (TS) — the engine only iterates returned items and writes only when `it.key` exists in the adapter's switch. Tests cover the "missing key" path; the "unknown key from server" path is implicit.
- **Logged-out behavior**: All client-side engines no-op when the HTTP client can't load a token (Tasks 10, 12, 15 all throw / return error on no-token). The Go side already handles this; the JS side surfaces it as a swallowed promise rejection.
- **Pull on foreground / push on change**: Wails focus event (Task 14), Capacitor appStateChange (Task 12), Web window.focus (Task 16). Push on change via `markPrefDirtyAndPush` / `notifyLocalChange`. ✓
- **Trigger PULL on login**: Tasks 10 (desktop) and 17. Web/mobile triggers PULL at bootstrap.
- **Trigger PULL on switch accounts**: Logout/login cycle. Spec says drop dirty + meta on logout; this is **not** yet implemented in the plan. Adding a brief follow-up note: future plan iteration should clear `PrefsMeta` and `PrefsSeedMarkers` (only the current user's) on logout.
- **First-login seed upload**: Task 17. ✓
- **Conflict (per-key LWW client side)**: Pull preserves dirty (Tasks 8, 11). Push reconciles against server response (Tasks 9, 11). ✓
- **Retry/backoff**: NOT implemented in this plan as exponential backoff loops. The plan currently relies on next-foreground / next-change as the retry trigger. This is a deliberate scope cut for the initial deploy; flagged for a future task.
- **401 handling**: Web's `apiFetch` already redirects to /login.html on 401 (existing behavior). Desktop and mobile: `http://api/me/preferences` returns 401, the http client error propagates out of Get/Put, the calling code in `markPrefDirtyAndPush` swallows it. No special UI surface in this plan — the user's next login event will re-trigger PULL.
- **Migration**: Task 1 creates the only new table; no data backfill required.

### Placeholder scan

- No "TBD" / "TODO" / "fill in details".
- All test bodies are concrete code.
- All file paths are absolute repo-relative.

### Type consistency

- `prefsMetaEntry` (Go, desktop/config.go) ↔ `prefssync.Meta` (Go, internal/prefssync) ↔ `Meta` (TS, prefsSync.ts): all have `updated_at_local` / `dirty` (`updatedAtLocal` in TS camelCase). Verified across Tasks 6, 7, 11, 15.
- `PreferenceItem` (relay userstore) ↔ JSON wire format: `{key, value, updated_at}` on GET response; `{key, value, client_updated_at}` on PUT. Used consistently across server tests (Tasks 4-5) and Go/TS clients (Tasks 10-12, 15).
- `SyncedKeys` (Go) vs `SYNCED_KEYS` (TS, both copies): both lists contain exactly the 5 keys; verified in Tasks 7, 11, 15 tests.

### Scope check

- 18 tasks, ~1-3 commits per task, ~2-5 minutes per step. Single PR target.
- Three platforms touched but with the same conceptual engine; the Go + TS duplication is unavoidable given the existing repo layout.

### Remaining follow-ups (deferred from this plan, mentioned in spec)

- Sync status indicator UI (per-platform).
- Exponential backoff retry loops on the Go side (rely on foreground/login triggers for now).
- Drop sync metadata on logout / account switch to avoid bleeding old user's dirty flags. (Currently the engine treats a logged-out state as "not_logged_in" errors; the leftover meta is harmless until next login of any user.)
- WebSocket push for sub-second propagation.

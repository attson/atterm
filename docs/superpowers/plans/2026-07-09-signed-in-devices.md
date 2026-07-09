# Signed-in Devices Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a new **已登录设备** tab in Settings that lists every device signed into the same relay account (mirroring web's Signed-in Devices panel) with revoke and sign-out-others actions.

**Architecture:** Backend adds 3 Wails-bound `App` methods that call the relay's existing `/api/me/sessions*` endpoints via the existing `relayHTTPClient` + `relayHTTPBase` helpers (same pattern as `FetchRelayMe`). Frontend adds a new `SettingsDevices.vue` component and wires it as a new tab in `SettingsDialog.vue`. No relay-side changes.

**Tech Stack:** Go (`desktop/`, `internal/`), Vue 3 + TypeScript (`desktop/frontend/`), Wails v2.12.0, vitest.

**Spec:** `docs/superpowers/specs/2026-07-09-signed-in-devices-design.md`

## Global Constraints

- Endpoint contract (relay side, unchanged): `GET /api/me/sessions` → `RelaySessionRow[]`; `DELETE /api/me/sessions/{id_hash}` → 2xx; `POST /api/me/sessions/sign-out-others` → `{deleted: number}`.
- `RelaySessionRow` fields (JSON snake_case): `id_hash`, `user_agent`, `ip_prefix`, `created_at` (unix ms), `expires_at` (unix ms), `is_current`.
- All 3 backend methods take the `Authorization: Bearer <cfg.RelaySessionToken>` header and use `relayHTTPClient(cfg.AllowInsecureRelay, 0)` — same TLS / insecure policy as `FetchRelayMe` at `desktop/app.go:1904-1932`.
- Empty token (not logged in) → return `fmt.Errorf("not authenticated")`; a 401 response → return `fmt.Errorf("session expired, please log in again")`.
- Frontend distinguishes the two: on `not authenticated`, show a friendly "log in first" hint; other errors go to `.error`.
- Current-device row (`is_current=true`) never shows a revoke button — a dim `.current-tag` is shown instead.
- Sign-out-others button is disabled when the number of non-current rows is 0.
- Confirm every mutating action (`revoke`, `sign-out-others`) via `window.confirm`.
- User-facing prose Chinese; code / commits / comments English.
- **No mobile / Capacitor implementation.** `SettingsDialog.vue` is desktop-only. `platform/capacitor.ts` gets a stub for interface completeness.

---

## File Structure

| File | New / Modified | Purpose |
|---|---|---|
| `desktop/relay_sessions.go` | NEW | Types `RelaySessionRow`, `SignOutOthersResult`; three internal helpers `meSessionsGET` / `meSessionDELETE` / `meSessionsSignOutOthers`; shared `doRelayHTTP` helper. |
| `desktop/app.go` | MODIFIED | Three new Wails-bound App methods after `FetchRelayMe`. |
| `desktop/relay_sessions_test.go` | NEW | 5 backend tests using httptest. |
| `desktop/frontend/wailsjs/go/main/App.d.ts` | MODIFIED | Three generated declarations. |
| `desktop/frontend/wailsjs/go/main/App.js` | MODIFIED | Three runtime wrappers. |
| `desktop/frontend/src/lib/api.ts` | MODIFIED | Two interfaces (`RelaySessionRow`, `SignOutOthersResult`); three `AppBindings` methods; three top-level wrappers. |
| `desktop/frontend/src/components/SettingsDevices.vue` | NEW | The new panel. |
| `desktop/frontend/src/components/SettingsDialog.vue` | MODIFIED | Add `devices` tab to `SettingsTabId` union, `tabMeta`, `tabIcons`, nav-item button, panel render slot. |
| `desktop/frontend/src/i18n/messages/en.ts` | MODIFIED | Add `settings.tabs.devices` + 13 `settings.devices.*` keys. |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | MODIFIED | Same 14 keys, zh-CN copy. |
| `desktop/frontend/src/components/__tests__/SettingsDevices.test.ts` | NEW | 6 vitest cases. |

---

## Task 1: Backend — relay session endpoints + 5 Go tests

**Files:**
- Create: `desktop/relay_sessions.go`
- Modify: `desktop/app.go` (add three methods after `FetchRelayMe`, around line 1932)
- Create: `desktop/relay_sessions_test.go`

**Interfaces:**
- Consumes: existing helpers `relayHTTPClient(allowInsecure bool, timeout time.Duration) *http.Client` (`desktop/relay_httpclient.go:22`), `relayHTTPBase(relayURL string) string` (`desktop/app.go:2143`), `appConfig` fields `RelayURL`, `RelaySessionToken`, `AllowInsecureRelay`.
- Produces:
  - `RelaySessionRow` struct (JSON tag `id_hash`, `user_agent`, `ip_prefix`, `created_at`, `expires_at`, `is_current`).
  - `SignOutOthersResult` struct (JSON tag `deleted`).
  - `App.ListRelaySessions() ([]RelaySessionRow, error)`.
  - `App.RevokeRelaySession(idHash string) error`.
  - `App.SignOutOtherRelaySessions() (SignOutOthersResult, error)`.

- [ ] **Step 1: Write the failing tests**

Create `desktop/relay_sessions_test.go`:

```go
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newSessionsApp(t *testing.T, srv *httptest.Server) *App {
	t.Helper()
	a := newRelayTestApp(t)
	// Seed relay config so the App methods find a valid URL + token.
	if err := a.cfgStore.Set(appConfig{
		RelayURL:          strings.Replace(srv.URL, "http://", "ws://", 1),
		RelaySessionToken: "atk_test",
		RemotePermission:  "full",
	}); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	return a
}

func TestListRelaySessions_ParsesRows(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/me/sessions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer atk_test" {
			t.Errorf("Authorization = %q; want %q", got, "Bearer atk_test")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id_hash":"h1","user_agent":"UA-1","ip_prefix":"1.2.3","created_at":1700000000000,"expires_at":1710000000000,"is_current":true},
			{"id_hash":"h2","user_agent":"UA-2","ip_prefix":"4.5.6","created_at":1700100000000,"expires_at":1710100000000,"is_current":false}
		]`))
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	rows, err := a.ListRelaySessions()
	if err != nil {
		t.Fatalf("ListRelaySessions err: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len = %d; want 2", len(rows))
	}
	if rows[0].IDHash != "h1" || !rows[0].IsCurrent {
		t.Errorf("row[0] = %+v; want IDHash=h1 IsCurrent=true", rows[0])
	}
	if rows[1].IDHash != "h2" || rows[1].IsCurrent {
		t.Errorf("row[1] = %+v; want IDHash=h2 IsCurrent=false", rows[1])
	}
}

func TestListRelaySessions_EmptyTokenReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("should not hit relay when token is empty")
	}))
	defer srv.Close()

	a := newRelayTestApp(t)
	if err := a.cfgStore.Set(appConfig{
		RelayURL: strings.Replace(srv.URL, "http://", "ws://", 1),
		// RelaySessionToken deliberately empty.
	}); err != nil {
		t.Fatalf("seed cfg: %v", err)
	}
	_, err := a.ListRelaySessions()
	if err == nil {
		t.Fatalf("expected error; got nil")
	}
	if !strings.Contains(err.Error(), "not authenticated") {
		t.Errorf("err = %q; want it to contain 'not authenticated'", err.Error())
	}
}

func TestListRelaySessions_401_ReturnsFriendlyError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"expired"}`))
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	_, err := a.ListRelaySessions()
	if err == nil {
		t.Fatalf("expected error on 401; got nil")
	}
	if !strings.Contains(err.Error(), "session expired") {
		t.Errorf("err = %q; want 'session expired' phrasing", err.Error())
	}
}

func TestRevokeRelaySession_DELETEsCorrectPath(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	if err := a.RevokeRelaySession("abc"); err != nil {
		t.Fatalf("RevokeRelaySession err: %v", err)
	}
	if gotMethod != "DELETE" {
		t.Errorf("method = %q; want DELETE", gotMethod)
	}
	if gotPath != "/api/me/sessions/abc" {
		t.Errorf("path = %q; want /api/me/sessions/abc", gotPath)
	}
	if gotAuth != "Bearer atk_test" {
		t.Errorf("Authorization = %q; want %q", gotAuth, "Bearer atk_test")
	}
}

func TestSignOutOthers_ParsesDeletedCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q; want POST", r.Method)
		}
		if r.URL.Path != "/api/me/sessions/sign-out-others" {
			t.Errorf("path = %q; want /api/me/sessions/sign-out-others", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = json.NewEncoder(w).Encode(map[string]any{"deleted": 3})
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	res, err := a.SignOutOtherRelaySessions()
	if err != nil {
		t.Fatalf("SignOutOtherRelaySessions err: %v", err)
	}
	if res.Deleted != 3 {
		t.Errorf("Deleted = %d; want 3", res.Deleted)
	}
}

// A non-2xx non-401 response should surface a "relay returned NNN: ..."
// error so the frontend can display it verbatim.
func TestListRelaySessions_500_SurfacesRawError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom"))
	}))
	defer srv.Close()

	a := newSessionsApp(t, srv)
	_, err := a.ListRelaySessions()
	if err == nil {
		t.Fatalf("expected error on 500; got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %q; want mention of 500", err.Error())
	}
}

var _ = context.Background // silence unused import if all tests use httptest
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./desktop -run 'TestListRelaySessions|TestRevokeRelaySession|TestSignOutOthers' -v`
Expected: FAIL — `a.ListRelaySessions undefined`, etc.

- [ ] **Step 3: Create `desktop/relay_sessions.go`**

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// RelaySessionRow mirrors the relay's SessionRow shape used by
// GET /api/me/sessions.
type RelaySessionRow struct {
	IDHash    string `json:"id_hash"`
	UserAgent string `json:"user_agent"`
	IPPrefix  string `json:"ip_prefix"`
	CreatedAt int64  `json:"created_at"` // unix ms
	ExpiresAt int64  `json:"expires_at"` // unix ms
	IsCurrent bool   `json:"is_current"`
}

// SignOutOthersResult mirrors the response of POST
// /api/me/sessions/sign-out-others.
type SignOutOthersResult struct {
	Deleted int `json:"deleted"`
}

// meSessionsGET issues GET /api/me/sessions and parses the response.
func (a *App) meSessionsGET(ctx context.Context, base, token string, allowInsecure bool) ([]RelaySessionRow, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", strings.TrimRight(base, "/")+"/api/me/sessions", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	body, err := a.doRelayHTTP(req, allowInsecure)
	if err != nil {
		return nil, err
	}
	var rows []RelaySessionRow
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("parse sessions response: %w", err)
	}
	return rows, nil
}

// meSessionDELETE revokes one session by id_hash.
func (a *App) meSessionDELETE(ctx context.Context, base, token, idHash string, allowInsecure bool) error {
	u := strings.TrimRight(base, "/") + "/api/me/sessions/" + url.PathEscape(idHash)
	req, err := http.NewRequestWithContext(ctx, "DELETE", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	_, err = a.doRelayHTTP(req, allowInsecure)
	return err
}

// meSessionsSignOutOthers revokes every session except the current one.
func (a *App) meSessionsSignOutOthers(ctx context.Context, base, token string, allowInsecure bool) (SignOutOthersResult, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(base, "/")+"/api/me/sessions/sign-out-others", nil)
	if err != nil {
		return SignOutOthersResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	body, err := a.doRelayHTTP(req, allowInsecure)
	if err != nil {
		return SignOutOthersResult{}, err
	}
	var out SignOutOthersResult
	if err := json.Unmarshal(body, &out); err != nil {
		return SignOutOthersResult{}, fmt.Errorf("parse sign-out-others response: %w", err)
	}
	return out, nil
}

// doRelayHTTP issues req via the shared relay http client and returns
// the body on 2xx. 401 gets a friendly "session expired" message; other
// non-2xx codes surface verbatim so the frontend can display them.
func (a *App) doRelayHTTP(req *http.Request, allowInsecure bool) ([]byte, error) {
	// Same TLS + proxy policy as FetchRelayMe (desktop/app.go:1919).
	client := relayHTTPClient(allowInsecure, 0)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, fmt.Errorf("session expired, please log in again")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("relay returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}
```

- [ ] **Step 4: Add the three App methods to `desktop/app.go`**

Find `FetchRelayMe` (around line 1904). Add these three methods immediately after its closing brace:

```go
// ListRelaySessions returns every active session for the currently
// logged-in relay account. Bound to Settings → Signed-in Devices tab.
func (a *App) ListRelaySessions() ([]RelaySessionRow, error) {
	if a.cfgStore == nil {
		return nil, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return nil, fmt.Errorf("not authenticated")
	}
	return a.meSessionsGET(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, cfg.AllowInsecureRelay)
}

// RevokeRelaySession revokes one session by id_hash. The current
// session cannot be revoked through this method (the relay endpoint
// itself refuses to revoke the caller's own session, so no extra
// guard is needed here).
func (a *App) RevokeRelaySession(idHash string) error {
	idHash = strings.TrimSpace(idHash)
	if idHash == "" {
		return fmt.Errorf("id_hash is empty")
	}
	if a.cfgStore == nil {
		return fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return fmt.Errorf("not authenticated")
	}
	return a.meSessionDELETE(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, idHash, cfg.AllowInsecureRelay)
}

// SignOutOtherRelaySessions revokes every session except the current
// one. Returns the number of sessions revoked.
func (a *App) SignOutOtherRelaySessions() (SignOutOthersResult, error) {
	if a.cfgStore == nil {
		return SignOutOthersResult{}, fmt.Errorf("config store not ready")
	}
	cfg := a.cfgStore.Get()
	if cfg.RelayURL == "" || cfg.RelaySessionToken == "" {
		return SignOutOthersResult{}, fmt.Errorf("not authenticated")
	}
	return a.meSessionsSignOutOthers(a.ctx, relayHTTPBase(cfg.RelayURL), cfg.RelaySessionToken, cfg.AllowInsecureRelay)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./desktop -run 'TestListRelaySessions|TestRevokeRelaySession|TestSignOutOthers' -v`
Expected: PASS for all 6 tests (`TestListRelaySessions_ParsesRows`, `_EmptyTokenReturnsError`, `_401_ReturnsFriendlyError`, `_500_SurfacesRawError`, `TestRevokeRelaySession_DELETEsCorrectPath`, `TestSignOutOthers_ParsesDeletedCount`).

If any test fails on "relay URL is ws:// but scheme rewriter didn't fire": look at `relayHTTPBase` at `desktop/app.go:2143` and confirm it converts `ws://` → `http://`. The test seeds `RelayURL` as `ws://<httptest addr>` (converted from `srv.URL` which is `http://<addr>`).

- [ ] **Step 6: Run the full desktop test suite**

Run: `go test ./desktop`
Expected: PASS. No regressions in existing relay/uplink tests.

- [ ] **Step 7: Commit**

```bash
git add desktop/relay_sessions.go desktop/relay_sessions_test.go desktop/app.go
git commit -m "feat(desktop): ListRelaySessions + RevokeRelaySession + SignOutOtherRelaySessions"
```

---

## Task 2: Wails bindings + `lib/api.ts` wiring

**Files:**
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.js`
- Modify: `desktop/frontend/src/lib/api.ts`

**Interfaces:**
- Consumes (from Task 1): `App.ListRelaySessions() ([]RelaySessionRow, error)`, `App.RevokeRelaySession(string) error`, `App.SignOutOtherRelaySessions() (SignOutOthersResult, error)`.
- Produces:
  - TS interfaces `RelaySessionRow`, `SignOutOthersResult`.
  - Three `AppBindings` methods.
  - Three top-level wrappers: `listRelaySessions()`, `revokeRelaySession(idHash)`, `signOutOtherRelaySessions()`.

- [ ] **Step 1: Add wailsjs declarations to `App.d.ts`**

Edit `desktop/frontend/wailsjs/go/main/App.d.ts`. Find `FetchRelayMe`. Add immediately after it:

```ts
export function ListRelaySessions():Promise<Array<main.RelaySessionRow>>;

export function RevokeRelaySession(arg1:string):Promise<void>;

export function SignOutOtherRelaySessions():Promise<main.SignOutOthersResult>;
```

If `main.RelaySessionRow` and `main.SignOutOthersResult` don't yet appear in the imported models block, `wails build` will add them; for hand-syncing, add them to `desktop/frontend/wailsjs/go/models.ts` too:

```ts
export namespace main {
    // ...existing entries unchanged...
    export class RelaySessionRow {
        id_hash: string;
        user_agent: string;
        ip_prefix: string;
        created_at: number;
        expires_at: number;
        is_current: boolean;
        static createFrom(source: any = {}): RelaySessionRow {
            return new RelaySessionRow(source);
        }
        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source);
            this.id_hash = source["id_hash"];
            this.user_agent = source["user_agent"];
            this.ip_prefix = source["ip_prefix"];
            this.created_at = source["created_at"];
            this.expires_at = source["expires_at"];
            this.is_current = source["is_current"];
        }
    }
    export class SignOutOthersResult {
        deleted: number;
        static createFrom(source: any = {}): SignOutOthersResult {
            return new SignOutOthersResult(source);
        }
        constructor(source: any = {}) {
            if ('string' === typeof source) source = JSON.parse(source);
            this.deleted = source["deleted"];
        }
    }
}
```

(If your `wails build` produces slightly different class shapes, match the file's existing style — the point is that the two names exist under `namespace main`.)

- [ ] **Step 2: Add wailsjs runtime wrappers to `App.js`**

Edit `desktop/frontend/wailsjs/go/main/App.js`. Find `FetchRelayMe`. Add immediately after it:

```js
export function ListRelaySessions() {
  return window['go']['main']['App']['ListRelaySessions']();
}

export function RevokeRelaySession(arg1) {
  return window['go']['main']['App']['RevokeRelaySession'](arg1);
}

export function SignOutOtherRelaySessions() {
  return window['go']['main']['App']['SignOutOtherRelaySessions']();
}
```

- [ ] **Step 3: Add TS interfaces and bindings in `lib/api.ts`**

Edit `desktop/frontend/src/lib/api.ts`. Find `RelayMe` interface (around line 122). Add these two interfaces immediately after it:

```ts
export interface RelaySessionRow {
  id_hash: string;
  user_agent: string;
  ip_prefix: string;
  created_at: number;   // unix ms
  expires_at: number;   // unix ms
  is_current: boolean;
}

export interface SignOutOthersResult {
  deleted: number;
}
```

Find `AppBindings`. Add these three methods adjacent to `FetchRelayMe(): Promise<RelayMe>`:

```ts
ListRelaySessions(): Promise<RelaySessionRow[]>;
RevokeRelaySession(idHash: string): Promise<void>;
SignOutOtherRelaySessions(): Promise<SignOutOthersResult>;
```

- [ ] **Step 4: Export the top-level wrappers**

In the same file, find the existing `fetchRelayMe` wrapper. Add immediately after it:

```ts
export function listRelaySessions(): Promise<RelaySessionRow[]> {
  return bindings().ListRelaySessions();
}

export function revokeRelaySession(idHash: string): Promise<void> {
  return bindings().RevokeRelaySession(idHash);
}

export function signOutOtherRelaySessions(): Promise<SignOutOthersResult> {
  return bindings().SignOutOtherRelaySessions();
}
```

- [ ] **Step 5: TypeScript compile check**

Run: `cd desktop/frontend && npx vue-tsc --noEmit`
Expected: no new errors mentioning `ListRelaySessions`, `RevokeRelaySession`, `SignOutOtherRelaySessions`, `RelaySessionRow`, or `SignOutOthersResult`. Pre-existing unrelated errors OK.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/wailsjs/go/main/App.d.ts \
        desktop/frontend/wailsjs/go/main/App.js \
        desktop/frontend/wailsjs/go/models.ts \
        desktop/frontend/src/lib/api.ts
git commit -m "feat(desktop): wire ListRelaySessions + RevokeRelaySession + SignOutOtherRelaySessions"
```

---

## Task 3: `SettingsDevices.vue` + `SettingsDialog.vue` integration + i18n + 6 vitest tests

**Files:**
- Create: `desktop/frontend/src/components/SettingsDevices.vue`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`
- Create: `desktop/frontend/src/components/__tests__/SettingsDevices.test.ts`

**Interfaces:**
- Consumes (from Task 2): `listRelaySessions()`, `revokeRelaySession(idHash)`, `signOutOtherRelaySessions()`, and the TS types.
- Produces: user-visible **已登录设备** tab in Settings.

- [ ] **Step 1: Add i18n keys to `en.ts`**

Edit `desktop/frontend/src/i18n/messages/en.ts`. Add to `settings.tabs.*`:

```ts
devices: 'Signed-in devices',
```

Add a new `settings.devices` block:

```ts
devices: {
  hint: 'Devices signed in to this relay account.',
  refresh: 'Refresh',
  signOutOthers: 'Sign out other devices',
  signingOut: 'Signing out…',
  signOutOthersConfirm: 'Sign out every device except this one?',
  revoke: 'Revoke',
  revoking: 'Revoking…',
  revokeConfirm: 'Revoke this device ({ua})?',
  currentTag: 'Current',
  loginLine: 'Signed in {time} · {ip}',
  timeFormat: '{y}-{m}-{d} {hh}:{mm}',
  empty: 'No signed-in devices yet.',
  unknownUA: 'Unknown device',
  notAuthenticated: 'Log in to your relay account first (Settings → Relay).',
},
```

- [ ] **Step 2: Add the same keys to `zh-CN.ts`**

```ts
devices: '已登录设备',
```

```ts
devices: {
  hint: '此账号已登录的浏览器 / 桌面 / 移动设备。',
  refresh: '刷新',
  signOutOthers: '登出其他设备',
  signingOut: '登出中…',
  signOutOthersConfirm: '确定要登出除本机外的所有设备吗？',
  revoke: '撤销',
  revoking: '撤销中…',
  revokeConfirm: '确定要撤销此设备 ({ua}) 吗？',
  currentTag: '当前',
  loginLine: '登录于 {time} · {ip}',
  timeFormat: '{y}年{m}月{d}日 {hh}:{mm}',
  empty: '尚未有登录设备。',
  unknownUA: '未知设备',
  notAuthenticated: '请先在 "Relay" 页登录 relay 账号。',
},
```

- [ ] **Step 3: Write the failing frontend tests**

Create `desktop/frontend/src/components/__tests__/SettingsDevices.test.ts`:

```ts
import { mount, flushPromises } from '@vue/test-utils'
import { describe, it, expect, vi, beforeEach, afterEach, type MockInstance } from 'vitest'

vi.mock('../../i18n/useI18n', () => ({
  useI18n: () => ({
    t: (k: string, params?: Record<string, unknown>) => {
      if (!params) return k
      const pairs = Object.entries(params).map(([kk, vv]) => `${kk}=${vv}`).join(',')
      return `${k}[${pairs}]`
    },
  }),
}))

import SettingsDevices from '../SettingsDevices.vue'
import * as api from '../../lib/api'

function baseRows(): api.RelaySessionRow[] {
  return [
    { id_hash: 'h-me',    user_agent: 'UA-A', ip_prefix: '1.2.3', created_at: 1_700_000_000_000, expires_at: 1_710_000_000_000, is_current: true },
    { id_hash: 'h-other', user_agent: 'UA-B', ip_prefix: '4.5.6', created_at: 1_700_100_000_000, expires_at: 1_710_100_000_000, is_current: false },
  ]
}

beforeEach(() => {
  vi.spyOn(api, 'listRelaySessions').mockResolvedValue(baseRows() as never)
})

afterEach(() => {
  vi.restoreAllMocks()
})

describe('SettingsDevices', () => {
  let confirmSpy: MockInstance<[message?: string], boolean>

  beforeEach(() => {
    confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true)
  })

  afterEach(() => {
    confirmSpy.mockRestore()
  })

  it('renders one row per session; current row shows Current tag; others show Revoke', async () => {
    const w = mount(SettingsDevices)
    await flushPromises()
    const rows = w.findAll('.device-row')
    expect(rows.length).toBe(2)
    // Row 0 is current: has .current-tag, no .danger-btn.
    expect(rows[0].find('.current-tag').exists()).toBe(true)
    expect(rows[0].find('.danger-btn').exists()).toBe(false)
    // Row 1 is non-current: opposite.
    expect(rows[1].find('.current-tag').exists()).toBe(false)
    expect(rows[1].find('.danger-btn').exists()).toBe(true)
  })

  it('clicking refresh calls listRelaySessions again', async () => {
    const spy = api.listRelaySessions as unknown as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    expect(spy).toHaveBeenCalledTimes(1)
    await w.find('.icon-btn').trigger('click')
    await flushPromises()
    expect(spy).toHaveBeenCalledTimes(2)
  })

  it('revoke confirm=true calls revokeRelaySession then reloads', async () => {
    const revokeSpy = vi.spyOn(api, 'revokeRelaySession').mockResolvedValue()
    const listSpy = api.listRelaySessions as unknown as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    const revokeBtn = w.findAll('.danger-btn')[0]
    await revokeBtn.trigger('click')
    await flushPromises()
    expect(revokeSpy).toHaveBeenCalledWith('h-other')
    expect(listSpy).toHaveBeenCalledTimes(2) // initial + post-revoke
  })

  it('revoke confirm=false does not call revokeRelaySession', async () => {
    confirmSpy.mockReturnValue(false)
    const revokeSpy = vi.spyOn(api, 'revokeRelaySession').mockResolvedValue()
    const w = mount(SettingsDevices)
    await flushPromises()
    await w.findAll('.danger-btn')[0].trigger('click')
    await flushPromises()
    expect(revokeSpy).not.toHaveBeenCalled()
  })

  it('sign-out-others confirm=true calls API then reloads', async () => {
    const signSpy = vi.spyOn(api, 'signOutOtherRelaySessions').mockResolvedValue({ deleted: 2 } as never)
    const listSpy = api.listRelaySessions as unknown as ReturnType<typeof vi.fn>
    const w = mount(SettingsDevices)
    await flushPromises()
    const btns = w.findAll('button.secondary')
    const signOut = btns.find((b) => b.text().includes('settings.devices.signOutOthers'))!
    await signOut.trigger('click')
    await flushPromises()
    expect(signSpy).toHaveBeenCalledTimes(1)
    expect(listSpy).toHaveBeenCalledTimes(2)
  })

  it('not-authenticated error switches to hint copy; hides header actions', async () => {
    ;(api.listRelaySessions as unknown as ReturnType<typeof vi.fn>).mockReset()
    ;(api.listRelaySessions as unknown as ReturnType<typeof vi.fn>).mockRejectedValue(new Error('not authenticated'))
    const w = mount(SettingsDevices)
    await flushPromises()
    expect(w.text()).toContain('settings.devices.notAuthenticated')
    expect(w.find('.secondary').exists()).toBe(false)
    expect(w.find('.icon-btn').exists()).toBe(false)
  })
})
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd desktop/frontend && npx vitest run src/components/__tests__/SettingsDevices.test.ts`
Expected: FAIL — component does not exist yet.

- [ ] **Step 5: Create `SettingsDevices.vue`**

Create `desktop/frontend/src/components/SettingsDevices.vue`:

```vue
<script lang="ts" setup>
import { onMounted, ref } from "vue";
import {
  listRelaySessions,
  revokeRelaySession,
  signOutOtherRelaySessions,
  type RelaySessionRow,
} from "../lib/api";
import { useI18n } from "../i18n/useI18n";

const { t } = useI18n();

const rows = ref<RelaySessionRow[]>([]);
const loading = ref(true);
const refreshing = ref(false);
const signingOutOthers = ref(false);
const revoking = ref<Record<string, boolean>>({});
const error = ref("");
const notAuthed = ref(false);

async function reload(silent = false) {
  if (!silent) loading.value = true;
  refreshing.value = true;
  error.value = "";
  notAuthed.value = false;
  try {
    rows.value = await listRelaySessions();
  } catch (e: any) {
    const msg = e?.message ?? String(e);
    if (msg.includes("not authenticated")) {
      notAuthed.value = true;
    } else {
      error.value = msg;
    }
  } finally {
    loading.value = false;
    refreshing.value = false;
  }
}

onMounted(() => {
  void reload();
});

async function onRefresh() {
  await reload(true);
}

async function onRevoke(row: RelaySessionRow) {
  const ua = row.user_agent || t("settings.devices.unknownUA");
  if (!window.confirm(t("settings.devices.revokeConfirm", { ua }))) return;
  revoking.value = { ...revoking.value, [row.id_hash]: true };
  try {
    await revokeRelaySession(row.id_hash);
    await reload(true);
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    const next = { ...revoking.value };
    delete next[row.id_hash];
    revoking.value = next;
  }
}

async function onSignOutOthers() {
  if (!window.confirm(t("settings.devices.signOutOthersConfirm"))) return;
  signingOutOthers.value = true;
  try {
    await signOutOtherRelaySessions();
    await reload(true);
  } catch (e: any) {
    error.value = e?.message ?? String(e);
  } finally {
    signingOutOthers.value = false;
  }
}

function formatTime(unixMs: number): string {
  const d = new Date(unixMs);
  return t("settings.devices.timeFormat", {
    y: d.getFullYear(),
    m: d.getMonth() + 1,
    d: d.getDate(),
    hh: String(d.getHours()).padStart(2, "0"),
    mm: String(d.getMinutes()).padStart(2, "0"),
  });
}
</script>

<template>
  <div class="tab-pane">
    <div v-if="loading" class="dim">{{ t("common.loading") }}</div>
    <template v-else>
      <div class="header-row">
        <p class="hint">{{ t("settings.devices.hint") }}</p>
        <div v-if="!notAuthed" class="header-actions">
          <button
            type="button"
            class="icon-btn"
            :disabled="refreshing"
            :aria-label="t('settings.devices.refresh')"
            @click="onRefresh"
          >
            {{ refreshing ? "…" : "⟳" }}
          </button>
          <button
            type="button"
            class="secondary"
            :disabled="signingOutOthers || rows.filter((r) => !r.is_current).length === 0"
            @click="onSignOutOthers"
          >
            {{
              signingOutOthers
                ? t("settings.devices.signingOut")
                : t("settings.devices.signOutOthers")
            }}
          </button>
        </div>
      </div>

      <p v-if="notAuthed" class="hint">{{ t("settings.devices.notAuthenticated") }}</p>
      <p v-else-if="error" class="error">{{ error }}</p>
      <p v-else-if="rows.length === 0" class="dim">{{ t("settings.devices.empty") }}</p>

      <div v-else class="device-list">
        <div v-for="row in rows" :key="row.id_hash" class="device-row">
          <div class="device-info">
            <div class="ua">{{ row.user_agent || t("settings.devices.unknownUA") }}</div>
            <div class="meta">
              {{
                t("settings.devices.loginLine", {
                  time: formatTime(row.created_at),
                  ip: row.ip_prefix,
                })
              }}
            </div>
          </div>
          <span v-if="row.is_current" class="current-tag">
            {{ t("settings.devices.currentTag") }}
          </span>
          <button
            v-else
            type="button"
            class="danger-btn"
            :disabled="!!revoking[row.id_hash]"
            @click="onRevoke(row)"
          >
            {{
              revoking[row.id_hash]
                ? t("settings.devices.revoking")
                : t("settings.devices.revoke")
            }}
          </button>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.tab-pane {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.dim {
  color: var(--fg-dim);
  font-size: 13px;
}
.hint {
  margin: 0;
  color: var(--fg-dim);
  font-size: 12px;
  line-height: 1.5;
}
.error {
  color: var(--bad);
  font-size: 12px;
  margin: 0;
}
.header-row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
}
.header-actions {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-shrink: 0;
}
.icon-btn {
  width: 28px;
  height: 28px;
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 6px;
  background: transparent;
  color: var(--fg-dim);
  cursor: pointer;
  font-size: 14px;
}
.icon-btn:hover:not(:disabled) {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.04);
}
.icon-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
button.secondary {
  background: transparent;
  color: var(--fg-dim);
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
}
button.secondary:hover:not(:disabled) {
  color: var(--fg);
  background: rgba(255, 255, 255, 0.04);
}
button.secondary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
.device-list {
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--border);
}
.device-row {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px solid var(--border);
}
.device-info {
  flex: 1;
  min-width: 0;
}
.ua {
  color: var(--fg);
  font-size: 13px;
  overflow-wrap: anywhere;
}
.meta {
  color: var(--fg-dim);
  font-size: 12px;
  margin-top: 2px;
}
.current-tag {
  color: var(--fg-dim);
  font-size: 12px;
  padding: 2px 8px;
  border: 1px solid var(--border);
  border-radius: 999px;
  flex-shrink: 0;
}
.danger-btn {
  background: transparent;
  color: var(--bad);
  border: 1px solid var(--bad);
  border-radius: 6px;
  padding: 6px 12px;
  font-size: 13px;
  cursor: pointer;
  flex-shrink: 0;
}
.danger-btn:hover:not(:disabled) {
  background: color-mix(in srgb, var(--bad) 8%, transparent 92%);
}
.danger-btn:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>
```

- [ ] **Step 6: Wire the tab into `SettingsDialog.vue`**

Edit `desktop/frontend/src/components/SettingsDialog.vue`.

**a.** Add the import (near line 12, next to `SettingsRelay`):

```ts
import SettingsDevices from "./SettingsDevices.vue";
```

**b.** Extend the `SettingsTabId` union type — it appears in two places (lines 75 & 89). Add `devices` to both:

```ts
initialTab?: "general" | "relay" | "logging" | "updates" | "shortcuts" | "diagnostics" | "templates" | "tasks" | "feishu" | "devices";
```

```ts
const activeTab = ref<"general" | "relay" | "logging" | "updates" | "plugins" | "shortcuts" | "diagnostics" | "templates" | "tasks" | "feishu" | "devices">(initialTab);
```

**c.** Add entry to `tabMeta` (line 36 block):

```ts
devices:     { labelKey: "settings.tabs.devices",       english: "Signed-in devices" },
```

**d.** Add entry to `tabIcons` (line 58 block). Use a simple monitor+person SVG:

```ts
devices:     `<svg ${icoBase}><rect x="1.6" y="2.4" width="12.8" height="8" rx="1.4"/><path d="M4.8 14h6.4M8 10.4V14"/></svg>`,
```

**e.** Add a nav-item button in the template. Insert immediately after the existing `relay` button (before the `plugins` button, around line 236):

```html
<button
  class="settings-nav-item"
  :class="{ active: activeTab === 'devices' }"
  @click="switchTab('devices')"
>
  <span class="nav-icon" v-html="tabIcons.devices"></span>
  <span class="nav-label">{{ t("settings.tabs.devices") }}</span>
</button>
```

**f.** Add the component render in the panel section (around line 307, after the `SettingsRelay` render, before `SettingsUpdates`):

```html
<SettingsDevices v-if="activeTab === 'devices'" />
```

- [ ] **Step 7: Run the frontend tests to verify they pass**

Run: `cd desktop/frontend && npx vitest run src/components/__tests__/SettingsDevices.test.ts`
Expected: PASS — 6/6 tests.

If `renders one row per session` fails because `.current-tag` is not found on the current-row: check that the `v-if="row.is_current"` guard is on the tag, not on the button. If `revoke confirm=true` fails on `expect(revokeSpy).toHaveBeenCalledWith('h-other')`: check that `w.findAll('.danger-btn')[0]` corresponds to the non-current row (the current row has no `.danger-btn`, so `[0]` should already be the non-current row's button).

- [ ] **Step 8: Run the wider vitest suite**

Run: `cd desktop/frontend && npx vitest run`
Expected: PASS (or same pre-existing failures as before this task).

- [ ] **Step 9: Run `vue-tsc` to catch type regressions**

Run: `cd desktop/frontend && npx vue-tsc --noEmit`
Expected: no new errors mentioning `SettingsDevices`, `RelaySessionRow`, or `SignOutOthersResult`.

- [ ] **Step 10: Manual smoke test (recommended, not required to gate the commit)**

Run: `make dev`. In the app:
- Open Settings, verify **已登录设备** appears as a new tab between Relay and Plugins.
- Not logged in: the panel shows the "log in first" hint; no header actions.
- Logged in with only this device: shows one row with the "当前" tag.
- Logged in with multiple devices: current row has tag; others have Revoke.
- Click Revoke → confirm → row disappears after re-fetch.
- Click "登出其他设备" → confirm → non-current rows disappear.
- Click ⟳ → list re-fetches.

- [ ] **Step 11: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts \
        desktop/frontend/src/components/SettingsDevices.vue \
        desktop/frontend/src/components/SettingsDialog.vue \
        desktop/frontend/src/components/__tests__/SettingsDevices.test.ts
git commit -m "feat(desktop): Signed-in devices tab + revoke + sign-out-others + i18n + tests"
```

---

## Post-implementation checklist

- [ ] Three tasks committed as separate commits.
- [ ] `go test ./desktop` passes.
- [ ] `cd desktop/frontend && npx vitest run` passes.
- [ ] Branch ready for `superpowers:finishing-a-development-branch`.

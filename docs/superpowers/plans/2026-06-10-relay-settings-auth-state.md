# Settings → Relay Auth State Restore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** When a user reopens Settings → Relay after a successful remote login, immediately show the "已连接为 X" pill and prefill the email field — no more "looks like I'm logged out" state caused by Settings missing the one-shot uplink auth-info event.

**Architecture:** Persist the most recent successful login email in `appConfig.RelayLastEmail` (write-once on `LoginRemoteRelay` success). Expose it through the Wails `RelayConfig` payload. The `SettingsRelay.vue` mount path then (a) prefills the email input from `cfg.last_email` and (b) eagerly calls `fetchRelayMe()` when a session token exists, instead of waiting for the `relay:auth-info` event.

**Tech Stack:** Go (Wails), Vue 3 SFC + `<script setup>`, vitest, scoped Go config persistence.

**Spec:** `docs/superpowers/specs/2026-06-10-relay-settings-auth-state-design.md`.

---

## File structure

| File | Status | Purpose |
|---|---|---|
| `desktop/config.go` | modify | Add `RelayLastEmail string` field to `appConfig` |
| `desktop/app.go` | modify | Add `LastEmail string` to Wails `RelayConfig`; `GetRelayConfig` populates it; `LoginRemoteRelay` writes `cfg.RelayLastEmail` on success; `SetRelayConfig` ignores `LastEmail` (only `LoginRemoteRelay` may write it) |
| `desktop/frontend/src/lib/api.ts` | modify | Add `last_email: string` to TS `RelayConfig` interface |
| `desktop/frontend/wailsjs/go/models.ts` | modify | Add `last_email` field to auto-gen `RelayConfig` class (manual edit — no `wails` CLI in env) |
| `desktop/frontend/src/components/SettingsRelay.vue` | modify | `onMounted`: prefill `email.value` from `cfg.last_email`; if `cfg.token`, eagerly `await fetchRelayMe()` and set `connectedEmail` / `connectedUserID` |
| `desktop/frontend/src/components/SettingsRelay.test.ts` | modify | Add tests asserting (a) `cfg.last_email` prefill, (b) eager `fetchRelayMe` call when token present |

Single PR, single task with TDD steps.

---

## Task 1: Auth-state restore on Settings open

### Files

- Modify: `desktop/config.go`
- Modify: `desktop/app.go`
- Modify: `desktop/frontend/src/lib/api.ts`
- Modify: `desktop/frontend/wailsjs/go/models.ts`
- Modify: `desktop/frontend/src/components/SettingsRelay.vue`
- Modify: `desktop/frontend/src/components/SettingsRelay.test.ts`

### Step 1: Write the failing tests

In `desktop/frontend/src/components/SettingsRelay.test.ts`, append two new tests inside the existing `describe("SettingsRelay", () => { ... })` block (just before the closing `})`). Use the same source-string-assertion pattern every other test in the file uses (read SFC via `?raw` import or `fs.readFileSync` — match the local convention).

```ts
  test("onMounted prefills email from cfg.last_email", () => {
    // Settings → Relay reads cfg.last_email to prefill the login form
    // so users don't retype on every open. See spec §设计.
    expect(source).toContain("cfg.last_email");
    expect(source).toContain("email.value = cfg.last_email");
  });

  test("onMounted eagerly fetches /api/me when a session token is configured", () => {
    // Pull current identity straight away instead of waiting for the
    // relay:auth-info uplink event — that event is one-shot and
    // missed when Settings opens *after* uplink connect.
    expect(source).toContain("if (cfg.token)");
    expect(source).toContain("fetchRelayMe()");
    // The pill state must come from the fetched identity.
    expect(source).toContain("connectedEmail.value = me.email");
  });
```

If the existing tests share a top-level `source` variable, reuse it. If each test reads the file ad-hoc, mirror that exact shape. Don't introduce a new pattern.

### Step 2: Run tests, verify they fail

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/SettingsRelay.test.ts -t "onMounted" 2>&1 | tail -15
```

Expected: 2 FAIL — assertions like `expect(source).toContain("cfg.last_email")` fail because the Vue file doesn't reference `last_email` yet.

### Step 3: Add `RelayLastEmail` to `appConfig` (Go)

In `desktop/config.go`, find the `appConfig` struct. The relay-related fields cluster near the top (look for `RelaySessionToken` and `RelaySessionExpiresAt`). Add **immediately after** `RelaySessionExpiresAt`:

```go
	// RelayLastEmail caches the email used in the most recent successful
	// LoginRemoteRelay call. Only used to prefill the Settings → Relay
	// form on next open; never used for authentication itself. Plaintext
	// is fine because the email is non-secret and lives next to the
	// session token in the same config.json.
	RelayLastEmail string `json:"relay_last_email,omitempty"`
```

Match the existing 1-tab indentation. Don't reorder existing fields.

### Step 4: Add `LastEmail` to Wails `RelayConfig` (Go)

In `desktop/app.go`, find `type RelayConfig struct {` (around line 64). Add `LastEmail string` immediately after `RelayURL` / `Token` / `SessionExpiresAt` / `AllowInsecureRelay` / `RemotePermission` (i.e. before `Connected` which is the runtime-derived field). Concrete line:

```go
	LastEmail string `json:"last_email"`
```

Result (showing the field's neighbors for clarity — don't reorder existing fields):

```go
type RelayConfig struct {
	URL                string `json:"url"`
	Token              string `json:"token"`
	SessionExpiresAt   int64  `json:"session_expires_at"`
	AllowInsecureRelay bool   `json:"allow_insecure_relay"`
	RemotePermission   string `json:"remote_permission"`
	LastEmail          string `json:"last_email"`
	Connected          bool   `json:"connected"`
	Paused             bool   `json:"paused"`
}
```

### Step 5: Populate `LastEmail` in `GetRelayConfig`

In `desktop/app.go`, find `func (a *App) GetRelayConfig() RelayConfig {` (around line 247). The function builds and returns a `RelayConfig{...}` literal. Add `LastEmail: cfg.RelayLastEmail,` to that literal, next to the other fields:

```go
return RelayConfig{
	URL:                cfg.RelayURL,
	Token:              cfg.RelaySessionToken,
	SessionExpiresAt:   cfg.RelaySessionExpiresAt,
	AllowInsecureRelay: cfg.AllowInsecureRelay,
	RemotePermission:   cfg.RemotePermissionOrDefault(),
	LastEmail:          cfg.RelayLastEmail,
	Connected:          connected,
	Paused:             cfg.RelayPaused,
}
```

Use the actual field names from the existing literal (the surrounding code may differ slightly from this template).

### Step 6: Persist `LastEmail` in `LoginRemoteRelay` on success

In `desktop/app.go`, find `func (a *App) LoginRemoteRelay(relayURL, email, password string) error {` (around line 305). At the bottom, before the final `return a.SetRelayConfig(...)`, persist the email directly to the underlying store so it survives even if `SetRelayConfig` resets fields:

Replace the trailing block (from `prev := a.GetRelayConfig()` to `})`) with:

```go
	// Preserve unrelated relay-config fields (AllowInsecureRelay, RemotePermission)
	// so login doesn't silently reset them. SetRelayConfig also restarts the uplink.
	prev := a.GetRelayConfig()
	if err := a.SetRelayConfig(RelayConfig{
		URL:                wsURL,
		Token:              out.SessionToken,
		SessionExpiresAt:   out.ExpiresAt,
		AllowInsecureRelay: prev.AllowInsecureRelay,
		RemotePermission:   prev.RemotePermission,
	}); err != nil {
		return err
	}
	// Persist the email separately — RelayConfig.LastEmail is read-only
	// from the frontend's perspective (SetRelayConfig intentionally
	// ignores it), so LoginRemoteRelay writes the cfgStore directly.
	if a.cfgStore != nil {
		cfg := a.cfgStore.Get()
		cfg.RelayLastEmail = email
		if err := a.cfgStore.Set(cfg); err != nil {
			return err
		}
	}
	return nil
```

If `SetRelayConfig` already plumbs `SessionExpiresAt` itself or the existing literal differs, adapt — the point is to **add the cfgStore write of `RelayLastEmail` after a successful `SetRelayConfig`**.

Note: `SetRelayConfig` does NOT accept a `LastEmail` from the frontend. The Wails struct has the field, `GetRelayConfig` populates it for reads, but the `SetRelayConfig` body never copies `req.LastEmail` into `cfg.RelayLastEmail`. Verify this by reading `SetRelayConfig` (around line 268) — it should only touch the URL/Token/AllowInsecureRelay/RemotePermission fields. If you find it accidentally copies `req.LastEmail`, remove that line.

### Step 7: Add `last_email` to TS `RelayConfig` interface

In `desktop/frontend/src/lib/api.ts`, find `export interface RelayConfig {` (around line 36). Add `last_email: string;` next to the other fields (matching JSON tag order from Go):

```ts
export interface RelayConfig {
  url: string;
  token: string;
  session_expires_at: number;
  allow_insecure_relay: boolean;
  remote_permission: string;
  last_email: string;
  connected: boolean;
  paused: boolean;
}
```

Use the actual field list from the file; just insert `last_email: string;` in the right spot.

### Step 8: Sync `desktop/frontend/wailsjs/go/models.ts`

Find `export class RelayConfig {` (around line 403). The class has properties for each `json:"..."` tag and a constructor that copies them from a `source: any` object. Add `last_email: string;` alongside the other properties **and** add `this.last_email = source["last_email"];` in the constructor body. Mimic the exact style of the surrounding lines (this is an auto-generated file but we're hand-editing because the `wails` CLI isn't always available locally).

Example (showing only the additions; preserve all existing lines):

```ts
	export class RelayConfig {
	    url: string;
	    token: string;
	    session_expires_at: number;
	    allow_insecure_relay: boolean;
	    remote_permission: string;
	    last_email: string;        // NEW
	    connected: boolean;
	    paused: boolean;

	    static createFrom(source: any = {}) {
	        return new RelayConfig(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.url = source["url"];
	        this.token = source["token"];
	        this.session_expires_at = source["session_expires_at"];
	        this.allow_insecure_relay = source["allow_insecure_relay"];
	        this.remote_permission = source["remote_permission"];
	        this.last_email = source["last_email"];   // NEW
	        this.connected = source["connected"];
	        this.paused = source["paused"];
	    }
	}
```

### Step 9: Update `SettingsRelay.vue` `onMounted` for prefill + eager fetch

In `desktop/frontend/src/components/SettingsRelay.vue`, find the existing `onMounted(async () => { ... })` block (around lines 80-107 per the earlier read — search for `cfg.allow_insecure_relay` to locate it). After the existing `snapshotPersisted();` line and before the `platform.events.on('relay:auth-info', ...)` listener registration, insert:

```ts
  // Prefill email from persisted config (set by LoginRemoteRelay on
  // last successful login). Password stays empty for security.
  email.value = cfg.last_email ?? "";

  // Show the "logged in as X" pill immediately without waiting for the
  // uplink's relay:auth-info event. The event listener below stays
  // registered as a fallback (covers identity changes during the dialog
  // session — e.g., login from another device, admin promotion).
  if (cfg.token) {
    try {
      const me = await fetchRelayMe();
      connectedEmail.value = me.email || "";
      connectedUserID.value = me.id || "";
    } catch {
      // Token rejected (401) or network error — pill stays empty; the
      // uplink event stream and apiFetch's 401 interceptor will produce
      // an accurate state if/when the session is actually invalid.
    }
  }
```

Verify `fetchRelayMe` is already imported at the top of `<script setup>` — search for an existing `import { ... fetchRelayMe ... } from`. If not imported, add it to the existing API import statement (likely from `@/lib/api`).

Verify `connectedUserID` is already declared as a ref (it's referenced by the existing `relay:auth-info` event handler at line 99). If `connectedEmail` and `connectedUserID` are declared elsewhere in the script setup, reuse them as-is.

### Step 10: Run tests, verify they pass

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/SettingsRelay.test.ts 2>&1 | tail -15
```

Expected: all tests PASS (including the 2 new "onMounted" tests).

### Step 11: Type-check + full vitest

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit 2>&1 | tail -5
```

Expected: clean (no output).

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test 2>&1 | tail -10
```

Expected: all PASS (count should be 992 + 2 = 994, matching the existing test inventory plus our 2 new tests).

### Step 12: Go side: build + relevant unit tests

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./... 2>&1 | tail -5
```

Expected: clean.

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/... 2>&1 | tail -5
```

Expected: all PASS. `desktop/app_test.go` may have a test that asserts on `RelayConfig` JSON shape — if so, it likely needs to include `last_email`. Update if necessary; otherwise leave alone.

### Step 13: Commit (stage individually — no `git add -A`)

```bash
cd /Users/attson/code/github.com.attson/atterm && git status
git add desktop/config.go \
        desktop/app.go \
        desktop/frontend/src/lib/api.ts \
        desktop/frontend/wailsjs/go/models.ts \
        desktop/frontend/src/components/SettingsRelay.vue \
        desktop/frontend/src/components/SettingsRelay.test.ts
git status
git commit -m "$(cat <<'COMMIT'
feat(desktop): restore Settings → Relay auth state on reopen

Settings used to look "not logged in" after reopen even when the uplink
was still happily connected — the "已连接为 X" pill relied on a one-shot
relay:auth-info event that's missed if the dialog opens *after* uplink
connect. The email field also stayed blank, so users thought their
credentials had been wiped.

- appConfig.RelayLastEmail persists the email from the last successful
  LoginRemoteRelay (plaintext, non-secret; sits next to the session
  token in the same config.json).
- Wails RelayConfig.LastEmail exposes it read-only — GetRelayConfig
  populates it, SetRelayConfig ignores it (only LoginRemoteRelay may
  write).
- SettingsRelay onMounted: prefills email from cfg.last_email and
  eagerly calls fetchRelayMe() when cfg.token exists; pill state no
  longer depends on the event listener. The listener stays as a
  fallback for live identity changes during the dialog session.
- Password is intentionally not persisted.

Spec: docs/superpowers/specs/2026-06-10-relay-settings-auth-state-design.md
COMMIT
)"
```

Do NOT use `git add -A`. `desktop/frontend/package.json.md5` and `.claude/` are commonly dirty and must stay out.

---

## Final verification

This is a one-task plan; once Task 1's Step 13 commit lands the plan is complete. The user will decide whether to ship as `v0.2.64` standalone or batch with other changes via the `ship-release` skill.

### Manual smoke test (after merge + redeploy)

- [ ] Log into remote relay → uplink turns green
- [ ] Close Settings → reopen → green "● 已连接为 X" pill is visible immediately
- [ ] Email field is prefilled with the last successful email
- [ ] Password field is empty (security)
- [ ] Restart desktop App → open Settings → pill + email still appear (uplink auto-reconnects using persisted session token)
- [ ] Revoke session server-side → reopen Settings → pill is empty, email still prefilled (helps quick re-login)

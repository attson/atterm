# Relay Uplink Pill + Form Unify Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the misleading green "uplink 运行中" pill with a 4-state pill (connected/connecting/invalid/off), flatten the Settings → Relay form (drop the "连接远程 relay" card + standalone 登录 button), and make "保存并连接" the single submit path that probes `/api/version`, logs in if creds are present, and persists the config.

**Architecture:** Add a `ProbeRelayVersion` Wails Go method (5 s timeout, validates the response is an atterm relay). Rewrite `SettingsRelay.vue` to (a) compute a 4-state pill from local URL syntax + AUTH_INFO presence, (b) flatten email/password into top-level inputs, (c) rewrite `save()` to chain URL-format-check → probe → login-or-reuse-token → SetRelayConfig. Drop 6 obsolete i18n keys (`loginTitle`/`loginHint`/`login`/`loginInProgress`/`loginFailed`/`loggedIn`) after the Vue refactor removes references.

**Tech Stack:** Go (Wails), Vue 3 SFC + `<script setup>`, vue-i18n, vitest, scoped CSS.

**Spec:** `docs/superpowers/specs/2026-06-10-relay-uplink-pill-and-form-unify-design.md`.

---

## File structure

| File | Status | Purpose | Task |
|---|---|---|---|
| `desktop/app.go` | modify | `ProbeRelayVersion(url string) error` method | T1 |
| `desktop/app_probe_test.go` | create | Unit tests for ProbeRelayVersion against httptest fixtures | T1 |
| `desktop/frontend/src/lib/api.ts` | modify | Add `ProbeRelayVersion` to `AppBindings` + `probeRelayVersion()` wrapper | T2 |
| `desktop/frontend/wailsjs/go/main/App.d.ts` | modify | Manual sync: add `ProbeRelayVersion` declaration | T2 |
| `desktop/frontend/wailsjs/go/main/App.js` | modify | Manual sync: add runtime stub | T2 |
| `desktop/frontend/src/i18n/messages/en.ts` | modify | +5 new keys (additive only at T3) | T3 |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | modify | +5 new keys (additive only at T3) | T3 |
| `desktop/frontend/src/components/SettingsRelay.vue` | modify | Flatten form, rewrite save(), 4-state pill, new CSS | T4 |
| `desktop/frontend/src/components/SettingsRelay.test.ts` | modify | Replace old login-card tests with 4-state pill + probe assertions | T4 |
| `desktop/frontend/src/i18n/messages/en.ts` | modify | −6 orphaned keys | T5 |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | modify | −6 orphaned keys | T5 |

5 tasks, single branch, single PR.

---

## Task 1: Add `ProbeRelayVersion` Wails method (Go)

### Files

- Modify: `desktop/app.go`
- Create: `desktop/app_probe_test.go`

### Step 1: Write the failing test

Create `desktop/app_probe_test.go`:

```go
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeRelayVersion_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"version":"v0.2.99"}`)
	}))
	defer srv.Close()

	app := &App{ctx: context.Background()}
	if err := app.ProbeRelayVersion(srv.URL); err != nil {
		t.Fatalf("ProbeRelayVersion: %v", err)
	}
}

func TestProbeRelayVersion_404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	app := &App{ctx: context.Background()}
	err := app.ProbeRelayVersion(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("expected 404 error; got %v", err)
	}
}

func TestProbeRelayVersion_NoVersionField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	app := &App{ctx: context.Background()}
	err := app.ProbeRelayVersion(srv.URL)
	if err == nil || !strings.Contains(err.Error(), "no version field") {
		t.Fatalf("expected 'no version field' error; got %v", err)
	}
}

func TestProbeRelayVersion_Unreachable(t *testing.T) {
	app := &App{ctx: context.Background()}
	// Port 1 is reserved / never bound — guaranteed connect failure.
	err := app.ProbeRelayVersion("http://127.0.0.1:1")
	if err == nil || !strings.Contains(err.Error(), "connect") {
		t.Fatalf("expected connect error; got %v", err)
	}
}

func TestProbeRelayVersion_EmptyURL(t *testing.T) {
	app := &App{ctx: context.Background()}
	err := app.ProbeRelayVersion("")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("expected empty-url error; got %v", err)
	}
}
```

### Step 2: Run, verify fail

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestProbeRelayVersion -v 2>&1 | tail -20
```

Expected: compile failure (`ProbeRelayVersion` undefined on `*App`).

### Step 3: Add method to `desktop/app.go`

Find a logical insertion point near other `LoginRemoteRelay` / HTTP-doing Wails methods (search for `func (a *App) LoginRemoteRelay`). Add this method directly below `LoginRemoteRelay`:

```go
// ProbeRelayVersion does a lightweight GET <relayURL>/api/version to verify
// the URL points at an atterm relay. Returns nil if the response is 200 and
// the JSON body has a non-empty "version" field. Otherwise returns an error
// the frontend surfaces as "无法连接到 relay" inline beneath the URL field.
//
// /api/version is auth-less per the session-token spec, so no credentials
// are sent. 5-second timeout keeps the UI from blocking on a stalled
// connection — the user can re-click "保存并连接" if the relay just woke up.
func (a *App) ProbeRelayVersion(relayURL string) error {
	relayURL = strings.TrimRight(strings.TrimSpace(relayURL), "/")
	if relayURL == "" {
		return fmt.Errorf("relay url is empty")
	}
	httpURL, _, err := relayLoginEndpoints(relayURL)
	if err != nil {
		return fmt.Errorf("invalid relay url: %w", err)
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, "GET", httpURL+"/api/version", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	var out struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return fmt.Errorf("not an atterm relay (decode): %w", err)
	}
	if out.Version == "" {
		return fmt.Errorf("not an atterm relay (no version field)")
	}
	return nil
}
```

Check imports — the existing file likely already has `strings`, `fmt`, `net/http`, `context`, `encoding/json`. Add `time` if missing:

```bash
grep -E '^\s+"time"' desktop/app.go | head -3
```

If `"time"` is not in the import block, add it.

### Step 4: Run tests, verify all 5 pass

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/ -run TestProbeRelayVersion -v 2>&1 | tail -20
```

Expected: 5 PASS.

### Step 5: Full desktop test sanity

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./desktop/... 2>&1 | tail -5
```

Expected: all PASS.

### Step 6: Commit

```bash
cd /Users/attson/code/github.com.attson/atterm && git status
git add desktop/app.go desktop/app_probe_test.go
git status
git commit -m "feat(desktop): ProbeRelayVersion Wails method (GET /api/version with timeout)"
```

DO NOT use `git add -A`.

---

## Task 2: Add TS binding for `ProbeRelayVersion`

### Files

- Modify: `desktop/frontend/src/lib/api.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.js`

### Step 1: Add `ProbeRelayVersion` to `AppBindings` interface

In `desktop/frontend/src/lib/api.ts`, find the `AppBindings` interface (search for `AppBindings`). Add `ProbeRelayVersion(arg1: string): Promise<void>;` next to other RelayConfig-related entries (e.g., near `LoginRemoteRelay`):

```ts
  ProbeRelayVersion(arg1: string): Promise<void>;
```

### Step 2: Add `probeRelayVersion` wrapper

In the same file, find a wrapper near `loginRemoteRelay` (search for `loginRemoteRelay`). Add a sibling:

```ts
// probeRelayVersion calls the Wails ProbeRelayVersion method on the Go side
// to verify the URL points at an atterm relay. Throws on probe failure.
export function probeRelayVersion(url: string): Promise<void> {
  return bindings().ProbeRelayVersion(url);
}
```

### Step 3: Sync `wailsjs/go/main/App.d.ts`

In `desktop/frontend/wailsjs/go/main/App.d.ts`, find existing Wails function declarations (each follows the pattern `export function <Name>(...): Promise<...>;`). Find `LoginRemoteRelay` and add directly below:

```ts
export function ProbeRelayVersion(arg1:string):Promise<void>;
```

Match the existing terse-spacing style (no space after `:`).

### Step 4: Sync `wailsjs/go/main/App.js`

In `desktop/frontend/wailsjs/go/main/App.js`, find existing function stubs. Each follows:

```js
export function LoginRemoteRelay(arg1, arg2, arg3) {
  return window['go']['main']['App']['LoginRemoteRelay'](arg1, arg2, arg3);
}
```

Add directly below:

```js
export function ProbeRelayVersion(arg1) {
  return window['go']['main']['App']['ProbeRelayVersion'](arg1);
}
```

Match the surrounding indentation.

### Step 5: Type-check + build

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit 2>&1 | tail -5
```

Expected: clean (no errors).

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test 2>&1 | tail -5
```

Expected: all PASS (no test reference yet but TS imports must resolve).

### Step 6: Commit

```bash
cd /Users/attson/code/github.com.attson/atterm && git add desktop/frontend/src/lib/api.ts \
        desktop/frontend/wailsjs/go/main/App.d.ts \
        desktop/frontend/wailsjs/go/main/App.js
git status
git commit -m "desktop(frontend): TS binding for ProbeRelayVersion"
```

---

## Task 3: Add new i18n keys (additive)

### Files

- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

### Step 1: Add keys to `en.ts`

Find the `settings.relay` namespace (search for `settings: {` then `relay: {`). Locate any existing key (e.g., `uplinkRunning: "uplink running"`). Add 5 new keys nearby, preserving the alphabetical / logical grouping:

```ts
      connecting: "Connecting…",
      relayInvalid: "Invalid relay address",
      versionProbeFailed: "Failed to reach relay: {reason}",
      loginFailedInline: "Login failed: {reason}",
      credentialsRequired: "Email and password required",
```

Match indentation (6 spaces under `settings.relay`).

### Step 2: Add keys to `zh-CN.ts`

Same location in `desktop/frontend/src/i18n/messages/zh-CN.ts`:

```ts
      connecting: "正在连接…",
      relayInvalid: "relay 地址无效",
      versionProbeFailed: "无法连接到 relay：{reason}",
      loginFailedInline: "登录失败：{reason}",
      credentialsRequired: "请填邮箱和密码",
```

### Step 3: Verify build/test still green

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit 2>&1 | tail -3
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test 2>&1 | tail -5
```

Both expected clean.

### Step 4: Commit

```bash
cd /Users/attson/code/github.com.attson/atterm && git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "i18n(desktop): add 5 keys for new 4-state relay pill + save flow"
```

---

## Task 4: Refactor `SettingsRelay.vue` + tests

This is the biggest task: flatten the form, rewrite `save()`, rewrite `statusPill` for 4 states, add `isValidRelayUrl` helper, drop `login()` + related refs, add warn/error CSS.

### Files

- Modify: `desktop/frontend/src/components/SettingsRelay.vue`
- Modify: `desktop/frontend/src/components/SettingsRelay.test.ts`

### Step 1: Write failing tests

In `desktop/frontend/src/components/SettingsRelay.test.ts`, append five new tests inside the existing `describe("SettingsRelay", () => {...})` block (just before its closing `})`). Match the existing source-substring-assertion pattern (look at how prior tests obtain the `source` variable and reuse the same mechanism):

```ts
  test("statusPill has 4 states with warn and error classes", () => {
    expect(source).toContain('cls: "warn"');
    expect(source).toContain('cls: "error"');
    expect(source).toContain("settings.relay.connecting");
    expect(source).toContain("settings.relay.relayInvalid");
  });

  test("save() probes /api/version via ProbeRelayVersion before login", () => {
    expect(source).toContain("probeRelayVersion");
    expect(source).toContain("settings.relay.versionProbeFailed");
  });

  test("isValidRelayUrl rejects non-http(s)/ws(s) and empty hosts", () => {
    // Source-level proof the helper exists with the right schemes
    expect(source).toContain("function isValidRelayUrl");
    expect(source).toContain('"http:"');
    expect(source).toContain('"https:"');
    expect(source).toContain('"ws:"');
    expect(source).toContain('"wss:"');
  });

  test("login card and standalone login button are gone", () => {
    // Negative assertions — these were the markers of the deleted card
    expect(source).not.toContain('data-testid="relay-login-form"');
    expect(source).not.toContain("settings.relay.loginTitle");
    expect(source).not.toContain("settings.relay.loginHint");
    expect(source).not.toContain("settings.relay.loginInProgress");
    expect(source).not.toContain("const loginInProgress");
    expect(source).not.toContain("const loginSuccess");
  });

  test("save() handles three credential paths", () => {
    // hasCreds → loginRemoteRelay; hasExistingToken → setRelayConfig; neither → credentialsRequired
    expect(source).toContain("loginRemoteRelay");
    expect(source).toContain("setRelayConfig");
    expect(source).toContain("settings.relay.credentialsRequired");
  });
```

### Step 2: Run tests, verify fail

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/SettingsRelay.test.ts 2>&1 | tail -20
```

Expected: 5 FAIL (assertions reference strings not yet in the SFC).

### Step 3: Remove the login section from the template

In `desktop/frontend/src/components/SettingsRelay.vue`, delete the entire `<section class="relay-login" data-testid="relay-login-form"> ... </section>` block (locate via `data-testid="relay-login-form"` — surrounding lines are roughly 252-336 per current state).

That block contains:
- `<div class="login-title">` + `<p class="hint">` (loginTitle + loginHint)
- email `<label>` + `<input>`
- password `<label>` + `<div class="password-field">` with show/hide toggle
- login error/success messages
- `<div class="login-actions">` with "登录" button

**Preserve** the email + password fields (move them out of the section). After deletion, **insert** at the SAME LOCATION (between the RELAY URL input and the "远端会话权限" SelectDropdown — search for `t("settings.relay.remotePermission")` to find the next section's anchor):

```html
      <label class="field-label" for="relay-email">{{ t("settings.relay.email") }}</label>
      <input
        id="relay-email"
        v-model="email"
        type="email"
        autocomplete="username"
        :disabled="saving"
        @keyup.enter="save"
      />

      <label class="field-label" for="relay-password">{{ t("settings.relay.password") }}</label>
      <div class="password-field">
        <input
          id="relay-password"
          v-model="password"
          :type="showPassword ? 'text' : 'password'"
          autocomplete="current-password"
          :disabled="saving"
          @keyup.enter="save"
        />
        <button
          type="button"
          class="password-toggle"
          :aria-label="showPassword ? t('settings.relay.passwordHide') : t('settings.relay.passwordShow')"
          :aria-pressed="showPassword"
          :disabled="saving"
          @click="showPassword = !showPassword"
        >
          <svg
            v-if="!showPassword"
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z" />
            <circle cx="12" cy="12" r="3" />
          </svg>
          <svg
            v-else
            width="18"
            height="18"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
            aria-hidden="true"
          >
            <path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24" />
            <line x1="1" y1="1" x2="23" y2="23" />
          </svg>
        </button>
      </div>
```

The `@keyup.enter` now calls `save` (not `login`) because there's no separate login action.

### Step 4: Delete obsolete `<script setup>` state

In `<script setup>`, delete these lines (search by name):

- `const loginInProgress = ref(false);`
- `const loginError = ref("");`
- `const loginSuccess = ref(false);`
- The entire `async function login() { ... }` block (search for `async function login(`)
- `const canLogin = computed(...)` (search for `canLogin`)

Also remove any disabled bindings that referenced `loginInProgress` — replace `:disabled="loginInProgress || saving"` with `:disabled="saving"` on remaining elements (the URL input, the SelectDropdown, etc.).

### Step 5: Add `isValidRelayUrl` helper

In `<script setup>`, near other helpers (above `save()` is a good spot), add:

```ts
function isValidRelayUrl(s: string): boolean {
  try {
    const u = new URL(s.trim());
    if (!u.host) return false;
    return ["http:", "https:", "ws:", "wss:"].includes(u.protocol);
  } catch {
    return false;
  }
}
```

### Step 6: Rewrite `statusPill` computed (4 states)

Find `const statusPill = computed(...)` in `<script setup>`. Replace the entire computed body with:

```ts
// Status pill: 4 states. Order matters — connected wins over everything.
const statusPill = computed(() => {
  if (connectedEmail.value) {
    return { text: t("settings.relay.connectedAs", { identity: connectedEmail.value }), cls: "on" };
  }
  if (connectedUserID.value) {
    return { text: t("settings.relay.connectedAs", { identity: connectedUserID.value.slice(0, 8) }), cls: "on" };
  }
  if (!url.value) {
    return { text: t("settings.relay.notConfigured"), cls: "off" };
  }
  if (paused.value) {
    return { text: t("settings.relay.paused"), cls: "off" };
  }
  if (!isValidRelayUrl(url.value)) {
    return { text: t("settings.relay.relayInvalid"), cls: "error" };
  }
  return { text: t("settings.relay.connecting"), cls: "warn" };
});
```

### Step 7: Rewrite `save()` for the unified flow

Find the existing `async function save()` (around lines 117-150 in current state). Replace its body entirely with:

```ts
async function save() {
  saving.value = true;
  error.value = "";

  // 1. URL format check (cheap, local)
  if (!isValidRelayUrl(url.value)) {
    error.value = t("settings.relay.relayInvalid");
    saving.value = false;
    return;
  }

  // 2. /api/version probe via Wails Go method
  try {
    await probeRelayVersion(url.value);
  } catch (e: any) {
    error.value = t("settings.relay.versionProbeFailed", { reason: e?.message ?? String(e) });
    saving.value = false;
    return;
  }

  // 3. Login vs reuse-token vs error
  const hasCreds = !!(email.value && password.value);
  const hasExistingToken = !!token.value;

  if (hasCreds) {
    try {
      await loginRemoteRelay(url.value, email.value, password.value);
    } catch (e: any) {
      error.value = t("settings.relay.loginFailedInline", { reason: e?.message ?? String(e) });
      saving.value = false;
      return;
    }
    password.value = "";
  } else if (hasExistingToken) {
    try {
      await setRelayConfig({
        url: url.value,
        token: token.value,
        session_expires_at: 0,
        allow_insecure_relay: allowInsecureRelay.value,
        remote_permission: remotePermission.value,
        last_email: "",
      });
    } catch (e: any) {
      error.value = e?.message ?? String(e);
      saving.value = false;
      return;
    }
  } else {
    error.value = t("settings.relay.credentialsRequired");
    saving.value = false;
    return;
  }

  // 4. Refresh — pick up updated token + last_email + connected
  try {
    const cfg = await getRelayConfig();
    token.value = cfg.token;
    email.value = cfg.last_email ?? "";
    snapshotPersisted();
  } catch (e: any) {
    error.value = e?.message ?? String(e);
    saving.value = false;
    return;
  }

  saving.value = false;
  emit("relay-config-changed");
}
```

If the existing `save()` already emits other events (e.g., `dirty`) or does additional state cleanup, preserve those — only the validation/probe/login decision tree is new. Compare against the pre-change function and merge selectively.

### Step 8: Add imports

At the top of `<script setup>`, ensure `probeRelayVersion` is imported alongside other API helpers (search for `loginRemoteRelay` to find the existing import block):

```ts
import {
  // ...existing...
  loginRemoteRelay,
  probeRelayVersion,
  setRelayConfig,
  getRelayConfig,
  // ...existing...
} from "@/lib/api";
```

Adapt to the existing import style (the file may use multiple import lines).

### Step 9: Add warn/error CSS

Find the `<style scoped>` block. Locate existing `.status-pill.on` / `.status-pill.off` rules. Add directly below:

```css
.status-pill.warn {
  color: var(--warn, #d97706);
}
.status-pill.warn .dot {
  color: var(--warn, #d97706);
}
.status-pill.error {
  color: var(--bad, #ef4444);
}
.status-pill.error .dot {
  color: var(--bad, #ef4444);
}
```

If the existing scoped styles use `var(--bad)` already (it's a common token), drop the `, #ef4444` fallback. Check by grepping `--bad` in the file.

Also **delete** the styles for the removed login card (search inside `<style scoped>` for `.relay-login`, `.login-title`, `.login-form`, `.login-actions`, `.login-btn`, `.login-error`, `.login-ok` — delete each rule block).

### Step 10: Run tests, expect PASS

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vitest run src/components/SettingsRelay.test.ts 2>&1 | tail -15
```

Expected: all PASS (including the 5 new tests).

### Step 11: vue-tsc + full vitest

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit 2>&1 | tail -5
```

Expected: clean.

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test 2>&1 | tail -10
```

Expected: full count PASS (no other test file should reference the deleted login-card elements; if any do, update those tests to the new structure).

### Step 12: Go build sanity

```bash
cd /Users/attson/code/github.com.attson/atterm && go build ./... 2>&1 | tail -5
```

Expected: clean (the Go side has no compile-affecting changes here, but it's free to verify).

### Step 13: Commit

```bash
cd /Users/attson/code/github.com.attson/atterm && git status
git add desktop/frontend/src/components/SettingsRelay.vue desktop/frontend/src/components/SettingsRelay.test.ts
git status
git commit -m "$(cat <<'COMMIT'
feat(desktop): unify Settings → Relay save flow + 4-state status pill

- Flatten the "连接远程 relay" card: email/password become top-level
  fields next to RELAY URL; standalone "登录" button is gone.
- statusPill computed now has 4 states (off / error / warn / on):
  - off: no URL or paused
  - error: URL syntax invalid → red
  - warn: URL valid + no AUTH_INFO yet → "正在连接…" yellow
  - on: connectedEmail / connectedUserID set → "已连接为 X" green
- save() is now the only submit path:
  1. URL format check (isValidRelayUrl)
  2. ProbeRelayVersion (5 s GET /api/version via Wails)
  3. loginRemoteRelay if email+password filled; or setRelayConfig
     if existing session token; else inline credentialsRequired
- Drops login(), loginInProgress, loginSuccess, loginError, canLogin —
  password input now @keyup.enter="save" same as URL.
- Inline error reuses the existing top-level `error` ref for all paths.

Spec: docs/superpowers/specs/2026-06-10-relay-uplink-pill-and-form-unify-design.md
COMMIT
)"
```

---

## Task 5: Delete orphaned i18n keys

After Task 4 lands, 6 keys are no longer referenced. This task removes them.

### Files

- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

### Step 1: Confirm no references

```bash
cd /Users/attson/code/github.com.attson/atterm && grep -rn 'settings\.relay\.\(loginTitle\|loginHint\|login\b\|loginInProgress\|loginFailed\|loggedIn\)' desktop/frontend/src/ 2>&1 | head -10
```

Expected: zero hits. If any reference remains, **stop** and fix that reference first (the Task 4 refactor missed something).

### Step 2: Delete from `en.ts`

In `desktop/frontend/src/i18n/messages/en.ts`, locate and delete these 6 lines inside `settings.relay`:

```ts
loginTitle: "Connect to remote relay",
loginHint: "Enter the email and password you registered on this relay.",
login: "Log in",
loginInProgress: "Logging in…",
loginFailed: "Login failed",
loggedIn: "Connected to remote relay",
```

The exact English wording may differ — use the actual lines from the file. Delete each line, preserving the trailing comma on the line above and the surrounding structure.

### Step 3: Delete from `zh-CN.ts`

Same in `desktop/frontend/src/i18n/messages/zh-CN.ts`:

```ts
loginTitle: "连接远程 relay",
loginHint: "请输入已在该 relay 注册的账号邮箱与密码。",
login: "登录",
loginInProgress: "登录中…",
loginFailed: "登录失败",
loggedIn: "已连接远程 relay",
```

### Step 4: Verify build + tests

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit 2>&1 | tail -3
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npm test 2>&1 | tail -5
```

Expected: clean. If a stale test still references one of these keys, update or delete that test.

### Step 5: Commit

```bash
cd /Users/attson/code/github.com.attson/atterm && git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "i18n(desktop): drop 6 orphaned keys (loginTitle/Hint/login/InProgress/Failed/loggedIn)"
```

---

## Final verification

After all 5 tasks land on the branch:

```bash
cd /Users/attson/code/github.com.attson/atterm && go test ./... 2>&1 | tail -8
cd /Users/attson/code/github.com.attson/atterm/desktop/frontend && npx vue-tsc --noEmit && npm test 2>&1 | tail -8
```

Both expected clean.

### Manual smoke test (after merge + redeploy)

- [ ] Open Settings → Relay. URL field empty → pill says "待配置" gray
- [ ] Type garbage URL like `http//` → pill switches to "relay 地址无效" red
- [ ] Type valid URL like `https://example.com` (no relay there) → pill switches to "正在连接…" yellow (still not connected — pill stays yellow until AUTH_INFO)
- [ ] Click "保存并连接" without filling email/password → inline error "请填邮箱和密码" (assuming no existing token)
- [ ] Fill garbage URL + click save → inline error "无法连接到 relay" red
- [ ] Fill valid relay URL + correct creds + click save → pill switches to "已连接为 X" green within ~1 s
- [ ] Close + reopen Settings → email prefilled (last_email), pill stays green
- [ ] Toggle pause → pill switches to "已暂停" gray
- [ ] No "连接远程 relay" card / no "登录" button visible anywhere

Once green, ship via `superpowers:ship-release` or `ship-release` skill — patch tag bump from v0.2.64 → v0.2.65.

# Mobile Session List Alignment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reshape `MobileSessionList.vue` around the same grouping model as the desktop `TaskSidebar` / `TaskGroupedList`: host ↔ state toggle, trimmed cards, completed-seen fold, unread + row/group/footer ✓. Wire iOS-side mark-seen to the relay by letting Bearer tokens bypass CSRF.

**Architecture:** Mobile imports the existing `composables/useSessions.ts` (passes an empty local list, the remote list, and a `groupBy` ref persisted via `useTaskGroupBy`). Card/group rendering reuses `<TaskStateIcon>` and the newly extracted `lib/sessionLabel.ts` helpers. `platform/capacitor.ts` gains `markSessionsSeen` that posts to `POST /api/sessions/seen` with Bearer auth; `internal/relay/csrfmw.go` skips CSRF when an `Authorization: Bearer …` header is present (Bearer is itself an anti-CSRF credential).

**Tech Stack:** Vue 3 `<script setup>` + TypeScript + Vitest for frontend; Go `net/http` + existing relay test scaffolding (`signupAndLogin`, `csrfTokenFor`) for backend.

**Reference spec:** `docs/superpowers/specs/2026-06-08-mobile-session-list-align-desktop-design.md`

---

## Task 1: Relay — RequireCSRF lets Bearer through

**Files:**
- Modify: `internal/relay/csrfmw.go`
- Modify: `internal/relay/csrfmw_test.go`

This is independent of every frontend task; do it first so the seen endpoint becomes reachable from a Bearer-only client.

- [ ] **Step 1.1: Add a failing test for the Bearer-passthrough path**

Append to `internal/relay/csrfmw_test.go`:

```go
func TestRequireCSRF_BearerPassThrough_NoCookieNoCSRF(t *testing.T) {
	resolver, _, _ := newCSRFSetup([]byte("csrf-secret-bytes"))
	handler := RequireCSRF(resolver, okHandler)

	// Bearer present, no cookie, no X-CSRF-Token: should pass.
	r := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	r.Header.Set("Authorization", "Bearer atk_anything-the-mw-does-not-validate")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (bearer bypasses CSRF), got %d", w.Code)
	}
}

func TestRequireCSRF_BearerWithoutPrefix_StillRequiresCookie(t *testing.T) {
	resolver, _, _ := newCSRFSetup([]byte("csrf-secret-bytes"))
	handler := RequireCSRF(resolver, okHandler)

	// Authorization header that is NOT "Bearer …" must NOT bypass CSRF.
	r := httptest.NewRequest(http.MethodPost, "/api/something", nil)
	r.Header.Set("Authorization", "Basic dXNlcjpwYXNz")

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("non-Bearer Authorization must NOT bypass CSRF; expected 401, got %d", w.Code)
	}
}
```

- [ ] **Step 1.2: Run the tests and confirm they fail**

```bash
cd /Users/attson/code/github.com.attson/atterm
go test ./internal/relay/ -run TestRequireCSRF_BearerPassThrough_NoCookieNoCSRF -v
```

Expected: `FAIL` — current `RequireCSRF` returns 401 because no cookie is present.

- [ ] **Step 1.3: Implement the Bearer bypass**

Replace `internal/relay/csrfmw.go` body of `RequireCSRF` with:

```go
func RequireCSRF(resolver *IdentityResolver, inner http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			inner.ServeHTTP(w, r)
			return
		}
		// Bearer tokens are out-of-band credentials — they are not auto-attached
		// by browsers, so CSRF does not apply. Auth is still enforced by inner
		// handlers via Resolver.Resolve / Principal.IsUser.
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			inner.ServeHTTP(w, r)
			return
		}
		c, err := r.Cookie("atterm_session")
		if err != nil || c.Value == "" {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		p := resolver.Resolve(r)
		if !p.IsUser() || len(p.CSRFSecret) == 0 {
			http.Error(w, "unauthenticated", http.StatusUnauthorized)
			return
		}
		want := CSRFToken(c.Value, p.CSRFSecret)
		got := r.Header.Get("X-CSRF-Token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
			http.Error(w, "csrf mismatch", http.StatusForbidden)
			return
		}
		inner.ServeHTTP(w, r)
	})
}
```

Add `"strings"` to the import block in the same file.

- [ ] **Step 1.4: Run the full relay test suite**

```bash
go test ./internal/relay/ -v -run "TestRequireCSRF|TestSessionsSeen"
```

Expected: all `TestRequireCSRF_*` pass (existing + new); all `TestSessionsSeen_*` cookie-based tests continue to pass.

- [ ] **Step 1.5: Commit**

```bash
git add internal/relay/csrfmw.go internal/relay/csrfmw_test.go
git commit -m "$(cat <<'EOF'
relay: let Authorization: Bearer skip CSRF

Bearer tokens are not auto-attached by browsers, so CSRF doesn't apply.
This lets the mobile (Bearer-only) client reach POST /api/sessions/seen
without first acquiring a cookie + CSRF token. Inner handlers continue
to enforce auth via Resolver.Resolve / IsUser.
EOF
)"
```

---

## Task 2: Relay — Bearer-token cases for the sessions-seen endpoint

**Files:**
- Modify: `internal/relay/sessions_seen_http_test.go`

Validate the new path end-to-end through the registered `POST /api/sessions/seen` route.

- [ ] **Step 2.1: Add a helper to mint an API token for a user**

Insert near the top of `sessions_seen_http_test.go` (after `addOwnedSession`):

```go
// issueAPIToken creates a personal API token for userID and returns the
// plaintext to be used as a Bearer credential.
func issueAPIToken(t *testing.T, store *userstore.SQLiteStore, userID string) string {
	t.Helper()
	secret, _, err := store.CreateAPIToken(context.Background(), userID, "test-mobile")
	if err != nil {
		t.Fatalf("CreateAPIToken: %v", err)
	}
	return secret.Expose()
}

// postJSONWithBearer marshals body to JSON and POSTs it to path with an
// Authorization: Bearer header. Returns the recorder.
func postJSONWithBearer(handler http.Handler, path string, body any, bearer string) *httptest.ResponseRecorder {
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+bearer)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}
```

Add the two missing imports to the file's top import block: `"bytes"` and `"net/http/httptest"`.

- [ ] **Step 2.2: Add the three failing Bearer test cases**

Append to `sessions_seen_http_test.go`:

```go
func TestSessionsSeen_Bearer_MarkAll(t *testing.T) {
	srv, store := newTestSeenServer(t)
	handler := http.Handler(srv)

	_, userAID, _ := signupAndLogin(t, handler, store, "bearerall@example.com", "correcthorsebattery")
	token := issueAPIToken(t, store, userAID)
	sessID := addOwnedSession(t, srv, userAID, 1000)

	w := postJSONWithBearer(handler, "/api/sessions/seen", map[string]any{"all": true}, token)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	seen, err := store.SeenAt(context.Background(), userAID)
	if err != nil {
		t.Fatalf("SeenAt: %v", err)
	}
	if _, ok := seen[sessID.String()]; !ok {
		t.Fatalf("expected session %s to be marked seen for user %s, got %v", sessID, userAID, seen)
	}
}

func TestSessionsSeen_Bearer_CrossUserIgnored(t *testing.T) {
	srv, store := newTestSeenServer(t)
	handler := http.Handler(srv)

	_, userAID, _ := signupAndLogin(t, handler, store, "bearerA@example.com", "correcthorsebattery")
	_, userBID, _ := signupAndLogin(t, handler, store, "bearerB@example.com", "correcthorsebattery")
	tokenA := issueAPIToken(t, store, userAID)
	sessBID := addOwnedSession(t, srv, userBID, 2000)

	w := postJSONWithBearer(handler, "/api/sessions/seen",
		map[string]any{"session_ids": []string{sessBID.String()}},
		tokenA)
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", w.Code, w.Body.String())
	}

	seen, err := store.SeenAt(context.Background(), userAID)
	if err != nil {
		t.Fatalf("SeenAt: %v", err)
	}
	if _, ok := seen[sessBID.String()]; ok {
		t.Errorf("cross-user: session %s must NOT be marked seen under user A; got %v", sessBID, seen)
	}
}

func TestSessionsSeen_Bearer_InvalidToken(t *testing.T) {
	srv, _ := newTestSeenServer(t)
	handler := http.Handler(srv)

	w := postJSONWithBearer(handler, "/api/sessions/seen",
		map[string]any{"all": true},
		"atk_does_not_exist_in_store")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("invalid bearer: expected 401, got %d: %s", w.Code, w.Body.String())
	}
}
```

- [ ] **Step 2.3: Run the new cases and confirm they pass**

```bash
go test ./internal/relay/ -v -run TestSessionsSeen_Bearer
```

Expected: all three new tests PASS.

- [ ] **Step 2.4: Re-run the full relay package to confirm no regression**

```bash
go test ./internal/relay/...
```

Expected: PASS.

- [ ] **Step 2.5: Commit**

```bash
git add internal/relay/sessions_seen_http_test.go
git commit -m "relay(test): cover Bearer-token paths on POST /api/sessions/seen"
```

---

## Task 3: Extract `lib/sessionLabel.ts`

**Files:**
- Create: `desktop/frontend/src/lib/sessionLabel.ts`
- Create: `desktop/frontend/src/lib/sessionLabel.test.ts`
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue`

The helpers `commandLabel` / `fullCommand` / `rowTitle` currently live inside `TaskGroupedList.vue`; they are not exported and have no unit tests. Pull them into `lib/sessionLabel.ts` and add `hostName` so mobile and desktop format identically.

- [ ] **Step 3.1: Write failing unit tests**

Create `desktop/frontend/src/lib/sessionLabel.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import {
  commandLabel,
  fullCommand,
  rowTitle,
  hostName,
} from './sessionLabel'

describe('sessionLabel.fullCommand', () => {
  it('prefers current_command over title', () => {
    expect(fullCommand({ current_command: 'claude --foo', title: 'fallback', session_id: 'abcd1234' }))
      .toBe('claude --foo')
  })
  it('falls back to title when current_command is empty', () => {
    expect(fullCommand({ current_command: '', title: 'zsh', session_id: 'abcd1234' })).toBe('zsh')
  })
  it('falls back to the first 8 chars of session_id when both are empty', () => {
    expect(fullCommand({ session_id: 'abcd12345678' })).toBe('abcd1234')
  })
})

describe('sessionLabel.commandLabel', () => {
  it('strips arguments and path prefix', () => {
    expect(commandLabel({ current_command: '/usr/local/bin/claude --permission-mode bypassPermissions', session_id: 'x' }))
      .toBe('claude')
  })
  it('returns the bare command when there is no slash and no arg', () => {
    expect(commandLabel({ current_command: 'codex', session_id: 'x' })).toBe('codex')
  })
})

describe('sessionLabel.rowTitle', () => {
  it('appends cwd on its own line when present', () => {
    expect(rowTitle({ current_command: 'ls', cwd: '/tmp', session_id: 'x' })).toBe('ls\n/tmp')
  })
  it('returns just the command when cwd is missing', () => {
    expect(rowTitle({ current_command: 'ls', session_id: 'x' })).toBe('ls')
  })
})

describe('sessionLabel.hostName', () => {
  it('returns the first session\'s host when present', () => {
    expect(hostName('h-1', [{ host: 'macbook' }], 'unknown')).toBe('macbook')
  })
  it('falls back to hostId when the list is empty', () => {
    expect(hostName('h-1', [], 'unknown')).toBe('h-1')
  })
  it('falls back to the provided unknown label when hostId is empty', () => {
    expect(hostName('', undefined, 'unknown host')).toBe('unknown host')
  })
})
```

- [ ] **Step 3.2: Run and confirm failure**

```bash
cd desktop/frontend
npx vitest run src/lib/sessionLabel.test.ts
```

Expected: FAIL — `Cannot find module './sessionLabel'`.

- [ ] **Step 3.3: Implement `lib/sessionLabel.ts`**

Create `desktop/frontend/src/lib/sessionLabel.ts`:

```ts
// Shared helpers for rendering a session row: short command label, full
// command for tooltips, and host display name. Used by both the desktop
// TaskGroupedList and the mobile session list so the two surfaces stay
// in lockstep.

export interface SessionLike {
  current_command?: string
  title?: string
  session_id: string
  cwd?: string
}

export function fullCommand(s: Pick<SessionLike, 'current_command' | 'title' | 'session_id'>): string {
  return s.current_command || s.title || s.session_id.slice(0, 8)
}

// commandLabel is the SHORT row display: only the executable name
// (first whitespace-separated token, with any leading path stripped).
// `/usr/local/bin/claude --permission-mode bypassPermissions` → `claude`.
export function commandLabel(s: Pick<SessionLike, 'current_command' | 'title' | 'session_id'>): string {
  const raw = fullCommand(s)
  const firstToken = raw.split(/\s+/)[0] || raw
  return firstToken.split('/').pop() || firstToken
}

export function rowTitle(s: SessionLike): string {
  const cmd = fullCommand(s)
  return s.cwd ? `${cmd}\n${s.cwd}` : cmd
}

// hostName resolves a host display name for a group key. The list is the
// sessions inside that group — the caller picks them out of byHost. Falls
// back to hostId, then to the provided unknown-host label.
export function hostName(
  hostId: string,
  list: { host?: string }[] | undefined,
  unknownHostFallback: string,
): string {
  const first = list?.[0]
  return first?.host || hostId || unknownHostFallback
}
```

- [ ] **Step 3.4: Run tests and confirm they pass**

```bash
npx vitest run src/lib/sessionLabel.test.ts
```

Expected: PASS.

- [ ] **Step 3.5: Switch `TaskGroupedList.vue` to the shared module**

In `desktop/frontend/src/components/TaskGroupedList.vue`:

1. Add the import at the top of `<script setup lang="ts">`:

   ```ts
   import { commandLabel, fullCommand, rowTitle, hostName as hostNameHelper } from "../lib/sessionLabel";
   ```

2. Delete the three local function declarations `fullCommand`, `commandLabel`, `rowTitle` (lines around 96-114 in the current file).

3. Replace the local `hostName` function with a thin wrapper that adapts the existing call sites:

   ```ts
   function hostName(hostId: string): string {
     return hostNameHelper(hostId, groups.value[hostId], t("sessions.unknownHost"));
   }
   ```

   The previous body was equivalent; this just delegates to the shared helper. The `stateLabel` function stays — it depends on i18n keys and the TaskState union.

- [ ] **Step 3.6: Run the desktop component tests**

```bash
npx vitest run src/components/TaskGroupedList.test.ts src/components/TaskSidebar.test.ts src/App.test.ts
```

Expected: all PASS — behaviour is identical, only the source of the helpers moved.

- [ ] **Step 3.7: Commit**

```bash
git add desktop/frontend/src/lib/sessionLabel.ts \
        desktop/frontend/src/lib/sessionLabel.test.ts \
        desktop/frontend/src/components/TaskGroupedList.vue
git commit -m "$(cat <<'EOF'
desktop(refactor): extract commandLabel/hostName helpers into lib/sessionLabel

TaskGroupedList kept these inline; mobile is about to reuse them, so
move them to lib/sessionLabel.ts with unit tests. No behaviour change
for the desktop sidebar.
EOF
)"
```

---

## Task 4: Platform bridge — `SessionBridge.markSessionsSeen`

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts`
- Modify: `desktop/frontend/src/platform/wails.ts`
- Modify: `desktop/frontend/src/platform/__tests__/_fakePlatform.ts`

Add an optional bridge method that both platforms implement (Wails delegates to the existing `lib/api.markSessionsSeen`; Capacitor gets its own POST in the next task).

- [ ] **Step 4.1: Extend the SessionBridge type**

In `desktop/frontend/src/platform/types.ts`, add the import and the optional method:

```ts
import type { MarkSessionsSeenOpts } from '../lib/api'
export type { MarkSessionsSeenOpts }
```

Inside the existing `SessionBridge` interface (around line 90), add the method after `listRemoteSessions`:

```ts
  /** Optional — mark the given sessions (or all owned sessions) as seen on
   *  the relay. Wails delegates to lib/api. Capacitor posts directly to
   *  /api/sessions/seen with Bearer auth. Throws 'relay_unauthorized' on
   *  HTTP 401. */
  markSessionsSeen?(opts: MarkSessionsSeenOpts): Promise<void>
```

- [ ] **Step 4.2: Wire the Wails bridge**

In `desktop/frontend/src/platform/wails.ts`, the existing `sessions` block currently lists `newSession / closeSession / listShells / listRemoteSessions`. Add:

```ts
      markSessionsSeen: api.markSessionsSeen,
```

`api.markSessionsSeen` is the existing export from `lib/api.ts`; no new import needed beyond what is already there. If the file imports `api` as a namespace this is enough; if it imports specific names, add `markSessionsSeen` to the named import.

- [ ] **Step 4.3: Make the fake platform expose markSessionsSeen**

In `desktop/frontend/src/platform/__tests__/_fakePlatform.ts`, inside the `sessions` block:

```ts
      markSessionsSeen: vi.fn().mockResolvedValue(undefined),
```

- [ ] **Step 4.4: Run the platform-type tests**

```bash
cd desktop/frontend
npx vitest run src/platform/
```

Expected: PASS (existing tests untouched; no new behaviour to assert here — fully covered in Task 5).

- [ ] **Step 4.5: Commit**

```bash
git add desktop/frontend/src/platform/types.ts \
        desktop/frontend/src/platform/wails.ts \
        desktop/frontend/src/platform/__tests__/_fakePlatform.ts
git commit -m "platform: add SessionBridge.markSessionsSeen (Wails delegates to lib/api)"
```

---

## Task 5: Capacitor — list mapping + `markSessionsSeen` implementation

**Files:**
- Modify: `desktop/frontend/src/platform/capacitor.ts`
- Create or modify: `desktop/frontend/src/platform/__tests__/capacitor.test.ts`

- [ ] **Step 5.1: Inspect the existing capacitor test file (if any)**

```bash
ls desktop/frontend/src/platform/__tests__/
```

If `capacitor.test.ts` does not exist, create it; otherwise extend the existing file.

- [ ] **Step 5.2: Write failing tests for the list mapping and markSessionsSeen**

Append (or create) `desktop/frontend/src/platform/__tests__/capacitor.test.ts`:

```ts
import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest'
import { createCapacitorPlatform } from '../capacitor'

const STORED_RELAY = JSON.stringify({ url: 'https://relay.example', token: 'atk_test' })

// capacitor.ts reads relay config from secureStorage first, then falls back to
// loadLegacyFromLocalStorage(). secureStorage uses an in-memory backend under
// vitest; we rely on that being empty and seed localStorage instead. Clearing
// both each test keeps tests independent.
beforeEach(async () => {
  const { secureStorage } = await import('../secureStorage')
  await secureStorage.remove('atterm.relay')
  localStorage.clear()
  localStorage.setItem('atterm.relay', STORED_RELAY)
  globalThis.fetch = vi.fn() as unknown as typeof fetch
})
afterEach(() => { vi.restoreAllMocks() })

describe('capacitor.listRemoteSessions', () => {
  it('maps unread and attention_at from the JSON payload', async () => {
    ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => [{
        id: 's1', host_id: 'h', host: 'box', user: 'me',
        title: 'zsh', command: 'zsh', cols: 80, rows: 24,
        unread: true, attention_at: 1234567,
      }],
    } as unknown as Response)

    const p = createCapacitorPlatform()
    const list = await p.sessions.listRemoteSessions()
    expect(list).toHaveLength(1)
    expect(list[0].unread).toBe(true)
    expect(list[0].attention_at).toBe(1234567)
  })
})

describe('capacitor.markSessionsSeen', () => {
  it('posts session_ids with Bearer auth', async () => {
    ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true, status: 204,
    } as unknown as Response)

    const p = createCapacitorPlatform()
    await p.sessions.markSessionsSeen!({ ids: ['s1', 's2'] })

    const [url, init] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit]
    expect(url).toBe('https://relay.example/api/sessions/seen')
    expect(init.method).toBe('POST')
    expect((init.headers as Record<string, string>)['Authorization']).toBe('Bearer atk_test')
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    expect(JSON.parse(init.body as string)).toEqual({ session_ids: ['s1', 's2'] })
  })

  it('posts {all: true} when called with { all: true }', async () => {
    ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: true, status: 204,
    } as unknown as Response)

    const p = createCapacitorPlatform()
    await p.sessions.markSessionsSeen!({ all: true })

    const [, init] = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0] as [string, RequestInit]
    expect(JSON.parse(init.body as string)).toEqual({ all: true })
  })

  it('throws relay_unauthorized on HTTP 401', async () => {
    ;(globalThis.fetch as ReturnType<typeof vi.fn>).mockResolvedValueOnce({
      ok: false, status: 401,
    } as unknown as Response)

    const p = createCapacitorPlatform()
    await expect(p.sessions.markSessionsSeen!({ all: true })).rejects.toThrow('relay_unauthorized')
  })
})
```

`createCapacitorPlatform` is the named export at `desktop/frontend/src/platform/capacitor.ts:54`. No alias needed.

- [ ] **Step 5.3: Run and confirm failure**

```bash
npx vitest run src/platform/__tests__/capacitor.test.ts
```

Expected: FAIL — both the unread mapping and `markSessionsSeen` calls are missing.

- [ ] **Step 5.4: Add unread / attention_at to the list mapping**

In `desktop/frontend/src/platform/capacitor.ts`, locate the raw type for `listRemoteSessions` (around line 138). Add the two fields:

```ts
        const raw = (await res.json()) as Array<{
          id: string; command: string; title: string; cwd: string; cols: number; rows: number;
          host_id: string; host: string; user: string; remote_permission?: string; task_state?: RemoteSession['task_state'];
          current_command?: string; command_started_at?: number; command_ended_at?: number; command_duration_ms?: number;
          command_exit_code?: number; last_output_at?: number; type?: string; summary?: SessionSummary;
          unread?: boolean; attention_at?: number;
        }>
```

And inside the mapping (after the existing `if (s.summary !== undefined) ...` line, before the final `return out`):

```ts
          if (s.unread !== undefined) out.unread = s.unread
          if (s.attention_at !== undefined) out.attention_at = s.attention_at
```

- [ ] **Step 5.5: Implement `markSessionsSeen` on the capacitor bridge**

In the same `sessions:` block (right after the `listRemoteSessions: async ...` arrow), add:

```ts
      markSessionsSeen: async (opts) => {
        const cfg = parseRelayJSON(await secureStorage.get(STORAGE_KEY))
                  ?? loadLegacyFromLocalStorage()
        if (!cfg?.url || !cfg.token) return
        const base = cfg.url.replace(/\/$/, '')
        const body = 'all' in opts && opts.all
          ? { all: true }
          : { session_ids: (opts as { ids: string[] }).ids }
        const res = await fetch(base + '/api/sessions/seen', {
          method: 'POST',
          headers: {
            Authorization: `Bearer ${cfg.token}`,
            'Content-Type': 'application/json',
          },
          credentials: 'omit',
          body: JSON.stringify(body),
        })
        if (res.status === 401) throw new Error('relay_unauthorized')
        if (!res.ok) throw new Error(`mark-seen: HTTP ${res.status}`)
      },
```

The constants `STORAGE_KEY`, `parseRelayJSON`, `loadLegacyFromLocalStorage`, and `secureStorage` are already in scope inside this factory (they are the same ones `listRemoteSessions` uses).

If `MarkSessionsSeenOpts` is not yet imported at the top of `capacitor.ts`, add:

```ts
import type { MarkSessionsSeenOpts } from './types'
```

and annotate the parameter `(opts: MarkSessionsSeenOpts)` if TypeScript inference needs it.

- [ ] **Step 5.6: Re-run the tests**

```bash
npx vitest run src/platform/__tests__/capacitor.test.ts
```

Expected: PASS.

- [ ] **Step 5.7: Commit**

```bash
git add desktop/frontend/src/platform/capacitor.ts \
        desktop/frontend/src/platform/__tests__/capacitor.test.ts
git commit -m "$(cat <<'EOF'
platform(capacitor): map unread/attention_at + implement markSessionsSeen

listRemoteSessions now forwards the relay's unread + attention_at fields
to RemoteSession. markSessionsSeen posts to /api/sessions/seen with
Bearer auth (relay-side CSRF bypass landed in the prior commit).
EOF
)"
```

---

## Task 6: `MobileSessionCard.vue`

**Files:**
- Create: `desktop/frontend/src/mobile/MobileSessionCard.vue`
- Create: `desktop/frontend/src/mobile/__tests__/MobileSessionCard.test.ts`

The trimmed-down card. Single responsibility: render one session row, emit `open` on tap, emit `markSeen` on row-level ✓.

- [ ] **Step 6.1: Write failing component tests**

Create `desktop/frontend/src/mobile/__tests__/MobileSessionCard.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import MobileSessionCard from '../MobileSessionCard.vue'
import type { RemoteSession } from '../../platform/types'

const base: RemoteSession = {
  session_id: 's1', host_id: 'h1', host: 'box1', user: 'me',
  title: 'zsh', cwd: '/Users/me/proj', cols: 80, rows: 24,
  task_state: 'running', current_command: '/usr/local/bin/claude --foo',
}

describe('MobileSessionCard', () => {
  it('renders the short command label and shortened cwd', () => {
    const w = mount(MobileSessionCard, { props: { session: base, home: '/Users/me' } })
    expect(w.text()).toContain('claude')
    expect(w.text()).not.toContain('/usr/local/bin/claude --foo')
    expect(w.text()).toContain('proj')
  })

  it('renders host·user on the helper line', () => {
    const w = mount(MobileSessionCard, { props: { session: base, home: '/Users/me' } })
    expect(w.text()).toContain('box1')
    expect(w.text()).toContain('me')
  })

  it('shows the unread dot and ✓ only when session.unread is true', () => {
    const seen = mount(MobileSessionCard, { props: { session: { ...base, unread: false }, home: '/' } })
    expect(seen.find('[data-testid="unread-dot"]').exists()).toBe(false)
    expect(seen.find('[data-testid="row-mark-read"]').exists()).toBe(false)

    const unread = mount(MobileSessionCard, { props: { session: { ...base, unread: true }, home: '/' } })
    expect(unread.find('[data-testid="unread-dot"]').exists()).toBe(true)
    expect(unread.find('[data-testid="row-mark-read"]').exists()).toBe(true)
  })

  it('emits open when the card body is tapped', async () => {
    const w = mount(MobileSessionCard, { props: { session: base, home: '/' } })
    await w.find('[data-testid="card-body"]').trigger('click')
    expect(w.emitted('open')).toBeTruthy()
    expect(w.emitted('open')![0]![0]).toEqual(base)
  })

  it('emits markSeen with this session id when ✓ is tapped, does not emit open', async () => {
    const session = { ...base, unread: true }
    const w = mount(MobileSessionCard, { props: { session, home: '/' } })
    await w.find('[data-testid="row-mark-read"]').trigger('click')
    expect(w.emitted('markSeen')).toBeTruthy()
    expect(w.emitted('markSeen')![0]![0]).toEqual({ ids: ['s1'] })
    expect(w.emitted('open')).toBeFalsy()
  })
})
```

- [ ] **Step 6.2: Confirm failure**

```bash
npx vitest run src/mobile/__tests__/MobileSessionCard.test.ts
```

Expected: FAIL — file does not exist.

- [ ] **Step 6.3: Implement the component**

Create `desktop/frontend/src/mobile/MobileSessionCard.vue`:

```vue
<script setup lang="ts">
import { computed } from 'vue'
import type { RemoteSession } from '../platform/types'
import type { TaskState } from '../lib/taskState'
import TaskStateIcon from '../components/TaskStateIcon.vue'
import { commandLabel } from '../lib/sessionLabel'
import { shortenCwd } from '../lib/shortenCwd'
import { useI18n } from '../i18n/useI18n'
import { useTaskPreset } from '../composables/useTaskPreset'

const props = defineProps<{ session: RemoteSession; home: string }>()
const emit = defineEmits<{
  (e: 'open', s: RemoteSession): void
  (e: 'markSeen', payload: { ids: string[] }): void
}>()

const { t } = useI18n()
const preset = useTaskPreset()
const showStateLabel = computed(() => preset.active.value.showLabel)

const cmd = computed(() => commandLabel(props.session))
const cwd = computed(() => shortenCwd(props.session.cwd, props.home))

function stateLabel(state: TaskState | string | undefined): string {
  switch (state) {
    case 'running': return t('mobile.taskStates.running')
    case 'waiting_input': return t('mobile.taskStates.waiting_input')
    case 'completed': return t('mobile.taskStates.completed')
    case 'failed': return t('mobile.taskStates.failed')
    case 'disconnected': return t('mobile.taskStates.disconnected')
    case 'closed': return t('mobile.taskStates.closed')
    default: return t('mobile.taskStates.idle')
  }
}

function onMark(e: MouseEvent) {
  e.stopPropagation()
  emit('markSeen', { ids: [props.session.session_id] })
}
</script>

<template>
  <div class="card" :class="`state-${session.task_state || 'idle'}`">
    <button
      class="body"
      data-testid="card-body"
      @click="emit('open', session)"
    >
      <TaskStateIcon :state="(session.task_state as TaskState | undefined) ?? 'idle'" :size="14" />
      <span v-if="showStateLabel" class="state-label">{{ stateLabel(session.task_state) }}</span>
      <span class="cmd-and-cwd">
        <span class="cmd">{{ cmd }}</span>
        <span v-if="cwd" class="cwd">·&nbsp;{{ cwd }}</span>
      </span>
      <span v-if="session.unread" class="unread-dot" data-testid="unread-dot">●</span>
      <span
        v-if="session.unread"
        class="row-mark-read"
        data-testid="row-mark-read"
        role="button"
        tabindex="0"
        :aria-label="t('tasks.markRead')"
        @click="onMark"
        @keydown.enter.stop.prevent="onMark($event as unknown as MouseEvent)"
      >✓</span>
    </button>
    <span class="helper">{{ session.host }}·{{ session.user }}</span>
  </div>
</template>

<style scoped>
.card { display: flex; flex-direction: column; gap: 2px; padding: 8px 12px; min-height: 56px; border-radius: 11px; background: #11182b; border: 1px solid #1e2638; margin-bottom: 8px; }
.body { display: flex; align-items: center; gap: 8px; padding: 0; background: none; border: none; color: inherit; text-align: left; cursor: pointer; }
.state-label { font-size: 0.72rem; opacity: 0.85; white-space: nowrap; }
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { font-family: var(--font-mono); white-space: nowrap; }
.cwd { color: var(--fg-dim, #9aa3b2); font-family: var(--font-mono); font-size: 0.78rem; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.unread-dot { font-size: 9px; color: currentColor; }
.row-mark-read { display: inline-flex; align-items: center; justify-content: center; min-width: 44px; min-height: 44px; padding: 0 4px; font-size: 16px; cursor: pointer; }
.helper { font-size: 0.72rem; color: #8d93a3; font-family: var(--font-mono); padding-left: 22px; }
</style>
```

- [ ] **Step 6.4: Run the tests**

```bash
npx vitest run src/mobile/__tests__/MobileSessionCard.test.ts
```

Expected: PASS.

- [ ] **Step 6.5: Commit**

```bash
git add desktop/frontend/src/mobile/MobileSessionCard.vue \
        desktop/frontend/src/mobile/__tests__/MobileSessionCard.test.ts
git commit -m "mobile: add MobileSessionCard component (trimmed session row)"
```

---

## Task 7: Rewrite `MobileSessionList.vue`

**Files:**
- Modify: `desktop/frontend/src/mobile/MobileSessionList.vue`
- Modify: `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts`

This is the heart of the change. The existing test file asserts the OLD bucket structure (`task-section-needs_attention` etc., `cols×rows`, `permission`, `last output` in the card body); those assertions must be replaced. The new test file is written below in its entirety to avoid drift from out-of-order edits.

- [ ] **Step 7.1: Replace the test file with the new behavioural spec**

Overwrite `desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts` with:

```ts
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { __setPlatformForTests } from '../../platform'
import { createFakePlatform } from '../../platform/__tests__/_fakePlatform'
import MobileSessionList from '../MobileSessionList.vue'
import { __resetForTests as resetGroupBy } from '../../composables/useTaskGroupBy'
import type { RemoteSession } from '../../platform/types'

const sessions: RemoteSession[] = [
  // attention-needing
  { session_id: 'a', host_id: 'h1', host: 'box1', user: 'me', title: 'codex', cwd: '/Users/me/proj', cols: 80, rows: 24, task_state: 'waiting_input', current_command: 'codex --plan', unread: true },
  // running
  { session_id: 'b', host_id: 'h1', host: 'box1', user: 'me', title: 'zsh',   cwd: '/Users/me',      cols: 100, rows: 30, task_state: 'running',       current_command: 'npm test' },
  // failed + unread
  { session_id: 'c', host_id: 'h2', host: 'box2', user: 'me', title: 'go',    cwd: '/srv/api',       cols: 120, rows: 40, task_state: 'failed',        current_command: 'go test ./...', unread: true },
  // completed + seen → fold
  { session_id: 'd', host_id: 'h2', host: 'box2', user: 'me', title: 'ls',    cwd: '/srv/api',       cols: 120, rows: 40, task_state: 'completed',     current_command: 'ls',           unread: false },
]
let platform: ReturnType<typeof createFakePlatform>

beforeEach(() => {
  vi.clearAllMocks()
  localStorage.clear()
  resetGroupBy()
  platform = createFakePlatform()
  ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>).mockResolvedValue(sessions)
  __setPlatformForTests(platform)
})
afterEach(() => { __setPlatformForTests(null) })

describe('MobileSessionList', () => {
  it('groups sessions by state by default (excluding completed+seen) and orders by STATE_ORDER', async () => {
    // Default groupBy is 'host'; flip via button to land on 'state' for this test.
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="group-toggle"]').trigger('click')
    await flushPromises()

    // Headers present, in waiting_input → failed → running order; completed+seen
    // belongs to the fold and must NOT appear as a state group.
    const headers = w.findAll('[data-testid^="state-group-"]').map((el) => el.attributes('data-testid'))
    expect(headers).toEqual([
      'state-group-waiting_input',
      'state-group-failed',
      'state-group-running',
    ])
  })

  it('groups by host when groupBy = host', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const headers = w.findAll('[data-testid^="host-group-"]').map((el) => el.attributes('data-testid'))
    expect(headers).toEqual(['host-group-h1', 'host-group-h2'])
  })

  it('renders a card per non-folded session with short command + cwd', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    const cards = w.findAll('[data-testid="task-card"]')
    expect(cards).toHaveLength(3)              // d is folded
    expect(cards[0]!.text()).toContain('codex')
  })

  it('emits open(session) when a card body is tapped', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="card-body"]').trigger('click')
    expect(w.emitted('open')).toBeTruthy()
  })

  it('row ✓ posts markSessionsSeen with just that session id', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    await w.find('[data-testid="row-mark-read"]').trigger('click')
    expect(platform.sessions.markSessionsSeen).toHaveBeenCalledWith({ ids: ['a'] })
  })

  it('group ✓ posts markSessionsSeen with unread ids of that group', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    // Group h1 has session a (unread). Click its group-level mark-all.
    await w.find('[data-testid="mark-all-host-h1"]').trigger('click')
    expect(platform.sessions.markSessionsSeen).toHaveBeenCalledWith({ ids: ['a'] })
  })

  it('footer ✓ posts markSessionsSeen with { all: true } when totalUnread > 0', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.find('[data-testid="footer-mark-all"]').exists()).toBe(true)
    await w.find('[data-testid="footer-mark-all"]').trigger('click')
    expect(platform.sessions.markSessionsSeen).toHaveBeenCalledWith({ all: true })
  })

  it('completed+seen sessions are hidden in the default fold and revealed when toggled', async () => {
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.find('[data-testid="completed-fold-row-d"]').exists()).toBe(false)
    await w.find('[data-testid="completed-fold-toggle"]').trigger('click')
    expect(w.find('[data-testid="completed-fold-row-d"]').exists()).toBe(true)
  })

  it('emits tokenInvalid when listRemoteSessions throws relay_unauthorized', async () => {
    ;(platform.sessions.listRemoteSessions as ReturnType<typeof vi.fn>)
      .mockRejectedValueOnce(new Error('relay_unauthorized'))
    const w = mount(MobileSessionList, { props: { openSessionIds: [] } })
    await flushPromises()
    expect(w.emitted('tokenInvalid')).toBeTruthy()
  })
})
```

- [ ] **Step 7.2: Confirm failure**

```bash
npx vitest run src/mobile/__tests__/MobileSessionList.test.ts
```

Expected: FAIL — the existing component renders the old layout and testids.

- [ ] **Step 7.3: Replace `MobileSessionList.vue` with the new implementation**

Overwrite `desktop/frontend/src/mobile/MobileSessionList.vue` with:

```vue
<script setup lang="ts">
import { computed, ref, onMounted } from 'vue'
import { usePlatform } from '../platform'
import type { RemoteSession } from '../platform/types'
import { useI18n } from '../i18n/useI18n'
import { useSessions } from '../composables/useSessions'
import { useTaskGroupBy } from '../composables/useTaskGroupBy'
import { hostName as hostNameHelper } from '../lib/sessionLabel'
import { getUserHomeDir } from '../lib/api'
import type { TaskState } from '../lib/taskState'
import TaskStateIcon from '../components/TaskStateIcon.vue'
import MobileSessionCard from './MobileSessionCard.vue'

defineProps<{ openSessionIds: string[] }>()
const emit = defineEmits<{
  (e: 'open', info: RemoteSession): void
  (e: 'openSettings'): void
  (e: 'tokenInvalid'): void
}>()

const platform = usePlatform()
const { t } = useI18n()
const groupByState = useTaskGroupBy()
const groupBy = computed(() => groupByState.activeId.value)

const remote = ref<RemoteSession[]>([])
const local = ref<RemoteSession[]>([])  // mobile has no local PTYs
const sessions = useSessions(local, remote)

// Single source of truth for which sessions belong to the bottom fold —
// reuse useSessions.completedSeen verbatim so mobile and desktop never drift.
const foldedIds = computed(() => new Set(sessions.completedSeen.value.map((s) => s.session_id)))
function inFold(s: RemoteSession): boolean { return foldedIds.value.has(s.session_id) }

const error = ref<string | null>(null)
const loading = ref(false)
const foldOpen = ref(false)
const home = ref('')

const STATE_ORDER: TaskState[] = [
  'waiting_input', 'failed', 'running',
  'completed', 'idle', 'disconnected', 'closed',
]

// Group keys for whichever mode is active. State mode honours STATE_ORDER;
// host mode is alphabetical by host_id. Either way, drop groups that have no
// non-folded sessions (e.g. a host group whose only sessions are completed+seen).
const groupKeys = computed<string[]>(() => {
  const candidates = groupBy.value === 'state'
    ? STATE_ORDER.filter((s) => (sessions.byState.value[s] ?? []).length > 0)
    : Object.keys(sessions.byHost.value).sort()
  return candidates.filter((k) => byGroup(k).some((s) => !inFold(s)))
})

function byGroup(key: string): RemoteSession[] {
  if (groupBy.value === 'state') return sessions.byState.value[key] ?? []
  return sessions.byHost.value[key] ?? []
}

function unreadCount(key: string): number {
  if (groupBy.value === 'state') return sessions.unreadByState.value[key] ?? 0
  return sessions.unreadByHost.value[key] ?? 0
}

function primaryState(key: string): TaskState {
  if (groupBy.value === 'state') return key as TaskState
  return sessions.primaryStateForHost(key)
}

function groupHeader(key: string): string {
  if (groupBy.value === 'state') {
    switch (key) {
      case 'running': return t('mobile.taskStates.running')
      case 'waiting_input': return t('mobile.taskStates.waiting_input')
      case 'completed': return t('mobile.taskStates.completed')
      case 'failed': return t('mobile.taskStates.failed')
      case 'disconnected': return t('mobile.taskStates.disconnected')
      case 'closed': return t('mobile.taskStates.closed')
      default: return t('mobile.taskStates.idle')
    }
  }
  return hostNameHelper(key, sessions.byHost.value[key], t('sessions.unknownHost'))
}

function unreadIdsForGroup(key: string): string[] {
  return byGroup(key).filter((s) => s.unread).map((s) => s.session_id)
}

function groupTestId(key: string): string {
  return groupBy.value === 'state' ? `state-group-${key}` : `host-group-${key}`
}

function groupMarkAllTestId(key: string): string {
  // Distinct prefix so [data-testid^="state-group-"] / [data-testid^="host-group-"]
  // does NOT pick up the ✓ button when querying for section headers.
  return groupBy.value === 'state' ? `mark-all-state-${key}` : `mark-all-host-${key}`
}

async function refresh(): Promise<void> {
  loading.value = true
  error.value = null
  try {
    remote.value = await platform.sessions.listRemoteSessions()
  } catch (e) {
    const msg = e instanceof Error ? e.message : String(e)
    if (msg === 'relay_unauthorized') { emit('tokenInvalid'); return }
    error.value = msg
    remote.value = []
  } finally {
    loading.value = false
  }
}

async function onMarkSeen(p: { ids: string[] } | { all: true }) {
  try {
    await platform.sessions.markSessionsSeen?.(p)
  } catch (e) {
    if (e instanceof Error && e.message === 'relay_unauthorized') {
      emit('tokenInvalid'); return
    }
    console.warn('mark-seen failed', e)
    return
  }
  await refresh()
}

function toggleGroupBy() {
  void groupByState.setGroupBy(groupBy.value === 'host' ? 'state' : 'host')
}

onMounted(async () => {
  try { home.value = await getUserHomeDir() } catch { /* leave empty */ }
  await refresh()
})
</script>

<template>
  <div class="list">
    <header class="bar">
      <span class="title">{{ t('mobile.sessionsTitle') }}<span v-if="remote.length" class="count"> · {{ remote.length }}</span></span>
      <button
        class="group-toggle"
        data-testid="group-toggle"
        :title="t('tasks.settings.groupBy')"
        @click="toggleGroupBy"
      >{{ groupBy === 'state' ? t('tasks.settings.groupByState') : t('tasks.settings.groupByHost') }}</button>
      <button data-testid="refresh" class="icon" :disabled="loading" @click="refresh" :aria-label="t('common.refresh')">
        <svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M21 12a9 9 0 1 1-2.64-6.36" /><path d="M21 3v6h-6" /></svg>
      </button>
      <button data-testid="gear" class="icon" @click="emit('openSettings')" :aria-label="t('common.settings')">
        <svg viewBox="0 0 24 24" width="19" height="19" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3" /><path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" /></svg>
      </button>
    </header>

    <div class="body">
      <p v-if="error" data-testid="relay-disconnected" class="empty disconnected">
        {{ t('mobile.relayDisconnected') }} · {{ error }}
      </p>

      <section
        v-for="key in groupKeys"
        :key="key"
        class="group"
        :data-testid="groupTestId(key)"
      >
        <header class="grouphdr">
          <span class="caret">▼</span>
          <span class="gname">{{ groupHeader(key) }}</span>
          <span class="counts">
            <TaskStateIcon :state="primaryState(key)" :size="10" />
            <span class="count">{{ byGroup(key).length }}</span>
          </span>
          <span v-if="unreadCount(key) > 0" class="unread-badge">{{ t('tasks.unreadBadge', { count: unreadCount(key) }) }}</span>
          <button
            v-if="unreadCount(key) > 0"
            class="group-mark-all"
            :data-testid="groupMarkAllTestId(key)"
            :title="t('tasks.markAllRead')"
            @click="onMarkSeen({ ids: unreadIdsForGroup(key) })"
          >✓</button>
        </header>
        <MobileSessionCard
          v-for="s in byGroup(key).filter((x) => !inFold(x))"
          :key="s.session_id"
          :session="s"
          :home="home"
          data-testid="task-card"
          @open="emit('open', s)"
          @markSeen="onMarkSeen"
        />
      </section>

      <section v-if="sessions.completedSeen.value.length > 0" class="completed-fold">
        <button
          class="fold-toggle"
          data-testid="completed-fold-toggle"
          @click="foldOpen = !foldOpen"
        >{{ foldOpen ? '▼' : '▶' }} {{ t('tasks.completedFold') }} · {{ sessions.completedSeen.value.length }}</button>
        <template v-if="foldOpen">
          <div
            v-for="s in sessions.completedSeen.value"
            :key="s.session_id"
            class="fold-row"
            :data-testid="`completed-fold-row-${s.session_id}`"
            @click="emit('open', s)"
          >
            <TaskStateIcon :state="(s.task_state as TaskState | undefined) ?? 'idle'" :size="12" />
            <span class="cmd">{{ s.current_command || s.title }}</span>
            <span class="meta">{{ s.host }}·{{ s.user }}</span>
          </div>
        </template>
      </section>

      <p v-if="!loading && !error && groupKeys.length === 0 && sessions.completedSeen.value.length === 0" class="empty">
        {{ t('mobile.noRemoteSessions') }}
      </p>
    </div>

    <footer v-if="sessions.totalUnread.value > 0" class="footer">
      <button
        class="footer-mark-all"
        data-testid="footer-mark-all"
        @click="onMarkSeen({ all: true })"
      >{{ t('tasks.markAllRead') }}</button>
    </footer>
  </div>
</template>

<style scoped>
.list { min-height: 100vh; box-sizing: border-box; padding: env(safe-area-inset-top) 0 0; display: flex; flex-direction: column; background: var(--bg, #05070d); color: var(--fg, #e6e7ea); font-family: var(--font-sans); }
.bar { display: flex; align-items: center; gap: 8px; height: 48px; padding: 0 12px; border-bottom: 1px solid #1e2638; background: #0b1020; }
.bar .title { flex: 1; font-weight: 600; }
.bar .count { color: #8d93a3; font-weight: 400; margin-left: 4px; font-family: var(--font-mono); font-size: 0.85em; }
.group-toggle { background: none; border: 1px solid rgba(255,255,255,0.12); color: inherit; border-radius: 4px; font-size: 11px; padding: 4px 10px; cursor: pointer; }
.group-toggle:hover { background: rgba(255,255,255,0.05); }
.icon { display: inline-flex; align-items: center; justify-content: center; background: none; border: none; color: #8d93a3; padding: 4px; min-width: 44px; min-height: 44px; }
.body { flex: 1; overflow: auto; padding: 12px; }
.group { margin-bottom: 14px; }
.grouphdr { display: flex; align-items: center; gap: 6px; padding: 4px 2px 8px; font-size: 0.78rem; color: #c6cad5; font-family: var(--font-mono); }
.grouphdr .caret { font-size: 9px; color: #8d93a3; }
.grouphdr .gname { flex: 0 0 auto; }
.grouphdr .counts { margin-left: auto; display: inline-flex; align-items: center; gap: 2px; }
.grouphdr .count { font-size: 0.72rem; color: #8d93a3; }
.unread-badge { font-size: 10px; opacity: 0.9; background: rgba(255,255,255,0.06); border-radius: 3px; padding: 1px 4px; }
.group-mark-all { background: none; border: none; color: inherit; cursor: pointer; padding: 0 6px; font-size: 14px; min-width: 32px; min-height: 32px; }
.completed-fold { border-top: 1px solid rgba(255,255,255,0.06); margin-top: 6px; padding-top: 4px; }
.fold-toggle { background: none; border: none; cursor: pointer; padding: 8px 6px; width: 100%; text-align: left; color: inherit; opacity: 0.75; font-family: var(--font-mono); font-size: 0.78rem; }
.fold-row { display: flex; align-items: center; gap: 8px; padding: 6px 10px; opacity: 0.7; font-family: var(--font-mono); font-size: 0.78rem; }
.fold-row .cmd { flex: 1 1 auto; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
.fold-row .meta { color: #8d93a3; }
.empty { color: #8d93a3; font-size: 0.85rem; text-align: center; padding: 40px 12px; line-height: 1.6; }
.disconnected { color: #f87171; }
.footer { padding: 8px 12px max(8px, env(safe-area-inset-bottom)); border-top: 1px solid #1e2638; background: #0b1020; }
.footer-mark-all { width: 100%; min-height: 44px; background: none; border: 1px solid rgba(255,255,255,0.12); color: inherit; border-radius: 6px; padding: 8px 12px; cursor: pointer; }
.footer-mark-all:hover { background: rgba(255,255,255,0.05); }
</style>
```

- [ ] **Step 7.4: Run the rewritten test file**

```bash
npx vitest run src/mobile/__tests__/MobileSessionList.test.ts
```

Expected: PASS.

- [ ] **Step 7.5: Run the wider mobile + composable test set to catch regressions**

```bash
npx vitest run src/mobile/ src/composables/ src/components/Task
```

Expected: PASS.

- [ ] **Step 7.6: Commit**

```bash
git add desktop/frontend/src/mobile/MobileSessionList.vue \
        desktop/frontend/src/mobile/__tests__/MobileSessionList.test.ts
git commit -m "$(cat <<'EOF'
mobile: align session list with desktop TaskSidebar

- Group sessions by host or state; toggle in the top bar, persisted via
  useTaskGroupBy (same singleton as the desktop sidebar)
- Cards: TaskStateIcon + optional state label + short command + cwd, with
  host·user as the helper line; drop type chip, open badge, dims,
  permission, duration, exit code, last-output, error-line, meta string
- Row ✓ / group ✓ / footer "Mark all read" all wired through
  platform.sessions.markSessionsSeen
- Completed-seen sessions move to a collapsed fold at the bottom
EOF
)"
```

---

## Task 8: End-to-end verification

**Files:** none

Validate the change holds together across the two surfaces and the relay.

- [ ] **Step 8.1: Run the full frontend test suite**

```bash
cd desktop/frontend
npx vitest run
```

Expected: PASS. If a desktop-side test fails, treat it as a regression of the refactor — re-examine the change rather than the test.

- [ ] **Step 8.2: Type-check the frontend**

```bash
npx vue-tsc --noEmit
```

Expected: no errors.

- [ ] **Step 8.3: Run the relay test suite**

```bash
cd ../../
go test ./internal/relay/...
```

Expected: PASS.

- [ ] **Step 8.4: Manually exercise the mobile list (Capacitor build)**

Build the Capacitor bundle and load it in the iOS simulator (per the project's existing capacitor workflow — `desktop/frontend/dist-capacitor` is the bundle root). Confirm:

1. List loads, group-toggle flips between Host and State.
2. Cards show command + cwd + host·user only.
3. Unread badges + row ✓ + footer ✓ appear and clearing them survives `refresh`.
4. completed-seen sessions appear only after expanding the fold.

If you cannot run the simulator (no macOS GUI access), explicitly note that step 8.4 is deferred and ask the user to verify before merging — do NOT claim the change is complete.

- [ ] **Step 8.5: Final summary commit (only if changes were made during 8.x)**

```bash
git status
# If clean, nothing to commit. If a follow-up fix was needed during 8.1-8.4,
# stage and commit it with a short message describing the regression.
```

---

## Spec coverage check

- Spec §"Top bar" → Task 7 (group-toggle button + refresh + settings).
- Spec §"Group header" → Task 7 (`state-group-*` / `host-group-*`, primary state icon, count, unread badge, group ✓).
- Spec §"Card" → Tasks 6 + 7 (state icon + state label + short cmd + cwd + host·user + unread dot + row ✓).
- Spec §"Completed-seen fold" → Task 7 (uses `sessions.completedSeen` directly).
- Spec §"Footer" → Task 7 (visible when `totalUnread > 0`).
- Spec §"Platform bridge" → Tasks 4 + 5.
- Spec §"Relay: Bearer bypass" → Tasks 1 + 2.
- Spec §"`lib/sessionLabel.ts`" → Task 3.
- Spec §"useSessions reuse" → Task 7 (mobile imports `useSessions`).
- Spec §"Edge cases — useTaskGroupBy localStorage fallback" → covered by `useTaskGroupBy`'s existing test path; Task 7 test uses `__resetForTests` to avoid singleton bleed.
- Spec §"Open risks" → Task 8 manual verification.

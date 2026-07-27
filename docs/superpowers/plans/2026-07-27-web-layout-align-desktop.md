# Web 主入口布局对齐 desktop Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Web 主入口从"两态简单页"切换到 desktop 布局（TabBar + TaskSidebar + PaneSplit），全端 pin 同步作为其中一小块随本次落地。

**Architecture:** 给 `desktop/frontend` 加第三个 platform 实现 `platform/web.ts` 与入口 `main.web.ts`，让 web 的 `main.html` 挂到 desktop 的 `App.vue`。Pin 通过既有 `PrefsSyncEngine` 加第 7 个 synced key (`pinned_session_ids`) 实现 LWW 全端收敛。窄屏 drawer 化 TaskSidebar 惠及浏览器窄视口 + desktop 拖窄窗口；Capacitor mobile app 挂 `MobileApp.vue` 不受本次影响。

**Tech Stack:** Vue 3 + TypeScript + Vite, Go `internal/prefssync`, localStorage, xterm.js, Wails/Capacitor platform bridges.

**Spec:** `docs/superpowers/specs/2026-07-27-web-layout-align-desktop-design.md`

## Global Constraints

- 用户面向对话中文；code / commit / doc 英文
- 无向后兼容包袱（本项目单用户 —— 见 memory `feedback_no_backward_compat.md`）
- 每个 PR 独立可 merge、独立可回滚；前 5 个 PR 全部是"加"，不改现有 web 主入口行为；PR 6 是唯一切换点
- LWW 冲突策略沿用现有 `PrefsSyncEngine`（整 list 覆写）
- 三端 UI 差异只通过 `Platform` interface 隔离；不引入运行时环境嗅探（除 vite build target）
- `Capabilities.localPty` 字段**已存在**（`platform/types.ts:38`），wails/capacitor 已填；本 plan 只需要在 UI 里"用"这个字段

---

## Phase 1 · Pin sync 接进 PrefsSyncEngine（PR 1）

后端 + TS 镜像。三端桌面 pin 同步链路上线；web 端此时还没接入（PR 4 才有 web），mobile 端 platform.getPins/setPins 在 Phase 2 才有——所以 Phase 1 上线后 pin 同步在 **两台桌面之间**就已生效。

### Task 1.1: Add `pinned_session_ids` to Go `syncedKeys`

**Files:**
- Modify: `internal/prefssync/sync.go:51-58`
- Test: `internal/prefssync/sync_test.go`

**Interfaces:**
- Consumes: (none)
- Produces: `syncedKeys` now contains `"pinned_session_ids"`; `SyncedKeys()` returns sorted list including it.

- [ ] **Step 1: Write the failing test**

Add to `internal/prefssync/sync_test.go`:

```go
func TestSyncedKeys_IncludesPinnedSessionIds(t *testing.T) {
	keys := SyncedKeys()
	found := false
	for _, k := range keys {
		if k == "pinned_session_ids" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SyncedKeys() = %v; want pinned_session_ids", keys)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/prefssync/ -run TestSyncedKeys_IncludesPinnedSessionIds -v`
Expected: FAIL (`want pinned_session_ids`)

- [ ] **Step 3: Add key to `syncedKeys`**

In `internal/prefssync/sync.go:51-58`, add one line at the end of the slice:

```go
var syncedKeys = []string{
	"locale_preference",
	"quick_templates",
	"notifications_enabled",
	"ai_notifications_only",
	"command_notify_threshold_seconds",
	"shell_integration_enabled",
	"pinned_session_ids",
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./internal/prefssync/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add internal/prefssync/sync.go internal/prefssync/sync_test.go
git commit -m "feat(prefssync): add pinned_session_ids to synced keys"
```

---

### Task 1.2: `appConfigAdapter` Read/Write for `pinned_session_ids`

**Files:**
- Modify: `desktop/prefssync_adapter.go:22-102`
- Test: `desktop/prefssync_adapter_test.go` (new file OR extend existing test file)

**Interfaces:**
- Consumes: `appConfig.PinnedSessionIDs []string` (already exists — `desktop/config.go:196`)
- Produces: `ReadValue("pinned_session_ids")` returns JSON of `[]string`; `WriteValue("pinned_session_ids", raw)` unmarshals into `PinnedSessionIDs`.

- [ ] **Step 1: Write the failing test**

Create or extend `desktop/prefssync_adapter_test.go`:

```go
package main

import (
	"encoding/json"
	"testing"

	"github.com/attson/atterm/internal/prefssync"
)

func TestAdapter_PinnedSessionIds_RoundTrip(t *testing.T) {
	cs := newTestConfigStore(t)  // reuse existing helper if present, else inline appConfig
	a := newAppConfigAdapter(cs)

	want := []string{"sid-a", "sid-b"}
	raw, _ := json.Marshal(want)
	if err := a.WriteValue("pinned_session_ids", raw); err != nil {
		t.Fatalf("WriteValue: %v", err)
	}

	got, ok := a.ReadValue("pinned_session_ids")
	if !ok {
		t.Fatal("ReadValue returned ok=false")
	}
	var back []string
	if err := json.Unmarshal(got, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(back) != 2 || back[0] != "sid-a" || back[1] != "sid-b" {
		t.Fatalf("got %v; want %v", back, want)
	}

	// Adapter must expose the new key.
	found := false
	for _, k := range a.Keys() {
		if k == "pinned_session_ids" { found = true; break }
	}
	if !found {
		t.Fatalf("Keys() missing pinned_session_ids: %v", a.Keys())
	}

	_ = prefssync.Meta{} // silence unused import if none other
}
```

If `newTestConfigStore` doesn't exist, add inline: build a tmpdir + call `newConfigStore` following patterns in `desktop/config_test.go`.

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./desktop/ -run TestAdapter_PinnedSessionIds -v`
Expected: FAIL (`unknown key pinned_session_ids`)

- [ ] **Step 3: Add `pinned_session_ids` cases to adapter**

In `desktop/prefssync_adapter.go`:

In `ReadValue` (after the `shell_integration_enabled` case, before the trailing `return nil, false`):

```go
case "pinned_session_ids":
	b, _ := json.Marshal(c.PinnedSessionIDs)
	return b, true
```

In `WriteValue` (after the `shell_integration_enabled` case, before the `default`):

```go
case "pinned_session_ids":
	var ids []string
	if err := json.Unmarshal(value, &ids); err != nil {
		return err
	}
	c.PinnedSessionIDs = ids
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./desktop/ -v`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add desktop/prefssync_adapter.go desktop/prefssync_adapter_test.go
git commit -m "feat(desktop): adapter reads/writes pinned_session_ids"
```

---

### Task 1.3: `SetPinnedSessionIds` triggers push; `isPrefCustomized` covers key

**Files:**
- Modify: `desktop/app.go:1391-1420` (`SetPinnedSessionIds`) and `desktop/app.go:2317+` (`isPrefCustomized`)
- Test: `desktop/app_test.go` (extend existing pin tests)

**Interfaces:**
- Consumes: `a.markPrefDirtyAndPush(key string)` (`desktop/app.go:2255`)
- Produces: Every `SetPinnedSessionIds` call ends with `markPrefDirtyAndPush("pinned_session_ids")`; `isPrefCustomized(c)("pinned_session_ids")` returns `true` when `len(c.PinnedSessionIDs) > 0`.

- [ ] **Step 1: Write the failing test**

Add to `desktop/app_test.go` after `TestPinnedSessionIds_NilClears`:

```go
func TestPinnedSessionIds_MarksPrefDirty(t *testing.T) {
	a := newTestApp(t)
	// After Set, prefsMeta[pinned_session_ids].Dirty must be true.
	if err := a.SetPinnedSessionIds([]string{"sid-x"}); err != nil {
		t.Fatalf("SetPinnedSessionIds: %v", err)
	}
	m := a.cfgStore.Get().PrefsMeta["pinned_session_ids"]
	if !m.Dirty {
		t.Fatalf("prefs_meta.pinned_session_ids.dirty = false; want true")
	}
	if m.UpdatedAtLocal <= 0 {
		t.Fatalf("UpdatedAtLocal = %d; want > 0", m.UpdatedAtLocal)
	}
}

func TestIsPrefCustomized_PinnedSessionIds(t *testing.T) {
	fn := isPrefCustomized(appConfig{PinnedSessionIDs: []string{"sid"}})
	if !fn("pinned_session_ids") {
		t.Fatal("expected true when list has entries")
	}
	empty := isPrefCustomized(appConfig{})
	if empty("pinned_session_ids") {
		t.Fatal("expected false when list empty")
	}
}
```

Note: `newTestApp` may or may not exist — if not, model after existing `TestPinnedSessionIds_RoundTrip` (which constructs `&App{}` + `cfgStore` in-line).

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./desktop/ -run "TestPinnedSessionIds_MarksPrefDirty|TestIsPrefCustomized_PinnedSessionIds" -v`
Expected: FAIL

- [ ] **Step 3: Add `markPrefDirtyAndPush` call to `SetPinnedSessionIds`**

At the end of `SetPinnedSessionIds` (`desktop/app.go`), after `cfg.PinnedSessionIDs = out` and the `cfgStore.Set(cfg)` (or equivalent), append:

```go
	a.markPrefDirtyAndPush("pinned_session_ids")
	return nil
```

(Adjust to match the actual return shape — if it already returns `nil`, add the `markPrefDirtyAndPush` line just before that `return`.)

- [ ] **Step 4: Add `case` to `isPrefCustomized`**

In `desktop/app.go::isPrefCustomized`, add after the `shell_integration_enabled` case:

```go
	case "pinned_session_ids":
		return len(c.PinnedSessionIDs) > 0
```

- [ ] **Step 5: Run tests — expect PASS**

Run: `go test ./desktop/ -v`
Expected: all PASS (existing pin RoundTrip / Dedupe / NilClears also still pass; the marker + isCustomized cases new-PASS)

- [ ] **Step 6: Commit**

```bash
git add desktop/app.go desktop/app_test.go
git commit -m "feat(desktop): pin changes trigger prefs push + seed on first login"
```

---

### Task 1.4: Regenerate Wails bindings (if needed) & add key to TS `SYNCED_KEYS`

**Files:**
- Modify: `desktop/frontend/src/lib/prefsSync.ts:6-14`
- Modify: `web/src/shared/sync/prefsSync.ts:8-14`
- Test: `desktop/frontend/src/lib/prefsSync.test.ts`, `web/tests/unit/shared/prefsSync.test.ts`

**Interfaces:**
- Consumes: (none)
- Produces: `SYNCED_KEYS` on both TS mirrors contains `'pinned_session_ids'`; both mirrors match Go's `syncedKeys` exactly (guarded by test).

- [ ] **Step 1: Write the failing tests**

`desktop/frontend/src/lib/prefsSync.test.ts` — add:

```ts
it('SYNCED_KEYS includes pinned_session_ids', () => {
  expect(SYNCED_KEYS).toContain('pinned_session_ids' as any)
})
```

`web/tests/unit/shared/prefsSync.test.ts` — add:

```ts
it('SYNCED_KEYS includes pinned_session_ids', () => {
  expect(SYNCED_KEYS).toContain('pinned_session_ids' as any)
})
```

- [ ] **Step 2: Run tests — expect FAIL**

Run: `cd desktop/frontend && npm test -- prefsSync.test`
Run: `cd web && npm test -- prefsSync.test`
Expected: both FAIL.

- [ ] **Step 3: Update both `SYNCED_KEYS`**

In `desktop/frontend/src/lib/prefsSync.ts:6-14`, add `'pinned_session_ids'` as the last entry:

```ts
export const SYNCED_KEYS = [
  'locale_preference',
  'quick_templates',
  'notifications_enabled',
  'command_notify_threshold_seconds',
  'shell_integration_enabled',
  'pinned_session_ids',
] as const
```

Note: the TS mirror is missing `ai_notifications_only` (the Go side has it at `sync.go:56`); this is a pre-existing drift, not our job in this PR. Only add `pinned_session_ids`.

Same edit for `web/src/shared/sync/prefsSync.ts:8-14`.

- [ ] **Step 4: Run tests — expect PASS**

Run: `cd desktop/frontend && npm test -- prefsSync.test`
Run: `cd web && npm test -- prefsSync.test`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/prefsSync.ts desktop/frontend/src/lib/prefsSync.test.ts \
        web/src/shared/sync/prefsSync.ts web/tests/unit/shared/prefsSync.test.ts
git commit -m "feat(prefssync): mirror pinned_session_ids into TS synced-keys"
```

**PR 1 checkpoint:** Bundle the 4 commits above into one PR titled `feat(prefssync): add pinned_session_ids as a synced preference`. Sanity check locally: `make test` (or the repo's `go test ./...` + `npm test`) passes across desktop/web.

---

## Phase 2 · `SessionBridge.getPins/setPins` + `useSessionPins` platform 化（PR 2）

给 `SessionBridge` 加两个方法，让 `useSessionPins` 从 Wails-only shim 抽出来走 platform。同时把 `prefs:changed` 桥接进 `platform.events`。

Phase 2 上线后，desktop pin 通过 `platform.sessions.getPins/setPins` 工作（行为不变）；Capacitor 上 pin 存 localStorage + push（本 spec 顺带修复 mobile pin 存储链路）。

### Task 2.1: Add `getPins`/`setPins` to `SessionBridge` + Wails/Capacitor impls

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts:123-133` (SessionBridge)
- Modify: `desktop/frontend/src/platform/wails.ts` (impl)
- Modify: `desktop/frontend/src/platform/capacitor.ts` (impl)
- Modify: `desktop/frontend/src/platform/__tests__/{wails,capacitor}.test.ts`
- Modify: `desktop/frontend/src/platform/__tests__/_fakePlatform.ts` (add mock)

**Interfaces:**
- Consumes:
  - Wails: `bindings().GetPinnedSessionIds()`, `bindings().SetPinnedSessionIds(ids)` (existing, `desktop/frontend/src/lib/api.ts:380+`)
  - Capacitor: `notifyLocalChange('pinned_session_ids')` (from `desktop/frontend/src/lib/prefsSync.capacitor.ts`)
- Produces:
  ```ts
  interface SessionBridge {
    // ...existing...
    getPins(): Promise<string[]>
    setPins(ids: string[]): Promise<void>
  }
  ```

- [ ] **Step 1: Add fields to interface**

`desktop/frontend/src/platform/types.ts::SessionBridge`, after `markSessionsSeen`:

```ts
  /** Read the persisted pin list. Wails reads from appConfig; Capacitor/
   *  Web from localStorage. */
  getPins(): Promise<string[]>
  /** Persist the pin list. Wails writes to appConfig (which triggers a
   *  prefs push server-side); Capacitor/Web write to localStorage and
   *  notify the prefsSync engine locally. */
  setPins(ids: string[]): Promise<void>
```

- [ ] **Step 2: Wails impl**

`desktop/frontend/src/platform/wails.ts::createWailsPlatform`, inside the `sessions:` object:

```ts
      getPins: () => getPinnedSessionIds(),
      setPins: (ids) => setPinnedSessionIds(ids),
```

(Add corresponding imports at top from `../lib/api`.)

- [ ] **Step 3: Capacitor impl**

`desktop/frontend/src/platform/capacitor.ts::createCapacitorPlatform`, inside the `sessions:` object:

```ts
      getPins: async () => {
        try {
          const raw = localStorage.getItem('atterm.pinned_session_ids.value')
          if (raw === null) return []
          const v = JSON.parse(raw)
          return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
        } catch {
          return []
        }
      },
      setPins: async (ids) => {
        localStorage.setItem('atterm.pinned_session_ids.value', JSON.stringify(ids))
        notifyLocalChange('pinned_session_ids')
      },
```

(`notifyLocalChange` is already imported at line 6 for the templates path.)

- [ ] **Step 4: Update _fakePlatform**

`desktop/frontend/src/platform/__tests__/_fakePlatform.ts` — add stubs to the `sessions` object so tests that use the fake don't error:

```ts
      getPins: vi.fn().mockResolvedValue([]),
      setPins: vi.fn().mockResolvedValue(undefined),
```

- [ ] **Step 5: Add contract tests**

`desktop/frontend/src/platform/__tests__/wails.test.ts` — add a test that asserts `platform.sessions.getPins()` calls the Wails binding, and `setPins(ids)` forwards the arg. Model after existing bridge-method tests in the file (mock `bindings()` via existing helper).

`desktop/frontend/src/platform/__tests__/capacitor.test.ts` — add:

```ts
it('getPins reads pinned_session_ids from localStorage', async () => {
  localStorage.setItem('atterm.pinned_session_ids.value', JSON.stringify(['a', 'b']))
  const p = createCapacitorPlatform()
  await expect(p.sessions.getPins()).resolves.toEqual(['a', 'b'])
})

it('setPins writes localStorage + calls notifyLocalChange', async () => {
  const notify = vi.spyOn(await import('../../lib/prefsSync.capacitor'), 'notifyLocalChange')
  const p = createCapacitorPlatform()
  await p.sessions.setPins(['x'])
  expect(localStorage.getItem('atterm.pinned_session_ids.value')).toBe('["x"]')
  expect(notify).toHaveBeenCalledWith('pinned_session_ids')
})
```

- [ ] **Step 6: Run all frontend tests**

Run: `cd desktop/frontend && npm test`
Expected: all PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/platform/types.ts \
        desktop/frontend/src/platform/wails.ts \
        desktop/frontend/src/platform/capacitor.ts \
        desktop/frontend/src/platform/__tests__/_fakePlatform.ts \
        desktop/frontend/src/platform/__tests__/wails.test.ts \
        desktop/frontend/src/platform/__tests__/capacitor.test.ts
git commit -m "feat(platform): SessionBridge.getPins/setPins on wails + capacitor"
```

---

### Task 2.2: Refactor `useSessionPins.ts` — go through platform + expose `reload()`

**Files:**
- Modify: `desktop/frontend/src/composables/useSessionPins.ts`
- Modify: `desktop/frontend/src/composables/useSessionPins.test.ts`

**Interfaces:**
- Consumes: `usePlatform().sessions.getPins/setPins` (from Task 2.1); `usePlatform().events.on('prefs:changed', cb)`
- Produces: `useSessionPins()` returns the same interface plus `reload(): Promise<void>` that re-reads from the store bypassing the `loaded` guard. Existing consumers get the same `pinnedIds/pin/unpin/toggle/rename/flushNow/ready` API unchanged.

- [ ] **Step 1: Write the failing tests**

Extend `desktop/frontend/src/composables/useSessionPins.test.ts`:

```ts
test('reload re-reads from platform on demand', async () => {
  const getPins = vi.fn()
    .mockResolvedValueOnce(['a'])
    .mockResolvedValueOnce(['a', 'b'])
  __setPlatformForTests({
    ...fakePlatform(),
    sessions: { ...fakePlatform().sessions, getPins, setPins: vi.fn().mockResolvedValue(undefined) },
  } as any)

  const p = useSessionPins()
  await p.ready()
  expect(Array.from(p.pinnedIds.value)).toEqual(['a'])

  await p.reload()
  expect(Array.from(p.pinnedIds.value)).toEqual(['a', 'b'])
  expect(getPins).toHaveBeenCalledTimes(2)
})

test('prefs:changed event triggers reload', async () => {
  const getPins = vi.fn()
    .mockResolvedValueOnce([])
    .mockResolvedValueOnce(['new'])
  const events = fakeEventBus()
  __setPlatformForTests({
    ...fakePlatform(),
    events,
    sessions: { ...fakePlatform().sessions, getPins, setPins: vi.fn().mockResolvedValue(undefined) },
  } as any)

  const p = useSessionPins()
  await p.ready()
  expect(p.pinnedIds.value.size).toBe(0)

  events.emit('prefs:changed', undefined)
  await vi.waitFor(() => expect(p.pinnedIds.value.has('new')).toBe(true))
})
```

`fakePlatform()` / `fakeEventBus()` — use existing `_fakePlatform.ts` helpers or add them if missing.

- [ ] **Step 2: Run tests — expect FAIL**

Run: `cd desktop/frontend && npm test -- useSessionPins`
Expected: FAIL (`reload` not defined, `prefs:changed` handler not wired).

- [ ] **Step 3: Refactor `useSessionPins.ts`**

Rewrite the file to source pins through `usePlatform()`:

```ts
import { ref, type Ref } from "vue";
import { usePlatform } from "../platform";

const pinnedIds = ref<Set<string>>(new Set());
let loaded = false;
let loadPromise: Promise<void> | null = null;
let flushHandle: ReturnType<typeof setTimeout> | null = null;
let eventsBound = false;

const PERSIST_DEBOUNCE_MS = 300;

async function readFromPlatform(): Promise<void> {
  try {
    const list = await usePlatform().sessions.getPins();
    pinnedIds.value = new Set(list);
  } catch {
    /* best-effort — leave whatever value we had */
  }
}

async function loadOnce(): Promise<void> {
  if (loaded) return;
  if (loadPromise) return loadPromise;
  loadPromise = (async () => {
    try { await readFromPlatform(); }
    finally { loaded = true; loadPromise = null; }
  })();
  return loadPromise;
}

function schedulePersist(): void {
  if (flushHandle) clearTimeout(flushHandle);
  flushHandle = setTimeout(() => {
    flushHandle = null;
    void usePlatform().sessions.setPins(Array.from(pinnedIds.value)).catch((e) => {
      console.warn("[pins] schedulePersist failed", e);
    });
  }, PERSIST_DEBOUNCE_MS);
}

function bindEventsOnce(): void {
  if (eventsBound) return;
  eventsBound = true;
  usePlatform().events.on('prefs:changed', () => {
    void readFromPlatform();
  });
}

// ... pinFn/unpinFn/renameFn/flushNowFn unchanged from the original ...

export interface UseSessionPins {
  pinnedIds: Readonly<Ref<ReadonlySet<string>>>;
  isPinned: (id: string) => boolean;
  pin: (id: string) => void;
  unpin: (id: string) => void;
  toggle: (id: string) => void;
  rename: (oldId: string, newId: string) => void;
  flushNow: () => Promise<void>;
  ready: () => Promise<void>;
  /** Force a re-read from the store, ignoring the internal loaded guard.
   *  Bound to prefs:changed event; also callable directly. */
  reload: () => Promise<void>;
}

export function useSessionPins(): UseSessionPins {
  void loadOnce();
  bindEventsOnce();
  return {
    pinnedIds,
    isPinned: (id) => pinnedIds.value.has(id),
    pin: pinFn,
    unpin: unpinFn,
    toggle: (id) => (pinnedIds.value.has(id) ? unpinFn(id) : pinFn(id)),
    rename: renameFn,
    flushNow: flushNowFn,
    ready: () => loadOnce(),
    reload: () => readFromPlatform(),
  };
}

export function __resetForTests(): void {
  pinnedIds.value = new Set();
  loaded = false;
  loadPromise = null;
  eventsBound = false;
  if (flushHandle) { clearTimeout(flushHandle); flushHandle = null; }
}
```

Copy `pinFn/unpinFn/renameFn/flushNowFn` verbatim from the existing file (unchanged).

- [ ] **Step 4: Run tests — expect PASS**

Run: `cd desktop/frontend && npm test -- useSessionPins`
Expected: PASS.

- [ ] **Step 5: Run full frontend tests to catch regressions**

Run: `cd desktop/frontend && npm test`
Expected: PASS (including `App.test.ts`, which mocks Wails bindings — should still be compatible since `useSessionPins` now takes the platform path but the fake platform provides the same values).

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/composables/useSessionPins.ts \
        desktop/frontend/src/composables/useSessionPins.test.ts
git commit -m "refactor(pins): route through platform + expose reload()"
```

---

### Task 2.3: Wire `prefs:changed` into `platform.events` at each entry

**Files:**
- Modify: `desktop/frontend/src/main.ts:24-26`
- Modify: `desktop/frontend/src/main.capacitor.ts:51-55`

**Interfaces:**
- Consumes: `platform.events.emit('prefs:changed', undefined)`
- Produces: `platform.events` fires `prefs:changed` after any pull/push completes on both wails and capacitor entries.

- [ ] **Step 1: Wails entry — bridge Wails event → platform.events**

In `desktop/frontend/src/main.ts:24-26`, replace the existing `EventsOn('prefs:changed', ...)` body:

```ts
  EventsOn('prefs:changed', () => {
    window.dispatchEvent(new CustomEvent('atterm:prefs-changed'))
    platform.events.emit('prefs:changed', undefined)
  })
```

(Keep the `window.dispatchEvent` for any other consumer still listening on it — no need to break them in this PR; harmless.)

- [ ] **Step 2: Capacitor entry — emit after every pull**

In `desktop/frontend/src/main.capacitor.ts`, wrap the two `void prefsSync.pull().catch(() => {})` sites:

```ts
  void prefsSync.pull().then(() => platform.events.emit('prefs:changed', undefined)).catch(() => {})
  // Foreground PULL
  CapacitorApp.addListener('appStateChange', (state) => {
    if (state.isActive) {
      void prefsSync.pull().then(() => platform.events.emit('prefs:changed', undefined)).catch(() => {})
    }
  })
```

Also wrap the push side by re-declaring the shared prefsSync's `push` (or exposing a wrapped setter). Simpler approach: leave push events out for now — the emitter fires only after pulls (which is the direction that affects other clients' state). Local pushes already reflect immediately via `pinnedIds.value` write.

- [ ] **Step 3: Run tests**

Run: `cd desktop/frontend && npm test`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/main.ts desktop/frontend/src/main.capacitor.ts
git commit -m "feat(prefssync): bridge pull completion into platform.events prefs:changed"
```

**PR 2 checkpoint:** Bundle 2.1-2.3 into one PR titled `feat(platform): pins go through platform bridge + prefs:changed event`. Manual verify: run desktop, pin a session, restart desktop → still pinned; run mobile Capacitor build (if available), pin from desktop, mobile should not crash.

---

## Phase 3 · `caps.localPty` UI gating in `App.vue` (PR 3)

`Capabilities.localPty` 字段已存在，wails/capacitor 已填正确值；`App.vue` 里对本地 shell 相关按钮和 recovery 分支加 `caps.localPty` 判断，让 web platform（`localPty=false`）自然进入正确路径。

### Task 3.1: Gate local shell / split entrypoints in `App.vue`

**Files:**
- Modify: `desktop/frontend/src/App.vue` (multiple spots — grep for `newSession`, "本地", "local shell", "+" tab menu handlers)
- Test: `desktop/frontend/src/App.test.ts` (add a case with `caps.localPty=false`)

**Interfaces:**
- Consumes: `caps.localPty` (already in scope at `App.vue:32` as `const caps = $platform.caps`)
- Produces: When `caps.localPty === false`, "new local shell" TabBar entry and "split into local shell" pane action are hidden; UI clicking these buttons is impossible.

- [ ] **Step 1: Enumerate the entrypoints**

Run: `grep -n "newSession\|开新会话\|new local\|local shell\|本地" desktop/frontend/src/App.vue | head -30`

Expected: 3-4 locations. Typical:
- TabBar `+` menu → `spawnLocalShell()` handler
- Pane split → `spawnLocalShell()` in the new slot
- Recovery snapshot → `newSession(req)` for non-remote panes (already gated by `snap.remote`, but the button that triggers spawnLocalShell in fresh empty tabs isn't)

- [ ] **Step 2: Add failing test**

`desktop/frontend/src/App.test.ts` — add:

```ts
test('caps.localPty=false hides new-local-shell button', async () => {
  const platform = fakePlatform()
  platform.caps = { ...platform.caps, localPty: false }
  __setPlatformForTests(platform)
  const wrapper = mount(App, { /* usual setup */ })
  await flushPromises()
  expect(wrapper.find('[data-test="new-local-shell"]').exists()).toBe(false)
})
```

Add `data-test="new-local-shell"` to whichever button element triggers spawn (in App.vue's template).

- [ ] **Step 3: Run test — expect FAIL**

Run: `cd desktop/frontend && npm test -- App.test`
Expected: FAIL (button still visible OR selector missing).

- [ ] **Step 4: Add `v-if="caps.localPty"` to the button**

In `App.vue`, find the button/menu-item that calls `spawnLocalShell()` (or similar) and add `v-if="caps.localPty"`.

If the "+" tab menu has multiple items and only "new local" should hide, gate that item. If pane-split has a "split as local shell" option in a submenu, gate that too.

- [ ] **Step 5: Run test — expect PASS**

Run: `cd desktop/frontend && npm test -- App.test`
Expected: PASS.

- [ ] **Step 6: Run full frontend tests**

Run: `cd desktop/frontend && npm test`
Expected: PASS (no regression in Wails/Capacitor branches where `caps.localPty` is true/false as expected).

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts
git commit -m "feat(app): gate local-shell UI entrypoints on caps.localPty"
```

---

### Task 3.2: Gate recovery snapshot's local-fork branch

**Files:**
- Modify: `desktop/frontend/src/App.vue::executeRestore` (around line 903-910, the `newSession(req)` call for `snap.remote===false`)
- Test: extend `desktop/frontend/src/App.test.ts`

**Interfaces:**
- Consumes: `caps.localPty`
- Produces: If `caps.localPty === false` and a snapshot pane has `snap.remote===false`, that pane restore is skipped (rendered as empty/`{sessionId: null, remote: false}`) instead of calling `newSession`.

- [ ] **Step 1: Write the failing test**

Add to `App.test.ts`:

```ts
test('recovery skips local-fork panes when caps.localPty=false', async () => {
  const platform = fakePlatform()
  platform.caps = { ...platform.caps, localPty: false }
  __setPlatformForTests(platform)
  const wrapper = mount(App, {
    global: { provide: { snapshot: buildSnapshotWithLocalPane('sid-old') } }
    // ...adjust to match existing test setup for recovery
  })
  await flushPromises()
  // Assert: no newSession call fired
  expect(platform.sessions.newSession).not.toHaveBeenCalled()
  // Assert: pane rendered as empty
  expect(wrapper.find('[data-test="pane-empty"]').exists()).toBe(true)
})
```

Adjust setup to match how existing recovery tests structure the mocks (see `App.test.ts:517` — pins mock — for patterns).

- [ ] **Step 2: Run test — expect FAIL**

- [ ] **Step 3: Add guard in `executeRestore`**

Around `desktop/frontend/src/App.vue:900-910` (the `try` block that calls `newSession(req)`), wrap:

```ts
      if (!caps.localPty) {
        t.panes[i] = { sessionId: null, remote: false };
        continue;
      }
      try {
        const oldSid = snap.session_id || "";
        // ...existing code...
```

- [ ] **Step 4: Run test — expect PASS**

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts
git commit -m "feat(app): recovery skips local-fork panes when caps.localPty=false"
```

**PR 3 checkpoint:** Bundle 3.1-3.2 into PR `feat(app): gate local-shell paths on caps.localPty`. Manual verify on desktop (wails): local shell still spawns; on Capacitor (if buildable): button hidden.

---

## Phase 4 · `platform/web.ts` + tabs snapshot lib + `main.web.ts` (PR 4)

Only-add PR. Web assets that don't yet get used by any HTML entry.

### Task 4.1: Skeleton `platform/web.ts` with `caps` + stub bridges + first tests

**Files:**
- Create: `desktop/frontend/src/platform/web.ts`
- Create: `desktop/frontend/src/platform/__tests__/web.test.ts`

**Interfaces:**
- Consumes: (browser globals only)
- Produces: `createWebPlatform(): Platform` returning correct `caps` shape + method-full `sessions/relay/system/events/templates/auxKeys` bridges. Only stubs in this task; subsequent tasks flesh them out.

- [ ] **Step 1: Create the file with caps + empty bridge stubs**

`desktop/frontend/src/platform/web.ts`:

```ts
import type { Platform, Capabilities } from './types'
import { apiFetch } from '../../../web/src/shared/api/client'
// ^ path from desktop/frontend/src/platform/ to web/src/shared/api/client
// vite alias may already exist; adjust if resolving via alias.

const CAPS: Capabilities = {
  localPty: false,
  autoUpdate: false,
  pluginHost: false,
  windowControls: false,
  systemClipboard: true,
  notifications: typeof Notification !== 'undefined',
  fileDialog: true,
}

export function createWebPlatform(): Platform {
  // Populated in subsequent tasks.
  return {
    caps: CAPS,
    relay: {} as any,
    sessions: {} as any,
    system: {} as any,
    events: {} as any,
    templates: {} as any,
    auxKeys: {} as any,
  }
}
```

Note: the `apiFetch` cross-package import needs resolution. Options:
1. Add a vite alias `@webshared` in `desktop/frontend/vite.config.ts` and use `@webshared/api/client`
2. Reach across via relative path (fragile)
3. Copy the small `apiFetch` helper into desktop/frontend/src/lib/

Prefer option 1 (least code duplication). Update `desktop/frontend/vite.config.ts`:

```ts
// inside resolve.alias
'@webshared': path.resolve(__dirname, '../../web/src/shared'),
```

Then in `platform/web.ts`: `import { apiFetch } from '@webshared/api/client'`.

- [ ] **Step 2: Add caps test**

`desktop/frontend/src/platform/__tests__/web.test.ts`:

```ts
import { describe, it, expect } from 'vitest'
import { createWebPlatform } from '../web'

describe('web platform', () => {
  it('caps: localPty=false autoUpdate=false pluginHost=false windowControls=false', () => {
    const p = createWebPlatform()
    expect(p.caps.localPty).toBe(false)
    expect(p.caps.autoUpdate).toBe(false)
    expect(p.caps.pluginHost).toBe(false)
    expect(p.caps.windowControls).toBe(false)
    expect(p.caps.systemClipboard).toBe(true)
    expect(p.caps.fileDialog).toBe(true)
  })
})
```

- [ ] **Step 3: Run test**

Run: `cd desktop/frontend && npm test -- web.test`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add desktop/frontend/src/platform/web.ts \
        desktop/frontend/src/platform/__tests__/web.test.ts \
        desktop/frontend/vite.config.ts
git commit -m "feat(platform/web): skeleton + caps"
```

---

### Task 4.2: Relay + Session bridges in `platform/web.ts`

**Files:**
- Modify: `desktop/frontend/src/platform/web.ts`
- Modify: `desktop/frontend/src/platform/__tests__/web.test.ts`

**Interfaces:**
- Consumes: `@webshared/api/client::apiFetch`
- Produces: `platform.relay.{load,save,clear,fetchMe,consumePairing}`; `platform.sessions.{closeSession,listShells,listRemoteSessions,markSessionsSeen,getPins,setPins}`

- [ ] **Step 1: Write failing tests**

Extend `web.test.ts`:

```ts
it('relay.load reads atterm.relay.session', () => {
  localStorage.setItem('atterm.relay.session', JSON.stringify({ baseURL: 'http://x', sessionToken: 't' }))
  const p = createWebPlatform()
  return expect(p.relay.load()).resolves.toMatchObject({ base_url: 'http://x', session_token: 't' })
})

it('sessions.getPins reads pinned_session_ids from localStorage', () => {
  localStorage.setItem('atterm.pinned_session_ids.value', JSON.stringify(['a', 'b']))
  return expect(createWebPlatform().sessions.getPins()).resolves.toEqual(['a', 'b'])
})

it('sessions.setPins writes localStorage', async () => {
  await createWebPlatform().sessions.setPins(['x'])
  expect(localStorage.getItem('atterm.pinned_session_ids.value')).toBe('["x"]')
})

it('sessions.listShells returns empty', () => {
  return expect(createWebPlatform().sessions.listShells()).resolves.toEqual([])
})
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement relay + sessions bridges**

Fill in `platform/web.ts`:

```ts
import type { Platform, Capabilities, RelayBridge, SessionBridge, RemoteSession } from './types'
import { apiFetch } from '@webshared/api/client'

// helper for relay config storage — mirrors web's current key
const RELAY_KEY = 'atterm.relay.session'

const relay: RelayBridge = {
  async load() {
    try {
      const raw = localStorage.getItem(RELAY_KEY)
      if (!raw) return null
      const j = JSON.parse(raw)
      // Adapt web's stored shape to the internal RelayConfig shape.
      return {
        base_url: j.baseURL ?? '',
        session_token: j.sessionToken ?? '',
        expires_at: j.expiresAt ?? 0,
        allow_insecure_relay: !!j.allowInsecure,
      } as any
    } catch { return null }
  },
  async save(cfg) {
    localStorage.setItem(RELAY_KEY, JSON.stringify({
      baseURL: (cfg as any).base_url,
      sessionToken: (cfg as any).session_token,
      expiresAt: (cfg as any).expires_at,
      allowInsecure: (cfg as any).allow_insecure_relay,
    }))
  },
  async clear() {
    localStorage.removeItem(RELAY_KEY)
  },
  async fetchMe() {
    const { data } = await apiFetch<any>('/api/me')
    return data
  },
}

const sessions: SessionBridge = {
  async closeSession(sid) {
    await apiFetch(`/api/sessions/${encodeURIComponent(sid)}`, { method: 'DELETE' })
  },
  async listShells() { return [] },
  async listRemoteSessions(): Promise<RemoteSession[]> {
    const { data } = await apiFetch<{ items?: any[] }>('/api/sessions')
    // Map SessionInfo → RemoteSession same way listRemoteSessions in Wails does
    return (data.items ?? []).map((s: any) => ({ ...s, session_id: s.id })) as RemoteSession[]
  },
  async markSessionsSeen(opts) {
    await apiFetch('/api/sessions/seen', { method: 'POST', body: JSON.stringify(opts) })
  },
  async getPins() {
    try {
      const raw = localStorage.getItem('atterm.pinned_session_ids.value')
      if (raw === null) return []
      const v = JSON.parse(raw)
      return Array.isArray(v) ? v.filter((x): x is string => typeof x === 'string') : []
    } catch { return [] }
  },
  async setPins(ids) {
    localStorage.setItem('atterm.pinned_session_ids.value', JSON.stringify(ids))
    const { notifyLocalChange } = await import('@webshared/sync/prefsSync')
    notifyLocalChange('pinned_session_ids')
  },
}
```

Replace the empty stubs in `createWebPlatform()` with `relay, sessions`.

- [ ] **Step 4: Run tests — expect PASS**

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/platform/web.ts desktop/frontend/src/platform/__tests__/web.test.ts
git commit -m "feat(platform/web): relay + sessions bridges"
```

---

### Task 4.3: System + Events + Templates + AuxKeys bridges

**Files:**
- Modify: `desktop/frontend/src/platform/web.ts`
- Modify: `desktop/frontend/src/platform/__tests__/web.test.ts`

**Interfaces:**
- Consumes: browser APIs (`Notification`, `navigator.clipboard`, `window.open`)
- Produces: full `Platform` — no more `{} as any` stubs.

- [ ] **Step 1: Add failing tests**

Extend `web.test.ts`:

```ts
it('events on/emit/off roundtrip', () => {
  const p = createWebPlatform()
  const fn = vi.fn()
  const off = p.events.on('x', fn)
  p.events.emit('x', 'v')
  expect(fn).toHaveBeenCalledWith('v')
  off()
  p.events.emit('x', 'v2')
  expect(fn).toHaveBeenCalledTimes(1)
})

it('templates load/save/clear via localStorage', async () => {
  const p = createWebPlatform()
  await p.templates.save([{ id: '1', text: 't1' } as any])
  const list = await p.templates.load()
  expect(list.length).toBe(1)
  await p.templates.clear()
  expect(await p.templates.load()).toEqual([])
})

it('system.getEnvironment returns buildType=web', async () => {
  const info = await createWebPlatform().system.getEnvironment()
  expect(info?.buildType).toBe('web')
})
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

Add to `platform/web.ts`:

```ts
import type { SystemBridge, EventBus, TemplateBridge, AuxKeyBridge } from './types'

const events: EventBus = (() => {
  const map = new Map<string, Set<(data: unknown) => void>>()
  return {
    on(name, handler) {
      let set = map.get(name)
      if (!set) { set = new Set(); map.set(name, set) }
      set.add(handler)
      return () => { set!.delete(handler) }
    },
    emit(name, data) {
      const set = map.get(name)
      if (!set) return
      for (const fn of Array.from(set)) { try { fn(data) } catch {} }
    },
  }
})()

const system: SystemBridge = {
  async showNotification(title, body, data) {
    if (typeof Notification === 'undefined') return
    try {
      const n = new Notification(title, { body })
      if (data) n.onclick = () => events.emit('notification:click', data)
    } catch { /* permission denied — silent */ }
  },
  async getClipboardPaste() {
    if (!navigator.clipboard) return { text: '', image: undefined, filename: '' } as any
    try {
      const text = await navigator.clipboard.readText()
      return { text, image: undefined, filename: '' } as any
    } catch {
      return { text: '', image: undefined, filename: '' } as any
    }
  },
  async openExternalURL(url) {
    window.open(url, '_blank', 'noopener')
  },
  async getEnvironment() {
    return { buildType: 'web', platform: navigator.userAgent, arch: '' }
  },
}

const TEMPLATES_KEY = 'atterm.quick_templates.value'
const TEMPLATES_HIDDEN_KEY = 'atterm.templates_hidden.value'
const AUXKEYS_KEY = 'atterm.aux_keys.value'

const templates: TemplateBridge = {
  async load() {
    try { const raw = localStorage.getItem(TEMPLATES_KEY); return raw ? JSON.parse(raw) : [] }
    catch { return [] }
  },
  async save(list) { localStorage.setItem(TEMPLATES_KEY, JSON.stringify(list)) },
  async clear() { localStorage.removeItem(TEMPLATES_KEY) },
  async loadHidden() {
    try { return JSON.parse(localStorage.getItem(TEMPLATES_HIDDEN_KEY) ?? 'false') }
    catch { return false }
  },
  async saveHidden(h) { localStorage.setItem(TEMPLATES_HIDDEN_KEY, JSON.stringify(h)) },
}

const auxKeys: AuxKeyBridge = {
  async load() {
    try { const raw = localStorage.getItem(AUXKEYS_KEY); return raw ? JSON.parse(raw) : [] }
    catch { return [] }
  },
  async save(list) { localStorage.setItem(AUXKEYS_KEY, JSON.stringify(list)) },
  async clear() { localStorage.removeItem(AUXKEYS_KEY) },
}
```

Wire them into `createWebPlatform()`:

```ts
export function createWebPlatform(): Platform {
  return { caps: CAPS, relay, sessions, system, events, templates, auxKeys }
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `cd desktop/frontend && npm test -- web.test`

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/platform/web.ts desktop/frontend/src/platform/__tests__/web.test.ts
git commit -m "feat(platform/web): system + events + templates + auxKeys bridges"
```

---

### Task 4.4: `webTabsSnapshot.ts` — localStorage tab persistence + URL routing helpers

**Files:**
- Create: `desktop/frontend/src/lib/webTabsSnapshot.ts`
- Create: `desktop/frontend/src/lib/webTabsSnapshot.test.ts`

**Interfaces:**
- Consumes: `sessionStorage`, `localStorage`, `location.hash`
- Produces:
  ```ts
  export interface WebTabsSnapshot { tabs: TabSnap[]; active_tab_id: string }
  export interface TabSnap { id: string; layout: string; active_pane_idx: number; col_ratio?: number; row_ratio?: number; panes: PaneSnap[] }
  export interface PaneSnap { slot: number; session_id: string; host_id?: string; sealed?: string }
  export function getWindowId(): string
  export function loadSnapshot(): WebTabsSnapshot | null
  export function saveSnapshot(snap: WebTabsSnapshot): void
  export function parseHashSid(hash: string): { sid: string | null; focus: 'input' | undefined; permission: 'view' | undefined }
  export function formatHash(sid: string, opts?: { focus?: 'input'; permission?: 'view' }): string
  ```

- [ ] **Step 1: Write failing tests**

`desktop/frontend/src/lib/webTabsSnapshot.test.ts`:

```ts
import { beforeEach, describe, expect, it } from 'vitest'
import { getWindowId, loadSnapshot, saveSnapshot, parseHashSid, formatHash } from './webTabsSnapshot'

beforeEach(() => { localStorage.clear(); sessionStorage.clear() })

describe('windowId', () => {
  it('is stable within the same session', () => {
    const a = getWindowId()
    const b = getWindowId()
    expect(a).toBe(b)
    expect(a).toMatch(/^[0-9a-f-]{36}$/)
  })
})

describe('snapshot roundtrip', () => {
  it('empty when no snapshot stored', () => {
    expect(loadSnapshot()).toBeNull()
  })
  it('persists per-window', () => {
    const snap = {
      tabs: [{ id: 't1', layout: 'single', active_pane_idx: 0,
               panes: [{ slot: 0, session_id: 'sid-a' }] }],
      active_tab_id: 't1',
    }
    saveSnapshot(snap)
    expect(loadSnapshot()).toEqual(snap)
    // Different window → separate storage
    const otherWin = 'other-window-uuid'
    localStorage.setItem(`atterm.web.tabs.v1.${otherWin}`, JSON.stringify({ tabs: [], active_tab_id: '' }))
    expect(loadSnapshot()).toEqual(snap)  // still ours
  })
})

describe('hash routing', () => {
  it('parse #/session/sid-a', () => {
    expect(parseHashSid('#/session/sid-a')).toEqual({ sid: 'sid-a', focus: undefined, permission: undefined })
  })
  it('parse #/session/sid-a?focus=input&permission=view', () => {
    expect(parseHashSid('#/session/sid-a?focus=input&permission=view')).toEqual({
      sid: 'sid-a', focus: 'input', permission: 'view',
    })
  })
  it('empty hash returns null sid', () => {
    expect(parseHashSid('')).toEqual({ sid: null, focus: undefined, permission: undefined })
    expect(parseHashSid('#/')).toEqual({ sid: null, focus: undefined, permission: undefined })
  })
  it('formatHash roundtrips', () => {
    expect(formatHash('sid-a')).toBe('#/session/sid-a')
    expect(formatHash('sid-a', { focus: 'input' })).toBe('#/session/sid-a?focus=input')
    expect(formatHash('sid-a', { permission: 'view' })).toBe('#/session/sid-a?permission=view')
  })
})
```

- [ ] **Step 2: Run — expect FAIL**

- [ ] **Step 3: Implement**

`desktop/frontend/src/lib/webTabsSnapshot.ts`:

```ts
const WINDOW_ID_KEY = 'atterm.web.window_id'
const SNAPSHOT_KEY_PREFIX = 'atterm.web.tabs.v1.'

function uuid(): string {
  if (crypto?.randomUUID) return crypto.randomUUID()
  const b = new Uint8Array(16); crypto.getRandomValues(b)
  b[6] = (b[6] & 0x0f) | 0x40; b[8] = (b[8] & 0x3f) | 0x80
  const h = Array.from(b, x => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0,8)}-${h.slice(8,12)}-${h.slice(12,16)}-${h.slice(16,20)}-${h.slice(20)}`
}

export function getWindowId(): string {
  let id = sessionStorage.getItem(WINDOW_ID_KEY)
  if (!id) { id = uuid(); sessionStorage.setItem(WINDOW_ID_KEY, id) }
  return id
}

export interface PaneSnap { slot: number; session_id: string; host_id?: string; sealed?: string }
export interface TabSnap {
  id: string; layout: string; active_pane_idx: number
  col_ratio?: number; row_ratio?: number
  panes: PaneSnap[]
}
export interface WebTabsSnapshot { tabs: TabSnap[]; active_tab_id: string }

export function loadSnapshot(): WebTabsSnapshot | null {
  const key = SNAPSHOT_KEY_PREFIX + getWindowId()
  try {
    const raw = localStorage.getItem(key)
    if (!raw) return null
    return JSON.parse(raw) as WebTabsSnapshot
  } catch { return null }
}

export function saveSnapshot(snap: WebTabsSnapshot): void {
  const key = SNAPSHOT_KEY_PREFIX + getWindowId()
  localStorage.setItem(key, JSON.stringify(snap))
}

export function parseHashSid(hash: string): {
  sid: string | null; focus: 'input' | undefined; permission: 'view' | undefined
} {
  const m = /^#\/session\/([^?]+)(?:\?(.*))?$/.exec(hash)
  if (!m) return { sid: null, focus: undefined, permission: undefined }
  const sid = decodeURIComponent(m[1])
  const params = new URLSearchParams(m[2] ?? '')
  const focus = params.get('focus') === 'input' ? 'input' : undefined
  const permission = params.get('permission') === 'view' ? 'view' : undefined
  return { sid, focus, permission }
}

export function formatHash(sid: string, opts?: { focus?: 'input'; permission?: 'view' }): string {
  const qs = new URLSearchParams()
  if (opts?.focus) qs.set('focus', opts.focus)
  if (opts?.permission) qs.set('permission', opts.permission)
  const q = qs.toString()
  return `#/session/${encodeURIComponent(sid)}${q ? '?' + q : ''}`
}
```

- [ ] **Step 4: Run tests — expect PASS**

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/webTabsSnapshot.ts \
        desktop/frontend/src/lib/webTabsSnapshot.test.ts
git commit -m "feat(web): tabs snapshot store + hash routing helpers"
```

---

### Task 4.5: `main.web.ts` entry point + integrate tabs snapshot with `App.vue`

**Files:**
- Create: `desktop/frontend/src/main.web.ts`
- Modify: `desktop/frontend/src/App.vue` (add: read `loadSnapshot()` at mount; write `saveSnapshot(...)` on tabs changes; listen `hashchange` + reflect active pane to hash)

**Interfaces:**
- Consumes: `createWebPlatform`, `loadSnapshot/saveSnapshot/parseHashSid/formatHash`, `PrefsSyncEngine` (`@webshared/sync/prefsSync`)
- Produces: `main.web.ts` boots the identical `App.vue` used by desktop; tabs persist per `windowId`; URL hash bi-directionally syncs with active pane.

- [ ] **Step 1: Create `main.web.ts`**

```ts
import { createApp } from 'vue'
import { createPinia } from 'pinia'
import App from './App.vue'
import { initI18n } from './i18n'
import { initPlatform } from './platform'
import { createWebPlatform } from './platform/web'
import { PrefsSyncEngine, localStorageAdapter, apiRelayClient, setSharedPrefsSync } from '@webshared/sync/prefsSync'
import './style.css'

async function bootstrap() {
  await initI18n({
    // TS-shared uses localStorage locale key `atterm.locale`; mirror the
    // pattern used by main.capacitor.ts
    loadPreference: async () => localStorage.getItem('atterm.locale'),
    savePreference: async (p) => localStorage.setItem('atterm.locale', String(p)),
  })

  const platform = initPlatform(createWebPlatform)

  const prefsSync = new PrefsSyncEngine(localStorageAdapter(), apiRelayClient())
  setSharedPrefsSync(prefsSync)
  void prefsSync.pull()
    .then(() => platform.events.emit('prefs:changed', undefined))
    .catch(() => {})

  const app = createApp(App)
  app.use(createPinia())
  app.provide('platform', platform)
  app.config.globalProperties.$platform = platform
  app.mount('#app')
}

void bootstrap()
```

- [ ] **Step 2: Wire tabs snapshot into `App.vue`**

In `desktop/frontend/src/App.vue`:

Import at top:

```ts
import { loadSnapshot, saveSnapshot, parseHashSid, formatHash } from './lib/webTabsSnapshot'
```

Add in `<script setup>` (near the tabs state). No is-web detection needed —
`loadSnapshot()` returns `null` on desktop/capacitor because the localStorage
key doesn't exist there, so this whole block is a no-op on non-web:

```ts
onMounted(async () => {
  const snap = loadSnapshot()
  if (snap && snap.tabs.length > 0) {
    // Restore tabs from snapshot (adapt each pane to the existing Tab shape)
    // then set activeTab
    restoreFromWebSnapshot(snap)  // implement using existing tab-create helpers
  } else {
    // Handle initial hash: `parseHashSid(location.hash)` — if sid, open a new tab
    const { sid, focus, permission } = parseHashSid(location.hash)
    if (sid) openRemoteAsTab(sid, { focus, permission })
  }
  window.addEventListener('hashchange', onHashChange)
})

function onHashChange() {
  const { sid, focus, permission } = parseHashSid(location.hash)
  if (!sid) return
  // If already in a tab/pane, activate; else openRemoteAsTab.
  // Reuse existing tab-finding helper.
}

watch(activeTab, (t) => {
  const activePane = t?.panes[t.activePaneIdx]
  const sid = activePane?.sessionId ?? ''
  const target = sid ? formatHash(sid) : '#/'
  if (location.hash !== target) history.replaceState({}, '', target)
})

watch(tabs, (t) => {
  // Debounced snapshot save
  scheduleSnapshotSave()
}, { deep: true })
```

Add helper functions in the same file:

```ts
let snapshotSaveHandle: ReturnType<typeof setTimeout> | null = null
function scheduleSnapshotSave() {
  if (snapshotSaveHandle) clearTimeout(snapshotSaveHandle)
  snapshotSaveHandle = setTimeout(() => {
    snapshotSaveHandle = null
    saveSnapshot({
      tabs: tabs.value.map(t => ({
        id: t.id,
        layout: t.layout,
        active_pane_idx: t.activePaneIdx,
        col_ratio: t.colRatio,
        row_ratio: t.rowRatio,
        panes: t.panes.map((p, slot) => ({
          slot,
          session_id: p.sessionId ?? '',
          host_id: p.lastSeenInfo?.host_id,
          sealed: p.lastSeenInfo?.sealed,
        })).filter(p => p.session_id),
      })),
      active_tab_id: activeTabId.value ?? '',
    })
  }, 300)
}

function restoreFromWebSnapshot(snap: WebTabsSnapshot) {
  // Adapt each snap tab into the internal Tab shape and push into `tabs.value`.
  // Follow the pattern in executeRestore for remote panes: sessionId + remote:true + lastSeenInfo synth.
}
```

**Important:** all of this is ADDITIVE — desktop and capacitor mount App.vue but `loadSnapshot()` will return `null` (their localStorage doesn't have the key), so their behavior is unchanged.

- [ ] **Step 3: Manual verify (no automated test possible without a full DOM harness)**

Add a comment in the plan:

```
Verification for this task is done in Task 6 (switchover), when the web entry
actually mounts. This task just wires the code paths; App.test.ts adds
coverage for the loadSnapshot=null branch (existing behavior).
```

- [ ] **Step 4: Run frontend tests**

Run: `cd desktop/frontend && npm test`
Expected: all PASS (no regression — desktop and capacitor paths unaffected).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/main.web.ts desktop/frontend/src/App.vue
git commit -m "feat(web): main.web.ts entry + wire tabs snapshot + hash routing into App.vue"
```

**PR 4 checkpoint:** Bundle 4.1-4.5 into PR `feat(platform/web): web platform + entry + tabs snapshot`. Manual verify: run desktop `npm run dev` (Wails), everything unchanged; run mobile Capacitor build (if available), unchanged.

---

## Phase 5 · Narrow-screen drawer in `TaskSidebar` (PR 5)

`TaskSidebar` 加 responsive drawer 模式：viewport width < 768px 时变覆盖式 drawer + 顶部汉堡菜单展开。惠及桌面拖窄窗口 + 未来的 web 窄浏览器视口（含手机浏览器访问 web）。

### Task 5.1: TaskSidebar drawer mode + hamburger toggle

**Files:**
- Modify: `desktop/frontend/src/components/TaskSidebar.vue`
- Add test: `desktop/frontend/src/components/__tests__/TaskSidebar.drawer.test.ts` (or extend an existing test file)

**Interfaces:**
- Consumes: `window.innerWidth`, `window.addEventListener('resize')`
- Produces: TaskSidebar exposes an internal `drawerOpen: Ref<boolean>` when in narrow mode; template adds a hamburger button visible only when `viewport < 768px`; sidebar itself gets `.drawer` class in narrow mode with `position: fixed; z-index: N; transform` animation.

- [ ] **Step 1: Write failing test**

```ts
import { mount } from '@vue/test-utils'
import TaskSidebar from '../TaskSidebar.vue'

test('renders drawer + hamburger when viewport < 768px', async () => {
  Object.defineProperty(window, 'innerWidth', { value: 500, writable: true, configurable: true })
  const w = mount(TaskSidebar, { props: {/* required props */} })
  await w.vm.$nextTick()
  expect(w.find('[data-test="sidebar-hamburger"]').exists()).toBe(true)
  expect(w.find('.task-sidebar.drawer').exists()).toBe(true)
})

test('normal layout when viewport >= 768px', async () => {
  Object.defineProperty(window, 'innerWidth', { value: 1400, writable: true, configurable: true })
  const w = mount(TaskSidebar, { props: {/* required props */} })
  await w.vm.$nextTick()
  expect(w.find('[data-test="sidebar-hamburger"]').exists()).toBe(false)
})

test('hamburger click toggles drawer open state', async () => {
  Object.defineProperty(window, 'innerWidth', { value: 500, writable: true, configurable: true })
  const w = mount(TaskSidebar, { props: {/* required props */} })
  await w.vm.$nextTick()
  const ham = w.find('[data-test="sidebar-hamburger"]')
  await ham.trigger('click')
  expect(w.find('.task-sidebar.drawer.open').exists()).toBe(true)
})
```

- [ ] **Step 2: Run tests — expect FAIL**

- [ ] **Step 3: Implement in `TaskSidebar.vue`**

Add to `<script setup>`:

```ts
const isNarrow = ref(false)
const drawerOpen = ref(false)

function updateIsNarrow() {
  const wasNarrow = isNarrow.value
  isNarrow.value = window.innerWidth < 768
  if (wasNarrow && !isNarrow.value) drawerOpen.value = false  // Exit narrow mode
}

onMounted(() => {
  updateIsNarrow()
  window.addEventListener('resize', updateIsNarrow)
})
onBeforeUnmount(() => {
  window.removeEventListener('resize', updateIsNarrow)
})
```

Add to template (at appropriate place — likely top of the sidebar container):

```vue
<button
  v-if="isNarrow"
  data-test="sidebar-hamburger"
  class="sidebar-hamburger"
  @click="drawerOpen = !drawerOpen"
  :aria-label="drawerOpen ? '关闭侧栏' : '打开侧栏'"
>☰</button>

<aside
  class="task-sidebar"
  :class="{ drawer: isNarrow, open: isNarrow && drawerOpen }"
>
  <!-- existing content -->
</aside>
```

Add scoped CSS:

```css
.sidebar-hamburger {
  position: fixed; top: 12px; left: 12px; z-index: 20;
  padding: 6px 10px; background: var(--panel-bg); border: 1px solid var(--border);
  border-radius: 4px; cursor: pointer;
}
.task-sidebar.drawer {
  position: fixed; top: 0; bottom: 0; left: 0; z-index: 15;
  transform: translateX(-100%); transition: transform 0.2s ease;
  box-shadow: 2px 0 8px rgba(0,0,0,0.3);
}
.task-sidebar.drawer.open { transform: translateX(0); }
```

- [ ] **Step 4: Run tests — expect PASS**

- [ ] **Step 5: Manual verify in browser**

Run desktop `npm run dev`; resize window narrower than 768px; hamburger appears; click opens drawer; click again closes; drag back wider hides hamburger + restores normal layout.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/TaskSidebar.vue \
        desktop/frontend/src/components/__tests__/TaskSidebar.drawer.test.ts
git commit -m "feat(sidebar): narrow-screen drawer mode + hamburger toggle"
```

**PR 5 checkpoint:** Solo PR `feat(sidebar): narrow-screen drawer + hamburger`. Desktop wide → unchanged. Desktop narrow → drawer works. No web/mobile impact (yet).

---

## Phase 6 · Switchover (PR 6)

The only behavior-changing PR. Web `main.html` starts mounting `App.vue` instead of the old `SessionList/TerminalView`.

### Task 6.1: Update `web/vite.config.ts` to point `main.html` at the new entry

**Files:**
- Modify: `web/vite.config.ts`
- Verify: `web/index.html` (or wherever `main.html` lives)

**Interfaces:**
- Produces: Vite dev + build resolves `main.html`'s `<script src>` to `../desktop/frontend/src/main.web.ts` via project's rollup input config.

- [ ] **Step 1: Inspect current vite config**

```bash
grep -n "input\|main.html\|main:" web/vite.config.ts
```

- [ ] **Step 2: Change the `main` input**

Update `web/vite.config.ts::rollupOptions.input.main` (or the equivalent structure — vite may auto-detect based on `<script src>` in HTML files):

If HTML-based:
- Update `web/index.html`'s `<script type="module" src="...">` to point at
  `../desktop/frontend/src/main.web.ts` (relative from `web/` root)

If input-based (in vite.config.ts):
```ts
rollupOptions: {
  input: {
    main: fileURLToPath(new URL('./index.html', import.meta.url)),
    // ...others unchanged...
  }
}
```

And still update `index.html` to source the new entry.

- [ ] **Step 3: Add alias if not present**

`web/vite.config.ts::resolve.alias`:
```ts
'@webshared': path.resolve(__dirname, 'src/shared'),
'@': path.resolve(__dirname, '../desktop/frontend/src'),
```

Ensure `../desktop/frontend/src` is inside vite's `fs.allow` if strict FS is enabled.

- [ ] **Step 4: Run dev server**

Run: `cd web && npm run dev`
Verify: opening `http://localhost:<port>/` shows the new desktop-style layout (TabBar + TaskSidebar).

- [ ] **Step 5: Commit**

```bash
git add web/vite.config.ts web/index.html
git commit -m "feat(web): main entry mounts desktop App.vue via platform/web"
```

---

### Task 6.2: Delete old `web/src/main/*`

**Files:**
- Delete: `web/src/main/App.vue`
- Delete: `web/src/main/main.ts`
- Delete: `web/src/main/components/{App,SessionList,TerminalView,ShortcutBar,InstallHint,PasteFallback,PasteImagePreviewHost,PasteFilePreviewHost}.vue`
- Delete: `web/src/main/lib/` (contents)
- Verify: no other file in `web/src` imports from `web/src/main/*`

**Interfaces:**
- Produces: old web/src/main tree gone; everything sourced from desktop/frontend.

- [ ] **Step 1: Grep for cross-references**

```bash
grep -rn "src/main/\|@/main/" web/src/ 2>/dev/null
```

Any hit outside `web/src/main` itself is a broken import to fix (probably in shared/, delete or rewire).

- [ ] **Step 2: Delete the files**

```bash
git rm -r web/src/main/App.vue web/src/main/main.ts web/src/main/components/ web/src/main/lib/
```

- [ ] **Step 3: Build web**

Run: `cd web && npm run build`
Expected: build succeeds; no missing-module errors.

- [ ] **Step 4: Run web dev + smoke test**

Run: `cd web && npm run dev`
Verify: login → main page shows desktop layout; open a remote session; UI works.

- [ ] **Step 5: Commit**

```bash
git commit -m "chore(web): delete old main entry sources"
```

---

### Task 6.3: Delete `web/src/settings/*` + `web/settings.html`

**Files:**
- Delete: `web/settings.html`
- Delete: `web/src/settings/*`
- Verify: main entry's `SettingsDialog.vue` (from desktop/frontend) covers what users need

**Interfaces:**
- Produces: `settings.html` URL 失效（spec §6.3 已声明破坏性变更）; settings 通过 desktop 的 `SettingsDialog` 在主入口内嵌打开.

- [ ] **Step 1: Ensure SettingsDialog is reachable from `App.vue`**

Verify that the ⚙ button / settings action in `App.vue` opens `SettingsDialog.vue`. If not visible in web (because e.g. it's under a `caps.autoUpdate` branch), unhide it. Check `App.vue` for the settings trigger.

- [ ] **Step 2: Delete**

```bash
git rm web/settings.html
git rm -r web/src/settings/
```

- [ ] **Step 3: Update `web/vite.config.ts`**

Remove the `settings` entry from `rollupOptions.input` if it was named there.

- [ ] **Step 4: Build web**

Run: `cd web && npm run build`
Expected: no missing-entry error.

- [ ] **Step 5: Manual verify settings flow**

Run: `cd web && npm run dev`
Open main → click ⚙ (or wherever the settings button is) → SettingsDialog opens → change locale → close → verify locale persisted after reload (via prefsSync).

- [ ] **Step 6: Commit**

```bash
git add web/vite.config.ts
git commit -m "chore(web): drop settings.html; use in-app SettingsDialog"
```

---

### Task 6.4: End-to-end smoke test + memo update

**Files:**
- No source changes; possibly update `docs/spec/architecture.md` if it references the old web layout

**Interfaces:**
- (verification only)

- [ ] **Step 1: Full test run**

Run:
- `go test ./...`
- `cd desktop/frontend && npm test`
- `cd web && npm test`
- `cd web && npm run build`

Expected: all pass.

- [ ] **Step 2: Manual smoke test on web**

Start:
- A working relay
- A desktop agent uplinked
- `cd web && npm run dev`, open in browser

Verify:
- Login → main → sidebar shows sessions grouped by host
- Right-click a session → `SessionRowMenu` → "置顶"; the session moves to 📌 group
- Refresh page → 📌 pin preserved (via localStorage + prefsSync pulled next tick)
- Open a session in a tab → xterm renders → type + read
- Split pane (if UI is present) → second pane
- Close tab; refresh → tab set gone (or restored if snapshot saved)
- Direct URL `#/session/<sid>` → opens directly to that session in a new tab
- Resize browser to <768px → sidebar → drawer; hamburger appears
- On desktop app, pin a different session → within a few seconds, web reflects it (via prefsSync)

- [ ] **Step 3: Update architecture doc if referenced**

Check: `docs/spec/architecture.md` for any mention of web's "two-state" main entry; update if outdated.

- [ ] **Step 4: Commit any doc updates**

```bash
git add docs/spec/architecture.md
git commit -m "docs(spec): update architecture — web main mounts desktop App.vue"
```

**PR 6 checkpoint:** Solo PR `feat(web): mount desktop App.vue at main entry`. This is the sole behavior-changing PR — squash-merge only after PR 1-5 are in.

---

## Post-Ship

- Watch relay logs for `/api/me/preferences` PUT with `pinned_session_ids` — should see traffic from web/mobile clients.
- Watch for user reports about the deleted `settings.html` URL (it's a break, mentioned in spec §6.3).
- If pin sync misbehaves cross-client, first check: (a) `PrefsSyncEngine` LWW behaves as expected (per-key `updated_at`), (b) each client's `prefs:changed` event actually fires after pull.

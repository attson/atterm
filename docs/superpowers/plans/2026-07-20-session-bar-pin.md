# 会话栏置顶 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a right-click "Pin to top" action on session rows in the desktop's left sidebar, gathering pinned sessions into a virtual "📌 Pinned" group at the top of the list, persisted to the desktop's local config.

**Architecture:** Persistence in `appConfig.PinnedSessionIDs` (new `desktop/config.go` field), read/write via two new Wails methods on `App`. A module-scoped `useSessionPins` composable (Vue 3, same shape as `useCollapsedGroups`) mirrors the state in-memory with debounced write-back. `TaskGroupedList` filters pinned sessions out of the host/state groups and renders them in a new top group; a lightweight `SessionRowMenu` component provides the right-click "Pin / Unpin" trigger.

**Tech Stack:** Go (desktop backend) + Wails v2 bindings + Vue 3 + TypeScript + Vitest + `@vue/test-utils`.

## Global Constraints

- Design spec: `docs/superpowers/specs/2026-07-20-session-bar-pin-design.md` — every task requirement follows this doc verbatim.
- Language: user-facing prose in memory says "reply in Chinese in this repo" — commit messages, code, comments stay in English.
- No backward-compat cruft (memory `feedback_no_backward_compat.md`): replace > deprecate; empty slice is the sole default sentinel for `PinnedSessionIDs`; no migration path needed (field is `omitempty` additive).
- Repo convention: TDD (test-first) + focused files + `git commit` per task.
- i18n keys must land in **both** `desktop/frontend/src/i18n/messages/en.ts` and `desktop/frontend/src/i18n/messages/zh-CN.ts`.
- Wails-generated files under `desktop/frontend/wailsjs/go/main/` are committed to git even though the header says "DO NOT EDIT" — regenerate via `wails generate module` from `desktop/`, or hand-append following the file's existing pattern.
- Go build/test tag on Linux: `-tags webkit2_41`. macOS/Windows can omit.
- Frontend test runner: `npm test` from `desktop/frontend`.

---

## File Structure

**Create:**
- `desktop/frontend/src/composables/useSessionPins.ts` — pin-state composable
- `desktop/frontend/src/composables/useSessionPins.test.ts`
- `desktop/frontend/src/components/SessionRowMenu.vue` — right-click popover
- `desktop/frontend/src/components/SessionRowMenu.test.ts`

**Modify:**
- `desktop/config.go` — add `PinnedSessionIDs []string` field on `appConfig`
- `desktop/app.go` — add `GetPinnedSessionIds()` / `SetPinnedSessionIds()`
- `desktop/app_test.go` — round-trip + dedupe tests
- `desktop/frontend/wailsjs/go/main/App.d.ts` — 2 new exported function signatures
- `desktop/frontend/wailsjs/go/main/App.js` — 2 new exported thunks
- `desktop/frontend/src/lib/api.ts` — `AppBindings` interface + `getPinnedSessionIds` / `setPinnedSessionIds` wrappers
- `desktop/frontend/src/components/TaskGroupedList.vue` — top virtual group + row filter + `@contextmenu` wiring
- `desktop/frontend/src/components/TaskGroupedList.test.ts` — new assertions
- `desktop/frontend/src/i18n/messages/en.ts` — 3 new keys under `tasks.pinned`
- `desktop/frontend/src/i18n/messages/zh-CN.ts` — 3 new keys under `tasks.pinned`

---

## Task 1: Go backend — config field + Get/Set methods + tests

**Files:**
- Modify: `desktop/config.go` (append field in `appConfig` struct)
- Modify: `desktop/app.go` (append methods next to `GetTaskSidebarWidth`/`SetTaskSidebarWidth`)
- Test: `desktop/app_test.go` (append new test funcs)

**Interfaces:**
- Consumes: `App.cfgStore` (existing `*configStore`), `appConfig` (existing struct)
- Produces:
  - `func (a *App) GetPinnedSessionIds() []string` — returns empty slice, never nil, when unset
  - `func (a *App) SetPinnedSessionIds(ids []string) error` — dedupes, drops empties, preserves first-occurrence order

- [ ] **Step 1: Write the failing Go tests**

Append to `desktop/app_test.go`:

```go
func TestPinnedSessionIds_EmptyDefault(t *testing.T) {
	a := newRelayTestApp(t)
	got := a.GetPinnedSessionIds()
	if got == nil {
		t.Fatal("GetPinnedSessionIds() = nil; want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("GetPinnedSessionIds() = %v; want []", got)
	}
}

func TestPinnedSessionIds_RoundTrip(t *testing.T) {
	a := newRelayTestApp(t)
	ids := []string{"aaa", "bbb", "ccc"}
	if err := a.SetPinnedSessionIds(ids); err != nil {
		t.Fatalf("SetPinnedSessionIds: %v", err)
	}
	got := a.GetPinnedSessionIds()
	if !reflect.DeepEqual(got, ids) {
		t.Fatalf("GetPinnedSessionIds() = %v; want %v", got, ids)
	}
}

func TestPinnedSessionIds_DedupeDropsEmpty(t *testing.T) {
	a := newRelayTestApp(t)
	input := []string{"aaa", "", "bbb", "aaa", "ccc", ""}
	if err := a.SetPinnedSessionIds(input); err != nil {
		t.Fatalf("SetPinnedSessionIds: %v", err)
	}
	got := a.GetPinnedSessionIds()
	want := []string{"aaa", "bbb", "ccc"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetPinnedSessionIds() = %v; want %v", got, want)
	}
}

func TestPinnedSessionIds_NilClears(t *testing.T) {
	a := newRelayTestApp(t)
	if err := a.SetPinnedSessionIds([]string{"aaa"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := a.SetPinnedSessionIds(nil); err != nil {
		t.Fatalf("SetPinnedSessionIds(nil): %v", err)
	}
	got := a.GetPinnedSessionIds()
	if len(got) != 0 {
		t.Fatalf("after clear: got %v; want []", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test -tags webkit2_41 -run TestPinnedSessionIds -timeout 30s ./desktop/`
(macOS/Windows: drop `-tags webkit2_41`.)
Expected: FAIL with `a.GetPinnedSessionIds undefined` / `a.SetPinnedSessionIds undefined`.

- [ ] **Step 3: Add the field on `appConfig`**

In `desktop/config.go`, immediately after the `TaskSidebarWidth` field (around line 188), insert:

```go
	// PinnedSessionIDs holds session_ids the user has pinned to the top of
	// the session bar. Order in the slice is not meaningful — the frontend
	// sorts pinned rows by task_state urgency, same as other groups. Stale
	// ids (session gone) are kept in place: they render as nothing while
	// absent and reappear if the session returns; the frontend never
	// deletes ids the user did not explicitly unpin.
	PinnedSessionIDs []string `json:"pinned_session_ids,omitempty"`
```

- [ ] **Step 4: Add the App methods**

In `desktop/app.go`, immediately after `SetTaskSidebarWidth` (line ≈ 1320), insert:

```go
// GetPinnedSessionIds returns the persisted list of session_ids the user
// has pinned to the top of the session bar. Always returns a non-nil
// slice so the frontend can rely on Array semantics.
func (a *App) GetPinnedSessionIds() []string {
	if a.cfgStore == nil {
		return []string{}
	}
	ids := a.cfgStore.Get().PinnedSessionIDs
	if ids == nil {
		return []string{}
	}
	return ids
}

// SetPinnedSessionIds persists the list, deduping and dropping empty
// entries while preserving first-occurrence order.
func (a *App) SetPinnedSessionIds(ids []string) error {
	if a.cfgStore == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	cfg := a.cfgStore.Get()
	cfg.PinnedSessionIDs = out
	return a.cfgStore.Set(cfg)
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test -tags webkit2_41 -run TestPinnedSessionIds -timeout 30s ./desktop/`
Expected: PASS (4 tests).

- [ ] **Step 6: Run go vet + broader Go tests to catch regressions**

Run: `go vet -tags webkit2_41 ./desktop/... && go test -tags webkit2_41 -timeout 60s ./desktop/`
Expected: PASS across the desktop package.

- [ ] **Step 7: Commit**

```bash
git add desktop/config.go desktop/app.go desktop/app_test.go
git commit -m "feat(desktop): persist PinnedSessionIDs + Get/Set app methods

New appConfig.PinnedSessionIDs field (omitempty) plus Wails-facing
Get/SetPinnedSessionIds on App. Set dedupes + drops empties while
preserving first-occurrence order; Get always returns a non-nil slice
so the frontend sees Array, never null."
```

---

## Task 2: Wails bindings + TS api.ts wrappers

**Files:**
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts`
- Modify: `desktop/frontend/wailsjs/go/main/App.js`
- Modify: `desktop/frontend/src/lib/api.ts` (interface + wrappers)

**Interfaces:**
- Consumes: `App.GetPinnedSessionIds()` / `App.SetPinnedSessionIds([]string)` from Task 1
- Produces:
  - `getPinnedSessionIds(): Promise<string[]>`
  - `setPinnedSessionIds(ids: string[]): Promise<void>`
  - Added to `AppBindings` TS interface

- [ ] **Step 1: Regenerate (or hand-append) the Wails bindings**

**Option A (preferred, matches how the file was authored):** run `wails generate module` from `desktop/`. This overwrites `desktop/frontend/wailsjs/go/main/App.d.ts` and `App.js`; diff should show only the two new methods added.

**Option B (offline):** hand-append. To `desktop/frontend/wailsjs/go/main/App.d.ts` (alphabetically near existing `Get…` block, e.g. right after `GetPairingToken` or wherever alphabetical order puts it):

```ts
export function GetPinnedSessionIds():Promise<Array<string>>;
```

To `desktop/frontend/wailsjs/go/main/App.js` (matching order):

```js
export function GetPinnedSessionIds() {
  return window['go']['main']['App']['GetPinnedSessionIds']();
}
```

And symmetrically for `SetPinnedSessionIds`:

```ts
export function SetPinnedSessionIds(arg1:Array<string>):Promise<void>;
```

```js
export function SetPinnedSessionIds(arg1) {
  return window['go']['main']['App']['SetPinnedSessionIds'](arg1);
}
```

- [ ] **Step 2: Add the failing wrapper tests**

Append to (create if absent) `desktop/frontend/src/lib/api.test.ts` — check existing tests first (`grep -l "__setBindingsForTest" desktop/frontend/src/lib/*.test.ts`); if none exist for the pattern, skip this test file and rely on Task 3's composable test to prove the wrapper.

Actually skip the wrapper test — the composable test in Task 3 exercises the wrapper end-to-end. Move straight to Step 3.

- [ ] **Step 3: Add the AppBindings interface entries**

In `desktop/frontend/src/lib/api.ts`, in the `AppBindings` interface (right after the `SetTaskSidebarWidth` line, around 381):

```ts
  GetPinnedSessionIds(): Promise<string[]>;
  SetPinnedSessionIds(ids: string[]): Promise<void>;
```

- [ ] **Step 4: Add the exported wrappers**

Immediately after the existing `setTaskSidebarWidth` export (around line 901):

```ts
export function getPinnedSessionIds(): Promise<string[]> {
  return bindings().GetPinnedSessionIds();
}
export function setPinnedSessionIds(ids: string[]): Promise<void> {
  return bindings().SetPinnedSessionIds(ids);
}
```

- [ ] **Step 5: Run type check + tests**

Run: `cd desktop/frontend && npx vue-tsc --noEmit && npm test -- --run`
Expected: PASS (no new failures; adds ~0 assertions).

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/wailsjs/go/main/App.d.ts \
        desktop/frontend/wailsjs/go/main/App.js \
        desktop/frontend/src/lib/api.ts
git commit -m "feat(desktop): wails bindings + api.ts wrappers for pinned session ids

Regenerated App.d.ts / App.js expose Get/SetPinnedSessionIds; api.ts
adds matching TS types on AppBindings and getPinnedSessionIds /
setPinnedSessionIds wrappers, following the sidebar-width pattern."
```

---

## Task 3: `useSessionPins` composable + tests

**Files:**
- Create: `desktop/frontend/src/composables/useSessionPins.ts`
- Create: `desktop/frontend/src/composables/useSessionPins.test.ts`

**Interfaces:**
- Consumes: `getPinnedSessionIds` / `setPinnedSessionIds` from Task 2's `../lib/api`
- Produces:
  ```ts
  export interface UseSessionPins {
    pinnedIds: Ref<Set<string>>;
    isPinned(id: string): boolean;
    pin(id: string): void;
    unpin(id: string): void;
    toggle(id: string): void;
  }
  export function useSessionPins(): UseSessionPins;
  export function __resetForTests(): void;
  ```
- Debounce: 300ms window between `pin/unpin/toggle` and disk write.

- [ ] **Step 1: Write the failing tests**

Create `desktop/frontend/src/composables/useSessionPins.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { effectScope, nextTick } from "vue";
import { flushPromises } from "@vue/test-utils";
import { useSessionPins, __resetForTests } from "./useSessionPins";
import * as api from "../lib/api";

describe("useSessionPins", () => {
  let scope: ReturnType<typeof effectScope>;
  beforeEach(() => {
    __resetForTests();
    vi.restoreAllMocks();
    vi.useFakeTimers();
    scope = effectScope();
  });
  afterEach(() => {
    vi.useRealTimers();
    scope.stop();
  });

  test("loads pinned ids from Wails on first use", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a", "b"]);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    await nextTick();
    expect(pins.isPinned("a")).toBe(true);
    expect(pins.isPinned("b")).toBe(true);
    expect(pins.isPinned("c")).toBe(false);
  });

  test("only fetches once across multiple useSessionPins() calls", async () => {
    const spy = vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue([]);
    scope.run(() => {
      useSessionPins();
      useSessionPins();
      useSessionPins();
    });
    await flushPromises();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  test("pin/unpin/toggle mutate pinnedIds", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue([]);
    vi.spyOn(api, "setPinnedSessionIds").mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    pins.pin("a");
    expect(pins.isPinned("a")).toBe(true);
    pins.toggle("a");
    expect(pins.isPinned("a")).toBe(false);
    pins.toggle("b");
    expect(pins.isPinned("b")).toBe(true);
    pins.unpin("b");
    expect(pins.isPinned("b")).toBe(false);
  });

  test("rapid toggles debounce into a single setPinnedSessionIds call", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue([]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    pins.pin("a");
    pins.pin("b");
    pins.pin("c");
    pins.unpin("b");
    expect(setSpy).not.toHaveBeenCalled();
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(setSpy).toHaveBeenCalledWith(["a", "c"]);
  });

  test("survives getPinnedSessionIds rejection", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockRejectedValue(new Error("boom"));
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();
    expect(pins.isPinned("a")).toBe(false);
  });
});
```

- [ ] **Step 2: Run to verify tests fail with "module not found"**

Run: `cd desktop/frontend && npm test -- --run useSessionPins`
Expected: FAIL — module `./useSessionPins` does not exist.

- [ ] **Step 3: Implement the composable**

Create `desktop/frontend/src/composables/useSessionPins.ts`:

```ts
import { ref, type Ref } from "vue";
import { getPinnedSessionIds, setPinnedSessionIds } from "../lib/api";

// Module-scoped so state is shared across all consumers within the app
// lifetime (matches useCollapsedGroups / useTaskGroupBy pattern).
const pinnedIds = ref<Set<string>>(new Set());
let loaded = false;
let loadPromise: Promise<void> | null = null;
let flushHandle: ReturnType<typeof setTimeout> | null = null;

const PERSIST_DEBOUNCE_MS = 300;

async function loadOnce(): Promise<void> {
  if (loaded) return;
  if (loadPromise) return loadPromise;
  loadPromise = (async () => {
    try {
      const list = await getPinnedSessionIds();
      pinnedIds.value = new Set(list);
    } catch {
      /* best-effort — leave the empty default in place */
    } finally {
      loaded = true;
      loadPromise = null;
    }
  })();
  return loadPromise;
}

function schedulePersist(): void {
  if (flushHandle) clearTimeout(flushHandle);
  flushHandle = setTimeout(() => {
    flushHandle = null;
    void setPinnedSessionIds(Array.from(pinnedIds.value)).catch(() => {
      /* best-effort */
    });
  }, PERSIST_DEBOUNCE_MS);
}

function pinFn(id: string): void {
  if (!id || pinnedIds.value.has(id)) return;
  const next = new Set(pinnedIds.value);
  next.add(id);
  pinnedIds.value = next;
  schedulePersist();
}

function unpinFn(id: string): void {
  if (!pinnedIds.value.has(id)) return;
  const next = new Set(pinnedIds.value);
  next.delete(id);
  pinnedIds.value = next;
  schedulePersist();
}

export interface UseSessionPins {
  pinnedIds: Ref<Set<string>>;
  isPinned: (id: string) => boolean;
  pin: (id: string) => void;
  unpin: (id: string) => void;
  toggle: (id: string) => void;
}

export function useSessionPins(): UseSessionPins {
  void loadOnce();
  return {
    pinnedIds,
    isPinned: (id) => pinnedIds.value.has(id),
    pin: pinFn,
    unpin: unpinFn,
    toggle: (id) => (pinnedIds.value.has(id) ? unpinFn(id) : pinFn(id)),
  };
}

export function __resetForTests(): void {
  pinnedIds.value = new Set();
  loaded = false;
  loadPromise = null;
  if (flushHandle) {
    clearTimeout(flushHandle);
    flushHandle = null;
  }
}
```

- [ ] **Step 4: Run to verify tests pass**

Run: `cd desktop/frontend && npm test -- --run useSessionPins`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useSessionPins.ts \
        desktop/frontend/src/composables/useSessionPins.test.ts
git commit -m "feat(desktop): add useSessionPins composable with debounced persistence

Module-scoped Set<string> tracks pinned session ids, loaded once from
Wails on first use, written back through setPinnedSessionIds with a
300ms debounce so rapid toggles collapse into one disk write. Shape
mirrors useCollapsedGroups / useTaskPreset."
```

---

## Task 4: `SessionRowMenu` component + tests

**Files:**
- Create: `desktop/frontend/src/components/SessionRowMenu.vue`
- Create: `desktop/frontend/src/components/SessionRowMenu.test.ts`

**Interfaces:**
- Consumes: nothing from earlier tasks (independent).
- Produces:
  ```ts
  defineProps<{
    open: boolean;
    x: number;
    y: number;
    pinned: boolean;
    // Menu labels are passed in so the component stays i18n-agnostic
    // and easily testable.
    labelPin: string;
    labelUnpin: string;
  }>();
  defineEmits<{
    (e: "close"): void;
    (e: "togglePin"): void;
  }>();
  ```
- Behavior: fixed-position at (x,y) with viewport-edge flipping; Esc / outside-click / focusout emit `close`; menu item click emits `togglePin` then `close`.

- [ ] **Step 1: Write the failing tests**

Create `desktop/frontend/src/components/SessionRowMenu.test.ts`:

```ts
import { describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import SessionRowMenu from "./SessionRowMenu.vue";

function factory(overrides: Partial<InstanceType<typeof SessionRowMenu>["$props"]> = {}) {
  return mount(SessionRowMenu, {
    attachTo: document.body,
    props: {
      open: true,
      x: 100,
      y: 100,
      pinned: false,
      labelPin: "Pin to top",
      labelUnpin: "Unpin",
      ...overrides,
    },
  });
}

describe("SessionRowMenu", () => {
  test("does not render when open=false", () => {
    const w = factory({ open: false });
    expect(w.find("[data-test=session-row-menu]").exists()).toBe(false);
  });

  test("shows Pin label when pinned=false", () => {
    const w = factory({ pinned: false });
    const item = w.find("[data-test=session-row-menu-item]");
    expect(item.text()).toBe("Pin to top");
  });

  test("shows Unpin label when pinned=true", () => {
    const w = factory({ pinned: true });
    const item = w.find("[data-test=session-row-menu-item]");
    expect(item.text()).toBe("Unpin");
  });

  test("clicking the item emits togglePin then close", async () => {
    const w = factory();
    await w.find("[data-test=session-row-menu-item]").trigger("click");
    expect(w.emitted("togglePin")).toHaveLength(1);
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("Escape emits close", async () => {
    const w = factory();
    await w.trigger("keydown", { key: "Escape" });
    expect(w.emitted("close")).toHaveLength(1);
  });

  test("outside click emits close", async () => {
    const w = factory();
    // Simulate click outside the menu root.
    document.body.dispatchEvent(new MouseEvent("mousedown", { bubbles: true }));
    expect(w.emitted("close")).toHaveLength(1);
    w.unmount();
  });

  test("positions to (x, y) via inline style", () => {
    const w = factory({ x: 200, y: 300 });
    const root = w.find("[data-test=session-row-menu]");
    const style = (root.element as HTMLElement).style;
    expect(style.left).toBe("200px");
    expect(style.top).toBe("300px");
  });
});
```

- [ ] **Step 2: Run to verify tests fail**

Run: `cd desktop/frontend && npm test -- --run SessionRowMenu`
Expected: FAIL — component does not exist.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/SessionRowMenu.vue`:

```vue
<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from "vue";

const props = withDefaults(defineProps<{
  open: boolean;
  x: number;
  y: number;
  pinned: boolean;
  labelPin: string;
  labelUnpin: string;
}>(), { open: false, pinned: false });

const emit = defineEmits<{
  (e: "close"): void;
  (e: "togglePin"): void;
}>();

const menuRef = ref<HTMLElement | null>(null);

// Viewport-edge flipping: when the menu would overflow the right/bottom edge,
// anchor it above/left of (x, y) instead. Recomputed each time it opens.
const positionedX = ref(0);
const positionedY = ref(0);

function updatePosition() {
  if (!menuRef.value) {
    positionedX.value = props.x;
    positionedY.value = props.y;
    return;
  }
  const rect = menuRef.value.getBoundingClientRect();
  const vw = window.innerWidth;
  const vh = window.innerHeight;
  positionedX.value = props.x + rect.width > vw ? Math.max(0, props.x - rect.width) : props.x;
  positionedY.value = props.y + rect.height > vh ? Math.max(0, props.y - rect.height) : props.y;
}

const style = computed(() => ({
  left: positionedX.value + "px",
  top: positionedY.value + "px",
}));

function onItemClick() {
  emit("togglePin");
  emit("close");
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === "Escape") {
    emit("close");
  }
}

function onOutside(e: MouseEvent) {
  if (!menuRef.value) return;
  if (!menuRef.value.contains(e.target as Node)) {
    emit("close");
  }
}

watch(
  () => props.open,
  (v) => {
    if (v) {
      positionedX.value = props.x;
      positionedY.value = props.y;
      // Wait a tick so the element is mounted with intrinsic size, then flip.
      requestAnimationFrame(updatePosition);
      window.addEventListener("keydown", onKeydown);
      window.addEventListener("mousedown", onOutside);
    } else {
      window.removeEventListener("keydown", onKeydown);
      window.removeEventListener("mousedown", onOutside);
    }
  },
  { immediate: true },
);

onMounted(() => {
  if (props.open) requestAnimationFrame(updatePosition);
});

onBeforeUnmount(() => {
  window.removeEventListener("keydown", onKeydown);
  window.removeEventListener("mousedown", onOutside);
});
</script>

<template>
  <div
    v-if="open"
    ref="menuRef"
    class="session-row-menu"
    data-test="session-row-menu"
    role="menu"
    :style="style"
  >
    <button
      class="menu-item"
      data-test="session-row-menu-item"
      role="menuitem"
      type="button"
      @click.stop="onItemClick"
    >
      {{ pinned ? labelUnpin : labelPin }}
    </button>
  </div>
</template>

<style scoped>
.session-row-menu {
  position: fixed;
  z-index: 1000;
  min-width: 140px;
  padding: 4px;
  background: var(--menu-bg, #1f1f22);
  border: 1px solid rgba(255, 255, 255, 0.12);
  border-radius: 6px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.4);
}
.menu-item {
  display: block;
  width: 100%;
  padding: 6px 10px;
  background: transparent;
  border: none;
  color: inherit;
  text-align: left;
  cursor: pointer;
  border-radius: 4px;
  font-size: 13px;
}
.menu-item:hover,
.menu-item:focus {
  background: rgba(255, 255, 255, 0.08);
  outline: none;
}
</style>
```

- [ ] **Step 4: Run to verify tests pass**

Run: `cd desktop/frontend && npm test -- --run SessionRowMenu`
Expected: PASS (7 tests).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SessionRowMenu.vue \
        desktop/frontend/src/components/SessionRowMenu.test.ts
git commit -m "feat(desktop): add SessionRowMenu right-click popover

Lightweight fixed-position menu with a single Pin / Unpin action.
Viewport-edge flipping, Esc + outside-click dismiss, i18n-agnostic
(labels passed in). ~120 LOC + tests; no third-party dropdown dep."
```

---

## Task 5: `TaskGroupedList` integration + i18n + tests

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts` (append `tasks.pinned` block)
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts` (append `tasks.pinned` block)
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue`
- Modify: `desktop/frontend/src/components/TaskGroupedList.test.ts` (append pinned-group assertions)

**Interfaces:**
- Consumes: `useSessionPins()` (Task 3), `SessionRowMenu` (Task 4)
- Produces: no new export (all wiring inside `TaskGroupedList`)

The virtual group's collapse key is the sentinel string `__pinned__` (host_ids are UUIDs and states are known enum values, so no collision risk).

- [ ] **Step 1: Add i18n keys**

In `desktop/frontend/src/i18n/messages/en.ts`, inside the `tasks:` block (near line 532), add a new `pinned` sub-object (near `sidebar` / `preset`):

```ts
    pinned: {
      title: "Pinned",
      menuPin: "Pin to top",
      menuUnpin: "Unpin",
    },
```

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, matching structure:

```ts
    pinned: {
      title: "置顶",
      menuPin: "置顶",
      menuUnpin: "取消置顶",
    },
```

- [ ] **Step 2: Write the failing frontend test assertions**

Append to `desktop/frontend/src/components/TaskGroupedList.test.ts` (structure inside an existing `describe("TaskGroupedList")`, or create a new `describe("TaskGroupedList pinned group")` if there's no shared setup to piggyback):

```ts
import { flushPromises } from "@vue/test-utils";
import { __resetForTests as resetPins } from "../composables/useSessionPins";
import * as api from "../lib/api";

// Helper: two sessions on host "h1", two on "h2"
function seededProps(pinnedIds: string[] = []) {
  const s = (id: string, host: string, state = "running") => ({
    session_id: id,
    host_id: host,
    host: "example.local",
    task_state: state,
    unread: false,
  });
  vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(pinnedIds);
  vi.spyOn(api, "setPinnedSessionIds").mockResolvedValue(undefined);
  return {
    byHost: { h1: [s("a", "h1"), s("b", "h1")], h2: [s("c", "h2"), s("d", "h2")] },
    unreadByHost: { h1: 0, h2: 0 },
    primaryStateForHost: () => "running" as const,
    completedSeen: [],
    groupBy: "host" as const,
  };
}

describe("TaskGroupedList pinned group", () => {
  beforeEach(() => {
    resetPins();
    vi.restoreAllMocks();
  });

  test("renders pinned group at the top when at least one pinned session exists", async () => {
    const w = mount(TaskGroupedList, { props: seededProps(["b"]) });
    await flushPromises();
    await nextTick();
    const header = w.find("[data-test=pinned-group-header]");
    expect(header.exists()).toBe(true);
  });

  test("pinned session does not appear in its original host group", async () => {
    const w = mount(TaskGroupedList, { props: seededProps(["b"]) });
    await flushPromises();
    await nextTick();
    const rows = w.findAll("[data-test=task-row]");
    const ids = rows.map((r) => r.attributes("data-session-id"));
    // "b" should appear exactly once (in pinned group), not twice.
    const count = ids.filter((id) => id === "b").length;
    expect(count).toBe(1);
  });

  test("pinned group is absent when no session is pinned", async () => {
    const w = mount(TaskGroupedList, { props: seededProps([]) });
    await flushPromises();
    await nextTick();
    expect(w.find("[data-test=pinned-group-header]").exists()).toBe(false);
  });

  test("@contextmenu on a task row opens the SessionRowMenu", async () => {
    const w = mount(TaskGroupedList, { props: seededProps([]) });
    await flushPromises();
    await nextTick();
    const firstRow = w.find("[data-test=task-row]");
    await firstRow.trigger("contextmenu");
    expect(w.find("[data-test=session-row-menu]").exists()).toBe(true);
  });
});
```

If the existing `TaskGroupedList.test.ts` doesn't already `mount` with an activeSessionId prop, look at the top of that file for the existing helper — reuse it; the seeder above is illustrative and may need field alignment (`current_command`, `cwd`, etc. as your existing test does).

Also ensure each `.task-row` template gains `:data-session-id="s.session_id"` to make the assertion above cheap (add in Step 3).

- [ ] **Step 3: Run to verify tests fail**

Run: `cd desktop/frontend && npm test -- --run TaskGroupedList`
Expected: FAIL — no `[data-test=pinned-group-header]`, no `data-session-id` on rows, no context-menu handler.

- [ ] **Step 4: Wire into TaskGroupedList.vue — script setup**

Open `desktop/frontend/src/components/TaskGroupedList.vue`. At the top of `<script setup>`, add imports:

```ts
import { computed, ref } from "vue";
import { useSessionPins } from "../composables/useSessionPins";
import SessionRowMenu from "./SessionRowMenu.vue";
```

(Merge with existing `import` block; keep the file's existing order.)

Add composable + menu state:

```ts
const pins = useSessionPins();

const menuState = ref<{
  open: boolean;
  x: number;
  y: number;
  session: RemoteSession | null;
}>({ open: false, x: 0, y: 0, session: null });

function onRowMenu(e: MouseEvent, s: RemoteSession) {
  e.preventDefault();
  menuState.value = { open: true, x: e.clientX, y: e.clientY, session: s };
}

function closeMenu() {
  menuState.value = { ...menuState.value, open: false, session: null };
}

function onToggleFromMenu() {
  const s = menuState.value.session;
  if (s) pins.toggle(s.session_id);
}

const pinnedSessions = computed<RemoteSession[]>(() => {
  const out: RemoteSession[] = [];
  const seen = new Set<string>();
  const source =
    props.groupBy === "state"
      ? props.byState ?? {}
      : props.byHost ?? {};
  for (const list of Object.values(source)) {
    for (const s of list) {
      if (seen.has(s.session_id)) continue;
      if (pins.isPinned(s.session_id)) {
        seen.add(s.session_id);
        out.push(s);
      }
    }
  }
  out.sort((a, b) => urgencyIndex(a.task_state) - urgencyIndex(b.task_state));
  return out;
});

const PINNED_KEY = "__pinned__";
```

- [ ] **Step 5: Filter pinned rows out of existing group iteration**

Find the existing group loop in the `<template>` — search for the loop that walks the current groups (host or state). Add a `.filter()` on the per-group session array so it excludes ids currently in `pins.pinnedIds.value`. Concretely, either wrap the source computed:

```ts
const filteredGroups = computed(() => {
  const source =
    props.groupBy === "state"
      ? props.byState ?? {}
      : props.byHost ?? {};
  const filtered: Record<string, RemoteSession[]> = {};
  for (const [k, list] of Object.entries(source)) {
    filtered[k] = list.filter((s) => !pins.isPinned(s.session_id));
  }
  return filtered;
});
```

…and replace the template's direct read of `byHost` / `byState` inside the group loop with `filteredGroups[key]`. If a group becomes empty after filtering, skip rendering it (existing empty-group behavior is preserved if the file already had that; if not, add `v-if="filteredGroups[key].length > 0"`).

- [ ] **Step 6: Render the pinned group at the top of the template**

Immediately before the existing group rendering (before the first `v-for` over group keys), add:

```vue
<template v-if="pinnedSessions.length > 0">
  <div
    class="group-header pinned-group"
    data-test="pinned-group-header"
    @click="toggleGroupCollapsed(PINNED_KEY)"
  >
    <span class="pin-icon" aria-hidden="true">📌</span>
    <span class="group-title">{{ t("tasks.pinned.title") }}</span>
    <span class="group-count">{{ pinnedSessions.length }}</span>
  </div>
  <template v-if="!isGroupCollapsed(PINNED_KEY)">
    <button
      v-for="s in pinnedSessions"
      :key="s.session_id"
      class="task-row"
      :class="{ active: s.session_id === activeSessionId }"
      data-test="task-row"
      :data-session-id="s.session_id"
      :data-active="s.session_id === activeSessionId ? 'true' : undefined"
      @click="emit('open', s)"
      @contextmenu="onRowMenu($event, s)"
    >
      <!-- Reuse the existing row inner content by refactoring it into a
           <slot> or extracting into a local <RowInner :s="s" /> subcomponent.
           If neither is present today, inline the row inner markup here,
           matching the existing group's row markup exactly. -->
    </button>
  </template>
</template>
```

**Note to implementer:** the row's inner markup (`.row-top`, `TaskStateIcon`, `.cmd-and-cwd`, `.row-mark-read`, `.cwd`) already exists in the file. To keep DRY, extract it into a small local component `TaskRowInner.vue` at the same time and use it from both the pinned group and each host/state group. If that extraction feels too big for this task, inline the same markup in both places and file a TODO — but prefer the extraction if practical (spec §4.4).

Also add `:data-session-id="s.session_id"` and `@contextmenu="onRowMenu($event, s)"` to the existing group's `.task-row` in the file (same file, both loops must have parity).

- [ ] **Step 7: Wire the SessionRowMenu at the template root**

At the bottom of the `<template>` (just before the closing tag of the root wrapper — or immediately outside the list if scoping needs it), add:

```vue
<SessionRowMenu
  :open="menuState.open"
  :x="menuState.x"
  :y="menuState.y"
  :pinned="menuState.session ? pins.isPinned(menuState.session.session_id) : false"
  :label-pin="t('tasks.pinned.menuPin')"
  :label-unpin="t('tasks.pinned.menuUnpin')"
  @close="closeMenu"
  @toggle-pin="onToggleFromMenu"
/>
```

- [ ] **Step 8: Run to verify tests pass**

Run: `cd desktop/frontend && npm test -- --run TaskGroupedList useSessionPins SessionRowMenu`
Expected: PASS (all new + existing tests).

Also run the full frontend suite: `npm test -- --run`
Expected: no other file regresses.

- [ ] **Step 9: Manual verification (dev build)**

```bash
cd desktop
wails dev -tags webkit2_41   # macOS/Windows may omit -tags
```

Verify:
- Right-click a session row → menu appears at cursor with "Pin to top"
- Click → row moves into a new "📌 Pinned" group at the top
- Right-click again → menu shows "Unpin"
- Restart the app → pinned state persists
- Switch groupBy host ↔ state via the header button → pinned group stays at the top
- Collapse the pinned group header → rows hide; expand → return

- [ ] **Step 10: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts \
        desktop/frontend/src/components/TaskGroupedList.vue \
        desktop/frontend/src/components/TaskGroupedList.test.ts
# If a TaskRowInner extraction was done, also add:
#   desktop/frontend/src/components/TaskRowInner.vue
git commit -m "feat(desktop): pin sessions to top of session bar

Right-click any session row → Pin to top; pinned sessions are pulled
out of their host / state group into a new '📌 Pinned' virtual group
at the top of the list, sorted by task_state urgency like other groups.
State persists via appConfig.PinnedSessionIDs (desktop-local only, no
cross-client sync). Empty pinned set hides the group header."
```

---

## Self-Review Checklist

Run through these before handing off to the executor:

**Spec coverage** — every section of `docs/superpowers/specs/2026-07-20-session-bar-pin-design.md` maps to a task:

| Spec section | Task |
|--------------|------|
| §4.1 Data storage (`PinnedSessionIDs` field) | Task 1 |
| §4.2 Go API (`Get/SetPinnedSessionIds`) | Task 1 |
| §4.3 Composable (`useSessionPins`) | Task 3 (bindings in Task 2) |
| §4.4 Rendering (virtual group + row filter + row `data-session-id`) | Task 5 |
| §4.5 Right-click menu (`SessionRowMenu`) | Task 4 + Task 5 wiring |
| §4.6 i18n (`tasks.pinned.*`) | Task 5 |
| §4.7 Edge cases | Covered by unit + manual tests in Tasks 1, 3, 4, 5 |
| §5 Test plan | Tests distributed across Task 1 (Go), 3 (composable), 4 (menu), 5 (grouped list) |

**Placeholder scan** — no TBD / TODO / "add appropriate error handling" / "similar to Task N" strings remain in the plan body.

**Type consistency:**
- `getPinnedSessionIds(): Promise<string[]>` — Task 2 defines, Task 3 imports, Task 3 test spies on it ✓
- `setPinnedSessionIds(ids: string[]): Promise<void>` — Task 2 defines, Task 3 imports, Task 3 test spies on it ✓
- `useSessionPins()` return type `UseSessionPins { pinnedIds, isPinned, pin, unpin, toggle }` — Task 3 defines, Task 5 uses `pins.isPinned` + `pins.toggle` ✓
- `SessionRowMenu` props (`open, x, y, pinned, labelPin, labelUnpin`) + emits (`close, togglePin`) — Task 4 defines, Task 5 wires with matching casing (`@toggle-pin` in template = camelCase `togglePin` emit) ✓
- Go `SetPinnedSessionIds([]string) error` — Task 1 signature, Task 2 binding declares `Promise<void>` (Wails discards `error` unless it's the last named return; check by regenerating in Step 1) ✓

**Ordering:** Task dependencies flow strictly forward. Tasks 3 and 4 are independent; either can precede the other. Task 5 depends on 3 and 4 both being merged.

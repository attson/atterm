# Desktop Task State Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the desktop frontend render `task_state`, `unread`, `type`, and `summary` from the relay-attention backend, via a new collapsible task sidebar, a state-driven tab dot, a richer Remote Sessions dialog, and two switchable visual presets (Vivid / Quiet).

**Architecture:** Three new components layered (atomic `TaskStateIcon` → shared `TaskGroupedList` → shell `TaskSidebar`); two composables (`useTaskPreset` for reactive preset, `useSessions` for merging local + remote lists with derived per-host counts); a pure preset registry in `lib/taskState.ts`; four new Wails Go↔TS bindings (preset preference, sidebar collapsed preference, mark-sessions-seen); and incremental edits to `TabBar.vue`, `RemoteSessionsDialog.vue`, `SettingsDialog.vue`, `App.vue`.

**Tech Stack:** Vue 3 `<script setup>` + Composition API; vitest + `@vue/test-utils` (mount-based) and source-string assertions per existing patterns; Wails v2 (Go-bound methods); CSS custom properties on `<html>` keyed by `data-task-preset`; no new runtime deps.

**Spec:** `docs/superpowers/specs/2026-06-06-desktop-task-state-display-design.md`.

---

## File structure

New files:
- `desktop/frontend/src/lib/taskState.ts` — preset registry, `TaskStatePreset` interface, `TaskState` union, `presets` map
- `desktop/frontend/src/composables/useTaskPreset.ts` — reactive active preset; writes `documentElement.dataset.taskPreset`
- `desktop/frontend/src/composables/useSessions.ts` — merge local + remote → byHost / unreadByHost / primaryStateForHost / totalUnread / completedSeen
- `desktop/frontend/src/components/TaskStateIcon.vue` — atomic visual
- `desktop/frontend/src/components/TaskGroupedList.vue` — shared list (host groups + rows + completed fold)
- `desktop/frontend/src/components/TaskSidebar.vue` — collapsible left rail
- `desktop/frontend/src/components/SettingsTasks.vue` — Settings dialog "Task display" tab
- Test files alongside each (`.test.ts`)

Modified files:
- `desktop/frontend/src/platform/types.ts` — `RemoteSession` gains `unread?: boolean`, `attention_at?: number`
- `desktop/frontend/src/i18n/messages/en.ts` + `zh-CN.ts` — `tasks.*` namespace
- `desktop/frontend/src/lib/api.ts` — Wails wrappers for new bindings
- `desktop/app.go` — new Go methods (preset, sidebar collapsed, MarkSessionsSeen) + Config additions
- `desktop/frontend/src/components/TabBar.vue` — replace `.dot` with `<TaskStateIcon>`; add unread `●`
- `desktop/frontend/src/components/RemoteSessionsDialog.vue` — render via `<TaskGroupedList>` + Mark-all-read
- `desktop/frontend/src/components/SettingsDialog.vue` — wire new "Task display" tab
- `desktop/frontend/src/lib/shortcutBindings.ts` — register `toggleTaskSidebar` action
- `desktop/frontend/src/App.vue` — mount `<TaskSidebar>`, consume `useSessions`, wire `Cmd/Ctrl+B`

---

## Task 1: `RemoteSession` type — add `unread` + `attention_at`

**Files:**
- Modify: `desktop/frontend/src/platform/types.ts` (`RemoteSession` interface, lines ~63-82)
- Test: `desktop/frontend/src/platform/types.test.ts` (new — small, just structural)

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/platform/types.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import type { RemoteSession } from "./types";

describe("RemoteSession type", () => {
  test("accepts unread and attention_at fields", () => {
    const s: RemoteSession = {
      session_id: "s1",
      host_id: "h1",
      host: "mac",
      user: "you",
      title: "claude",
      cols: 80,
      rows: 24,
      unread: true,
      attention_at: 1700000000,
    };
    expect(s.unread).toBe(true);
    expect(s.attention_at).toBe(1700000000);
  });
});
```

- [ ] **Step 2: Run the test, expect compile failure**

Run:
```bash
cd desktop/frontend && npx vitest run src/platform/types.test.ts
```
Expected: FAIL — TS error "Object literal may only specify known properties, and 'unread' does not exist in type 'RemoteSession'".

- [ ] **Step 3: Add the fields**

In `desktop/frontend/src/platform/types.ts`, inside `interface RemoteSession`, after the `summary?: SessionSummary` line, add:

```ts
  /** Per-user unread flag — computed by the relay from attention_at vs seen_at vs
   *  subscriberCount. Local-only sessions (not uplinked) leave this undefined. */
  unread?: boolean;
  /** Unix seconds of the session's last attention-worthy transition
   *  (waiting_input, or non-shell completed/failed). 0/undefined = none pending. */
  attention_at?: number;
```

- [ ] **Step 4: Run the test, expect pass**

```bash
cd desktop/frontend && npx vitest run src/platform/types.test.ts
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/platform/types.ts desktop/frontend/src/platform/types.test.ts
git commit -m "desktop/types: add unread + attention_at to RemoteSession"
```

---

## Task 2: i18n `tasks.*` namespace (en + zh-CN)

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts` (add `tasks: {...}` namespace at top level)
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts` (mirror)
- Test: `desktop/frontend/src/i18n/messages/tasks.test.ts` (new — parity check)

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/i18n/messages/tasks.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { en } from "./en";
import { zhCN } from "./zh-CN";

describe("tasks i18n namespace", () => {
  test("en has the full tasks namespace", () => {
    expect(en.tasks.sidebar.title).toBeTypeOf("string");
    expect(en.tasks.preset.vivid.name).toBeTypeOf("string");
    expect(en.tasks.preset.vivid.description).toBeTypeOf("string");
    expect(en.tasks.preset.quiet.name).toBeTypeOf("string");
    expect(en.tasks.preset.quiet.description).toBeTypeOf("string");
    expect(en.tasks.markAllRead).toBeTypeOf("string");
    expect(en.tasks.markRead).toBeTypeOf("string");
    expect(en.tasks.completedFold).toBeTypeOf("string");
    expect(en.tasks.unreadBadge).toBeTypeOf("string");
    expect(en.tasks.settings.section).toBeTypeOf("string");
    expect(en.tasks.settings.expandByDefault).toBeTypeOf("string");
    expect(en.tasks.unavailableToast).toBeTypeOf("string");
  });
  test("zh-CN matches en shape", () => {
    expect(Object.keys(zhCN.tasks)).toEqual(Object.keys(en.tasks));
    expect(Object.keys(zhCN.tasks.preset.vivid)).toEqual(
      Object.keys(en.tasks.preset.vivid),
    );
    expect(Object.keys(zhCN.tasks.settings)).toEqual(
      Object.keys(en.tasks.settings),
    );
  });
});
```

- [ ] **Step 2: Run the test, expect failure**

```bash
cd desktop/frontend && npx vitest run src/i18n/messages/tasks.test.ts
```
Expected: FAIL — `Cannot read properties of undefined (reading 'sidebar')`.

- [ ] **Step 3: Add `tasks` namespace to `en.ts`**

In `desktop/frontend/src/i18n/messages/en.ts`, add at the top level (after the existing `mobile: {...}` block but before the closing `}` of the `en` object):

```ts
  tasks: {
    sidebar: {
      title: "Tasks",
      collapse: "Collapse sidebar (Cmd/Ctrl+B)",
      expand: "Expand sidebar (Cmd/Ctrl+B)",
    },
    preset: {
      vivid: {
        name: "Vivid",
        description: "Colorful, animated, with type icons. Quick to spot what's happening.",
      },
      quiet: {
        name: "Quiet",
        description: "Muted palette, only the running spinner animates, no type icons. For focused work.",
      },
    },
    markAllRead: "Mark all read",
    markRead: "Mark read",
    completedFold: "Completed",
    unreadBadge: "{count} unread",
    settings: {
      section: "Task display",
      preset: "Visual preset",
      expandByDefault: "Expand task sidebar by default",
    },
    unavailableToast: "Mark-read unavailable — please update the relay.",
  },
```

- [ ] **Step 4: Mirror in `zh-CN.ts`**

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, add the same shape with Chinese values:

```ts
  tasks: {
    sidebar: {
      title: "任务",
      collapse: "折叠侧栏 (Cmd/Ctrl+B)",
      expand: "展开侧栏 (Cmd/Ctrl+B)",
    },
    preset: {
      vivid: {
        name: "鲜明",
        description: "色彩鲜明 + 动画 + 类型图标。一眼看到正在发生什么。",
      },
      quiet: {
        name: "柔和",
        description: "柔和色调,仅运行中带动画,无类型图标。专注时使用。",
      },
    },
    markAllRead: "全部标已读",
    markRead: "标已读",
    completedFold: "已完成",
    unreadBadge: "{count} 条未读",
    settings: {
      section: "任务状态显示",
      preset: "视觉预设",
      expandByDefault: "默认展开任务侧栏",
    },
    unavailableToast: "标已读不可用 — 请升级 relay。",
  },
```

- [ ] **Step 5: Run the test, expect pass**

```bash
cd desktop/frontend && npx vitest run src/i18n/messages/tasks.test.ts
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts \
        desktop/frontend/src/i18n/messages/tasks.test.ts
git commit -m "desktop/i18n: add tasks.* namespace (en + zh-CN)"
```

---

## Task 3: `lib/taskState.ts` — preset registry

**Files:**
- Create: `desktop/frontend/src/lib/taskState.ts`
- Test: `desktop/frontend/src/lib/taskState.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/taskState.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import {
  presets,
  type PresetId,
  type TaskState,
  ALL_TASK_STATES,
} from "./taskState";

const STATES: TaskState[] = [
  "idle",
  "running",
  "waiting_input",
  "completed",
  "failed",
  "disconnected",
  "closed",
];

describe("taskState presets", () => {
  test("ALL_TASK_STATES enumerates every state", () => {
    expect([...ALL_TASK_STATES].sort()).toEqual([...STATES].sort());
  });

  for (const id of ["vivid", "quiet"] as PresetId[]) {
    describe(id, () => {
      const p = presets[id];
      test("has id + i18nKey", () => {
        expect(p.id).toBe(id);
        expect(p.i18nKey).toContain("tasks.preset." + id);
      });
      test.each(STATES)("colorOf(%s) returns a hex color", (s) => {
        expect(p.colorOf(s)).toMatch(/^#[0-9a-f]{6}$/i);
      });
      test.each(STATES)("glyphOf(%s) returns spinner or a single char", (s) => {
        const g = p.glyphOf(s);
        expect(typeof g).toBe("string");
        if (g !== "spinner") expect(g.length).toBe(1);
      });
      test("running uses spinner glyph", () => {
        expect(p.glyphOf("running")).toBe("spinner");
      });
      test("spinnerDurationMs(running) > 0", () => {
        expect(p.spinnerDurationMs("running")).toBeGreaterThan(0);
      });
      test("spinnerDurationMs(non-running) is 0", () => {
        for (const s of STATES.filter((x) => x !== "running")) {
          expect(p.spinnerDurationMs(s)).toBe(0);
        }
      });
    });
  }

  test("vivid pulses waiting_input; quiet does not", () => {
    expect(presets.vivid.animatePulse("waiting_input")).toBe(true);
    expect(presets.quiet.animatePulse("waiting_input")).toBe(false);
  });
  test("only vivid shows type icon", () => {
    expect(presets.vivid.showTypeIcon).toBe(true);
    expect(presets.quiet.showTypeIcon).toBe(false);
  });
  test("quiet text opacity is lower than vivid", () => {
    expect(presets.quiet.textOpacity).toBeLessThan(presets.vivid.textOpacity);
  });
});
```

- [ ] **Step 2: Run the test, expect failure**

```bash
cd desktop/frontend && npx vitest run src/lib/taskState.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `lib/taskState.ts`**

Create `desktop/frontend/src/lib/taskState.ts`:

```ts
export type TaskState =
  | "idle"
  | "running"
  | "waiting_input"
  | "completed"
  | "failed"
  | "disconnected"
  | "closed";

export type PresetId = "vivid" | "quiet";

export const ALL_TASK_STATES: readonly TaskState[] = [
  "idle",
  "running",
  "waiting_input",
  "completed",
  "failed",
  "disconnected",
  "closed",
] as const;

export interface TaskStatePreset {
  id: PresetId;
  i18nKey: string; // tasks.preset.<id>
  colorOf(state: TaskState): string;
  glyphOf(state: TaskState): "spinner" | string;
  spinnerDurationMs(state: TaskState): number;
  animatePulse(state: TaskState): boolean;
  showTypeIcon: boolean;
  textOpacity: number;
}

const VIVID_COLORS: Record<TaskState, string> = {
  idle: "#6b7280",
  running: "#06b6d4",
  waiting_input: "#f59e0b",
  completed: "#22c55e",
  failed: "#ef4444",
  disconnected: "#6b7280",
  closed: "#6b7280",
};

const QUIET_COLORS: Record<TaskState, string> = {
  idle: "#6b7280",
  running: "#4b8a93",
  waiting_input: "#b88239",
  completed: "#4a8b6a",
  failed: "#a04b4b",
  disconnected: "#6b7280",
  closed: "#6b7280",
};

const GLYPHS: Record<TaskState, "spinner" | string> = {
  idle: "·",
  running: "spinner",
  waiting_input: "◐",
  completed: "✓",
  failed: "✗",
  disconnected: "·",
  closed: "·",
};

function makePreset(
  id: PresetId,
  colors: Record<TaskState, string>,
  spinDuration: number,
  pulseWaiting: boolean,
  showTypeIcon: boolean,
  textOpacity: number,
): TaskStatePreset {
  return {
    id,
    i18nKey: `tasks.preset.${id}`,
    colorOf: (s) => colors[s],
    glyphOf: (s) => GLYPHS[s],
    spinnerDurationMs: (s) => (s === "running" ? spinDuration : 0),
    animatePulse: (s) => pulseWaiting && s === "waiting_input",
    showTypeIcon,
    textOpacity,
  };
}

export const presets: Record<PresetId, TaskStatePreset> = {
  vivid: makePreset("vivid", VIVID_COLORS, 1500, true, true, 1.0),
  quiet: makePreset("quiet", QUIET_COLORS, 2500, false, false, 0.75),
};
```

- [ ] **Step 4: Run the test, expect pass**

```bash
cd desktop/frontend && npx vitest run src/lib/taskState.test.ts
```
Expected: PASS (all ~24 cases).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/taskState.ts desktop/frontend/src/lib/taskState.test.ts
git commit -m "desktop/lib: taskState preset registry (Vivid + Quiet)"
```

---

## Task 4: Wails bindings — task preset, sidebar collapsed, mark-sessions-seen

**Files:**
- Modify: `desktop/app.go` (add four Go methods)
- Modify: `desktop/config.go` (add two fields to the persisted Config struct) — confirm by reading
- Modify: `desktop/frontend/src/lib/api.ts` (extend `AppBindings`, add typed wrappers)
- Test: `desktop/frontend/src/lib/api.test.ts` (new — wrapper passthrough only)

> **Implementer reading first:** open `desktop/app.go`, find `GetTerminalTheme` and `SetTerminalTheme`. Mirror that exact pattern (storage write, error return) for the two new Get/Set pairs. For `MarkSessionsSeen`, find an existing Go-side method that makes an authenticated HTTP call to the relay (e.g. `FetchRelayMe` or `CreatePairingToken`) and follow the same auth/URL/error-handling idiom. The plan below shows the *frontend* contract precisely; the Go side mirrors prior patterns.

- [ ] **Step 1: Write the failing wrapper test**

Create `desktop/frontend/src/lib/api.test.ts`:

```ts
import { afterEach, describe, expect, test, vi } from "vitest";
import {
  getTaskPreset,
  setTaskPreset,
  getTaskSidebarCollapsed,
  setTaskSidebarCollapsed,
  markSessionsSeen,
  __setBindingsForTest,
} from "./api";

afterEach(() => {
  __setBindingsForTest(undefined);
});

describe("task display api wrappers", () => {
  test("getTaskPreset delegates to bindings", async () => {
    const fn = vi.fn().mockResolvedValue("vivid");
    __setBindingsForTest({ GetTaskPreset: fn } as any);
    await expect(getTaskPreset()).resolves.toBe("vivid");
    expect(fn).toHaveBeenCalledOnce();
  });
  test("setTaskPreset passes the preset string", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ SetTaskPreset: fn } as any);
    await setTaskPreset("quiet");
    expect(fn).toHaveBeenCalledWith("quiet");
  });
  test("getTaskSidebarCollapsed returns boolean", async () => {
    const fn = vi.fn().mockResolvedValue(true);
    __setBindingsForTest({ GetTaskSidebarCollapsed: fn } as any);
    await expect(getTaskSidebarCollapsed()).resolves.toBe(true);
  });
  test("setTaskSidebarCollapsed passes the flag", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ SetTaskSidebarCollapsed: fn } as any);
    await setTaskSidebarCollapsed(true);
    expect(fn).toHaveBeenCalledWith(true);
  });
  test("markSessionsSeen ids variant", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ MarkSessionsSeen: fn } as any);
    await markSessionsSeen({ ids: ["a", "b"] });
    expect(fn).toHaveBeenCalledWith(["a", "b"], false);
  });
  test("markSessionsSeen all variant", async () => {
    const fn = vi.fn().mockResolvedValue(undefined);
    __setBindingsForTest({ MarkSessionsSeen: fn } as any);
    await markSessionsSeen({ all: true });
    expect(fn).toHaveBeenCalledWith([], true);
  });
});
```

> If `__setBindingsForTest` is not the existing test-injection helper name, look at how other api.ts test files inject fakes (e.g. `__setPlatformForTests` is used at the platform level). Use whatever real seam exists; if none, expose a small one alongside the `bindings()` factory (one of the simplest changes — gate the override on `import.meta.env.MODE === "test"` if you want belt-and-suspenders).

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/lib/api.test.ts
```
Expected: FAIL — wrappers undefined.

- [ ] **Step 3: Extend `AppBindings` + add wrappers in `lib/api.ts`**

In `desktop/frontend/src/lib/api.ts`, in the `interface AppBindings` block, add (alongside existing entries):

```ts
  GetTaskPreset(): Promise<string>;
  SetTaskPreset(preset: string): Promise<void>;
  GetTaskSidebarCollapsed(): Promise<boolean>;
  SetTaskSidebarCollapsed(collapsed: boolean): Promise<void>;
  MarkSessionsSeen(ids: string[], all: boolean): Promise<void>;
```

After the existing wrapper functions, append:

```ts
import type { PresetId } from "./taskState";

export function getTaskPreset(): Promise<string> {
  return bindings().GetTaskPreset();
}
export function setTaskPreset(preset: PresetId): Promise<void> {
  return bindings().SetTaskPreset(preset);
}
export function getTaskSidebarCollapsed(): Promise<boolean> {
  return bindings().GetTaskSidebarCollapsed();
}
export function setTaskSidebarCollapsed(collapsed: boolean): Promise<void> {
  return bindings().SetTaskSidebarCollapsed(collapsed);
}

export type MarkSessionsSeenOpts = { ids: string[] } | { all: true };

export function markSessionsSeen(opts: MarkSessionsSeenOpts): Promise<void> {
  if ("all" in opts && opts.all) {
    return bindings().MarkSessionsSeen([], true);
  }
  return bindings().MarkSessionsSeen((opts as { ids: string[] }).ids, false);
}
```

- [ ] **Step 4: Add Go-side stubs (read existing patterns first)**

Open `desktop/app.go` and find `GetTerminalTheme` + `SetTerminalTheme`. Replicate that pattern for `GetTaskPreset` / `SetTaskPreset` (string field "vivid" default) and `GetTaskSidebarCollapsed` / `SetTaskSidebarCollapsed` (bool default `false`). Persisted field locations: the same Config struct used for theme. Defaults applied on load.

`MarkSessionsSeen` — find an existing relay-HTTP helper (e.g. `FetchRelayMe`). Add:

```go
// MarkSessionsSeen marks the given session ids as seen for the current
// relay user (or "all" owned sessions when all is true) by POSTing to
// /api/sessions/seen on the connected relay. Errors propagate so the
// frontend can show a toast.
func (a *App) MarkSessionsSeen(ids []string, all bool) error {
    body, _ := json.Marshal(map[string]any{
        "session_ids": ids,
        "all":         all,
    })
    return a.relayPost("/api/sessions/seen", body) // adapt to existing helper name
}
```

Use whatever auth/URL/error-handling pattern the existing relay-HTTP methods use; do NOT invent a parallel mechanism. If there's no shared `relayPost` helper, inline the same pattern an existing call uses.

- [ ] **Step 5: Regenerate Wails bindings if needed**

```bash
# Project root
cd desktop && wails generate module
# If the project doesn't pin a wails CLI version, use the same command from
# package.json's "ios:sync"-style scripts (check desktop/package.json for the
# binding regen step; older projects rely on `wails build` to regen).
```

- [ ] **Step 6: Run frontend tests, expect pass**

```bash
cd desktop/frontend && npx vitest run src/lib/api.test.ts
```
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add desktop/app.go desktop/config.go \
        desktop/frontend/src/lib/api.ts \
        desktop/frontend/src/lib/api.test.ts \
        desktop/frontend/wailsjs/   # regenerated bindings, if present
git commit -m "desktop: Wails bindings for task preset, sidebar collapsed, mark-seen"
```

---

## Task 5: `composables/useTaskPreset.ts`

**Files:**
- Create: `desktop/frontend/src/composables/useTaskPreset.ts`
- Test: `desktop/frontend/src/composables/useTaskPreset.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/composables/useTaskPreset.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { effectScope, nextTick } from "vue";
import { useTaskPreset } from "./useTaskPreset";
import * as api from "../lib/api";

describe("useTaskPreset", () => {
  let scope: ReturnType<typeof effectScope>;
  beforeEach(() => {
    document.documentElement.dataset.taskPreset = undefined;
    vi.restoreAllMocks();
    scope = effectScope();
  });
  afterEach(() => scope.stop());

  test("loads preset from Wails and writes html dataset", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("quiet");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => {
      preset = useTaskPreset();
    });
    await nextTick();
    await nextTick();
    expect(preset!.activeId.value).toBe("quiet");
    expect(document.documentElement.dataset.taskPreset).toBe("quiet");
  });

  test("setPreset writes through Wails and updates dataset", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("vivid");
    const setSpy = vi.spyOn(api, "setTaskPreset").mockResolvedValue(undefined);
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await nextTick();
    await preset!.setPreset("quiet");
    expect(setSpy).toHaveBeenCalledWith("quiet");
    expect(preset!.activeId.value).toBe("quiet");
    expect(document.documentElement.dataset.taskPreset).toBe("quiet");
  });

  test("falls back to localStorage when bindings missing", async () => {
    vi.spyOn(api, "getTaskPreset").mockRejectedValue(new Error("no bindings"));
    localStorage.setItem("taskPreset", "quiet");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await nextTick();
    await nextTick();
    expect(preset!.activeId.value).toBe("quiet");
  });

  test("defaults to vivid when nothing is stored anywhere", async () => {
    vi.spyOn(api, "getTaskPreset").mockRejectedValue(new Error("no bindings"));
    localStorage.removeItem("taskPreset");
    let preset: ReturnType<typeof useTaskPreset> | undefined;
    scope.run(() => (preset = useTaskPreset()));
    await nextTick();
    await nextTick();
    expect(preset!.activeId.value).toBe("vivid");
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/composables/useTaskPreset.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 3: Implement the composable**

Create `desktop/frontend/src/composables/useTaskPreset.ts`:

```ts
import { computed, ref, watch } from "vue";
import { getTaskPreset, setTaskPreset } from "../lib/api";
import { presets, type PresetId, type TaskStatePreset } from "../lib/taskState";

const STORAGE_KEY = "taskPreset";

function isPresetId(s: string | null | undefined): s is PresetId {
  return s === "vivid" || s === "quiet";
}

// Module-level singleton. Multiple call sites share one source of truth.
const activeId = ref<PresetId>("vivid");
let initialized = false;

function applyDataset(id: PresetId) {
  document.documentElement.dataset.taskPreset = id;
}

async function loadInitial() {
  try {
    const v = await getTaskPreset();
    if (isPresetId(v)) {
      activeId.value = v;
      applyDataset(v);
      return;
    }
  } catch {
    /* fall through */
  }
  // Fallback: localStorage
  if (typeof localStorage !== "undefined") {
    const stored = localStorage.getItem(STORAGE_KEY);
    if (isPresetId(stored)) {
      activeId.value = stored;
      applyDataset(stored);
      return;
    }
  }
  applyDataset("vivid");
}

export interface UseTaskPreset {
  activeId: typeof activeId;
  active: ReturnType<typeof computed<TaskStatePreset>>;
  setPreset(id: PresetId): Promise<void>;
}

export function useTaskPreset(): UseTaskPreset {
  if (!initialized) {
    initialized = true;
    void loadInitial();
    // Keep dataset in sync if activeId changes from any path.
    watch(activeId, (v) => applyDataset(v));
  }
  const active = computed(() => presets[activeId.value]);
  async function setPreset(id: PresetId) {
    activeId.value = id;
    applyDataset(id);
    try {
      await setTaskPreset(id);
    } catch {
      if (typeof localStorage !== "undefined") {
        localStorage.setItem(STORAGE_KEY, id);
      }
    }
  }
  return { activeId, active, setPreset };
}

// Test-only reset (do not export from package index).
export function __resetForTests() {
  initialized = false;
  activeId.value = "vivid";
  document.documentElement.dataset.taskPreset = undefined as any;
}
```

In the test file, import and call `__resetForTests()` in `beforeEach` to clear the singleton between cases.

- [ ] **Step 4: Update the test to reset the singleton**

In `useTaskPreset.test.ts` `beforeEach`, add:

```ts
import { __resetForTests } from "./useTaskPreset";
// ...
beforeEach(() => {
  __resetForTests();
  // ... existing setup
});
```

- [ ] **Step 5: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/composables/useTaskPreset.test.ts
```
Expected: PASS (4 cases).

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/composables/useTaskPreset.ts \
        desktop/frontend/src/composables/useTaskPreset.test.ts
git commit -m "desktop/composable: useTaskPreset (Wails-backed, html dataset)"
```

---

## Task 6: `composables/useSessions.ts`

**Files:**
- Create: `desktop/frontend/src/composables/useSessions.ts`
- Test: `desktop/frontend/src/composables/useSessions.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/composables/useSessions.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { ref } from "vue";
import type { RemoteSession } from "../platform/types";
import { useSessions } from "./useSessions";

function mk(over: Partial<RemoteSession>): RemoteSession {
  return {
    session_id: "s",
    host_id: "h",
    host: "host",
    user: "u",
    title: "",
    cols: 80,
    rows: 24,
    ...over,
  };
}

describe("useSessions", () => {
  test("merges local + remote; relay wins on same session_id", () => {
    const local = ref<RemoteSession[]>([
      mk({ session_id: "s1", host_id: "h1", task_state: "running" }),
    ]);
    const remote = ref<RemoteSession[]>([
      mk({
        session_id: "s1",
        host_id: "h1",
        task_state: "completed",
        unread: true,
        attention_at: 1000,
      }),
    ]);
    const { all } = useSessions(local, remote);
    expect(all.value.length).toBe(1);
    expect(all.value[0].task_state).toBe("completed"); // relay wins
    expect(all.value[0].unread).toBe(true);
  });

  test("byHost groups by host_id; remoteByHost excludes local-only hosts", () => {
    const local = ref<RemoteSession[]>([
      mk({ session_id: "L1", host_id: "h-local", host: "mac" }),
    ]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "R1", host_id: "h-remote", host: "server-1" }),
    ]);
    const { byHost, remoteByHost } = useSessions(local, remote, {
      localHostId: "h-local",
    });
    expect(Object.keys(byHost.value).sort()).toEqual(["h-local", "h-remote"]);
    expect(Object.keys(remoteByHost.value)).toEqual(["h-remote"]);
  });

  test("unreadByHost counts only sessions with unread===true", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "a", host_id: "h", attention_at: 1, unread: true }),
      mk({ session_id: "b", host_id: "h", attention_at: 1, unread: false }),
      mk({ session_id: "c", host_id: "h" }), // unread undefined
    ]);
    const { unreadByHost, totalUnread } = useSessions(local, remote);
    expect(unreadByHost.value["h"]).toBe(1);
    expect(totalUnread.value).toBe(1);
  });

  test("primaryStateForHost respects urgency order", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "a", host_id: "h", task_state: "completed" }),
      mk({ session_id: "b", host_id: "h", task_state: "running" }),
      mk({ session_id: "c", host_id: "h", task_state: "waiting_input" }),
    ]);
    const { primaryStateForHost } = useSessions(local, remote);
    expect(primaryStateForHost("h")).toBe("waiting_input");
  });

  test("completedSeen is sessions completed/failed with unread===false", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({ session_id: "a", host_id: "h", task_state: "completed", unread: false }),
      mk({ session_id: "b", host_id: "h", task_state: "failed", unread: false }),
      mk({ session_id: "c", host_id: "h", task_state: "completed", unread: true }),
      mk({ session_id: "d", host_id: "h", task_state: "running" }),
    ]);
    const { completedSeen } = useSessions(local, remote);
    expect(completedSeen.value.map((s) => s.session_id).sort()).toEqual(["a", "b"]);
  });

  test("rows within a host sorted: unread-first then urgency then last_output_at desc", () => {
    const local = ref<RemoteSession[]>([]);
    const remote = ref<RemoteSession[]>([
      mk({
        session_id: "a",
        host_id: "h",
        task_state: "running",
        last_output_at: 100,
      }),
      mk({
        session_id: "b",
        host_id: "h",
        task_state: "completed",
        unread: true,
        attention_at: 1,
      }),
      mk({
        session_id: "c",
        host_id: "h",
        task_state: "waiting_input",
        attention_at: 1,
      }),
    ]);
    const { byHost } = useSessions(local, remote);
    expect(byHost.value["h"].map((s) => s.session_id)).toEqual(["c", "b", "a"]);
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/composables/useSessions.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `useSessions.ts`**

Create `desktop/frontend/src/composables/useSessions.ts`:

```ts
import { computed, type Ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";

const URGENCY: TaskState[] = [
  "waiting_input",
  "failed",
  "running",
  "completed",
  "idle",
  "disconnected",
  "closed",
];
const urgencyIndex = (s?: TaskState): number => {
  const i = s ? URGENCY.indexOf(s) : -1;
  return i === -1 ? URGENCY.length : i;
};

export interface UseSessionsOptions {
  /** When set, sessions on this host_id are considered "local" and excluded
   *  from remoteByHost. */
  localHostId?: string;
}

export interface UseSessionsReturn {
  all: ReturnType<typeof computed<RemoteSession[]>>;
  byHost: ReturnType<typeof computed<Record<string, RemoteSession[]>>>;
  remoteByHost: ReturnType<typeof computed<Record<string, RemoteSession[]>>>;
  unreadByHost: ReturnType<typeof computed<Record<string, number>>>;
  totalUnread: ReturnType<typeof computed<number>>;
  completedSeen: ReturnType<typeof computed<RemoteSession[]>>;
  primaryStateForHost(hostId: string): TaskState;
}

export function useSessions(
  localList: Ref<RemoteSession[]>,
  remoteList: Ref<RemoteSession[]>,
  options: UseSessionsOptions = {},
): UseSessionsReturn {
  const all = computed<RemoteSession[]>(() => {
    const byId = new Map<string, RemoteSession>();
    for (const s of localList.value) byId.set(s.session_id, s);
    for (const s of remoteList.value) byId.set(s.session_id, s); // relay wins
    return [...byId.values()];
  });

  const byHost = computed<Record<string, RemoteSession[]>>(() => {
    const out: Record<string, RemoteSession[]> = {};
    for (const s of all.value) {
      const k = s.host_id || "";
      (out[k] ||= []).push(s);
    }
    for (const k of Object.keys(out)) {
      out[k].sort((a, b) => {
        const au = a.unread ? 0 : 1;
        const bu = b.unread ? 0 : 1;
        if (au !== bu) return au - bu;
        const ai = urgencyIndex(a.task_state as TaskState | undefined);
        const bi = urgencyIndex(b.task_state as TaskState | undefined);
        if (ai !== bi) return ai - bi;
        return (b.last_output_at ?? 0) - (a.last_output_at ?? 0);
      });
    }
    return out;
  });

  const remoteByHost = computed<Record<string, RemoteSession[]>>(() => {
    if (!options.localHostId) return byHost.value;
    const out: Record<string, RemoteSession[]> = {};
    for (const k of Object.keys(byHost.value)) {
      if (k !== options.localHostId) out[k] = byHost.value[k];
    }
    return out;
  });

  const unreadByHost = computed<Record<string, number>>(() => {
    const out: Record<string, number> = {};
    for (const [k, list] of Object.entries(byHost.value)) {
      out[k] = list.filter((s) => s.unread === true).length;
    }
    return out;
  });

  const totalUnread = computed<number>(() =>
    Object.values(unreadByHost.value).reduce((a, b) => a + b, 0),
  );

  const completedSeen = computed<RemoteSession[]>(() =>
    all.value.filter(
      (s) =>
        (s.task_state === "completed" || s.task_state === "failed") &&
        s.unread === false,
    ),
  );

  function primaryStateForHost(hostId: string): TaskState {
    const list = byHost.value[hostId] ?? [];
    let best: TaskState = "idle";
    let bestIdx = urgencyIndex("idle");
    for (const s of list) {
      const st = (s.task_state as TaskState | undefined) ?? "idle";
      const idx = urgencyIndex(st);
      if (idx < bestIdx) {
        best = st;
        bestIdx = idx;
      }
    }
    return best;
  }

  return {
    all,
    byHost,
    remoteByHost,
    unreadByHost,
    totalUnread,
    completedSeen,
    primaryStateForHost,
  };
}
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/composables/useSessions.test.ts
```
Expected: PASS (6 cases).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/composables/useSessions.ts \
        desktop/frontend/src/composables/useSessions.test.ts
git commit -m "desktop/composable: useSessions (merge + derive byHost/unread/primary)"
```

---

## Task 7: `components/TaskStateIcon.vue`

**Files:**
- Create: `desktop/frontend/src/components/TaskStateIcon.vue`
- Test: `desktop/frontend/src/components/TaskStateIcon.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/TaskStateIcon.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskStateIcon from "./TaskStateIcon.vue";
import { presets } from "../lib/taskState";

describe("TaskStateIcon", () => {
  test("renders the glyph for a static state under vivid", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.vivid },
    });
    expect(w.text()).toContain("◐");
    expect(w.attributes("style")).toContain("color: #f59e0b");
  });
  test("renders an SVG spinner for running", () => {
    const w = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.vivid },
    });
    expect(w.find("svg.task-spinner").exists()).toBe(true);
    expect(w.find("svg.task-spinner").attributes("style")).toContain(
      "animation-duration: 1500ms",
    );
  });
  test("running spinner duration differs between presets", () => {
    const v = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.vivid },
    });
    const q = mount(TaskStateIcon, {
      props: { state: "running", preset: presets.quiet },
    });
    expect(v.find("svg.task-spinner").attributes("style")).toContain("1500ms");
    expect(q.find("svg.task-spinner").attributes("style")).toContain("2500ms");
  });
  test("waiting_input pulses in vivid, not in quiet", () => {
    const v = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.vivid },
    });
    const q = mount(TaskStateIcon, {
      props: { state: "waiting_input", preset: presets.quiet },
    });
    expect(v.classes()).toContain("pulse");
    expect(q.classes()).not.toContain("pulse");
  });
  test("vivid renders type icon when type is provided; quiet does not", () => {
    const v = mount(TaskStateIcon, {
      props: { state: "running", type: "ai", preset: presets.vivid },
    });
    const q = mount(TaskStateIcon, {
      props: { state: "running", type: "ai", preset: presets.quiet },
    });
    expect(v.find("svg.task-type").exists()).toBe(true);
    expect(q.find("svg.task-type").exists()).toBe(false);
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/TaskStateIcon.test.ts
```
Expected: FAIL — component not found.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/TaskStateIcon.vue`:

```vue
<script setup lang="ts">
import { computed } from "vue";
import { presets, type PresetId, type TaskState, type TaskStatePreset } from "../lib/taskState";
import { displayForType, type DisplayKey } from "../lib/sessionType";
import { useTaskPreset } from "../composables/useTaskPreset";

const props = withDefaults(
  defineProps<{
    state: TaskState;
    type?: DisplayKey;
    size?: number;
    preset?: TaskStatePreset;
  }>(),
  { size: 12 },
);

const fallback = useTaskPreset();
const preset = computed(() => props.preset ?? fallback.active.value);

const color = computed(() => preset.value.colorOf(props.state));
const glyph = computed(() => preset.value.glyphOf(props.state));
const spinMs = computed(() => preset.value.spinnerDurationMs(props.state));
const pulse = computed(() => preset.value.animatePulse(props.state));
const typeDisplay = computed(() =>
  preset.value.showTypeIcon && props.type ? displayForType(props.type) : null,
);
</script>

<template>
  <span
    class="task-state-icon"
    :class="{ pulse }"
    :style="{
      color,
      width: size + 'px',
      height: size + 'px',
      display: 'inline-flex',
      alignItems: 'center',
      gap: '2px',
    }"
  >
    <svg
      v-if="glyph === 'spinner'"
      class="task-spinner"
      :width="size"
      :height="size"
      viewBox="0 0 16 16"
      fill="none"
      :stroke="color"
      stroke-width="2"
      stroke-linecap="round"
      :style="{ animationDuration: spinMs + 'ms' }"
      aria-hidden="true"
    >
      <!-- 3/4 arc with a gap -->
      <path d="M14 8 a6 6 0 1 1 -3 -5.196" />
    </svg>
    <span v-else class="task-glyph" :style="{ fontSize: size + 'px', lineHeight: 1 }">
      {{ glyph }}
    </span>
    <svg
      v-if="typeDisplay"
      class="task-type"
      :width="size"
      :height="size"
      viewBox="0 0 16 16"
      fill="none"
      :stroke="typeDisplay.color"
      stroke-width="1.6"
      stroke-linecap="round"
      stroke-linejoin="round"
      aria-hidden="true"
    >
      <path :d="typeDisplay.iconPath" />
    </svg>
  </span>
</template>

<style scoped>
.task-state-icon.pulse {
  animation: task-pulse 1.2s ease-in-out infinite alternate;
}
.task-spinner {
  animation: task-spin 1.5s linear infinite;
}
@keyframes task-pulse {
  from {
    opacity: 0.5;
  }
  to {
    opacity: 1;
  }
}
@keyframes task-spin {
  to {
    transform: rotate(360deg);
  }
}
</style>
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/TaskStateIcon.test.ts
```
Expected: PASS (5 cases).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TaskStateIcon.vue \
        desktop/frontend/src/components/TaskStateIcon.test.ts
git commit -m "desktop/component: TaskStateIcon (state → glyph/color/animation)"
```

---

## Task 8: `components/TaskGroupedList.vue`

**Files:**
- Create: `desktop/frontend/src/components/TaskGroupedList.vue`
- Test: `desktop/frontend/src/components/TaskGroupedList.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/TaskGroupedList.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskGroupedList from "./TaskGroupedList.vue";
import type { RemoteSession } from "../platform/types";

function mk(over: Partial<RemoteSession>): RemoteSession {
  return {
    session_id: "s",
    host_id: "h",
    host: "host",
    user: "u",
    title: "",
    cols: 80,
    rows: 24,
    ...over,
  };
}

describe("TaskGroupedList", () => {
  test("renders a row per session under host header", () => {
    const byHost = {
      h: [
        mk({ session_id: "s1", host: "mac", task_state: "running", title: "claude" }),
        mk({ session_id: "s2", host: "mac", task_state: "waiting_input", title: "test" }),
      ],
    };
    const w = mount(TaskGroupedList, {
      props: {
        byHost,
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    expect(w.findAll('[data-test="task-row"]').length).toBe(2);
    expect(w.find('[data-test="host-header"]').text()).toContain("mac");
  });

  test("clicking a row emits open with the session", async () => {
    const sess = mk({ session_id: "s1", host: "mac", title: "claude" });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 0 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    await w.find('[data-test="task-row"]').trigger("click");
    expect(w.emitted("open")?.[0]?.[0]).toEqual(sess);
  });

  test("clicking row mark-read emits markSeen ids", async () => {
    const sess = mk({ session_id: "s1", host: "mac", unread: true, attention_at: 1 });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [sess] },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    await w.find('[data-test="row-mark-read"]').trigger("click");
    expect(w.emitted("markSeen")?.[0]?.[0]).toEqual({ ids: ["s1"] });
  });

  test("host header mark-all emits markSeen ids for that host's unread", async () => {
    const a = mk({ session_id: "a", host: "mac", unread: true, attention_at: 1 });
    const b = mk({ session_id: "b", host: "mac", unread: true, attention_at: 1 });
    const c = mk({ session_id: "c", host: "mac", unread: false });
    const w = mount(TaskGroupedList, {
      props: {
        byHost: { h: [a, b, c] },
        unreadByHost: { h: 2 },
        primaryStateForHost: () => "running",
        completedSeen: [],
      },
    });
    await w.find('[data-test="host-mark-all"]').trigger("click");
    expect(w.emitted("markSeen")?.[0]?.[0]).toEqual({
      ids: ["a", "b"],
    });
  });

  test("completed fold is collapsed by default and expands on click", async () => {
    const w = mount(TaskGroupedList, {
      props: {
        byHost: {},
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [mk({ session_id: "x", task_state: "completed" })],
      },
    });
    expect(w.find('[data-test="completed-fold-row"]').exists()).toBe(false);
    await w.find('[data-test="completed-fold-toggle"]').trigger("click");
    expect(w.find('[data-test="completed-fold-row"]').exists()).toBe(true);
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/TaskGroupedList.test.ts
```
Expected: FAIL — component not found.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/TaskGroupedList.vue`:

```vue
<script setup lang="ts">
import { computed, ref } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskStateIcon from "./TaskStateIcon.vue";
import { t } from "../i18n";

const props = defineProps<{
  byHost: Record<string, RemoteSession[]>;
  unreadByHost: Record<string, number>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
}>();

const emit = defineEmits<{
  (e: "open", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
}>();

const hostKeys = computed(() => Object.keys(props.byHost).sort());

const foldOpen = ref(false);

function hostName(hostId: string): string {
  const first = props.byHost[hostId]?.[0];
  return first?.host || hostId || t("sessions.unknownHost");
}

function unreadIdsFor(hostId: string): string[] {
  return (props.byHost[hostId] ?? []).filter((s) => s.unread).map((s) => s.session_id);
}

function onMarkRead(s: RemoteSession) {
  emit("markSeen", { ids: [s.session_id] });
}
function onMarkHost(hostId: string) {
  emit("markSeen", { ids: unreadIdsFor(hostId) });
}
function onMarkFold() {
  emit("markSeen", {
    ids: props.completedSeen.map((s) => s.session_id),
  });
}
</script>

<template>
  <div class="task-grouped-list">
    <section
      v-for="hostId in hostKeys"
      :key="hostId"
      class="host-group"
      :data-test="`host-group-${hostId}`"
    >
      <header class="host-header" data-test="host-header">
        <span class="caret">▼</span>
        <span class="host-name">{{ hostName(hostId) }}</span>
        <span class="counts">
          <TaskStateIcon :state="primaryStateForHost(hostId)" :size="10" />
          <span class="count">{{ byHost[hostId].length }}</span>
        </span>
        <span v-if="unreadByHost[hostId] > 0" class="unread-badge">
          {{ t("tasks.unreadBadge", { count: unreadByHost[hostId] }) }}
        </span>
        <button
          v-if="unreadByHost[hostId] > 0"
          class="mark-all"
          data-test="host-mark-all"
          :title="t('tasks.markAllRead')"
          @click="onMarkHost(hostId)"
        >
          ✓
        </button>
      </header>
      <button
        v-for="s in byHost[hostId]"
        :key="s.session_id"
        class="task-row"
        data-test="task-row"
        @click="emit('open', s)"
      >
        <TaskStateIcon
          :state="(s.task_state as TaskState | undefined) ?? 'idle'"
          :type="(s.type as any) ?? undefined"
        />
        <span class="cmd">{{ s.current_command || s.title || s.session_id.slice(0, 8) }}</span>
        <span v-if="s.unread" class="unread-dot" data-test="unread-dot">●</span>
        <span
          v-if="s.unread"
          class="row-mark-read"
          data-test="row-mark-read"
          :title="t('tasks.markRead')"
          @click.stop="onMarkRead(s)"
        >
          ✓
        </span>
      </button>
    </section>
    <section v-if="completedSeen.length > 0" class="completed-fold">
      <button
        class="fold-toggle"
        data-test="completed-fold-toggle"
        @click="foldOpen = !foldOpen"
      >
        {{ foldOpen ? "▼" : "▶" }} {{ t("tasks.completedFold") }} · {{ completedSeen.length }}
      </button>
      <template v-if="foldOpen">
        <div
          v-for="s in completedSeen"
          :key="s.session_id"
          class="task-row dim"
          data-test="completed-fold-row"
          @click="emit('open', s)"
        >
          <TaskStateIcon :state="(s.task_state as TaskState | undefined) ?? 'idle'" />
          <span class="cmd">{{ s.current_command || s.title || s.session_id.slice(0, 8) }}</span>
        </div>
        <button class="fold-mark-all" @click="onMarkFold">
          {{ t("tasks.markAllRead") }}
        </button>
      </template>
    </section>
  </div>
</template>

<style scoped>
.task-grouped-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
  font-size: 12px;
}
.host-header {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 500;
  padding: 4px 6px;
}
.host-name { flex: 0 0 auto; }
.counts { margin-left: auto; display: inline-flex; gap: 2px; align-items: center; }
.unread-badge {
  font-size: 10px;
  opacity: 0.8;
  background: rgba(255, 255, 255, 0.06);
  border-radius: 3px;
  padding: 1px 4px;
}
.mark-all { background: none; border: none; cursor: pointer; padding: 0 4px; color: inherit; }
.task-row {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 3px 8px;
  border: none;
  background: none;
  width: 100%;
  text-align: left;
  cursor: pointer;
  color: inherit;
  border-radius: 3px;
}
.task-row:hover { background: rgba(255, 255, 255, 0.05); }
.task-row.dim { opacity: 0.6; }
.cmd { flex: 1 1 auto; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); }
.unread-dot { font-size: 9px; color: currentColor; }
.row-mark-read { font-size: 11px; padding: 0 4px; cursor: pointer; }
.completed-fold { border-top: 1px solid rgba(255, 255, 255, 0.06); margin-top: 6px; padding-top: 4px; }
.fold-toggle { background: none; border: none; cursor: pointer; padding: 4px 6px; width: 100%; text-align: left; color: inherit; opacity: 0.7; }
.fold-mark-all { background: none; border: 1px solid rgba(255, 255, 255, 0.12); cursor: pointer; padding: 4px 8px; margin: 4px 6px; color: inherit; border-radius: 3px; }
</style>
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/TaskGroupedList.test.ts
```
Expected: PASS (5 cases).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TaskGroupedList.vue \
        desktop/frontend/src/components/TaskGroupedList.test.ts
git commit -m "desktop/component: TaskGroupedList (shared host groups + fold)"
```

---

## Task 9: `components/TaskSidebar.vue`

**Files:**
- Create: `desktop/frontend/src/components/TaskSidebar.vue`
- Test: `desktop/frontend/src/components/TaskSidebar.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/TaskSidebar.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TaskSidebar from "./TaskSidebar.vue";
import type { RemoteSession } from "../platform/types";

function mk(over: Partial<RemoteSession>): RemoteSession {
  return {
    session_id: "s",
    host_id: "h",
    host: "mac",
    user: "u",
    title: "",
    cols: 80,
    rows: 24,
    ...over,
  };
}

describe("TaskSidebar", () => {
  test("expanded shows TaskGroupedList and Mark-all-read button when unread > 0", () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: { h: [mk({ unread: true, attention_at: 1 })] },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "running",
        completedSeen: [],
        totalUnread: 1,
      },
    });
    expect(w.find('[data-test="task-grouped-list"]').exists()).toBe(true);
    expect(w.find('[data-test="sidebar-mark-all"]').exists()).toBe(true);
  });

  test("collapsed shows narrow rail with icons and total-unread badge", () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: true,
        byHost: {
          h: [
            mk({ session_id: "s1", task_state: "waiting_input", attention_at: 1, unread: true }),
            mk({ session_id: "s2", task_state: "running" }),
          ],
        },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "waiting_input",
        completedSeen: [],
        totalUnread: 1,
      },
    });
    expect(w.find('[data-test="sidebar-rail"]').exists()).toBe(true);
    expect(w.find('[data-test="sidebar-rail-badge"]').text()).toBe("1");
    expect(w.findAll('[data-test="sidebar-rail-icon"]').length).toBe(2);
  });

  test("collapse button emits update:collapsed=true", async () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {},
        unreadByHost: {},
        primaryStateForHost: () => "idle",
        completedSeen: [],
        totalUnread: 0,
      },
    });
    await w.find('[data-test="collapse-button"]').trigger("click");
    expect(w.emitted("update:collapsed")?.[0]).toEqual([true]);
  });

  test("Mark-all-read button emits markSeen all", async () => {
    const w = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: { h: [mk({ unread: true, attention_at: 1 })] },
        unreadByHost: { h: 1 },
        primaryStateForHost: () => "running",
        completedSeen: [],
        totalUnread: 1,
      },
    });
    await w.find('[data-test="sidebar-mark-all"]').trigger("click");
    expect(w.emitted("markSeen")?.[0]?.[0]).toEqual({ all: true });
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/TaskSidebar.test.ts
```
Expected: FAIL — component not found.

- [ ] **Step 3: Implement the component**

Create `desktop/frontend/src/components/TaskSidebar.vue`:

```vue
<script setup lang="ts">
import { computed } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
import TaskGroupedList from "./TaskGroupedList.vue";
import TaskStateIcon from "./TaskStateIcon.vue";
import { t } from "../i18n";

const props = defineProps<{
  collapsed: boolean;
  byHost: Record<string, RemoteSession[]>;
  unreadByHost: Record<string, number>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  totalUnread: number;
}>();

const emit = defineEmits<{
  (e: "update:collapsed", v: boolean): void;
  (e: "open", session: RemoteSession): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
}>();

// Collapsed rail: urgency-sorted flat icon list.
const URGENCY: TaskState[] = [
  "waiting_input",
  "failed",
  "running",
  "completed",
  "idle",
  "disconnected",
  "closed",
];
const railIcons = computed(() => {
  const all: RemoteSession[] = [];
  for (const list of Object.values(props.byHost)) all.push(...list);
  all.sort((a, b) => {
    const ai = URGENCY.indexOf((a.task_state as TaskState) ?? "idle");
    const bi = URGENCY.indexOf((b.task_state as TaskState) ?? "idle");
    if (ai !== bi) return (ai === -1 ? 99 : ai) - (bi === -1 ? 99 : bi);
    return 0;
  });
  return all.slice(0, 20);
});
</script>

<template>
  <aside class="task-sidebar" :class="{ collapsed }">
    <div v-if="collapsed" class="rail" data-test="sidebar-rail">
      <button
        class="expand-button"
        :title="t('tasks.sidebar.expand')"
        @click="emit('update:collapsed', false)"
      >
        »
      </button>
      <span v-if="totalUnread > 0" class="rail-badge" data-test="sidebar-rail-badge">
        {{ totalUnread }}
      </span>
      <span
        v-for="(s, i) in railIcons"
        :key="s.session_id"
        class="rail-icon"
        data-test="sidebar-rail-icon"
        @click="emit('open', s)"
      >
        <TaskStateIcon
          :state="(s.task_state as TaskState | undefined) ?? 'idle'"
          :size="14"
        />
      </span>
    </div>
    <div v-else class="expanded">
      <header class="sidebar-header">
        <span class="title">{{ t("tasks.sidebar.title") }}</span>
        <button
          class="collapse-button"
          data-test="collapse-button"
          :title="t('tasks.sidebar.collapse')"
          @click="emit('update:collapsed', true)"
        >
          «
        </button>
      </header>
      <div class="list-wrap" data-test="task-grouped-list">
        <TaskGroupedList
          :by-host="byHost"
          :unread-by-host="unreadByHost"
          :primary-state-for-host="primaryStateForHost"
          :completed-seen="completedSeen"
          @open="emit('open', $event)"
          @markSeen="emit('markSeen', $event)"
        />
      </div>
      <footer v-if="totalUnread > 0">
        <button
          class="mark-all"
          data-test="sidebar-mark-all"
          @click="emit('markSeen', { all: true })"
        >
          {{ t("tasks.markAllRead") }}
        </button>
      </footer>
    </div>
  </aside>
</template>

<style scoped>
.task-sidebar { background: var(--bg-elev, #0e1116); border-right: 1px solid rgba(255,255,255,0.06); display: flex; flex-direction: column; height: 100%; }
.task-sidebar.collapsed { width: 32px; }
.task-sidebar:not(.collapsed) { width: 240px; }
.sidebar-header { display: flex; align-items: center; padding: 8px 10px; border-bottom: 1px solid rgba(255,255,255,0.05); }
.title { flex: 1; font-weight: 500; }
.collapse-button, .expand-button { background: none; border: none; cursor: pointer; color: inherit; font-size: 14px; }
.list-wrap { flex: 1 1 auto; overflow: auto; padding: 4px; }
footer { padding: 8px; border-top: 1px solid rgba(255,255,255,0.05); }
.mark-all { background: none; border: 1px solid rgba(255,255,255,0.12); cursor: pointer; padding: 6px 10px; color: inherit; border-radius: 3px; width: 100%; }
.rail { display: flex; flex-direction: column; align-items: center; padding: 6px 0; gap: 4px; }
.rail-badge { font-size: 10px; background: #ef4444; color: white; border-radius: 8px; padding: 1px 5px; }
.rail-icon { cursor: pointer; padding: 2px; }
</style>
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/TaskSidebar.test.ts
```
Expected: PASS (4 cases).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TaskSidebar.vue \
        desktop/frontend/src/components/TaskSidebar.test.ts
git commit -m "desktop/component: TaskSidebar (expand/collapse + rail)"
```

---

## Task 10: `components/SettingsTasks.vue` + register in `SettingsDialog`

**Files:**
- Create: `desktop/frontend/src/components/SettingsTasks.vue`
- Test: `desktop/frontend/src/components/SettingsTasks.test.ts`
- Modify: `desktop/frontend/src/components/SettingsDialog.vue`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/SettingsTasks.test.ts`:

```ts
import { afterEach, beforeEach, describe, expect, test, vi } from "vitest";
import { mount } from "@vue/test-utils";
import { nextTick } from "vue";
import SettingsTasks from "./SettingsTasks.vue";
import * as api from "../lib/api";
import { __resetForTests as resetPreset } from "../composables/useTaskPreset";

beforeEach(() => {
  resetPreset();
  vi.restoreAllMocks();
});
afterEach(() => resetPreset());

describe("SettingsTasks", () => {
  test("loads current preset and renders two radio options", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("vivid");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const w = mount(SettingsTasks);
    await nextTick();
    await nextTick();
    expect(w.findAll('input[type="radio"][name="preset"]').length).toBe(2);
  });

  test("selecting Quiet calls setTaskPreset", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("vivid");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const set = vi.spyOn(api, "setTaskPreset").mockResolvedValue(undefined);
    const w = mount(SettingsTasks);
    await nextTick();
    await nextTick();
    await w.find('input[value="quiet"]').setValue(true);
    expect(set).toHaveBeenCalledWith("quiet");
  });

  test("toggling 'expand by default' calls setTaskSidebarCollapsed", async () => {
    vi.spyOn(api, "getTaskPreset").mockResolvedValue("vivid");
    vi.spyOn(api, "getTaskSidebarCollapsed").mockResolvedValue(false);
    const set = vi.spyOn(api, "setTaskSidebarCollapsed").mockResolvedValue(undefined);
    const w = mount(SettingsTasks);
    await nextTick();
    await nextTick();
    await w.find('input[type="checkbox"]').setValue(false); // uncheck "expand by default" → collapsed=true
    expect(set).toHaveBeenCalledWith(true);
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/SettingsTasks.test.ts
```
Expected: FAIL — component not found.

- [ ] **Step 3: Implement `SettingsTasks.vue`**

Create `desktop/frontend/src/components/SettingsTasks.vue`:

```vue
<script setup lang="ts">
import { onMounted, ref } from "vue";
import { presets, type PresetId } from "../lib/taskState";
import {
  getTaskPreset,
  setTaskPreset,
  getTaskSidebarCollapsed,
  setTaskSidebarCollapsed,
} from "../lib/api";
import { useTaskPreset } from "../composables/useTaskPreset";
import TaskStateIcon from "./TaskStateIcon.vue";
import { t } from "../i18n";

const expandByDefault = ref(true);
const presetIds: PresetId[] = ["vivid", "quiet"];
const preset = useTaskPreset();

onMounted(async () => {
  try {
    const v = await getTaskPreset();
    if (v === "vivid" || v === "quiet") preset.activeId.value = v;
  } catch {/* fallback already applied */}
  try {
    const c = await getTaskSidebarCollapsed();
    expandByDefault.value = !c;
  } catch {/* default */}
});

async function onPresetChange(e: Event) {
  const id = (e.target as HTMLInputElement).value as PresetId;
  await preset.setPreset(id);
}
async function onToggleExpand(e: Event) {
  const checked = (e.target as HTMLInputElement).checked;
  expandByDefault.value = checked;
  await setTaskSidebarCollapsed(!checked);
}
</script>

<template>
  <section class="settings-tasks">
    <h3 class="section-title">{{ t("tasks.settings.section") }}</h3>
    <div class="preset-list">
      <label
        v-for="id in presetIds"
        :key="id"
        class="preset-option"
      >
        <input
          type="radio"
          name="preset"
          :value="id"
          :checked="preset.activeId.value === id"
          @change="onPresetChange"
        />
        <div class="preset-meta">
          <div class="preset-name">{{ t(`tasks.preset.${id}.name`) }}</div>
          <div class="preset-desc">{{ t(`tasks.preset.${id}.description`) }}</div>
          <div class="preset-preview">
            <TaskStateIcon state="running" :preset="presets[id]" />
            <TaskStateIcon state="waiting_input" :preset="presets[id]" />
            <TaskStateIcon state="completed" :preset="presets[id]" />
            <TaskStateIcon state="failed" :preset="presets[id]" />
          </div>
        </div>
      </label>
    </div>
    <label class="expand-toggle">
      <input
        type="checkbox"
        :checked="expandByDefault"
        @change="onToggleExpand"
      />
      {{ t("tasks.settings.expandByDefault") }}
    </label>
  </section>
</template>

<style scoped>
.section-title { font-size: 14px; font-weight: 500; margin: 0 0 12px; }
.preset-list { display: flex; flex-direction: column; gap: 12px; margin-bottom: 16px; }
.preset-option { display: flex; gap: 10px; padding: 10px; border: 1px solid rgba(255,255,255,0.08); border-radius: 4px; cursor: pointer; }
.preset-option:hover { background: rgba(255,255,255,0.03); }
.preset-name { font-weight: 500; }
.preset-desc { font-size: 12px; opacity: 0.7; margin: 4px 0; }
.preset-preview { display: flex; gap: 10px; margin-top: 6px; }
.expand-toggle { display: flex; align-items: center; gap: 8px; padding: 6px 0; }
</style>
```

- [ ] **Step 4: Register in `SettingsDialog.vue`**

In `desktop/frontend/src/components/SettingsDialog.vue`:

Add at top of `<script setup>`:

```ts
import SettingsTasks from "./SettingsTasks.vue";
```

In the tab nav block (where `general` / `relay` buttons live, near lines 166-208), add another button:

```vue
<button
  class="settings-nav-item"
  :class="{ active: activeTab === 'tasks' }"
  @click="switchTab('tasks')"
>{{ t("tasks.settings.section") }}</button>
```

In the tab content area (where `<SettingsGeneral v-show...>` and `<SettingsRelay v-show...>` are), add:

```vue
<SettingsTasks v-show="activeTab === 'tasks'" />
```

If `switchTab` / `activeTab` use a union string type that lists current tabs, extend it to include `'tasks'`. The exact identifier name is in the component — match it.

- [ ] **Step 5: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/SettingsTasks.test.ts
```
Expected: PASS (3 cases).

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/SettingsTasks.vue \
        desktop/frontend/src/components/SettingsTasks.test.ts \
        desktop/frontend/src/components/SettingsDialog.vue
git commit -m "desktop/component: SettingsTasks tab (preset + expand-by-default)"
```

---

## Task 11: `components/TabBar.vue` — state icon + unread dot

**Files:**
- Modify: `desktop/frontend/src/components/TabBar.vue`
- Modify: `desktop/frontend/src/components/TabBar.test.ts` (or create if absent — check first)

- [ ] **Step 1: Add the failing test**

In an existing `TabBar.test.ts` (or new file `desktop/frontend/src/components/TabBar.test.ts`) add:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";
import TabBar from "./TabBar.vue";

const baseTab = {
  id: "t1",
  layout: "single" as const,
  activeRemote: false,
  paneCount: 1,
  disconnected: false,
};

describe("TabBar state icon + unread", () => {
  test("uses TaskStateIcon for the active session's task_state", () => {
    const tab = {
      ...baseTab,
      activeSession: {
        session_id: "s1",
        host_id: "h",
        host: "mac",
        user: "u",
        title: "claude",
        cols: 80,
        rows: 24,
        task_state: "waiting_input",
      },
    };
    const w = mount(TabBar, {
      props: { tabs: [tab], currentId: "t1" },
    });
    expect(w.find(".task-state-icon").exists()).toBe(true);
    expect(w.find(".task-state-icon").text()).toContain("◐");
  });

  test("renders unread dot when activeSession.unread is true", () => {
    const tab = {
      ...baseTab,
      activeSession: {
        session_id: "s1",
        host_id: "h",
        host: "mac",
        user: "u",
        title: "claude",
        cols: 80,
        rows: 24,
        task_state: "completed",
        unread: true,
      },
    };
    const w = mount(TabBar, {
      props: { tabs: [tab], currentId: "t1" },
    });
    expect(w.find('[data-test="tab-unread-dot"]').exists()).toBe(true);
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts
```
Expected: FAIL — `.task-state-icon` not found / unread dot not rendered.

- [ ] **Step 3: Modify TabBar.vue**

In `desktop/frontend/src/components/TabBar.vue`:

Add to `<script setup>` imports:

```ts
import TaskStateIcon from "./TaskStateIcon.vue";
import type { TaskState } from "../lib/taskState";
```

In the template, replace the dot block (lines 83-85):

```vue
<span v-if="t.layout !== 'single'" class="layout-icon" :title="layoutTitle(t)">{{ layoutLabel(t) }}</span>
<span v-else-if="t.activeRemote" class="dot remote-dot" :class="{ disconnected: t.disconnected }">●</span>
<span v-else class="dot">●</span>
```

with:

```vue
<span v-if="t.layout !== 'single'" class="layout-icon" :title="layoutTitle(t)">{{ layoutLabel(t) }}</span>
<span
  v-else-if="t.activeRemote"
  class="dot remote-dot"
  :class="{ disconnected: t.disconnected }"
  :title="t.activeRemote ? 'remote' : ''"
>
  <TaskStateIcon
    :state="(t.activeSession?.task_state as TaskState | undefined) ?? 'idle'"
    :size="10"
  />
</span>
<TaskStateIcon
  v-else
  :state="(t.activeSession?.task_state as TaskState | undefined) ?? 'idle'"
  :size="10"
/>
```

After the title span (line 91), add the unread dot:

```vue
<span class="title">{{ shortTitle(t.activeSession) }}</span>
<span
  v-if="t.activeSession?.unread"
  class="tab-unread-dot"
  data-test="tab-unread-dot"
>●</span>
```

Add CSS in the `<style>` block:

```css
.tab .tab-unread-dot { font-size: 8px; margin-left: 4px; color: currentColor; }
```

The existing `.dot.remote-dot` styling can stay; the orange ring around the icon comes from coloring the wrapper span.

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TabBar.vue \
        desktop/frontend/src/components/TabBar.test.ts
git commit -m "desktop/TabBar: state icon + unread dot"
```

---

## Task 12: `components/RemoteSessionsDialog.vue` — use TaskGroupedList

**Files:**
- Modify: `desktop/frontend/src/components/RemoteSessionsDialog.vue`
- Modify: `desktop/frontend/src/components/RemoteSessionsDialog.test.ts` (existing — keep its assertions valid)

- [ ] **Step 1: Add a new failing test**

Append to `desktop/frontend/src/components/RemoteSessionsDialog.test.ts`:

```ts
import type { RemoteSession } from "../platform/types";

test("renders via TaskGroupedList and emits markSeen all on the top button", async () => {
  const sessions: RemoteSession[] = [
    {
      session_id: "s1",
      host_id: "h",
      host: "mac",
      user: "u",
      title: "claude",
      cols: 80,
      rows: 24,
      task_state: "running",
      unread: true,
      attention_at: 1,
    },
  ];
  const w = mount(RemoteSessionsDialog, {
    props: { sessions, open: true },
  });
  expect(w.find('[data-test="task-row"]').exists()).toBe(true);
  await w.find('[data-test="dialog-mark-all"]').trigger("click");
  expect(w.emitted("markSeen")?.[0]?.[0]).toEqual({ all: true });
});

test("clicking a row still emits open with the session id", async () => {
  const session: RemoteSession = {
    session_id: "s1",
    host_id: "h",
    host: "mac",
    user: "u",
    title: "claude",
    cols: 80,
    rows: 24,
  };
  const w = mount(RemoteSessionsDialog, {
    props: { sessions: [session], open: true },
  });
  await w.find('[data-test="task-row"]').trigger("click");
  // Existing dialog contract is to emit "open" with the session ID.
  expect(w.emitted("open")?.[0]?.[0]).toBe("s1");
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/RemoteSessionsDialog.test.ts
```
Expected: FAIL — new selectors absent; markSeen never emitted.

- [ ] **Step 3: Refactor the dialog**

In `desktop/frontend/src/components/RemoteSessionsDialog.vue`:

Add imports:

```ts
import TaskGroupedList from "./TaskGroupedList.vue";
import { useSessions } from "../composables/useSessions";
import { ref, computed } from "vue";
import type { RemoteSession } from "../platform/types";
import type { TaskState } from "../lib/taskState";
```

Replace the existing groups/template logic. New `<script setup>` body (keep `props.sessions` shape; emit shape extends with `markSeen`):

```ts
const props = defineProps<{ sessions: RemoteSession[]; open: boolean }>();
const emit = defineEmits<{
  (e: "open", sessionId: string): void;
  (e: "markSeen", payload: { ids: string[] } | { all: true }): void;
  (e: "close"): void;
}>();

// Adapter: useSessions expects two refs (local + remote). For the dialog, all
// passed-in sessions are treated as "remote".
const localList = ref<RemoteSession[]>([]);
const remoteList = computed(() => props.sessions);
const { byHost, unreadByHost, primaryStateForHost, completedSeen, totalUnread } =
  useSessions(localList, remoteList as any);
```

Replace the template body inside the modal:

```vue
<header class="dialog-head">
  <h2>{{ t("sessions.remoteTitle") }}</h2>
  <button
    v-if="totalUnread > 0"
    class="mark-all"
    data-test="dialog-mark-all"
    @click="emit('markSeen', { all: true })"
  >
    {{ t("tasks.markAllRead") }}
  </button>
</header>
<TaskGroupedList
  :by-host="byHost"
  :unread-by-host="unreadByHost"
  :primary-state-for-host="primaryStateForHost"
  :completed-seen="completedSeen"
  @open="(s) => emit('open', s.session_id)"
  @markSeen="(p) => emit('markSeen', p)"
/>
```

Remove the local `groupSessionsByHost` import and template loop. Keep the modal backdrop + close button as-is.

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/RemoteSessionsDialog.test.ts
```
Expected: PASS (existing tests + the two new ones).

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/RemoteSessionsDialog.vue \
        desktop/frontend/src/components/RemoteSessionsDialog.test.ts
git commit -m "desktop/RemoteSessionsDialog: render via TaskGroupedList + mark-all-read"
```

---

## Task 13: `App.vue` integration (mount sidebar, useSessions, hotkey, markSeen flow)

**Files:**
- Modify: `desktop/frontend/src/lib/shortcutBindings.ts`
- Modify: `desktop/frontend/src/composables/useTerminalShortcuts.ts` (handler arg shape)
- Modify: `desktop/frontend/src/App.vue`

> No new test file for App.vue itself (it's the top-level glue). Existing tests must still pass after this task; any sidebar/dialog/markSeen behavior is covered by the per-component tests already.

- [ ] **Step 1: Register the hotkey action**

In `desktop/frontend/src/lib/shortcutBindings.ts`, find the `ACTIONS` registry (lines 21-34) and add:

```ts
{
  id: "toggleTaskSidebar",
  defaultBinding: "Mod+B", // Mod = Cmd on macOS, Ctrl elsewhere
  describe: () => "Toggle task sidebar",
},
```

(Match the existing shape — same fields the other 12 entries have. If it uses a different schema, mirror that exactly.)

- [ ] **Step 2: Add the handler slot**

In `desktop/frontend/src/composables/useTerminalShortcuts.ts`, extend the handlers parameter type to include `onToggleTaskSidebar: () => void`. In the dispatch table (where keys map to handler calls), add the `toggleTaskSidebar` branch:

```ts
if (action.id === "toggleTaskSidebar") {
  handlers.onToggleTaskSidebar?.();
  return;
}
```

(Or whatever idiom the existing dispatch uses — match it.)

- [ ] **Step 3: Wire `useSessions` + sidebar in `App.vue`**

In `desktop/frontend/src/App.vue` `<script setup>`:

Add imports:

```ts
import TaskSidebar from "./components/TaskSidebar.vue";
import { useSessions } from "./composables/useSessions";
import {
  markSessionsSeen,
  getTaskSidebarCollapsed,
  setTaskSidebarCollapsed,
} from "./lib/api";
import { ref, onMounted } from "vue";
```

After the existing `localList` / `remoteList` refs (lines 85-87), add:

```ts
const localHostId = ref<string>(""); // populated by your existing local-host detect logic; leave "" if unknown
const sessions = useSessions(localList, remoteList, {
  // Pass localHostId reactively if your code knows it; otherwise leave undefined.
  localHostId: localHostId.value || undefined,
});

const sidebarCollapsed = ref<boolean>(false);
onMounted(async () => {
  try {
    sidebarCollapsed.value = await getTaskSidebarCollapsed();
  } catch {/* default expanded */}
});

async function setSidebarCollapsed(v: boolean) {
  sidebarCollapsed.value = v;
  try {
    await setTaskSidebarCollapsed(v);
  } catch {/* persistence best-effort */}
}

function onSidebarOpen(s: RemoteSession) {
  // Reuse the same handler RemoteSessionsDialog already uses to open a remote
  // session as a tab (search the file for openRemoteSession or equivalent).
  openRemoteSession(s.session_id);
}

async function onMarkSeen(payload: { ids: string[] } | { all: true }) {
  try {
    await markSessionsSeen(payload);
  } catch {
    // Surface via existing toast/notification surface used elsewhere in App.vue.
    // If a `showToast(...)` helper exists, call it here with the i18n key
    // tasks.unavailableToast. Otherwise console.warn for now.
    console.warn("markSessionsSeen failed", payload);
  }
}
```

Then in the existing `useTerminalShortcuts({ ... handlers })` call (lines 699-709), add:

```ts
onToggleTaskSidebar: () => setSidebarCollapsed(!sidebarCollapsed.value),
```

In the `<template>`, find the layout that contains `<TabBar>` + main pane area. Wrap it (or position the sidebar as a flex sibling on the left):

```vue
<div class="app-layout">
  <TaskSidebar
    :collapsed="sidebarCollapsed"
    :by-host="sessions.byHost.value"
    :unread-by-host="sessions.unreadByHost.value"
    :primary-state-for-host="sessions.primaryStateForHost"
    :completed-seen="sessions.completedSeen.value"
    :total-unread="sessions.totalUnread.value"
    @update:collapsed="setSidebarCollapsed"
    @open="onSidebarOpen"
    @markSeen="onMarkSeen"
  />
  <div class="app-main">
    <!-- existing TabBar + PaneGrid markup unchanged -->
  </div>
</div>
```

Add CSS:

```css
.app-layout { display: flex; flex: 1; min-height: 0; }
.app-main { flex: 1; display: flex; flex-direction: column; min-width: 0; }
```

Also: the `RemoteSessionsDialog` invocation needs a `@markSeen="onMarkSeen"` event handler addition. Find where `<RemoteSessionsDialog ...>` is mounted in App.vue and add the listener.

- [ ] **Step 4: Run the full test suite**

```bash
cd desktop/frontend && npx vitest run
```
Expected: all existing tests + all new tests pass.

- [ ] **Step 5: Manual smoke test (if Wails build is available)**

```bash
cd desktop && wails dev
```
Verify:
- Sidebar appears on the left, expanded by default.
- `Cmd/Ctrl+B` toggles it; preference persists across restarts.
- A waiting-input session shows orange pulsing `◐`; running shows cyan spinner; completed/failed show their glyphs.
- Settings → Task display → switch Quiet → all icons re-render instantly in muted colors.
- Mark-read controls (row `✓`, host `✓`, sidebar bottom button, dialog top button) clear the unread dots; refresh confirms server-side state via the next session list push.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/lib/shortcutBindings.ts \
        desktop/frontend/src/composables/useTerminalShortcuts.ts \
        desktop/frontend/src/App.vue
git commit -m "desktop/App: mount TaskSidebar + useSessions + Cmd/Ctrl+B hotkey"
```

---

## Final verification

- [ ] **Run the full desktop frontend suite**

```bash
cd desktop/frontend && npx vitest run
```
Expected: PASS.

- [ ] **Type check**

```bash
cd desktop/frontend && npx vue-tsc --noEmit
```
Expected: no errors.

- [ ] **Build**

```bash
cd desktop && wails build  # or `npm --workspace=desktop/frontend run build`
```
Expected: success.

---

## Spec coverage check

| Spec section | Task |
| --- | --- |
| §1 (goal: sidebar, tab icon, dialog upgrade, presets) | Tasks 7-13 |
| §2 (architecture, file layout) | Reflected in plan file structure section |
| §3.preset shape | Task 3 |
| §3.preset table | Task 3 |
| §3.running spinner / waiting pulse / unread dot / type icon | Tasks 7, 8, 11 |
| §3.CSS custom property scope | Task 5 (`useTaskPreset` writes dataset) |
| §4.1 TaskStateIcon | Task 7 |
| §4.2 TaskGroupedList | Task 8 |
| §4.3 TaskSidebar (expand/collapse/rail/Cmd-B) | Tasks 9, 13 |
| §4.4 TabBar changes | Task 11 |
| §4.5 RemoteSessionsDialog changes | Task 12 |
| §4.6 SettingsDialog new section | Task 10 |
| §4.7 App.vue changes | Task 13 |
| §5 useSessions composable | Task 6 |
| §6 mark-read flow | Tasks 4 (binding), 8/9/10/12 (UI), 13 (App-level handler) |
| §7 settings persistence | Tasks 4 (Wails binding), 5 (load/save flow), 10 (UI) |
| §8 testing | Each task has its own vitest tests |
| §9 migration / older-relay degrade | Task 13 (try/catch + toast) |

No spec sections lack a corresponding task.

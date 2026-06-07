# Desktop Task Sidebar UX Trio Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Three sidebar polishes — drop the AI type icon everywhere, display each session's cwd inline in the task row with smart truncation, and let the user drag-resize the sidebar with a persisted width.

**Architecture:** Pure desktop frontend + a thin Go/Wails binding pair for sidebar width. The type icon removal is subtractive across `TaskStateIcon.vue`, `TabBar.vue`, `TaskStatePreset`, and tests. The cwd display reuses the existing `TaskGroupedList` row, fed by a new pure helper `shortenCwd()` and a new Wails `GetUserHomeDir()` binding. The drag-resize mirrors the existing `Get/SetTaskSidebarCollapsed` pattern with bounds clamping in both the Go setter and the frontend handler.

**Tech Stack:** Vue 3 `<script setup>`, vitest + `@vue/test-utils`, Wails v2 Go↔TS bindings, `time.Duration`/`time.Timer` patterns (none needed here, but for symmetry with the silence work). No protocol or migration changes.

**Spec:** `docs/superpowers/specs/2026-06-07-desktop-task-sidebar-ux-trio-design.md`.

---

## File structure

- `desktop/frontend/src/lib/taskState.ts` (modify): drop `showTypeIcon` field
- `desktop/frontend/src/components/TaskStateIcon.vue` (modify): drop type SVG + prop
- `desktop/frontend/src/components/TaskStateIcon.test.ts` (modify): drop type-icon test
- `desktop/frontend/src/components/TabBar.vue` (modify): drop type-icon SVG block + helper
- `desktop/frontend/src/components/TabBar.test.ts` (modify): drop type-icon assertions
- `desktop/frontend/src/components/TaskGroupedList.vue` (modify): row markup + cwd; drop `:type`
- `desktop/frontend/src/components/TaskGroupedList.test.ts` (modify): assert cwd renders
- `desktop/frontend/src/components/TaskSidebar.vue` (modify): drag handle + width state; drop `:type`
- `desktop/frontend/src/components/TaskSidebar.test.ts` (modify): assert drag emits / persists
- `desktop/frontend/src/lib/shortenCwd.ts` (new): pure helper
- `desktop/frontend/src/lib/shortenCwd.test.ts` (new): table-driven tests
- `desktop/frontend/src/lib/api.ts` (modify): 3 new wrappers + `AppBindings` entries
- `desktop/app.go` (modify): `GetUserHomeDir`, `GetTaskSidebarWidth`, `SetTaskSidebarWidth`
- `desktop/config.go` (modify): `TaskSidebarWidth` field + default helper
- `desktop/frontend/wailsjs/go/main/App.d.ts` + `App.js` (modify): 3 hand-added stubs

---

## Task 1: Drop AI type icon across TaskStateIcon, TabBar, and presets

Purely subtractive. The `Type` field on `SessionInfo` stays — only the visual rendering of its icon disappears.

**Files:**
- Modify: `desktop/frontend/src/lib/taskState.ts` (`TaskStatePreset` interface; `makePreset`; both preset literals)
- Modify: `desktop/frontend/src/components/TaskStateIcon.vue`
- Modify: `desktop/frontend/src/components/TaskStateIcon.test.ts`
- Modify: `desktop/frontend/src/components/TabBar.vue`
- Modify: `desktop/frontend/src/components/TabBar.test.ts`
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue` (drop `:type` prop)
- Modify: `desktop/frontend/src/components/TaskSidebar.vue` (drop `:type` prop)

- [ ] **Step 1: Update the failing tests first (red)**

In `desktop/frontend/src/components/TabBar.test.ts`, REMOVE the existing assertions that the type-icon SVG renders for AI tabs and not for shell tabs (the lines around 109-112: `expect(aiTab.find('.type-icon').exists()).toBe(true)` and `expect(shellTab.find('.type-icon').exists()).toBe(false)`). Then, near the same block, ADD:

```ts
test("type icon is no longer rendered for any session type", () => {
  const aiTab = {
    ...baseTab,
    activeSession: {
      session_id: "s1", host_id: "h", host: "mac", user: "u", title: "claude",
      cols: 80, rows: 24, task_state: "running", type: "ai",
    },
  };
  const w = mount(TabBar, { props: { tabs: [aiTab], currentId: "t1" } });
  expect(w.find(".type-icon").exists()).toBe(false);
});
```

In `desktop/frontend/src/components/TaskStateIcon.test.ts`, REMOVE the test `"vivid renders type icon when type is provided; quiet does not"` (around line 43). REPLACE with:

```ts
test("neither preset renders a type icon when type is provided", () => {
  const v = mount(TaskStateIcon, {
    props: { state: "running", type: "ai", preset: presets.vivid },
  });
  const q = mount(TaskStateIcon, {
    props: { state: "running", type: "ai", preset: presets.quiet },
  });
  expect(v.find("svg.task-type").exists()).toBe(false);
  expect(q.find("svg.task-type").exists()).toBe(false);
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts src/components/TaskStateIcon.test.ts
```
Expected: FAIL — the new assertions fail because type icons still render.

- [ ] **Step 3: Drop `showTypeIcon` from the preset**

In `desktop/frontend/src/lib/taskState.ts`, delete the `showTypeIcon: boolean;` field from the `TaskStatePreset` interface (~line 29). Update `makePreset` signature and body to drop the `showTypeIcon` argument and the `showTypeIcon,` field of the returned literal. Update the two `presets.vivid` / `presets.quiet` calls to omit the `true`/`false` argument that was being passed for it.

The full reworked file should look like (drop only this one field, leave the rest):

```ts
export interface TaskStatePreset {
  id: PresetId;
  i18nKey: string;
  colorOf(state: TaskState): string;
  glyphOf(state: TaskState): "spinner" | string;
  spinnerDurationMs(state: TaskState): number;
  animatePulse(state: TaskState): boolean;
  textOpacity: number;
}
```

```ts
function makePreset(
  id: PresetId,
  colors: Record<TaskState, string>,
  spinDuration: number,
  pulseWaiting: boolean,
  textOpacity: number,
): TaskStatePreset {
  return {
    id,
    i18nKey: `tasks.preset.${id}`,
    colorOf: (s) => colors[s],
    glyphOf: (s) => GLYPHS[s],
    spinnerDurationMs: (s) => (s === "running" ? spinDuration : 0),
    animatePulse: (s) => pulseWaiting && s === "waiting_input",
    textOpacity,
  };
}

export const presets: Record<PresetId, TaskStatePreset> = {
  vivid: makePreset("vivid", VIVID_COLORS, 1500, true, 1.0),
  quiet: makePreset("quiet", QUIET_COLORS, 2500, false, 0.75),
};
```

- [ ] **Step 4: Remove type icon from TaskStateIcon.vue**

In `desktop/frontend/src/components/TaskStateIcon.vue`, delete:
- The `import { displayForType, type DisplayKey } from "../lib/sessionType";` line (no longer used here).
- The `type?: DisplayKey` prop from `defineProps`.
- The `const typeDisplay = computed(...)` block (around line 24).
- The whole `<svg v-if="typeDisplay" class="task-type" ... />` block from the template (around lines 65-78).

The final `<script setup>` reads:

```ts
import { computed } from "vue";
import type { TaskState, TaskStatePreset } from "../lib/taskState";
import { useTaskPreset } from "../composables/useTaskPreset";

const props = withDefaults(
  defineProps<{
    state: TaskState;
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
```

The template's outer wrapping span + spinner SVG + glyph span stay; only the trailing type-svg block is removed.

- [ ] **Step 5: Remove type icon from TabBar.vue**

In `desktop/frontend/src/components/TabBar.vue`:
- Delete the `import { displayForType } from "../lib/sessionType";` line (top of script).
- Delete the `function typeForTab(t: TabSummary) { ... }` helper (around line 62).
- Delete the `<span v-if="typeForTab(t)" class="type-icon" ...>` block in the template (around lines 101-104).
- Delete the `.tab .type-icon` CSS rule in the `<style>` block (around line 163).

Also drop `:type` prop from `<TaskStateIcon>` calls in this file (there's at least one inside the tab v-for).

- [ ] **Step 6: Drop `:type` from TaskStateIcon usages in list/sidebar**

In `desktop/frontend/src/components/TaskGroupedList.vue`, find the `<TaskStateIcon ...>` invocations and remove any `:type="..."` prop. The state prop stays.

In `desktop/frontend/src/components/TaskSidebar.vue`, do the same — drop `:type` from `<TaskStateIcon>` usages (typically in the rail's icon stack).

- [ ] **Step 7: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/TabBar.test.ts src/components/TaskStateIcon.test.ts
```
Expected: PASS — both tests now pass; nothing renders the icon anywhere.

- [ ] **Step 8: Type check the whole frontend**

```bash
cd desktop/frontend && npx tsc --noEmit
```
Expected: clean. (If a stale `:type` is left somewhere, tsc will flag it.)

- [ ] **Step 9: Commit**

```bash
git add desktop/frontend/src/lib/taskState.ts \
        desktop/frontend/src/components/TaskStateIcon.vue \
        desktop/frontend/src/components/TaskStateIcon.test.ts \
        desktop/frontend/src/components/TabBar.vue \
        desktop/frontend/src/components/TabBar.test.ts \
        desktop/frontend/src/components/TaskGroupedList.vue \
        desktop/frontend/src/components/TaskSidebar.vue
git commit -m "desktop: drop AI/test/build/deploy type icon from sidebar + tab bar"
```

---

## Task 2: `shortenCwd` pure helper

Self-contained, no Wails dependency. Just a string-manipulation function with a small test table.

**Files:**
- Create: `desktop/frontend/src/lib/shortenCwd.ts`
- Test: `desktop/frontend/src/lib/shortenCwd.test.ts`

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/lib/shortenCwd.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { shortenCwd } from "./shortenCwd";

describe("shortenCwd", () => {
  const HOME = "/Users/attson";

  test("empty input returns empty string", () => {
    expect(shortenCwd("", HOME)).toBe("");
    expect(shortenCwd(undefined, HOME)).toBe("");
  });

  test("HOME prefix replaced with ~", () => {
    expect(shortenCwd("/Users/attson", HOME)).toBe("~");
    expect(shortenCwd("/Users/attson/code", HOME)).toBe("~/code");
    expect(shortenCwd("/Users/attson/code/atterm", HOME)).toBe("~/code/atterm");
  });

  test("paths with 2 or fewer segments are kept verbatim", () => {
    expect(shortenCwd("/tmp", HOME)).toBe("/tmp");
    expect(shortenCwd("/tmp/build", HOME)).toBe("/tmp/build");
    expect(shortenCwd("~/code", HOME)).toBe("~/code");
  });

  test("long absolute paths get …/last/two", () => {
    expect(shortenCwd("/Users/attson/code/github.com.attson/atterm", HOME))
      .toBe("…/github.com.attson/atterm");
    // even if HOME doesn't match (e.g. another user)
    expect(shortenCwd("/Users/someone/a/b/c", HOME))
      .toBe("…/b/c");
  });

  test("long HOME-rooted paths get …/last/two", () => {
    expect(shortenCwd("/Users/attson/a/b/c/d", HOME)).toBe("…/c/d");
  });

  test("empty HOME falls back to no substitution", () => {
    expect(shortenCwd("/Users/attson/code/atterm", "")).toBe("/Users/attson/code/atterm");
  });
});
```

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/lib/shortenCwd.test.ts
```
Expected: FAIL — module not found.

- [ ] **Step 3: Implement `shortenCwd`**

Create `desktop/frontend/src/lib/shortenCwd.ts`:

```ts
/**
 * Shorten a cwd for inline display in a tight row.
 *
 * Strategy:
 *  - empty / undefined → "" (callers can v-if it away)
 *  - if cwd begins with the user's $HOME, replace that prefix with `~`
 *  - if the result has more than 2 path segments (under either `/` or
 *    `~/`), collapse to `…/last/two`
 *  - otherwise return the substituted path verbatim
 *
 * The full path is always available via the row's `title` attribute, so
 * the truncation is non-destructive.
 */
export function shortenCwd(cwd: string | undefined, home: string): string {
  if (!cwd) return "";
  let s = cwd;
  if (home) {
    if (s === home) return "~";
    if (s.startsWith(home + "/")) {
      s = "~" + s.slice(home.length);
    }
  }
  const tildePrefixed = s.startsWith("~/");
  const body = tildePrefixed ? s.slice(2) : s.replace(/^\//, "");
  const parts = body.split("/").filter(Boolean);
  if (parts.length <= 2) {
    return s;
  }
  return "…/" + parts.slice(-2).join("/");
}
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/lib/shortenCwd.test.ts
```
Expected: PASS — 6/6.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/shortenCwd.ts desktop/frontend/src/lib/shortenCwd.test.ts
git commit -m "desktop/lib: shortenCwd helper for inline cwd display"
```

---

## Task 3: Wails `GetUserHomeDir()` binding

Tiny addition so the frontend can pass the host's HOME into `shortenCwd`.

**Files:**
- Modify: `desktop/app.go` (add `GetUserHomeDir` method near other `Get*` methods)
- Modify: `desktop/frontend/src/lib/api.ts` (interface entry + wrapper)
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts` + `App.js` (stub)
- Test: `desktop/frontend/src/lib/api.test.ts` (extend existing — already has bindings-pass-through pattern)

- [ ] **Step 1: Write the failing wrapper test**

Append to `desktop/frontend/src/lib/api.test.ts`:

```ts
test("getUserHomeDir delegates to bindings", async () => {
  const fn = vi.fn().mockResolvedValue("/Users/attson");
  __setBindingsForTest({ GetUserHomeDir: fn } as any);
  await expect(getUserHomeDir()).resolves.toBe("/Users/attson");
  expect(fn).toHaveBeenCalledOnce();
});
```

Make sure `getUserHomeDir` is in the import list at the top of `api.test.ts`.

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/lib/api.test.ts
```
Expected: FAIL — `getUserHomeDir` undefined.

- [ ] **Step 3: Go side**

In `desktop/app.go`, near the existing `GetTaskSidebarCollapsed` method (line ~489), ADD:

```go
// GetUserHomeDir returns the OS-reported user home directory, so the
// frontend can collapse paths starting with HOME to "~" for display.
// Empty string on failure (the frontend already treats empty HOME as
// "don't substitute").
func (a *App) GetUserHomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
```

The `os` import should already be present in `app.go` (used elsewhere). If not, add it.

- [ ] **Step 4: TypeScript wrapper**

In `desktop/frontend/src/lib/api.ts`, in the `AppBindings` interface add:

```ts
  GetUserHomeDir(): Promise<string>;
```

Add the wrapper near the existing `getTaskSidebarCollapsed`:

```ts
export function getUserHomeDir(): Promise<string> {
  return bindings().GetUserHomeDir();
}
```

- [ ] **Step 5: Hand-edit wailsjs stubs**

In `desktop/frontend/wailsjs/go/main/App.d.ts`, near other `Get*` declarations, add:

```ts
export function GetUserHomeDir():Promise<string>;
```

In `desktop/frontend/wailsjs/go/main/App.js`, near other `Get*` exports, add:

```js
export function GetUserHomeDir() {
  return window['go']['main']['App']['GetUserHomeDir']();
}
```

- [ ] **Step 6: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/lib/api.test.ts
cd desktop/frontend && npx tsc --noEmit
```
Expected: PASS; tsc clean.

- [ ] **Step 7: Go build**

```bash
cd desktop && go build ./...
```
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add desktop/app.go desktop/frontend/src/lib/api.ts \
        desktop/frontend/src/lib/api.test.ts \
        desktop/frontend/wailsjs/go/main/App.d.ts \
        desktop/frontend/wailsjs/go/main/App.js
git commit -m "desktop: GetUserHomeDir Wails binding"
```

---

## Task 4: Wire cwd display in `TaskGroupedList`

Now hook the helper + Wails value into the row.

**Files:**
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue`
- Modify: `desktop/frontend/src/components/TaskGroupedList.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `desktop/frontend/src/components/TaskGroupedList.test.ts`:

```ts
test("row renders the session cwd alongside the command", () => {
  const byHost = {
    h: [
      mk({
        session_id: "s1",
        host: "mac",
        task_state: "running",
        current_command: "claude",
        cwd: "/Users/attson/code/atterm",
      }),
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
  const row = w.find('[data-test="task-row"]');
  // We can't assume HOME is exactly /Users/attson in CI; the helper falls
  // back to verbatim when HOME is empty, so assert one of the two valid
  // outputs is present.
  const text = row.text();
  expect(text).toContain("claude");
  expect(text).toMatch(/atterm/); // last segment always present
});
```

If `mk()` (the helper that builds a `RemoteSession`) doesn't yet accept `cwd`, extend it to spread overrides — typical pattern in this test file.

Also stub `getUserHomeDir` in the test setup so the home value is deterministic:

At the top of the file (or before each suite), add a vi.mock for `../../lib/api`:

```ts
vi.mock('../../lib/api', () => ({
  getUserHomeDir: vi.fn().mockResolvedValue('/Users/attson'),
}));
```

(Adapt to whatever existing mock setup the file already has; if there is none, this is the cleanest place to add it.)

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/TaskGroupedList.test.ts
```
Expected: FAIL — row text does not contain cwd yet.

- [ ] **Step 3: Modify the row markup**

In `desktop/frontend/src/components/TaskGroupedList.vue`, at the top of `<script setup>` add:

```ts
import { onMounted, ref } from "vue";
import { shortenCwd } from "../lib/shortenCwd";
import { getUserHomeDir } from "../lib/api";

const home = ref("");
onMounted(async () => {
  try {
    home.value = await getUserHomeDir();
  } catch { /* leave empty — helper will skip substitution */ }
});

function commandLabel(s: { current_command?: string; title?: string; session_id: string }): string {
  return s.current_command || s.title || s.session_id.slice(0, 8);
}

function rowTitle(s: { cwd?: string; current_command?: string; title?: string; session_id: string }): string {
  const cmd = commandLabel(s);
  return s.cwd ? `${cmd}\n${s.cwd}` : cmd;
}
```

In the template, find each `<span class="cmd">` inside `<button class="task-row" ...>` and replace with the new two-element block. There are two such places (the expanded host row at ~line 84 and the completed-fold row at ~line 122). The new markup for each:

```vue
<span class="cmd-and-cwd" :title="rowTitle(s)">
  <span class="cmd">{{ commandLabel(s) }}</span>
  <span v-if="shortenCwd(s.cwd, home)" class="cwd">·&nbsp;{{ shortenCwd(s.cwd, home) }}</span>
</span>
```

In the `<style scoped>` block, REPLACE the existing `.cmd { ... }` rule with:

```css
.cmd-and-cwd { flex: 1 1 auto; min-width: 0; display: flex; gap: 6px; overflow: hidden; align-items: baseline; }
.cmd { white-space: nowrap; text-overflow: ellipsis; overflow: hidden; font-family: var(--font-mono); }
.cwd { color: var(--fg-dim); white-space: nowrap; flex-shrink: 1; overflow: hidden; text-overflow: ellipsis; font-family: var(--font-mono); font-size: 0.85em; }
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/TaskGroupedList.test.ts
cd desktop/frontend && npx tsc --noEmit
```
Expected: PASS; tsc clean.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/TaskGroupedList.vue \
        desktop/frontend/src/components/TaskGroupedList.test.ts
git commit -m "desktop/TaskGroupedList: show cwd in task rows with smart truncation"
```

---

## Task 5: Wails `Get/SetTaskSidebarWidth` binding

Mirrors the existing `Get/SetTaskSidebarCollapsed` pattern with bounds clamping in the Go setter.

**Files:**
- Modify: `desktop/config.go` (add `TaskSidebarWidth int` + default helper)
- Modify: `desktop/app.go` (Get + Set methods, bounds clamp)
- Modify: `desktop/frontend/src/lib/api.ts` (interface + wrappers)
- Modify: `desktop/frontend/wailsjs/go/main/App.d.ts` + `App.js` (stubs)
- Test: `desktop/frontend/src/lib/api.test.ts` (extend)
- Test: `desktop/config_test.go` (or `desktop/app_test.go`) — if a Go-side test file exists, add round-trip + clamp tests; otherwise skip (Go-side correctness is small and the integration test in Task 6 exercises it indirectly).

- [ ] **Step 1: Write the failing wrapper test**

Append to `desktop/frontend/src/lib/api.test.ts`:

```ts
test("getTaskSidebarWidth delegates to bindings", async () => {
  const fn = vi.fn().mockResolvedValue(300);
  __setBindingsForTest({ GetTaskSidebarWidth: fn } as any);
  await expect(getTaskSidebarWidth()).resolves.toBe(300);
});

test("setTaskSidebarWidth passes the px value", async () => {
  const fn = vi.fn().mockResolvedValue(undefined);
  __setBindingsForTest({ SetTaskSidebarWidth: fn } as any);
  await setTaskSidebarWidth(280);
  expect(fn).toHaveBeenCalledWith(280);
});
```

Update the import line to add `getTaskSidebarWidth` and `setTaskSidebarWidth`.

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/lib/api.test.ts
```
Expected: FAIL — wrappers undefined.

- [ ] **Step 3: Config struct**

In `desktop/config.go`, in the `appConfig` struct (or whatever the persisted config struct is named — currently has `TaskSidebarCollapsed bool` at line 111), add:

```go
	// TaskSidebarWidth is the persisted pixel width of the task sidebar
	// (expanded mode). 0 means "use default 240"; valid range enforced
	// on Set in app.go.
	TaskSidebarWidth int `json:"task_sidebar_width,omitempty"`
```

Add the default helper near the existing `TaskPresetOrDefault` (if there is one) or just below the struct:

```go
const defaultTaskSidebarWidth = 240

func (c appConfig) TaskSidebarWidthOrDefault() int {
	if c.TaskSidebarWidth == 0 {
		return defaultTaskSidebarWidth
	}
	return c.TaskSidebarWidth
}
```

- [ ] **Step 4: Go-side Get/Set methods**

In `desktop/app.go`, near the existing `GetTaskSidebarCollapsed` / `SetTaskSidebarCollapsed` (lines ~489-505), ADD:

```go
const (
	minTaskSidebarWidth = 180
	maxTaskSidebarWidth = 480
)

// GetTaskSidebarWidth returns the persisted task sidebar pixel width,
// clamped to a sane range. Returns the default (240) when no value is
// stored or when the stored value is out of range.
func (a *App) GetTaskSidebarWidth() int {
	if a.cfgStore == nil {
		return defaultTaskSidebarWidth
	}
	w := a.cfgStore.Get().TaskSidebarWidthOrDefault()
	if w < minTaskSidebarWidth || w > maxTaskSidebarWidth {
		return defaultTaskSidebarWidth
	}
	return w
}

// SetTaskSidebarWidth clamps px to [minTaskSidebarWidth, maxTaskSidebarWidth]
// and persists. Out-of-range values are silently corrected rather than
// rejected, so a frontend bug that emits 1500 still leaves the user in
// a usable state.
func (a *App) SetTaskSidebarWidth(px int) error {
	if a.cfgStore == nil {
		return nil
	}
	if px < minTaskSidebarWidth {
		px = minTaskSidebarWidth
	}
	if px > maxTaskSidebarWidth {
		px = maxTaskSidebarWidth
	}
	cfg := a.cfgStore.Get()
	cfg.TaskSidebarWidth = px
	return a.cfgStore.Set(cfg)
}
```

- [ ] **Step 5: TS wrappers**

In `desktop/frontend/src/lib/api.ts`, in the `AppBindings` interface (where `GetTaskSidebarCollapsed` lives), add:

```ts
  GetTaskSidebarWidth(): Promise<number>;
  SetTaskSidebarWidth(px: number): Promise<void>;
```

Below the existing wrappers, add:

```ts
export function getTaskSidebarWidth(): Promise<number> {
  return bindings().GetTaskSidebarWidth();
}
export function setTaskSidebarWidth(px: number): Promise<void> {
  return bindings().SetTaskSidebarWidth(px);
}
```

- [ ] **Step 6: Hand-edit wailsjs stubs**

In `desktop/frontend/wailsjs/go/main/App.d.ts`:

```ts
export function GetTaskSidebarWidth():Promise<number>;
export function SetTaskSidebarWidth(arg1:number):Promise<void>;
```

In `App.js`:

```js
export function GetTaskSidebarWidth() {
  return window['go']['main']['App']['GetTaskSidebarWidth']();
}

export function SetTaskSidebarWidth(arg1) {
  return window['go']['main']['App']['SetTaskSidebarWidth'](arg1);
}
```

- [ ] **Step 7: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/lib/api.test.ts
cd desktop/frontend && npx tsc --noEmit
cd desktop && go build ./...
```
Expected: all clean.

- [ ] **Step 8: Commit**

```bash
git add desktop/app.go desktop/config.go \
        desktop/frontend/src/lib/api.ts \
        desktop/frontend/src/lib/api.test.ts \
        desktop/frontend/wailsjs/go/main/App.d.ts \
        desktop/frontend/wailsjs/go/main/App.js
git commit -m "desktop: Get/SetTaskSidebarWidth Wails binding with bounds clamping"
```

---

## Task 6: TaskSidebar drag handle + persistence

Wire the resize affordance into the sidebar component, using the binding from Task 5.

**Files:**
- Modify: `desktop/frontend/src/components/TaskSidebar.vue`
- Modify: `desktop/frontend/src/components/TaskSidebar.test.ts`

- [ ] **Step 1: Write the failing test**

Append to `desktop/frontend/src/components/TaskSidebar.test.ts`:

```ts
test("drag handle emits pointerdown→move→up and persists width", async () => {
  const setSpy = vi.fn().mockResolvedValue(undefined);
  vi.spyOn(api, "getTaskSidebarWidth").mockResolvedValue(240);
  vi.spyOn(api, "setTaskSidebarWidth").mockImplementation(setSpy);

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
  await flushPromises();

  const handle = w.find('[data-test="sidebar-resize-handle"]');
  expect(handle.exists()).toBe(true);

  // pointerdown at x=240, then move to x=340 (=+100), then up.
  await handle.trigger("pointerdown", { clientX: 240, pointerId: 1 });
  await handle.trigger("pointermove", { clientX: 340, pointerId: 1 });
  await handle.trigger("pointerup", { clientX: 340, pointerId: 1 });
  await flushPromises();

  expect(setSpy).toHaveBeenCalledTimes(1);
  expect(setSpy).toHaveBeenCalledWith(340);
});

test("drag handle clamps to bounds [180, 480]", async () => {
  const setSpy = vi.fn().mockResolvedValue(undefined);
  vi.spyOn(api, "getTaskSidebarWidth").mockResolvedValue(240);
  vi.spyOn(api, "setTaskSidebarWidth").mockImplementation(setSpy);

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
  await flushPromises();

  const handle = w.find('[data-test="sidebar-resize-handle"]');
  await handle.trigger("pointerdown", { clientX: 240, pointerId: 1 });
  await handle.trigger("pointermove", { clientX: 1000, pointerId: 1 });
  await handle.trigger("pointerup", { clientX: 1000, pointerId: 1 });
  await flushPromises();

  expect(setSpy).toHaveBeenCalledWith(480);
});

test("collapsed sidebar does not render drag handle", () => {
  vi.spyOn(api, "getTaskSidebarWidth").mockResolvedValue(240);
  const w = mount(TaskSidebar, {
    props: {
      collapsed: true,
      byHost: {},
      unreadByHost: {},
      primaryStateForHost: () => "idle",
      completedSeen: [],
      totalUnread: 0,
    },
  });
  expect(w.find('[data-test="sidebar-resize-handle"]').exists()).toBe(false);
});
```

Make sure imports include `import * as api from "../../lib/api";` and `flushPromises` from `@vue/test-utils`.

- [ ] **Step 2: Run, expect failure**

```bash
cd desktop/frontend && npx vitest run src/components/TaskSidebar.test.ts
```
Expected: FAIL — no drag handle, no width state.

- [ ] **Step 3: Implement drag-resize in TaskSidebar.vue**

Add to `<script setup>` (preserve existing imports):

```ts
import { onMounted, ref } from "vue";
import { getTaskSidebarWidth, setTaskSidebarWidth } from "../lib/api";

const widthPx = ref(240);
const minWidth = 180;
const maxWidth = 480;
let dragOriginX = 0;
let dragOriginWidth = 0;
let dragging = false;

onMounted(async () => {
  try {
    const stored = await getTaskSidebarWidth();
    if (stored > 0) widthPx.value = clampWidth(stored);
  } catch { /* default 240 */ }
});

function clampWidth(px: number): number {
  return Math.max(minWidth, Math.min(maxWidth, px));
}

function onDragStart(e: PointerEvent) {
  if (props.collapsed) return;
  dragging = true;
  dragOriginX = e.clientX;
  dragOriginWidth = widthPx.value;
  (e.target as HTMLElement).setPointerCapture(e.pointerId);
}

function onDragMove(e: PointerEvent) {
  if (!dragging) return;
  widthPx.value = clampWidth(dragOriginWidth + (e.clientX - dragOriginX));
}

async function onDragEnd(e: PointerEvent) {
  if (!dragging) return;
  dragging = false;
  try {
    (e.target as HTMLElement).releasePointerCapture(e.pointerId);
  } catch { /* element may have been re-rendered */ }
  try {
    await setTaskSidebarWidth(widthPx.value);
  } catch { /* persistence is best-effort */ }
}
```

In the template, change the root `<aside>` to bind width when not collapsed and add the drag handle:

```vue
<aside
  class="task-sidebar"
  :class="{ collapsed }"
  :style="!collapsed ? { width: widthPx + 'px' } : undefined"
>
  <!-- existing expanded/collapsed inner content stays unchanged -->
  <div
    v-if="!collapsed"
    class="resize-handle"
    data-test="sidebar-resize-handle"
    @pointerdown="onDragStart"
    @pointermove="onDragMove"
    @pointerup="onDragEnd"
    @pointercancel="onDragEnd"
  />
</aside>
```

In the `<style scoped>` block, ensure these rules exist (add or update):

```css
.task-sidebar { position: relative; }
.task-sidebar:not(.collapsed) { width: 240px; }
.task-sidebar.collapsed { width: 32px; }
.resize-handle {
  position: absolute;
  top: 0;
  right: -2px;
  width: 4px;
  height: 100%;
  cursor: ew-resize;
  user-select: none;
  z-index: 1;
}
.resize-handle:hover { background: rgba(255, 255, 255, 0.06); }
```

- [ ] **Step 4: Run, expect pass**

```bash
cd desktop/frontend && npx vitest run src/components/TaskSidebar.test.ts
cd desktop/frontend && npx tsc --noEmit
```
Expected: PASS — 3 new tests + all existing TaskSidebar tests pass.

- [ ] **Step 5: Run the whole frontend suite**

```bash
cd desktop/frontend && npx vitest run
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/TaskSidebar.vue \
        desktop/frontend/src/components/TaskSidebar.test.ts
git commit -m "desktop/TaskSidebar: drag-resize handle + persisted width"
```

---

## Final verification

- [ ] **Whole frontend suite**

```bash
cd desktop/frontend && npx vitest run
cd desktop/frontend && npx tsc --noEmit
```
Expected: PASS; clean.

- [ ] **Go build**

```bash
cd desktop && go build ./...
go vet ./...
```
Expected: clean.

- [ ] **Manual smoke (if Wails dev is available)**

```bash
cd desktop && wails dev
```
Verify:
- No type icon next to claude tab or sidebar row.
- Sidebar rows show "command · cwd" with truncation for long paths.
- Hover a row → tooltip shows full cwd.
- Drag right edge of expanded sidebar → width changes within bounds.
- Restart → width is preserved.

---

## Spec coverage check

| Spec section | Task |
| --- | --- |
| §1 Goal (3 UX changes) | Tasks 1, 2-4, 5-6 |
| §2 Architecture / file map | Tasks 1-6 collectively |
| §3 Cwd display (`shortenCwd` + row markup + home) | Task 2 (helper) + Task 3 (binding) + Task 4 (wiring) |
| §4 Drag-resize state + handlers + template | Task 6 |
| §5 Wails persistence (Get/Set width + clamp) | Task 5 |
| §6 Testing (unit + component + Go) | Distributed across Tasks 1, 2, 3, 4, 5, 6 |
| §7 Migration / compatibility (additive) | Implicit — additive struct field with default helper |

No spec section is unmapped. Type field on `proto.SessionInfo` and i18n keys for `taskTypes` are explicitly preserved per spec §1 "out of scope".

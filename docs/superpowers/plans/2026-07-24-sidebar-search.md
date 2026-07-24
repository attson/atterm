# Sidebar Session Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a persistent search input to the left session sidebar's header — inline to the right of the "会话" title — that filters visible sessions live by matching the query against `title` + `cwd` + `current_command` (case-insensitive substring).

**Architecture:** Pure helper `matchesSession(session, query)` in `lib/sessionMatch.ts` (unit-testable in isolation). `TaskSidebar` owns the `query` ref, renders the input inline, and passes it down to `TaskGroupedList` as a `searchQuery` prop. `TaskGroupedList` layers `matchesSession` filtering on top of the existing `filteredGroups` / `pinnedSessions` / `groupKeys` computed chain, short-circuits `isGroupCollapsed` while a query is active (so folded groups auto-expand to reveal matches), and renders an empty-state hint when nothing matches. A new shortcut action `sidebar.focus-search` (default `Mod+KeyF`) focuses the input; `Esc` inside the input clears the query.

**Tech Stack:** Vue 3 + TypeScript + Vitest.

## Global Constraints

- User-facing prose 中文, code/commits English (memory `feedback_reply_language.md`).
- No backward-compat shims (memory `feedback_no_backward_compat.md`); pure additive feature.
- No new frontend dependencies.
- Match key remains `title` + `cwd` + `current_command`; do NOT extend to `host_id`, `session_id`, or `type` (spec §2 non-goals).
- Case-insensitive substring only; no regex, fuzzy, or multi-token AND (spec §2).
- Query state is component-local, not persisted (spec §4.2 lifecycle table).
- Query field lives on `TaskSidebar`; `TaskGroupedList` receives it via `searchQuery: string` prop.
- Pinned sessions ARE subject to filtering (spec §2: "不豁免 pin").
- Existing `isGroupCollapsed` / `collapsedGroups` state MUST NOT be mutated by search; short-circuit in the read function only (spec §4.4).
- i18n file names are `desktop/frontend/src/i18n/messages/{en.ts, zh-CN.ts}` — NOT `zh.ts` (verify before editing).
- Shortcut action id: `sidebar.focus-search`, default `Mod+KeyF`, group `sidebar`, label key `settings.shortcuts.focusSidebarSearch`.
- Commit style: lowercase verb start, ≤72-char subject, one logical change per commit.

Spec: [`docs/superpowers/specs/2026-07-24-sidebar-search-design.md`](../specs/2026-07-24-sidebar-search-design.md)

## File Overview

- Create: `desktop/frontend/src/lib/sessionMatch.ts` — pure `matchesSession(session, query)` helper.
- Create: `desktop/frontend/src/lib/sessionMatch.test.ts` — unit tests for the helper.
- Modify: `desktop/frontend/src/components/TaskSidebar.vue` — add `query` ref, inline `<input>` in header, `searchQuery` prop plumbing, `Esc` clear, `focusSearch()` expose.
- Modify: `desktop/frontend/src/components/TaskSidebar.test.ts` — cover the new UI (visibility, prop plumbing, Esc, focusSearch expose).
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue` — accept `searchQuery` prop, layer `matchesSession` into filters, short-circuit `isGroupCollapsed` under active query, add empty-state hint.
- Modify: `desktop/frontend/src/components/TaskGroupedList.test.ts` — cover the filter behavior, auto-expand, empty state.
- Modify: `desktop/frontend/src/lib/shortcutBindings.ts` — add `sidebar.focus-search` action.
- Modify: `desktop/frontend/src/composables/useTerminalShortcuts.ts` — extend `ShortcutHandlers` and `dispatch` for the new action.
- Modify: `desktop/frontend/src/App.vue` — instantiate a `TaskSidebar` template ref, wire `onFocusSidebarSearch` handler.
- Modify: `desktop/frontend/src/i18n/messages/en.ts` — add `tasks.sidebar.searchPlaceholder`, `tasks.search.empty`, `settings.shortcuts.focusSidebarSearch`.
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts` — same 3 keys in Chinese.

Go side: **untouched**.

---

### Task 1: `matchesSession` pure helper + tests

**Files:**
- Create: `desktop/frontend/src/lib/sessionMatch.ts`
- Create: `desktop/frontend/src/lib/sessionMatch.test.ts`

**Interfaces:**
- Consumes: `RemoteSession` from `../platform/types` (fields `title: string`, `cwd?: string`, `current_command?: string`).
- Produces: `matchesSession(s: RemoteSession, q: string): boolean` — `q` MUST be pre-trimmed and pre-lowercased by the caller (this keeps the per-keystroke hot loop allocation-free). Empty `q` short-circuits to `true` (no filter).

- [ ] **Step 1: Write the failing tests**

Create `desktop/frontend/src/lib/sessionMatch.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import type { RemoteSession } from "../platform/types";
import { matchesSession } from "./sessionMatch";

function mk(overrides: Partial<RemoteSession> = {}): RemoteSession {
  return {
    session_id: "s1",
    host_id: "h1",
    title: "",
    cwd: "",
    command: "",
    cols: 80,
    rows: 24,
    started_at: 0,
    ...overrides,
  } as RemoteSession;
}

describe("matchesSession", () => {
  test("empty query matches every session", () => {
    expect(matchesSession(mk({ title: "anything" }), "")).toBe(true);
    expect(matchesSession(mk({ title: "" }), "")).toBe(true);
  });

  test("matches title (case-insensitive substring)", () => {
    const s = mk({ title: "Feishu Gateway" });
    expect(matchesSession(s, "feishu")).toBe(true);
    expect(matchesSession(s, "gateway")).toBe(true);
    expect(matchesSession(s, "shu ga")).toBe(true);
    expect(matchesSession(s, "nope")).toBe(false);
  });

  test("matches cwd", () => {
    const s = mk({ title: "shell", cwd: "/Users/attson/proj/web" });
    expect(matchesSession(s, "proj")).toBe(true);
    expect(matchesSession(s, "/web")).toBe(true);
    expect(matchesSession(s, "attson")).toBe(true);
  });

  test("matches current_command", () => {
    const s = mk({ title: "shell", current_command: "npm run build" });
    expect(matchesSession(s, "npm")).toBe(true);
    expect(matchesSession(s, "run bui")).toBe(true);
  });

  test("matches CJK", () => {
    const s = mk({ title: "支付网关" });
    expect(matchesSession(s, "支付")).toBe(true);
    expect(matchesSession(s, "网关")).toBe(true);
    expect(matchesSession(s, "别的")).toBe(false);
  });

  test("null / undefined fields never contribute", () => {
    const s = mk({ title: "", cwd: undefined, current_command: undefined });
    expect(matchesSession(s, "x")).toBe(false);
  });

  test("query with internal whitespace is treated literally", () => {
    const s = mk({ title: "proj web" });
    expect(matchesSession(s, "proj web")).toBe(true);
    // Not a multi-token AND: "web proj" is not a substring of "proj web".
    expect(matchesSession(s, "web proj")).toBe(false);
  });

  test("caller is responsible for trim + lowercase (contract test)", () => {
    // matchesSession does NOT re-lowercase q; this documents the contract.
    const s = mk({ title: "Feishu Gateway" });
    // Callers pass q pre-lowercased. If they forget, uppercase won't match.
    expect(matchesSession(s, "FEISHU")).toBe(false);
    expect(matchesSession(s, "feishu")).toBe(true);
  });
});
```

- [ ] **Step 2: Run to verify failure**

Run:
```bash
cd desktop/frontend && npm test -- sessionMatch
```
Expected: fail with `Cannot find module './sessionMatch'`.

- [ ] **Step 3: Write the helper**

Create `desktop/frontend/src/lib/sessionMatch.ts`:

```ts
import type { RemoteSession } from "../platform/types";

// matchesSession returns true when q is empty (no filter) or when q is a
// case-insensitive substring of any of the session's user-visible identity
// fields (title / cwd / current_command). Fields are matched independently;
// a match in any one wins. q must already be trimmed and lowercased by the
// caller — this keeps the hot path (called per session per keystroke)
// branch-free of allocation. Empty / null / undefined fields never
// contribute.
export function matchesSession(s: RemoteSession, q: string): boolean {
  if (!q) return true;
  const t = s.title;
  if (t && t.toLocaleLowerCase().includes(q)) return true;
  const c = s.cwd;
  if (c && c.toLocaleLowerCase().includes(q)) return true;
  const cc = s.current_command;
  if (cc && cc.toLocaleLowerCase().includes(q)) return true;
  return false;
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd desktop/frontend && npm test -- sessionMatch
```
Expected: 8/8 passing.

- [ ] **Step 5: Type-check**

Run:
```bash
cd desktop/frontend && npm run build
```
Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/lib/sessionMatch.ts \
        desktop/frontend/src/lib/sessionMatch.test.ts
git commit -m "feat(desktop): add matchesSession helper for sidebar search"
```

---

### Task 2: Search input in header + filter wiring in grouped list

**Files:**
- Modify: `desktop/frontend/src/components/TaskSidebar.vue` — add `query` ref, header inline input, prop plumbing to `TaskGroupedList`
- Modify: `desktop/frontend/src/components/TaskSidebar.test.ts` — cover input visibility, Esc, prop plumbing
- Modify: `desktop/frontend/src/components/TaskGroupedList.vue` — `searchQuery` prop, `q` computed, layer `matchesSession` into `filteredGroups` + `pinnedSessions` + `groupKeys` + `hasAnyMatch`, short-circuit `isGroupCollapsed` under active query, empty-state hint
- Modify: `desktop/frontend/src/components/TaskGroupedList.test.ts` — cover filter, auto-expand, pin filtering, empty state
- Modify: `desktop/frontend/src/i18n/messages/en.ts` — add `tasks.sidebar.searchPlaceholder`, `tasks.search.empty`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts` — same

**Interfaces:**
- Consumes: `matchesSession(s, q)` from Task 1.
- Produces:
  - `TaskSidebar` renders a `<input ref="searchEl" type="search">` in `.sidebar-header` between `.title` and `.group-toggle`; owns `const query = ref("")`.
  - `TaskSidebar` passes `:search-query="query"` to `TaskGroupedList`.
  - `TaskGroupedList` accepts `searchQuery?: string` with default `""`, exposes no new emits.

- [ ] **Step 1: Add i18n keys**

Edit `desktop/frontend/src/i18n/messages/en.ts` — inside the `tasks.sidebar` block (around line 538-542), add `searchPlaceholder`:

Before:
```ts
    sidebar: {
      title: "Sessions",
      collapse: "Collapse sidebar (Cmd/Ctrl+B)",
      expand: "Expand sidebar (Cmd/Ctrl+B)",
    },
```

After:
```ts
    sidebar: {
      title: "Sessions",
      collapse: "Collapse sidebar (Cmd/Ctrl+B)",
      expand: "Expand sidebar (Cmd/Ctrl+B)",
      searchPlaceholder: "Search",
    },
```

Then at the same nesting level (inside `tasks:`), after the existing `pinned:` block (around line 557), add a new `search:` block:

```ts
    search: {
      empty: 'No sessions match "{q}"',
    },
```

Do the same in `desktop/frontend/src/i18n/messages/zh-CN.ts` — inside `tasks.sidebar`:

```ts
    sidebar: {
      title: "会话",
      collapse: "折叠侧栏 (Cmd/Ctrl+B)",
      expand: "展开侧栏 (Cmd/Ctrl+B)",
      searchPlaceholder: "搜索",
    },
```

And after `pinned:`:

```ts
    search: {
      empty: '搜索 "{q}" 无匹配会话',
    },
```

Run `cd desktop/frontend && npm run build` to confirm both messages files still type-check.

- [ ] **Step 2: Write failing tests for TaskGroupedList search filter**

Read the existing `desktop/frontend/src/components/TaskGroupedList.test.ts` first to see the mount helper it uses. Append new tests inside the existing `describe` (before the closing `});`). Use the same session/prop factory as neighboring tests. Add:

```ts
  it("filters rows by searchQuery across title/cwd/current_command", async () => {
    const s1 = { session_id: "a", host_id: "h", title: "Feishu Gateway", cwd: "/proj/api", cols: 80, rows: 24, started_at: 0 } as any;
    const s2 = { session_id: "b", host_id: "h", title: "Web app", cwd: "/proj/web", cols: 80, rows: 24, started_at: 0 } as any;
    const s3 = { session_id: "c", host_id: "h", title: "Runner", cwd: "/tmp", current_command: "npm run build", cols: 80, rows: 24, started_at: 0 } as any;
    const wrapper = mount(TaskGroupedList, {
      props: {
        byHost: { h: [s1, s2, s3] },
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        groupBy: "host",
        byState: {},
        searchQuery: "feishu",
      },
    });
    await nextTick();
    const rows = wrapper.findAll('[data-test="task-row"]');
    expect(rows).toHaveLength(1);
    expect(rows[0].attributes("data-session-id")).toBe("a");
  });

  it("auto-expands a collapsed group when a match lives inside it", async () => {
    const s1 = { session_id: "a", host_id: "h", title: "Feishu Gateway", cwd: "", cols: 80, rows: 24, started_at: 0 } as any;
    const wrapper = mount(TaskGroupedList, {
      props: {
        byHost: { h: [s1] },
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        groupBy: "host",
        byState: {},
        searchQuery: "",
      },
    });
    await nextTick();
    // Collapse the group first.
    await wrapper.find('[data-test="host-header"]').trigger("click");
    await nextTick();
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(0);
    // Now type a matching query — the collapse should short-circuit and the
    // row should reappear without us having to click to expand.
    await wrapper.setProps({ searchQuery: "feishu" });
    await nextTick();
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(1);
    // Clearing the query restores the original (still-collapsed) state.
    await wrapper.setProps({ searchQuery: "" });
    await nextTick();
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(0);
  });

  it("hides groups whose filtered list is empty and shows an empty-state hint when nothing matches", async () => {
    const s1 = { session_id: "a", host_id: "h", title: "Feishu Gateway", cwd: "", cols: 80, rows: 24, started_at: 0 } as any;
    const wrapper = mount(TaskGroupedList, {
      props: {
        byHost: { h: [s1] },
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        groupBy: "host",
        byState: {},
        searchQuery: "no-such-thing",
      },
    });
    await nextTick();
    expect(wrapper.findAll('[data-test="task-row"]')).toHaveLength(0);
    expect(wrapper.findAll('[data-test="host-header"]')).toHaveLength(0);
    const empty = wrapper.find('[data-test="search-empty"]');
    expect(empty.exists()).toBe(true);
    expect(empty.text()).toContain("no-such-thing");
  });
```

Do NOT add a new "pinned participates in filter" test unless the existing test file already exercises the pinned code path with a mocked `useSessionPins`; look up the file first. If the existing suite already has pinned coverage (search for `pinned-group-header` in `TaskGroupedList.test.ts`), extend the closest matching one to also drive `searchQuery` and assert the pinned row disappears when the query doesn't match its fields. If not, skip that specific assertion and note it in your report — Task 3's manual verification covers pinned-filter behavior in the running app.

- [ ] **Step 3: Run tests to verify they fail**

Run:
```bash
cd desktop/frontend && npm test -- TaskGroupedList
```
Expected: the 3 new tests fail because `searchQuery` prop is not declared, filter doesn't run, and empty-state hint isn't rendered. Existing tests still pass.

- [ ] **Step 4: Implement filter in TaskGroupedList**

Edit `desktop/frontend/src/components/TaskGroupedList.vue`.

Add import (after existing imports around line 19):

```ts
import { matchesSession } from "../lib/sessionMatch";
```

Extend props (line 24-51). Before:

```ts
const props = withDefaults(defineProps<{
  byHost: Record<string, RemoteSession[]>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  groupBy?: "host" | "state";
  byState?: Record<string, RemoteSession[]>;
  activeSessionId?: string | null;
  localHostId?: string;
  localHost?: string;
  openSessionIds?: string[];
}>(), {
  groupBy: "host",
  byState: () => ({}),
  activeSessionId: null,
  localHostId: "",
  localHost: "",
  openSessionIds: () => [],
});
```

After — add `searchQuery` prop:

```ts
const props = withDefaults(defineProps<{
  byHost: Record<string, RemoteSession[]>;
  primaryStateForHost: (hostId: string) => TaskState;
  completedSeen: RemoteSession[];
  groupBy?: "host" | "state";
  byState?: Record<string, RemoteSession[]>;
  activeSessionId?: string | null;
  localHostId?: string;
  localHost?: string;
  openSessionIds?: string[];
  // Live search query from the sidebar header. Empty string = no filter.
  // Matched against title / cwd / current_command (case-insensitive
  // substring). See docs/superpowers/specs/2026-07-24-sidebar-search-design.md.
  searchQuery?: string;
}>(), {
  groupBy: "host",
  byState: () => ({}),
  activeSessionId: null,
  localHostId: "",
  localHost: "",
  openSessionIds: () => [],
  searchQuery: "",
});
```

Add a `q` computed near the top of `<script setup>` (right after `const pins = useSessionPins();` around line 72):

```ts
// Normalized query — trimmed + lowercased once per input change, then handed
// verbatim to matchesSession (which does not re-lowercase). Empty string
// short-circuits the filter (matchesSession returns true unconditionally).
const q = computed(() => props.searchQuery.trim().toLocaleLowerCase());
```

Update `filteredGroups` (around line 81-87). Before:

```ts
const filteredGroups = computed<Record<string, RemoteSession[]>>(() => {
  const out: Record<string, RemoteSession[]> = {};
  for (const [k, list] of Object.entries(groups.value)) {
    out[k] = list.filter((s) => !pins.isPinned(s.session_id));
  }
  return out;
});
```

After:

```ts
const filteredGroups = computed<Record<string, RemoteSession[]>>(() => {
  const out: Record<string, RemoteSession[]> = {};
  for (const [k, list] of Object.entries(groups.value)) {
    out[k] = list.filter(
      (s) => !pins.isPinned(s.session_id) && matchesSession(s, q.value),
    );
  }
  return out;
});
```

Update `groupKeys` (line 90-102). Before:

```ts
const groupKeys = computed<string[]>(() => {
  if (props.groupBy === "state") {
    return STATE_ORDER.filter((s) => (groups.value[s] ?? []).length > 0);
  }
  const keys = Object.keys(groups.value).sort();
  ...
});
```

After — the length check reads from `filteredGroups` so empty groups vanish under an active query:

```ts
const groupKeys = computed<string[]>(() => {
  if (props.groupBy === "state") {
    return STATE_ORDER.filter((s) => (filteredGroups.value[s] ?? []).length > 0);
  }
  const keys = Object.keys(groups.value).sort()
    .filter((k) => (filteredGroups.value[k] ?? []).length > 0);
  if (!props.localHostId) return keys;
  const i = keys.indexOf(props.localHostId);
  if (i <= 0) return keys;
  return [props.localHostId, ...keys.slice(0, i), ...keys.slice(i + 1)];
});
```

Update `pinnedSessions` (line 140-155) to also apply the filter — add `.filter((s) => matchesSession(s, q.value))` as the final step before the sort. Before:

```ts
const pinnedSessions = computed<RemoteSession[]>(() => {
  const out: RemoteSession[] = [];
  const seen = new Set<string>();
  const source = props.groupBy === "state" ? props.byState ?? {} : props.byHost ?? {};
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
```

After — inject the search filter into the inner conditional so the loop body stays O(n) with no extra pass:

```ts
const pinnedSessions = computed<RemoteSession[]>(() => {
  const out: RemoteSession[] = [];
  const seen = new Set<string>();
  const source = props.groupBy === "state" ? props.byState ?? {} : props.byHost ?? {};
  for (const list of Object.values(source)) {
    for (const s of list) {
      if (seen.has(s.session_id)) continue;
      if (pins.isPinned(s.session_id) && matchesSession(s, q.value)) {
        seen.add(s.session_id);
        out.push(s);
      }
    }
  }
  out.sort((a, b) => urgencyIndex(a.task_state) - urgencyIndex(b.task_state));
  return out;
});
```

Update `isGroupCollapsed` (line 108-110) with the search short-circuit:

Before:
```ts
function isGroupCollapsed(key: string): boolean {
  return collapsedGroups.value.has(key);
}
```

After:
```ts
function isGroupCollapsed(key: string): boolean {
  // Active query overrides folded state: every group appears expanded so
  // matches inside are visible. The underlying set is not mutated — clearing
  // the query restores the user's real collapse state.
  if (q.value) return false;
  return collapsedGroups.value.has(key);
}
```

Add a `hasAnyMatch` computed (right after `pinnedSessions`, around line 155):

```ts
const hasAnyMatch = computed(
  () => pinnedSessions.value.length > 0 || groupKeys.value.length > 0,
);
```

In the template (line 231-232), after the closing `</template>` for `groupKeys` iteration and before the `completed-fold` section, add the empty-state hint. Find the block:

```vue
    </template>
    <section v-if="completedSeen.length > 0" class="completed-fold">
```

Change to:

```vue
    </template>
    <div
      v-if="q && !hasAnyMatch"
      class="empty-hint"
      data-test="search-empty"
    >
      {{ t('tasks.search.empty', { q: props.searchQuery }) }}
    </div>
    <section v-if="completedSeen.length > 0" class="completed-fold">
```

In `<style scoped>` (bottom of file), add:

```css
.empty-hint {
  padding: 24px 12px;
  text-align: center;
  color: var(--fg);
  opacity: 0.55;
  font-size: 12px;
}
```

- [ ] **Step 5: Run TaskGroupedList tests to verify they pass**

Run:
```bash
cd desktop/frontend && npm test -- TaskGroupedList
```
Expected: all tests pass (existing + 3 new).

- [ ] **Step 6: Write failing tests for TaskSidebar input**

Read the existing `desktop/frontend/src/components/TaskSidebar.test.ts` first to see the mount pattern. Append new tests inside the existing `describe`. Before writing, note that `getTaskSidebarWidth` is mocked in existing tests — reuse the same mock harness.

Add:

```ts
  it("renders a search input in the expanded header and hides it when collapsed", async () => {
    const wrapper = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        totalUnread: 0,
      },
    });
    await flushPromises();
    expect(wrapper.find('[data-test="sidebar-search"]').exists()).toBe(true);

    await wrapper.setProps({ collapsed: true });
    await nextTick();
    expect(wrapper.find('[data-test="sidebar-search"]').exists()).toBe(false);
  });

  it("passes the typed query down to TaskGroupedList", async () => {
    const wrapper = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        totalUnread: 0,
      },
    });
    await flushPromises();
    const input = wrapper.find('[data-test="sidebar-search"]');
    await input.setValue("feishu");
    await nextTick();
    const list = wrapper.findComponent({ name: "TaskGroupedList" });
    expect(list.props("searchQuery")).toBe("feishu");
  });

  it("Esc inside the search input clears the query", async () => {
    const wrapper = mount(TaskSidebar, {
      props: {
        collapsed: false,
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        totalUnread: 0,
      },
    });
    await flushPromises();
    const input = wrapper.find<HTMLInputElement>('[data-test="sidebar-search"]');
    await input.setValue("proj");
    expect(input.element.value).toBe("proj");
    await input.trigger("keydown", { key: "Escape" });
    await nextTick();
    expect(input.element.value).toBe("");
  });

  it("focusSearch() expose focuses the input (and expands the sidebar if collapsed)", async () => {
    const wrapper = mount(TaskSidebar, {
      props: {
        collapsed: true,
        byHost: {},
        primaryStateForHost: () => "idle" as const,
        completedSeen: [],
        totalUnread: 0,
      },
      attachTo: document.body,
    });
    await flushPromises();
    // Call the exposed method — should emit collapse:false then focus.
    await (wrapper.vm as any).focusSearch();
    await nextTick();
    expect(wrapper.emitted("update:collapsed")?.[0]).toEqual([false]);

    // Simulate the parent responding to update:collapsed by setting the prop.
    await wrapper.setProps({ collapsed: false });
    await nextTick();
    await (wrapper.vm as any).focusSearch();
    await nextTick();
    const input = wrapper.find<HTMLInputElement>('[data-test="sidebar-search"]');
    expect(document.activeElement).toBe(input.element);
    wrapper.unmount();
  });
```

Ensure `flushPromises` and `nextTick` are imported at the top of the test file (they are in the existing file — verify).

- [ ] **Step 7: Run tests to verify they fail**

Run:
```bash
cd desktop/frontend && npm test -- TaskSidebar
```
Expected: 4 new tests fail — `sidebar-search` element missing / `searchQuery` not passed / `focusSearch` not exposed.

- [ ] **Step 8: Implement search input in TaskSidebar**

Edit `desktop/frontend/src/components/TaskSidebar.vue`.

Add `nextTick` to the vue import and add `ref` if not already there:

```ts
import { computed, nextTick, onMounted, ref } from "vue";
```

After the existing `const widthPx = ref(240);` block (around line 44-49), add:

```ts
const query = ref("");
const searchEl = ref<HTMLInputElement | null>(null);

function onSearchEsc() {
  query.value = "";
}

async function focusSearch(): Promise<void> {
  if (props.collapsed) {
    emit("update:collapsed", false);
    await nextTick();
  }
  searchEl.value?.focus();
  searchEl.value?.select();
}

defineExpose({ focusSearch });
```

In the template, edit `.sidebar-header` (around line 168-189). Before:

```vue
      <header class="sidebar-header">
        <span class="title">{{ t("tasks.sidebar.title") }}</span>
        <button
          class="group-toggle"
          data-test="group-toggle"
          :title="t('tasks.settings.groupBy')"
          @click="onToggleGroupBy"
        >
          {{ groupByState.activeId.value === 'state'
            ? t('tasks.settings.groupByState')
            : t('tasks.settings.groupByHost') }}
        </button>
        <button
          class="collapse-button"
          data-test="collapse-button"
          :title="t('tasks.sidebar.collapse')"
          @click="emit('update:collapsed', true)"
        >
          «
        </button>
      </header>
```

After — insert the input between `.title` and `.group-toggle`, and shrink `.title` from flex:1:

```vue
      <header class="sidebar-header">
        <span class="title">{{ t("tasks.sidebar.title") }}</span>
        <input
          ref="searchEl"
          v-model="query"
          type="search"
          class="sidebar-search"
          data-test="sidebar-search"
          :placeholder="t('tasks.sidebar.searchPlaceholder')"
          :aria-label="t('tasks.sidebar.searchPlaceholder')"
          @keydown.esc.prevent="onSearchEsc"
        />
        <button
          class="group-toggle"
          data-test="group-toggle"
          :title="t('tasks.settings.groupBy')"
          @click="onToggleGroupBy"
        >
          {{ groupByState.activeId.value === 'state'
            ? t('tasks.settings.groupByState')
            : t('tasks.settings.groupByHost') }}
        </button>
        <button
          class="collapse-button"
          data-test="collapse-button"
          :title="t('tasks.sidebar.collapse')"
          @click="emit('update:collapsed', true)"
        >
          «
        </button>
      </header>
```

Pass the query down to `TaskGroupedList` (around line 191-204). Add `:search-query="query"` to the props:

```vue
        <TaskGroupedList
          :by-host="byHost"
          :primary-state-for-host="primaryStateForHost"
          :completed-seen="completedSeen"
          :group-by="groupByState.activeId.value"
          :by-state="byStateGroups"
          :active-session-id="activeSessionId"
          :open-session-ids="openSessionIds"
          :local-host-id="localHostId"
          :local-host="localHost"
          :search-query="query"
          @open="(s) => emit('open', s)"
          @close="(s) => emit('close', s)"
          @markSeen="(p) => emit('markSeen', p)"
        />
```

Update `.title` CSS (line 258). Before:
```css
.title { flex: 1; font-weight: 500; }
```

After:
```css
.title { flex: 0 0 auto; font-weight: 500; margin-right: 6px; }
```

Add `.sidebar-search` styles at the same style block (after `.group-toggle` around line 278):

```css
.sidebar-search {
  flex: 1 1 auto;
  min-width: 60px;
  background: rgba(255, 255, 255, 0.03);
  border: 1px solid transparent;
  color: inherit;
  border-radius: 3px;
  padding: 1px 6px;
  font-size: 12px;
  font-family: inherit;
  line-height: 20px;
  margin-right: 6px;
  outline: none;
}
.sidebar-search:focus { border-color: var(--border); background: rgba(255, 255, 255, 0.05); }
.sidebar-search::placeholder { opacity: 0.5; }
/* Chromium/WebKit render a native × clear button for type="search"; leave it
   alone (colors match via `color: inherit`). */
```

- [ ] **Step 9: Run TaskSidebar tests to verify they pass**

Run:
```bash
cd desktop/frontend && npm test -- TaskSidebar
```
Expected: all pass (existing + 4 new).

- [ ] **Step 10: Full-suite regression + type-check**

Run:
```bash
cd desktop/frontend && npm test
cd desktop/frontend && npm run build
```
Expected: pass. If any App.vue-level test breaks because `TaskSidebar` prop shape changed, inspect the failure — the added prop is optional-with-default so nothing should regress, but a snapshot test might flag the extra attribute in the DOM.

- [ ] **Step 11: Commit**

```bash
git add desktop/frontend/src/components/TaskSidebar.vue \
        desktop/frontend/src/components/TaskSidebar.test.ts \
        desktop/frontend/src/components/TaskGroupedList.vue \
        desktop/frontend/src/components/TaskGroupedList.test.ts \
        desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "feat(desktop): live-filter session sidebar via header search input"
```

---

### Task 3: `Cmd+F` shortcut to focus the sidebar search

**Files:**
- Modify: `desktop/frontend/src/lib/shortcutBindings.ts` — add action
- Modify: `desktop/frontend/src/composables/useTerminalShortcuts.ts` — extend handlers + dispatch
- Modify: `desktop/frontend/src/composables/useTerminalShortcuts.test.ts` — cover the new action
- Modify: `desktop/frontend/src/App.vue` — template ref + handler
- Modify: `desktop/frontend/src/i18n/messages/en.ts` — `settings.shortcuts.focusSidebarSearch`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts` — same

**Interfaces:**
- Consumes: `TaskSidebar.focusSearch(): Promise<void>` from Task 2.
- Produces: shortcut action id `sidebar.focus-search`, default binding `Mod+KeyF`, handler `onFocusSidebarSearch?: () => void`.

- [ ] **Step 1: Add i18n key for the shortcut label**

In `desktop/frontend/src/i18n/messages/en.ts` inside the `settings.shortcuts` block (around line 293-314), add before the closing `}` of `shortcuts:`:

```ts
      focusSidebarSearch: "Focus sidebar search",
```

In `desktop/frontend/src/i18n/messages/zh-CN.ts` inside its `settings.shortcuts` block:

```ts
      focusSidebarSearch: "聚焦侧栏搜索",
```

- [ ] **Step 2: Write the failing test for the shortcut**

Append inside the existing `describe` in `desktop/frontend/src/composables/useTerminalShortcuts.test.ts`:

```ts
  test("Mod+KeyF dispatches sidebar.focus-search", () => {
    const onFocusSidebarSearch = vi.fn();
    const handlers = makeHandlers({ onFocusSidebarSearch });
    const scope = effectScope();
    scope.run(() => {
      useTerminalShortcuts(handlers, { mod: "Control" });
    });
    const evt = new KeyboardEvent("keydown", {
      code: "KeyF",
      ctrlKey: true,
      bubbles: true,
      cancelable: true,
    });
    document.dispatchEvent(evt);
    expect(onFocusSidebarSearch).toHaveBeenCalledTimes(1);
    expect(evt.defaultPrevented).toBe(true);
    scope.stop();
  });
```

Note: `makeHandlers` is the test's existing helper (grep the file). If it doesn't exist, replicate the pattern of any other test in the file that constructs `handlers` inline.

- [ ] **Step 3: Run to verify failure**

Run:
```bash
cd desktop/frontend && npm test -- useTerminalShortcuts
```
Expected: the new test fails — the handler isn't invoked (the action isn't in the registry yet).

- [ ] **Step 4: Add the action to the registry**

Edit `desktop/frontend/src/lib/shortcutBindings.ts`. Add a new entry to `ACTIONS` (around line 34, after the `toggleTaskSidebar` line):

Before:
```ts
  { id: "toggleTaskSidebar",          group: "sidebar", labelKey: "tasks.sidebar.collapse",                               defaultBinding: "Mod+KeyB" },
] as const;
```

After:
```ts
  { id: "toggleTaskSidebar",          group: "sidebar", labelKey: "tasks.sidebar.collapse",                               defaultBinding: "Mod+KeyB" },
  { id: "sidebar.focus-search",       group: "sidebar", labelKey: "settings.shortcuts.focusSidebarSearch",                defaultBinding: "Mod+KeyF" },
] as const;
```

- [ ] **Step 5: Extend the handlers interface and dispatch table**

Edit `desktop/frontend/src/composables/useTerminalShortcuts.ts`. In `ShortcutHandlers` (line 14-22), add:

Before:
```ts
export interface ShortcutHandlers {
  onSplitVertical: (mode: SplitMode) => void;
  onSplitHorizontal: (mode: SplitMode) => void;
  onClosePane: () => void;
  onFocusPane: (dir: FocusDir) => void;
  onNewTab: () => void;
  onSwitchTab: (delta: number) => void;
  onToggleTaskSidebar?: () => void;
}
```

After:
```ts
export interface ShortcutHandlers {
  onSplitVertical: (mode: SplitMode) => void;
  onSplitHorizontal: (mode: SplitMode) => void;
  onClosePane: () => void;
  onFocusPane: (dir: FocusDir) => void;
  onNewTab: () => void;
  onSwitchTab: (delta: number) => void;
  onToggleTaskSidebar?: () => void;
  onFocusSidebarSearch?: () => void;
}
```

In `dispatch` (line 37-54), add a case before the closing `}`:

Before:
```ts
    case "toggleTaskSidebar":          h.onToggleTaskSidebar?.(); return true;
  }
  return false;
}
```

After:
```ts
    case "toggleTaskSidebar":          h.onToggleTaskSidebar?.(); return true;
    case "sidebar.focus-search":       h.onFocusSidebarSearch?.(); return true;
  }
  return false;
}
```

- [ ] **Step 6: Run the shortcut test to verify pass**

Run:
```bash
cd desktop/frontend && npm test -- useTerminalShortcuts
```
Expected: the new test passes.

- [ ] **Step 7: Wire the handler in App.vue**

Edit `desktop/frontend/src/App.vue`. Find the `<TaskSidebar>` element (around line 1319-1320) and add `ref="taskSidebarRef"`:

Before:
```vue
      <TaskSidebar
        :collapsed="sidebarCollapsed"
```

After:
```vue
      <TaskSidebar
        ref="taskSidebarRef"
        :collapsed="sidebarCollapsed"
```

At setup scope, add the ref declaration near where other component refs live (grep for other `ref<` template refs in the file to find the section). If none exist, add near the top of `<script setup>` after the existing composable calls — around line 140:

```ts
const taskSidebarRef = ref<InstanceType<typeof TaskSidebar> | null>(null);
```

In the `useTerminalShortcuts` call (line 1092-1101), add the new handler. Before:

```ts
useTerminalShortcuts(
  {
    onSplitVertical: (mode) => onSplit("vertical", mode),
    onSplitHorizontal: (mode) => onSplit("horizontal", mode),
    onClosePane,
    onFocusPane: (dir) => focusNeighbor(...args),
    onNewTab: openNewTab,
    onSwitchTab: cycleTab,
    onToggleTaskSidebar: () => setSidebarCollapsedAndPersist(!sidebarCollapsed.value),
  },
  ...
);
```

After — add `onFocusSidebarSearch` calling the exposed method (use `void` to swallow the returned Promise, since the handler signature is sync):

```ts
useTerminalShortcuts(
  {
    onSplitVertical: (mode) => onSplit("vertical", mode),
    onSplitHorizontal: (mode) => onSplit("horizontal", mode),
    onClosePane,
    onFocusPane: (dir) => focusNeighbor(...args),
    onNewTab: openNewTab,
    onSwitchTab: cycleTab,
    onToggleTaskSidebar: () => setSidebarCollapsedAndPersist(!sidebarCollapsed.value),
    onFocusSidebarSearch: () => {
      void taskSidebarRef.value?.focusSearch();
    },
  },
  ...
);
```

Preserve the actual argument shape of `onFocusPane` — the "..." above is a placeholder for whatever's already there; do NOT modify that handler.

- [ ] **Step 8: Full-suite regression + type-check**

Run:
```bash
cd desktop/frontend && npm test
cd desktop/frontend && npm run build
```
Expected: pass. `App.vue`'s template ref typing may need `InstanceType<typeof TaskSidebar>` (as above); if TypeScript complains, that's the fix.

- [ ] **Step 9: Go vet + Go tests (regression only)**

Run:
```bash
cd /Users/attson/code/github.com.attson/atterm && go vet -tags webkit2_41 ./...
cd /Users/attson/code/github.com.attson/atterm && go test -tags webkit2_41 -timeout 60s ./desktop/
```
Expected: pass.

- [ ] **Step 10: Commit**

```bash
git add desktop/frontend/src/lib/shortcutBindings.ts \
        desktop/frontend/src/composables/useTerminalShortcuts.ts \
        desktop/frontend/src/composables/useTerminalShortcuts.test.ts \
        desktop/frontend/src/App.vue \
        desktop/frontend/src/i18n/messages/en.ts \
        desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "feat(desktop): Cmd+F focuses sidebar search input"
```

---

### Task 4: Manual verification

**Files:** none.

- [ ] **Step 1: Build**

```bash
cd /Users/attson/code/github.com.attson/atterm/desktop && wails build -tags webkit2_41
open "build/bin/AT Term.app"
```

- [ ] **Step 2: Basic filtering**

Ensure ≥5 sessions in the sidebar. Type a substring of one session's title into the header input; verify only matching rows remain. Verify count in group headers still reflects the total (or the filtered count — either is acceptable per spec; note which was implemented).

- [ ] **Step 3: Auto-expand folded group**

Collapse a host group. Type a query that matches a session inside it. Verify the group auto-expands and shows the match. Clear the query; verify the group snaps back to collapsed.

- [ ] **Step 4: Empty state**

Type gibberish (`zzzzzzz`). Verify:
- No task rows visible.
- No group headers visible.
- The empty-state hint (`No sessions match "zzzzzzz"` / `搜索 "zzzzzzz" 无匹配会话`) is shown centered.

- [ ] **Step 5: Cmd+F shortcut**

- Sidebar expanded: press `Cmd+F` (`Ctrl+F` on Linux). Input should focus.
- Sidebar collapsed (rail): press `Cmd+F`. Sidebar should expand AND input should focus in one action.
- `Esc` inside the input: query clears; focus stays on the input.

- [ ] **Step 6: Pinned filter**

Pin a session. Verify it appears in the 📌 group. Type a query that does NOT match it. Verify the pinned session disappears from the 📌 group (does NOT get a pin bypass). Clear query → pinned session returns.

- [ ] **Step 7: Cross-language**

Change UI language to Chinese (Settings → General → 语言 → 中文, if that pathway exists — grep for it). Verify placeholder and empty-state hint render in Chinese.

- [ ] **Step 8: Narrow sidebar**

Drag the sidebar to its 180px minimum. Verify all four header elements (`会话` title, search input, `按主机` toggle, `«` button) remain visible and non-overlapping. Input should be usable.

- [ ] **Step 9: CJK query**

Rename or find a session whose title contains Chinese characters. Type Chinese characters into the input; verify substring matching works.

- [ ] **Step 10: Done — no commit**

Verification only. If any step fails, cycle back to the corresponding task.

---

## Rollout

- No relay / protocol / mobile / web changes.
- Ships on `v0.3-dev`; next tag `v0.3.(Z+1)` per AGENTS.md red line #29 — do NOT tag on `main`.

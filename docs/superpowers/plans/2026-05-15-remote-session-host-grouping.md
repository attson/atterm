# Remote session host grouping — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Group sessions by `host_id` (with hostname as the display name) in the three remote-session selection surfaces — desktop `RemoteSessionsDialog`, the remote section of desktop `SessionPickerDialog`, and the web session grid.

**Architecture:** Presentation-only change. A pure helper `groupSessionsByHost(sessions)` returns ordered host groups. Implemented once in TypeScript for the desktop frontend (`desktop/frontend/src/lib/sessions.ts`) and once in plain JS for the web client (added to `web/app-core.js`, the existing pure-function module). The three UIs consume the helper and render one section per group. No Go/backend changes.

**Tech Stack:** Vue 3 + Vite + TypeScript (desktop), vanilla ES modules + `node:test` (web), vitest (desktop tests).

**Spec:** `docs/superpowers/specs/2026-05-15-remote-session-host-grouping-design.md`

---

## File map

- Create: `desktop/frontend/src/lib/sessions.ts` — TS helper + `SessionGroup` type.
- Create: `desktop/frontend/src/lib/sessions.test.ts` — vitest unit tests.
- Modify: `web/app-core.js` — add and export `groupSessionsByHost`.
- Modify: `web/app-core.test.mjs` — extend with `groupSessionsByHost` cases.
- Modify: `desktop/frontend/src/components/RemoteSessionsDialog.vue` — group sessions; drop `.host` from cards; add group-header styles.
- Create: `desktop/frontend/src/components/RemoteSessionsDialog.test.ts` — render assertions.
- Modify: `desktop/frontend/src/components/SessionPickerDialog.vue` — group remote section; replace `user@host` with `user` only.
- Modify: `desktop/frontend/src/components/SessionPickerDialog.test.ts` *if exists, else create* — assert remote section grouping. (No existing test; create one.)
- Modify: `web/app.js` — import `groupSessionsByHost`; rewrite `renderList`.
- Modify: `web/style.css` — add `.host-group` styles; drop now-unused `.card .host *` rules.

---

## Task 1: Desktop helper — `groupSessionsByHost`

**Files:**
- Create: `desktop/frontend/src/lib/sessions.ts`
- Create: `desktop/frontend/src/lib/sessions.test.ts`

The helper buckets sessions by `host_id` (empty → synthetic `__unknown__` bucket), picks a display hostname from the freshest entry in each bucket, sorts buckets lexicographically by hostname, and forces the unknown bucket last.

- [ ] **Step 1: Write the failing test file**

Create `desktop/frontend/src/lib/sessions.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import type { SessionInfo } from "./connection";
import { groupSessionsByHost } from "./sessions";

function makeSession(overrides: Partial<SessionInfo> = {}): SessionInfo {
  return {
    id: "s",
    command: "bash",
    cwd: "/",
    title: "",
    cols: 80,
    rows: 24,
    started_at: 0,
    host_id: "",
    host: "",
    user: "",
    remote_permission: "",
    ...overrides,
  };
}

describe("groupSessionsByHost", () => {
  it("returns [] for empty input", () => {
    expect(groupSessionsByHost([])).toEqual([]);
  });

  it("groups by host_id and sorts groups by hostname ascending", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "a1", host_id: "hidA", host: "mac-mini", started_at: 1 }),
      makeSession({ id: "b1", host_id: "hidB", host: "attson-air", started_at: 1 }),
      makeSession({ id: "a2", host_id: "hidA", host: "mac-mini", started_at: 2 }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups.map((g) => g.hostname)).toEqual(["attson-air", "mac-mini"]);
    expect(groups[0].sessions.map((s) => s.id)).toEqual(["b1"]);
    expect(groups[1].sessions.map((s) => s.id)).toEqual(["a1", "a2"]);
  });

  it("places sessions with empty host_id into a trailing __unknown__ group", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "u1", host_id: "", host: "" }),
      makeSession({ id: "z1", host_id: "hidZ", host: "zeta" }),
      makeSession({ id: "a1", host_id: "hidA", host: "alpha" }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups.map((g) => g.key)).toEqual(["hidA", "hidZ", "__unknown__"]);
    expect(groups[2].hostname).toBe("unknown host");
    expect(groups[2].sessions.map((s) => s.id)).toEqual(["u1"]);
  });

  it("picks display hostname from the entry with the largest started_at", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "old", host_id: "hidX", host: "old-name", started_at: 100 }),
      makeSession({ id: "new", host_id: "hidX", host: "new-name", started_at: 200 }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups).toHaveLength(1);
    expect(groups[0].hostname).toBe("new-name");
  });

  it("falls back to 'unknown host' when host is empty across the bucket", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "x1", host_id: "hidX", host: "" }),
      makeSession({ id: "x2", host_id: "hidX", host: "" }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups).toHaveLength(1);
    expect(groups[0].key).toBe("hidX");
    expect(groups[0].hostname).toBe("unknown host");
  });

  it("collapses every empty-host_id session into a single __unknown__ group", () => {
    const sessions: SessionInfo[] = [
      makeSession({ id: "u1", host_id: "" }),
      makeSession({ id: "u2", host_id: "" }),
    ];
    const groups = groupSessionsByHost(sessions);
    expect(groups).toHaveLength(1);
    expect(groups[0].key).toBe("__unknown__");
    expect(groups[0].sessions.map((s) => s.id)).toEqual(["u1", "u2"]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/lib/sessions.test.ts`
Expected: FAIL — cannot find module `./sessions`.

- [ ] **Step 3: Implement the helper**

Create `desktop/frontend/src/lib/sessions.ts`:

```ts
import type { SessionInfo } from "./connection";

export type SessionGroup = {
  key: string;          // host_id, or "__unknown__"
  hostname: string;     // display name; "unknown host" if no entry has a non-empty host
  hostId: string;       // raw host_id; "" for the unknown group
  sessions: SessionInfo[];
};

const UNKNOWN_KEY = "__unknown__";
const UNKNOWN_HOSTNAME = "unknown host";

export function groupSessionsByHost(sessions: SessionInfo[]): SessionGroup[] {
  const buckets = new Map<string, SessionInfo[]>();
  for (const s of sessions) {
    const key = s.host_id ? s.host_id : UNKNOWN_KEY;
    let bucket = buckets.get(key);
    if (!bucket) {
      bucket = [];
      buckets.set(key, bucket);
    }
    bucket.push(s);
  }

  const groups: SessionGroup[] = [];
  for (const [key, bucket] of buckets) {
    let displayHost = "";
    let bestStartedAt = -Infinity;
    for (const s of bucket) {
      const h = s.host || "";
      if (h && s.started_at >= bestStartedAt) {
        displayHost = h;
        bestStartedAt = s.started_at;
      }
    }
    groups.push({
      key,
      hostname: displayHost || UNKNOWN_HOSTNAME,
      hostId: key === UNKNOWN_KEY ? "" : key,
      sessions: bucket,
    });
  }

  groups.sort((a, b) => {
    if (a.key === UNKNOWN_KEY) return 1;
    if (b.key === UNKNOWN_KEY) return -1;
    return a.hostname.localeCompare(b.hostname);
  });

  return groups;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/lib/sessions.test.ts`
Expected: PASS — 6 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/sessions.ts desktop/frontend/src/lib/sessions.test.ts
git commit -m "feat(desktop): add groupSessionsByHost helper"
```

---

## Task 2: Web helper — `groupSessionsByHost`

**Files:**
- Modify: `web/app-core.js`
- Modify: `web/app-core.test.mjs`

Mirror the desktop helper in plain JS as a new exported function. Add test cases to the existing `node:test` suite.

- [ ] **Step 1: Write the failing test cases**

In `web/app-core.test.mjs`, add `groupSessionsByHost` to the existing import block (alphabetical):

```js
import {
  apiURL,
  buildDownloadURL,
  canRegisterServiceWorker,
  detectClientMode,
  formatReplayProgress,
  formatHost,
  groupSessionsByHost,
  isIOSWebKit,
  shouldAutoScrollToBottom,
  shouldShowInstallHint,
  tokenURLWithoutSecret,
  relayBaseURLFromLocation,
  parseSessionRoute,
  persistInsecureMode,
  normalizeRelayBaseURL,
  insecureModeFromStorage,
  shortcutInput,
  tokenFromLocation,
  versionLabel,
  webSocketAuth,
} from "./app-core.js";
```

Append at the end of the file:

```js
function makeSession(overrides = {}) {
  return {
    id: "s",
    command: "bash",
    cwd: "/",
    title: "",
    cols: 80,
    rows: 24,
    started_at: 0,
    host_id: "",
    host: "",
    user: "",
    remote_permission: "",
    ...overrides,
  };
}

test("groupSessionsByHost returns [] for empty input", () => {
  assert.deepEqual(groupSessionsByHost([]), []);
});

test("groupSessionsByHost groups by host_id and sorts by hostname ascending", () => {
  const sessions = [
    makeSession({ id: "a1", host_id: "hidA", host: "mac-mini", started_at: 1 }),
    makeSession({ id: "b1", host_id: "hidB", host: "attson-air", started_at: 1 }),
    makeSession({ id: "a2", host_id: "hidA", host: "mac-mini", started_at: 2 }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.deepEqual(groups.map((g) => g.hostname), ["attson-air", "mac-mini"]);
  assert.deepEqual(groups[0].sessions.map((s) => s.id), ["b1"]);
  assert.deepEqual(groups[1].sessions.map((s) => s.id), ["a1", "a2"]);
});

test("groupSessionsByHost places empty host_id sessions into trailing __unknown__ group", () => {
  const sessions = [
    makeSession({ id: "u1", host_id: "", host: "" }),
    makeSession({ id: "z1", host_id: "hidZ", host: "zeta" }),
    makeSession({ id: "a1", host_id: "hidA", host: "alpha" }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.deepEqual(groups.map((g) => g.key), ["hidA", "hidZ", "__unknown__"]);
  assert.equal(groups[2].hostname, "unknown host");
  assert.deepEqual(groups[2].sessions.map((s) => s.id), ["u1"]);
});

test("groupSessionsByHost picks display hostname from the freshest started_at", () => {
  const sessions = [
    makeSession({ id: "old", host_id: "hidX", host: "old-name", started_at: 100 }),
    makeSession({ id: "new", host_id: "hidX", host: "new-name", started_at: 200 }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].hostname, "new-name");
});

test("groupSessionsByHost falls back to 'unknown host' when host is empty across the bucket", () => {
  const sessions = [
    makeSession({ id: "x1", host_id: "hidX", host: "" }),
    makeSession({ id: "x2", host_id: "hidX", host: "" }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].key, "hidX");
  assert.equal(groups[0].hostname, "unknown host");
});

test("groupSessionsByHost collapses every empty-host_id session into one __unknown__ group", () => {
  const sessions = [
    makeSession({ id: "u1", host_id: "" }),
    makeSession({ id: "u2", host_id: "" }),
  ];
  const groups = groupSessionsByHost(sessions);
  assert.equal(groups.length, 1);
  assert.equal(groups[0].key, "__unknown__");
  assert.deepEqual(groups[0].sessions.map((s) => s.id), ["u1", "u2"]);
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && node --test app-core.test.mjs`
Expected: FAIL — `groupSessionsByHost` is not exported from `app-core.js`.

- [ ] **Step 3: Implement the helper in `web/app-core.js`**

Append to `web/app-core.js` (after the last export):

```js
const HOST_GROUP_UNKNOWN_KEY = "__unknown__";
const HOST_GROUP_UNKNOWN_HOSTNAME = "unknown host";

export function groupSessionsByHost(sessions) {
  const buckets = new Map();
  for (const s of sessions) {
    const key = s.host_id ? s.host_id : HOST_GROUP_UNKNOWN_KEY;
    let bucket = buckets.get(key);
    if (!bucket) {
      bucket = [];
      buckets.set(key, bucket);
    }
    bucket.push(s);
  }

  const groups = [];
  for (const [key, bucket] of buckets) {
    let displayHost = "";
    let bestStartedAt = -Infinity;
    for (const s of bucket) {
      const h = s.host || "";
      if (h && s.started_at >= bestStartedAt) {
        displayHost = h;
        bestStartedAt = s.started_at;
      }
    }
    groups.push({
      key,
      hostname: displayHost || HOST_GROUP_UNKNOWN_HOSTNAME,
      hostId: key === HOST_GROUP_UNKNOWN_KEY ? "" : key,
      sessions: bucket,
    });
  }

  groups.sort((a, b) => {
    if (a.key === HOST_GROUP_UNKNOWN_KEY) return 1;
    if (b.key === HOST_GROUP_UNKNOWN_KEY) return -1;
    return a.hostname.localeCompare(b.hostname);
  });

  return groups;
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && node --test app-core.test.mjs`
Expected: PASS — all existing tests + 6 new tests pass.

- [ ] **Step 5: Commit**

```bash
git add web/app-core.js web/app-core.test.mjs
git commit -m "feat(web): add groupSessionsByHost helper"
```

---

## Task 3: Desktop — `RemoteSessionsDialog` grouped layout

**Files:**
- Modify: `desktop/frontend/src/components/RemoteSessionsDialog.vue`
- Create: `desktop/frontend/src/components/RemoteSessionsDialog.test.ts`

Convert the single grid into one section per host group. Drop the per-card `.host` block (host info now lives in the group header). Add styles for `.host-group` and its header.

- [ ] **Step 1: Write the failing component test**

Create `desktop/frontend/src/components/RemoteSessionsDialog.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";

import RemoteSessionsDialog from "./RemoteSessionsDialog.vue";
import type { SessionInfo } from "../lib/connection";

function s(overrides: Partial<SessionInfo>): SessionInfo {
  return {
    id: "s",
    command: "bash",
    cwd: "/",
    title: "",
    cols: 80,
    rows: 24,
    started_at: 0,
    host_id: "",
    host: "",
    user: "",
    remote_permission: "",
    ...overrides,
  };
}

describe("RemoteSessionsDialog", () => {
  test("renders one host-group section per host_id in hostname order", () => {
    const sessions = [
      s({ id: "a", host_id: "hidB", host: "mac-mini", started_at: 1 }),
      s({ id: "b", host_id: "hidA", host: "attson-air", started_at: 1 }),
      s({ id: "c", host_id: "hidB", host: "mac-mini", started_at: 2 }),
    ];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    const groups = wrapper.findAll(".host-group");
    expect(groups).toHaveLength(2);
    expect(groups[0].find("header .hostname").text()).toBe("attson-air");
    expect(groups[1].find("header .hostname").text()).toBe("mac-mini");
  });

  test("header shows short host_id, full host_id in title, and session count", () => {
    const sessions = [
      s({ id: "a", host_id: "3f9a2c1d11112222", host: "mac-mini" }),
      s({ id: "b", host_id: "3f9a2c1d11112222", host: "mac-mini" }),
    ];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    const header = wrapper.get(".host-group header");
    const hostid = header.get(".hostid");
    expect(hostid.text()).toBe("3f9a2c1d");
    expect(hostid.attributes("title")).toBe("host_id 3f9a2c1d11112222");
    expect(header.get(".count").text()).toBe("2 sessions");
  });

  test("singular count for groups with exactly one session", () => {
    const sessions = [s({ id: "a", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    expect(wrapper.get(".host-group header .count").text()).toBe("1 session");
  });

  test("unknown host group renders without a host_id chip", () => {
    const sessions = [s({ id: "u", host_id: "", host: "" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    const header = wrapper.get(".host-group header");
    expect(header.get(".hostname").text()).toBe("unknown host");
    expect(header.find(".hostid").exists()).toBe(false);
  });

  test("cards no longer render the per-card host line", () => {
    const sessions = [s({ id: "a", host_id: "hidA", host: "alpha", user: "alice" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    expect(wrapper.find(".card .host").exists()).toBe(false);
  });

  test("clicking a card emits 'open' with the session id", () => {
    const sessions = [s({ id: "abc", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions } });
    wrapper.get(".card").trigger("click");
    expect(wrapper.emitted("open")).toEqual([["abc"]]);
  });

  test("shows existing empty state when sessions is empty", () => {
    const wrapper = mount(RemoteSessionsDialog, { props: { sessions: [] } });
    expect(wrapper.find(".host-group").exists()).toBe(false);
    expect(wrapper.find(".empty").exists()).toBe(true);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/components/RemoteSessionsDialog.test.ts`
Expected: FAIL — `.host-group` not found / cards still render `.host`.

- [ ] **Step 3: Rewrite `RemoteSessionsDialog.vue`**

Replace the entire file with:

```vue
<script lang="ts" setup>
import { computed } from "vue";

import type { SessionInfo } from "../lib/connection";
import { groupSessionsByHost } from "../lib/sessions";

const props = defineProps<{
  sessions: SessionInfo[];
}>();

const emit = defineEmits<{
  (e: "open", sessionId: string): void;
  (e: "close"): void;
}>();

const groups = computed(() => groupSessionsByHost(props.sessions));
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="dialog">
      <h2>remote sessions</h2>

      <div v-if="sessions.length === 0" class="empty">
        no remote sessions visible. start one in another AT Term app connected
        to the same relay.
      </div>

      <div v-else class="groups">
        <section v-for="g in groups" :key="g.key" class="host-group">
          <header>
            <span class="hostname">{{ g.hostname }}</span>
            <span
              v-if="g.hostId"
              class="hostid"
              :title="'host_id ' + g.hostId"
            >{{ g.hostId.slice(0, 8) }}</span>
            <span class="count">{{ g.sessions.length }} {{ g.sessions.length === 1 ? 'session' : 'sessions' }}</span>
          </header>
          <div class="grid">
            <div
              v-for="s in g.sessions"
              :key="s.id"
              class="card"
              @click="emit('open', s.id)"
            >
              <div class="cmd">{{ s.command || "(unknown)" }}</div>
              <div class="meta">
                <span class="id">{{ s.id.slice(0, 8) }}</span>
                <span class="size">{{ s.cols }}×{{ s.rows }}</span>
                <span class="cwd">{{ s.cwd }}</span>
              </div>
            </div>
          </div>
        </section>
      </div>

      <div class="row">
        <button @click="emit('close')">close</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed;
  inset: 0;
  background: rgba(0, 0, 0, 0.6);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 100;
}
.dialog {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 20px 24px;
  width: 720px;
  max-width: calc(100vw - 32px);
  max-height: calc(100vh - 64px);
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.dialog h2 {
  margin: 0;
  font-size: 14px;
  font-weight: 600;
  letter-spacing: 0.05em;
  text-transform: uppercase;
  color: var(--fg-dim);
}
.empty {
  color: var(--fg-dim);
  font-size: 13px;
  text-align: center;
  padding: 40px 0;
}
.groups {
  display: flex;
  flex-direction: column;
  gap: 14px;
  overflow-y: auto;
  max-height: 50vh;
}
.host-group > header {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  padding: 4px 0 6px;
}
.host-group > header .hostname { color: var(--fg); }
.host-group > header .hostid { color: var(--fg-dim); font-size: 11px; cursor: help; }
.host-group > header .count { color: var(--fg-dim); font-size: 11px; margin-left: auto; }
.host-group > .grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px;
}
.card {
  background: #0d1117;
  border: 1px solid var(--border);
  border-radius: 6px;
  padding: 10px 12px;
  cursor: pointer;
  transition: border-color 120ms;
}
.card:hover { border-color: var(--accent); }
.card .cmd {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px;
  color: var(--fg);
  margin-bottom: 4px;
  word-break: break-all;
}
.card .meta {
  font-size: 11px;
  color: var(--fg-dim);
  display: flex;
  gap: 10px;
  flex-wrap: wrap;
}
.card .meta .id { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.card .meta .cwd { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
.row {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 4px;
}
</style>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/components/RemoteSessionsDialog.test.ts`
Expected: PASS — 7 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/RemoteSessionsDialog.vue desktop/frontend/src/components/RemoteSessionsDialog.test.ts
git commit -m "feat(desktop): group RemoteSessionsDialog by host"
```

---

## Task 4: Desktop — `SessionPickerDialog` remote section grouped

**Files:**
- Modify: `desktop/frontend/src/components/SessionPickerDialog.vue`
- Create: `desktop/frontend/src/components/SessionPickerDialog.test.ts`

The `local` section keeps its flat grid. The `remote` section becomes a list of host groups using the same header treatment as Task 3. Each remote card's "who" meta line shows `user` only (omitted when empty).

- [ ] **Step 1: Write the failing component test**

Create `desktop/frontend/src/components/SessionPickerDialog.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { mount } from "@vue/test-utils";

import SessionPickerDialog from "./SessionPickerDialog.vue";
import type { SessionInfo } from "../lib/connection";

function s(overrides: Partial<SessionInfo>): SessionInfo {
  return {
    id: "s",
    command: "bash",
    cwd: "/",
    title: "",
    cols: 80,
    rows: 24,
    started_at: 0,
    host_id: "",
    host: "",
    user: "",
    remote_permission: "",
    ...overrides,
  };
}

describe("SessionPickerDialog", () => {
  test("local section remains flat (no host groups)", () => {
    const local = [s({ id: "L1", cwd: "/home/me/work" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: local, remoteSessions: [] },
    });
    const localSection = wrapper.get("section.local");
    expect(localSection.find(".host-group").exists()).toBe(false);
    expect(localSection.findAll(".card")).toHaveLength(1);
  });

  test("remote section renders one host-group per host_id", () => {
    const remote = [
      s({ id: "r1", host_id: "hidA", host: "alpha", user: "alice" }),
      s({ id: "r2", host_id: "hidB", host: "beta", user: "bob" }),
      s({ id: "r3", host_id: "hidA", host: "alpha", user: "alice" }),
    ];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: [], remoteSessions: remote },
    });
    const remoteSection = wrapper.get("section.remote");
    const groups = remoteSection.findAll(".host-group");
    expect(groups).toHaveLength(2);
    expect(groups[0].get("header .hostname").text()).toBe("alpha");
    expect(groups[1].get("header .hostname").text()).toBe("beta");
  });

  test("remote card meta shows user only (not user@host)", () => {
    const remote = [s({ id: "r1", host_id: "hidA", host: "alpha", user: "alice" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: [], remoteSessions: remote },
    });
    const who = wrapper.get("section.remote .card .who");
    expect(who.text()).toBe("alice");
    expect(who.text()).not.toContain("@");
  });

  test("remote card hides .who span when user is empty", () => {
    const remote = [s({ id: "r1", host_id: "hidA", host: "alpha", user: "" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: [], remoteSessions: remote },
    });
    expect(wrapper.find("section.remote .card .who").exists()).toBe(false);
  });

  test("excludeSessionIds filters out matching sessions from both sections", () => {
    const local = [s({ id: "L1" })];
    const remote = [s({ id: "r1", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: ["L1", "r1"], localSessions: local, remoteSessions: remote },
    });
    expect(wrapper.find(".empty").exists()).toBe(true);
    expect(wrapper.find(".host-group").exists()).toBe(false);
  });

  test("clicking a remote card emits pick with remote:true", () => {
    const remote = [s({ id: "rid", host_id: "hidA", host: "alpha" })];
    const wrapper = mount(SessionPickerDialog, {
      props: { excludeSessionIds: [], localSessions: [], remoteSessions: remote },
    });
    wrapper.get("section.remote .card").trigger("click");
    expect(wrapper.emitted("pick")).toEqual([[{ sessionId: "rid", remote: true }]]);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd desktop/frontend && npx vitest run src/components/SessionPickerDialog.test.ts`
Expected: FAIL — `section.local`/`section.remote` classes missing, no `.host-group`.

- [ ] **Step 3: Rewrite `SessionPickerDialog.vue`**

Replace the entire file with:

```vue
<script lang="ts" setup>
import { computed, onMounted, onBeforeUnmount } from "vue";

import type { SessionInfo } from "../lib/connection";
import { groupSessionsByHost } from "../lib/sessions";

const props = defineProps<{
  excludeSessionIds: string[];
  localSessions: SessionInfo[];
  remoteSessions: SessionInfo[];
}>();

const emit = defineEmits<{
  (e: "pick", payload: { sessionId: string; remote: boolean }): void;
  (e: "close"): void;
}>();

const exclude = computed(() => new Set(props.excludeSessionIds));
const localOptions = computed(() =>
  props.localSessions.filter((s) => !exclude.value.has(s.id)),
);
const remoteOptions = computed(() =>
  props.remoteSessions.filter((s) => !exclude.value.has(s.id)),
);
const remoteGroups = computed(() => groupSessionsByHost(remoteOptions.value));

function onEsc(e: KeyboardEvent) {
  if (e.key === "Escape") emit("close");
}
onMounted(() => document.addEventListener("keydown", onEsc));
onBeforeUnmount(() => document.removeEventListener("keydown", onEsc));

function shortTitle(s: SessionInfo): string {
  if (s.cwd) {
    const stripped = s.cwd.replace(/\/+$/, "");
    if (stripped !== "") return stripped.split("/").pop() || stripped;
  }
  const first = (s.command || "").split(/\s+/)[0] || "shell";
  return first.split("/").pop() || first;
}
</script>

<template>
  <div class="backdrop" @click.self="emit('close')">
    <div class="dialog">
      <h2>pick a session</h2>

      <div v-if="localOptions.length + remoteOptions.length === 0" class="empty">
        no sessions available — none running locally and no eligible remote.
      </div>

      <template v-else>
        <section v-if="localOptions.length > 0" class="local">
          <h3>local</h3>
          <div class="grid">
            <button
              v-for="s in localOptions"
              :key="s.id"
              class="card"
              @click="emit('pick', { sessionId: s.id, remote: false })"
            >
              <div class="title">{{ shortTitle(s) }}</div>
              <div class="meta">
                <span class="cmd">{{ s.command || "(unknown)" }}</span>
                <span class="cwd">{{ s.cwd }}</span>
              </div>
            </button>
          </div>
        </section>

        <section v-if="remoteOptions.length > 0" class="remote">
          <h3>remote</h3>
          <section
            v-for="g in remoteGroups"
            :key="g.key"
            class="host-group"
          >
            <header>
              <span class="hostname">{{ g.hostname }}</span>
              <span
                v-if="g.hostId"
                class="hostid"
                :title="'host_id ' + g.hostId"
              >{{ g.hostId.slice(0, 8) }}</span>
              <span class="count">{{ g.sessions.length }} {{ g.sessions.length === 1 ? 'session' : 'sessions' }}</span>
            </header>
            <div class="grid">
              <button
                v-for="s in g.sessions"
                :key="s.id"
                class="card remote"
                @click="emit('pick', { sessionId: s.id, remote: true })"
              >
                <div class="title">{{ shortTitle(s) }}</div>
                <div class="meta">
                  <span class="cmd">{{ s.command || "(unknown)" }}</span>
                  <span class="cwd">{{ s.cwd }}</span>
                  <span v-if="s.user" class="who">{{ s.user }}</span>
                </div>
              </button>
            </div>
          </section>
        </section>
      </template>

      <div class="row">
        <button class="cancel" @click="emit('close')">cancel (esc)</button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.backdrop {
  position: fixed; inset: 0; background: rgba(0, 0, 0, 0.6);
  display: flex; align-items: center; justify-content: center; z-index: 100;
}
.dialog {
  background: var(--panel); border: 1px solid var(--border);
  border-radius: 8px; padding: 20px 24px; width: 720px;
  max-width: calc(100vw - 32px); max-height: calc(100vh - 64px);
  display: flex; flex-direction: column; gap: 12px;
}
h2 {
  margin: 0; font-size: 14px; font-weight: 600; letter-spacing: 0.05em;
  text-transform: uppercase; color: var(--fg-dim);
}
h3 {
  margin: 12px 0 6px; font-size: 11px; letter-spacing: 0.08em;
  text-transform: uppercase; color: var(--fg-dim);
}
.empty {
  color: var(--fg-dim); font-size: 13px; text-align: center; padding: 40px 0;
}
.host-group { margin-bottom: 10px; }
.host-group > header {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  padding: 2px 0 6px;
}
.host-group > header .hostname { color: var(--fg); }
.host-group > header .hostid { color: var(--fg-dim); font-size: 11px; cursor: help; }
.host-group > header .count { color: var(--fg-dim); font-size: 11px; margin-left: auto; }
.grid {
  display: grid; grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  gap: 10px; max-height: 30vh; overflow-y: auto;
}
.card {
  text-align: left; background: #0d1117; border: 1px solid var(--border);
  border-radius: 6px; padding: 10px 12px; cursor: pointer;
  transition: border-color 120ms; color: var(--fg);
  font-family: inherit;
}
.card:hover { border-color: var(--accent); }
.card.remote .title { color: #d29922; }
.card .title {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 13px; margin-bottom: 4px;
}
.card .meta {
  font-size: 11px; color: var(--fg-dim);
  display: flex; gap: 10px; flex-wrap: wrap;
}
.card .meta .cwd { white-space: nowrap; overflow: hidden; text-overflow: ellipsis; max-width: 100%; }
.row {
  display: flex; justify-content: flex-end; gap: 8px; margin-top: 4px;
}
.cancel {
  padding: 6px 12px; background: transparent; border: 1px solid var(--border);
  color: var(--fg-dim); border-radius: 4px; cursor: pointer;
}
.cancel:hover { color: var(--fg); border-color: var(--accent); }
</style>
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd desktop/frontend && npx vitest run src/components/SessionPickerDialog.test.ts`
Expected: PASS — 6 tests pass.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SessionPickerDialog.vue desktop/frontend/src/components/SessionPickerDialog.test.ts
git commit -m "feat(desktop): group SessionPickerDialog remote section by host"
```

---

## Task 5: Web — grouped `renderList` + styles

**Files:**
- Modify: `web/app.js`
- Modify: `web/style.css`

Wire `groupSessionsByHost` into `renderList`. Render one `<section class="host-group">` per group with a header and the existing card grid inside. Drop the per-card `.host` block. Add CSS for `.host-group` and its header; remove unused `.card .host *` rules.

- [ ] **Step 1: Update imports in `web/app.js`**

In the import block (currently `import { apiURL as makeAPIURL, … } from "./app-core.js"`), add `groupSessionsByHost,` alphabetically (between `formatHost,` and `insecureModeFromStorage,`):

```js
import {
  apiURL as makeAPIURL,
  arrayBufferToBase64,
  canRegisterServiceWorker,
  copyTerminalSelection,
  detectClientMode,
  formatReplayProgress,
  formatHost,
  groupSessionsByHost,
  insecureModeFromStorage,
  isTerminalCopyShortcut,
  parseSessionRoute,
  persistInsecureMode,
  persistRelayBaseURL,
  persistToken,
  relayBaseURLFromLocation,
  replayProgressPercent,
  sessionTitle,
  shouldShowInstallHint,
  shouldAutoScrollToBottom,
  shortSessionID,
  shortcutInput,
  tokenFromLocation,
  tokenURLWithoutSecret,
  versionLabel,
  webSocketAuth as makeWebSocketAuth,
} from "./app-core.js";
```

(Verify `formatHost` may no longer be needed after Step 2 — leave it in for now; if Step 2's final code does not reference it, remove it before committing.)

- [ ] **Step 2: Rewrite `renderList` in `web/app.js`**

Replace the existing `renderList` function (currently `function renderList(sessions) { ... }` at app.js:284) with:

```js
function renderList(sessions) {
  listEl.innerHTML = "";
  if (sessions.length === 0) {
    emptyEl.hidden = false;
    return;
  }
  emptyEl.hidden = true;
  const groups = groupSessionsByHost(sessions);
  for (const g of groups) {
    const section = document.createElement("section");
    section.className = "host-group";

    const header = document.createElement("header");
    const hostnameSpan = document.createElement("span");
    hostnameSpan.className = "hostname";
    hostnameSpan.textContent = g.hostname;
    header.appendChild(hostnameSpan);
    if (g.hostId) {
      const hostidSpan = document.createElement("span");
      hostidSpan.className = "hostid";
      hostidSpan.textContent = g.hostId.slice(0, 8);
      hostidSpan.title = "host_id " + g.hostId;
      header.appendChild(hostidSpan);
    }
    const countSpan = document.createElement("span");
    countSpan.className = "count";
    countSpan.textContent =
      g.sessions.length + " " + (g.sessions.length === 1 ? "session" : "sessions");
    header.appendChild(countSpan);
    section.appendChild(header);

    const grid = document.createElement("div");
    grid.className = "grid";
    for (const s of g.sessions) {
      const card = document.createElement("button");
      card.type = "button";
      card.className = "card";
      card.innerHTML = `
        <div class="cmd"></div>
        <div class="meta">
          <span class="id"></span>
          <span class="size"></span>
          <span class="cwd"></span>
        </div>`;
      card.querySelector(".cmd").textContent = s.command || "(unknown)";
      card.querySelector(".id").textContent = shortSessionID(s.id);
      card.querySelector(".size").textContent = `${s.cols}×${s.rows}`;
      card.querySelector(".cwd").textContent = s.cwd || "";
      card.addEventListener("click", () => {
        location.hash = "#/s/" + s.id;
      });
      grid.appendChild(card);
    }
    section.appendChild(grid);
    listEl.appendChild(section);
  }
}
```

If `formatHost` is no longer used anywhere in `app.js` after this change, remove it from the import block as well. (Use `grep formatHost web/app.js` to confirm before deletion.)

- [ ] **Step 3: Update `web/style.css`**

Replace the three existing `.card .host*` rules (currently at style.css:159-161):

```css
.card .host { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; margin-bottom: 8px; display: flex; align-items: baseline; gap: 8px; }
.card .host .who { color: var(--accent-2); }
.card .host .hostid { color: var(--fg-dim); font-size: 11px; }
```

with the new group styles:

```css
.host-group { margin-bottom: 18px; }
.host-group > header {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  padding: 4px 2px 8px;
}
.host-group > header .hostname { color: var(--fg); }
.host-group > header .hostid { color: var(--fg-dim); font-size: 11px; cursor: help; }
.host-group > header .count { color: var(--fg-dim); font-size: 11px; margin-left: auto; }
.host-group > .grid { display: grid; grid-template-columns: 1fr; gap: 12px; }
```

`#list` continues to be the outer container (`#list { display: grid; grid-template-columns: 1fr; gap: 12px; }` at style.css:144 stays — sections now stack as its grid items).

- [ ] **Step 4: Manually verify in the web app**

Build/serve the relay (the web client is served by the Go relay; the standard way to run locally is via the repo's run instructions — check `README.md` if unsure). Open the web client. With at least two `atterm` instances reporting to the relay (real or via fake `host_id`):

- Two host groups visible, headers correct (hostname, host_id chip, count).
- Cards no longer display the host line; cmd/id/size/cwd intact.
- Clicking a card still navigates to `#/s/<id>`.
- Stop one instance → its group disappears on refresh.

Report what was verified in a one-line note in the commit body.

- [ ] **Step 5: Run web tests to ensure nothing else broke**

Run: `cd web && node --test`
Expected: all existing tests + the 6 from Task 2 pass.

- [ ] **Step 6: Commit**

```bash
git add web/app.js web/style.css
git commit -m "feat(web): group session list by host"
```

---

## Task 6: Cross-cutting verification

**Files:** none (build + manual check)

- [ ] **Step 1: Build the desktop frontend to catch type errors**

Run: `cd desktop/frontend && npm run build`
Expected: build succeeds (vue-tsc + vite). If `vue-tsc` reports errors, fix them in the offending file and re-run.

- [ ] **Step 2: Run all desktop unit tests**

Run: `cd desktop/frontend && npm test`
Expected: every test passes — including pre-existing ones.

- [ ] **Step 3: Run all web tests**

Run: `cd web && node --test`
Expected: every test passes.

- [ ] **Step 4: Manual desktop verification**

Launch the desktop app (`wails dev` from `desktop/` per the project's README) with at least one local session and at least two remote sessions reporting different `host_id` values. Open:
- The remote sessions dialog: confirm groups in alphabetical order, headers correct, cards minus the host line.
- The session picker (the dialog that shows local + remote): confirm local stays flat, remote is grouped, remote cards show `user` only (no `@hostname`).

If a remote session is missing `host_id` (rare — older clients), confirm it lands in an `unknown host` group at the bottom.

- [ ] **Step 5: Commit any follow-up fixes from manual testing**

If manual verification surfaced a bug, fix it on the same branch with a focused commit (`fix(desktop): ...` or `fix(web): ...`). If no follow-up is needed, this step is a no-op.

---

# SSH Panel Search Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make host and key lists searchable through visible fields and safe type-to-search keyboard routing.

**Architecture:** Keep matching rules in a small pure helper and let `SshHostsPanel.vue` own the two independent query refs and focus behavior. Search spans all host groups when non-empty, while an empty host query preserves the existing group-navigation semantics.

**Tech Stack:** Vue 3 Composition API, TypeScript, Vitest, Vue Test Utils, lucide-vue-next.

**Spec:** `docs/superpowers/specs/2026-11-28-sftp-workspace-design.md`

## Global Constraints

- Hosts match alias, host, user, tags, and group path case-insensitively.
- Keys match name and key type case-insensitively.
- Type-to-search must not capture input, textarea, select, contenteditable, Meta/Ctrl/Alt shortcuts, or drawer/dialog input.
- Hosts, keys, and tunnels keep independent query state.
- All new visible copy must exist in both `en.ts` and `zh-CN.ts`.

---

### Task 1: Pure SSH list matching

**Files:**
- Create: `desktop/frontend/src/lib/sshListSearch.ts`
- Create: `desktop/frontend/src/lib/sshListSearch.test.ts`
- Modify: `desktop/frontend/src/components/SshHostsPanel.vue`

**Interfaces:**
- Consumes: `SSHHost` and `SSHKey` from `desktop/frontend/src/lib/api/ssh.ts`.
- Produces: `matchesSSHHost(host, query)` and `matchesSSHKey(key, query)`.

- [ ] **Step 1: Write failing matching tests**

```ts
import { describe, expect, it } from "vitest";
import { matchesSSHHost, matchesSSHKey } from "./sshListSearch";

describe("sshListSearch", () => {
  it("matches a host across identity, tag and group fields", () => {
    const host = {
      id: "h1", alias: "Production API", host: "10.0.0.8", user: "deploy",
      auth_kind: "key", tags: ["urgent"], group_path: ["Cloud", "AWS"],
    } as const;
    expect(matchesSSHHost(host, " production ")).toBe(true);
    expect(matchesSSHHost(host, "AWS")).toBe(true);
    expect(matchesSSHHost(host, "deploy")).toBe(true);
    expect(matchesSSHHost(host, "missing")).toBe(false);
  });

  it("matches keys by name and type", () => {
    const key = { id: "k1", name: "GitHub Attson", key_type: "ED25519" };
    expect(matchesSSHKey(key, " github ")).toBe(true);
    expect(matchesSSHKey(key, "ed25519")).toBe(true);
    expect(matchesSSHKey(key, "rsa")).toBe(false);
  });
});
```

- [ ] **Step 2: Run the test and verify RED**

Run: `cd desktop/frontend && npm test -- src/lib/sshListSearch.test.ts`

Expected: FAIL because `sshListSearch.ts` does not exist.

- [ ] **Step 3: Implement the pure matchers**

```ts
import type { SSHHost, SSHKey } from "./api/ssh";

function q(raw: string): string { return raw.trim().toLocaleLowerCase(); }

export function matchesSSHHost(host: SSHHost, raw: string): boolean {
  const needle = q(raw);
  if (!needle) return true;
  return [host.alias, host.host, host.user, ...(host.tags ?? []), ...(host.group_path ?? [])]
    .filter(Boolean)
    .join("\n")
    .toLocaleLowerCase()
    .includes(needle);
}

export function matchesSSHKey(key: SSHKey, raw: string): boolean {
  const needle = q(raw);
  if (!needle) return true;
  return `${key.name}\n${key.key_type ?? ""}`.toLocaleLowerCase().includes(needle);
}
```

Change `filteredHosts` so group-path filtering runs only when the trimmed query is empty; use `matchesSSHHost` for non-empty queries. Add `keyQuery` and `filteredKeys` using `matchesSSHKey`.

- [ ] **Step 4: Run matcher and panel tests**

Run: `cd desktop/frontend && npm test -- src/lib/sshListSearch.test.ts src/components/SshHostsPanel.test.ts`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/lib/sshListSearch.ts desktop/frontend/src/lib/sshListSearch.test.ts desktop/frontend/src/components/SshHostsPanel.vue
git commit -m "feat(ssh): add host and key list matching"
```

### Task 2: Visible key search and safe type-to-search

**Files:**
- Modify: `desktop/frontend/src/components/SshHostsPanel.vue`
- Modify: `desktop/frontend/src/components/SshHostsPanel.test.ts`
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

**Interfaces:**
- Consumes: `filteredHosts` and `filteredKeys` from Task 1.
- Produces: `data-test="ssh-key-search"`, root key routing, localized key no-match state.

- [ ] **Step 1: Write failing component tests**

Add tests that mount hosts and keys, then assert:

```ts
await wrapper.trigger("keydown", { key: "a" });
expect((wrapper.get('[data-test="ssh-search"]').element as HTMLInputElement).value).toBe("a");
expect(document.activeElement).toBe(wrapper.get('[data-test="ssh-search"]').element);

await wrapper.get('[data-test="ssh-tab-keys"]').trigger("click");
await wrapper.trigger("keydown", { key: "E" });
expect((wrapper.get('[data-test="ssh-key-search"]').element as HTMLInputElement).value).toBe("E");
expect(wrapper.findAll(".key-card")).toHaveLength(1);
```

Add a form-safety case:

```ts
await wrapper.get('[data-test="ssh-key-new"]').trigger("click");
const name = wrapper.get('[data-test="ssh-key-name"]');
await name.setValue("private");
await name.trigger("keydown", { key: "x" });
expect((wrapper.get('[data-test="ssh-key-search"]').element as HTMLInputElement).value).toBe("");
```

Add a host case proving a non-empty query finds a host outside `selectedGroupPath`, an Escape case
that clears only the active tab's search, and a drawer-button case proving printable keys do not route
to search while any host/key/import/tunnel drawer or TOFU dialog is open.

- [ ] **Step 2: Run the panel test and verify RED**

Run: `cd desktop/frontend && npm test -- src/components/SshHostsPanel.test.ts`

Expected: FAIL because the key search field and root type-to-search behavior are absent.

- [ ] **Step 3: Implement keyboard routing and key search UI**

Add refs and routing:

```ts
const hostSearchInput = ref<HTMLInputElement | null>(null);
const keySearchInput = ref<HTMLInputElement | null>(null);

function onPanelKeydown(event: KeyboardEvent) {
  if (activeTab.value !== "hosts" && activeTab.value !== "keys") return;
  if (hostDrawer.value || keyDrawer.value || configImportDrawer.value || termiusDrawer.value || forwardDrawer.value || hostKey.value) return;
  if (event.metaKey || event.ctrlKey || event.altKey || event.isComposing) return;
  const target = event.target as HTMLElement | null;
  if (target?.matches("input, textarea, select, [contenteditable='true']")) return;
  if (event.key === "Escape") {
    (activeTab.value === "hosts" ? query : keyQuery).value = "";
    return;
  }
  if (event.key.length !== 1) return;
  const model = activeTab.value === "hosts" ? query : keyQuery;
  model.value += event.key;
  event.preventDefault();
  void nextTick(() => {
    const input = activeTab.value === "hosts" ? hostSearchInput.value : keySearchInput.value;
    input?.focus();
    input?.setSelectionRange(input.value.length, input.value.length);
  });
}
```

Bind `@keydown="onPanelKeydown"` to `.ssh-shell`, render search inputs for both hosts and keys, loop over `filteredKeys`, and render a localized no-match empty state. Use the existing `Search` icon and `.search` styles.

Add i18n keys:

```ts
keyFilterPlaceholder: "Search keys…",
keys: { noMatchTitle: "No matching keys", noMatchSub: "Try a different name or key type." }
```

```ts
keyFilterPlaceholder: "搜索密钥…",
keys: { noMatchTitle: "没有匹配的密钥", noMatchSub: "尝试其他名称或密钥类型。" }
```

- [ ] **Step 4: Run focused tests and typecheck**

Run: `cd desktop/frontend && npm test -- src/lib/sshListSearch.test.ts src/components/SshHostsPanel.test.ts`

Run: `cd desktop/frontend && npx vue-tsc --noEmit`

Expected: both commands PASS.

- [ ] **Step 5: Commit**

```bash
git add desktop/frontend/src/components/SshHostsPanel.vue desktop/frontend/src/components/SshHostsPanel.test.ts desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "feat(ssh): add key search and type-to-search"
```

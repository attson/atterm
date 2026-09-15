# Startup Update Prompt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** On desktop startup, before entering the first session, pop a dedicated update dialog when an update is detected; otherwise boot silently as today.

**Architecture:** A new leaf component `StartupUpdateDialog.vue` (modeled on `RecoveryDialog.vue`, reusing existing update bindings) is opened exactly once by a new boot gate in `App.vue`. The current recovery-or-startNewTab branch is extracted into `resumeBoot()` so the gate can defer it until the user dismisses the dialog. No Go backend changes — all bindings already exist.

**Tech Stack:** Vue 3 `<script setup>` + TypeScript, Wails bindings via `./lib/api`, Vitest (`?raw` source-assertion component tests, matching repo convention).

## Global Constraints

- Desktop (Wails) only. Gate on `caps.wailsBindings && caps.autoUpdate`. Web/Capacitor unchanged (`caps.autoUpdate === false`).
- **Hard invariant:** the startup update dialog opens ONLY from the one-time boot auto-start block, guarded by `autoStarted` + `tabs.length === 0`. It MUST NOT be opened by the 5s `getUpdateState()` poll (which only toggles the `updateBadge` dot) or by the background 24h check. `startupUpdateOpen.value = true` must appear exactly once in `App.vue`.
- Download-ready does NOT auto-install; the user must click "Install & Restart".
- Boot update check is bounded by `BOOT_UPDATE_CHECK_TIMEOUT_MS = 3000`; on timeout/error, boot proceeds and the ⚙ badge covers the late case.
- New user-visible copy must be added to BOTH `desktop/frontend/src/i18n/messages/en.ts` and `zh-CN.ts` (English + Chinese in sync).
- No Go changes. Do not modify web/mobile.
- Run tests from `desktop/frontend/` with `npm run test`.

---

### Task 1: i18n keys for the startup update dialog

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts` (add `startupUpdate` group; insert just before the `recovery:` group ~line 1117)
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts` (add the parallel `startupUpdate` group at the matching location)
- Test: `desktop/frontend/src/i18n/messages/startupUpdate-keys.test.ts` (new)

**Interfaces:**
- Produces: i18n keys `startupUpdate.title`, `startupUpdate.currentToLatest` (params `{current, latest}`), `startupUpdate.releaseNotes`, `startupUpdate.statusAvailable` (param `{version}`), `startupUpdate.statusDownloading` (params `{version, pct}`), `startupUpdate.statusReady` (param `{version}`), `startupUpdate.downloadInstall`, `startupUpdate.cancel` (param `{pct}`), `startupUpdate.cancelling`, `startupUpdate.installRestart`, `startupUpdate.later`.

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/i18n/messages/startupUpdate-keys.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import { en } from "./en";
import { zhCN } from "./zh-CN";

const KEYS = [
  "title",
  "currentToLatest",
  "releaseNotes",
  "statusAvailable",
  "statusDownloading",
  "statusReady",
  "downloadInstall",
  "cancel",
  "cancelling",
  "installRestart",
  "later",
] as const;

describe("startupUpdate i18n", () => {
  test("en has every startupUpdate key", () => {
    for (const k of KEYS) {
      expect((en as any).startupUpdate?.[k], `en.startupUpdate.${k}`).toBeTruthy();
    }
  });
  test("zh-CN mirrors every startupUpdate key", () => {
    for (const k of KEYS) {
      expect((zhCN as any).startupUpdate?.[k], `zh-CN.startupUpdate.${k}`).toBeTruthy();
    }
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npm run test -- src/i18n/messages/startupUpdate-keys.test.ts`
Expected: FAIL — `en.startupUpdate.title` is undefined.

- [ ] **Step 3: Add the English group**

In `desktop/frontend/src/i18n/messages/en.ts`, immediately before the `recovery: {` line (~1117), insert:

```ts
  startupUpdate: {
    title: "Update available",
    currentToLatest: "{current} → {latest}",
    releaseNotes: "release notes",
    statusAvailable: "{version} is available",
    statusDownloading: "downloading {version} ({pct}%)",
    statusReady: "{version} downloaded — ready to install",
    downloadInstall: "Download & install",
    cancel: "Cancel ({pct}%)",
    cancelling: "Cancelling…",
    installRestart: "Install & restart",
    later: "Later",
  },
```

- [ ] **Step 4: Add the Chinese group**

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, at the matching location (immediately before its `recovery: {` group), insert:

```ts
  startupUpdate: {
    title: "有可用更新",
    currentToLatest: "{current} → {latest}",
    releaseNotes: "更新说明",
    statusAvailable: "{version} 可用",
    statusDownloading: "正在下载 {version}（{pct}%）",
    statusReady: "{version} 已下载 — 可安装",
    downloadInstall: "下载并安装",
    cancel: "取消（{pct}%）",
    cancelling: "正在取消…",
    installRestart: "安装并重启",
    later: "稍后",
  },
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd desktop/frontend && npm run test -- src/i18n/messages/startupUpdate-keys.test.ts`
Expected: PASS (both tests).

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts desktop/frontend/src/i18n/messages/startupUpdate-keys.test.ts
git commit -m "i18n: add startupUpdate keys (en + zh-CN)"
```

---

### Task 2: `StartupUpdateDialog.vue` component

**Files:**
- Create: `desktop/frontend/src/components/StartupUpdateDialog.vue`
- Test: `desktop/frontend/src/components/StartupUpdateDialog.test.ts` (new)

**Interfaces:**
- Consumes: `getUpdateState`, `startDownload`, `cancelDownload`, `installUpdate`, `type UpdateState` from `../lib/api`; `useI18n` from `../i18n/useI18n`; the `startupUpdate.*` keys from Task 1.
- Produces: a component emitting `(e: "dismiss"): void`. No props. Parent (`App.vue`, Task 3) renders it with `v-if` and handles `@dismiss`. The install path is internal (`installUpdate()` quits the app), so no parent action is needed for install.

- [ ] **Step 1: Write the failing test**

Create `desktop/frontend/src/components/StartupUpdateDialog.test.ts`:

```ts
import { describe, expect, test } from "vitest";
import source from "./StartupUpdateDialog.vue?raw";

describe("StartupUpdateDialog", () => {
  test("emits dismiss on Later", () => {
    expect(source).toMatch(/\(e:\s*"dismiss"\)\s*:\s*void/);
    expect(source).toContain('@click="emit(\'dismiss\')"');
    expect(source).toContain("startupUpdate.later");
  });

  test("polls update state and clears the interval on unmount", () => {
    expect(source).toContain("getUpdateState");
    expect(source).toContain("setInterval");
    expect(source).toContain("onBeforeUnmount");
    expect(source).toContain("clearInterval");
  });

  test("wires download, cancel, and install bindings", () => {
    expect(source).toContain("startDownload");
    expect(source).toContain("cancelDownload");
    expect(source).toContain("installUpdate");
  });

  test("shows install button only when ready, download button otherwise", () => {
    expect(source).toContain("startupUpdate.installRestart");
    expect(source).toContain("startupUpdate.downloadInstall");
    expect(source).toContain("state?.ready");
    expect(source).toContain("state?.downloading");
  });

  test("renders release notes and current→latest header", () => {
    expect(source).toContain("startupUpdate.title");
    expect(source).toContain("startupUpdate.currentToLatest");
    expect(source).toContain("startupUpdate.releaseNotes");
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npm run test -- src/components/StartupUpdateDialog.test.ts`
Expected: FAIL — cannot resolve `./StartupUpdateDialog.vue`.

- [ ] **Step 3: Create the component**

Create `desktop/frontend/src/components/StartupUpdateDialog.vue`:

```vue
<script lang="ts" setup>
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
import {
  cancelDownload,
  getUpdateState,
  installUpdate,
  startDownload,
  type UpdateState,
} from "../lib/api";
import { useI18n } from "../i18n/useI18n";

const emit = defineEmits<{ (e: "dismiss"): void }>();

const { t } = useI18n();

const state = ref<UpdateState | null>(null);
const clickInFlight = ref(false);
const cancelling = ref(false);
let pollHandle: number | null = null;

onMounted(async () => {
  try {
    state.value = await getUpdateState();
  } catch {
    /* poll below refreshes; never trap boot on an updater failure */
  }
  pollHandle = window.setInterval(async () => {
    try {
      state.value = await getUpdateState();
    } catch {
      /* ignore — the ⚙ badge already surfaces updater health */
    }
  }, 2000);
});

onBeforeUnmount(() => {
  if (pollHandle !== null) window.clearInterval(pollHandle);
});

const statusLine = computed(() => {
  const st = state.value;
  if (!st) return "";
  if (st.error) return st.error;
  if (st.ready) return t("startupUpdate.statusReady", { version: st.latest });
  if (st.downloading)
    return t("startupUpdate.statusDownloading", { version: st.latest, pct: st.download_pct });
  return t("startupUpdate.statusAvailable", { version: st.latest });
});

async function onDownload() {
  clickInFlight.value = true;
  try {
    await startDownload();
  } catch {
    /* state.error reflects in poll */
  } finally {
    clickInFlight.value = false;
  }
}

async function onCancel() {
  cancelling.value = true;
  try {
    await cancelDownload();
  } catch {
    /* poll surfaces error */
  } finally {
    cancelling.value = false;
  }
}

async function onInstall() {
  try {
    await installUpdate();
  } catch {
    /* state.error reflects in poll */
  }
}
</script>

<template>
  <div class="startup-update-backdrop" role="dialog" aria-modal="true">
    <div class="startup-update-dialog">
      <header>
        <h2>{{ t("startupUpdate.title") }}</h2>
        <p class="subtitle">
          {{ t("startupUpdate.currentToLatest", { current: state?.current ?? "", latest: state?.latest ?? "" }) }}
        </p>
      </header>

      <p class="status">{{ statusLine }}</p>

      <details v-if="state?.notes" class="notes">
        <summary>{{ t("startupUpdate.releaseNotes") }}</summary>
        <pre>{{ state.notes }}</pre>
      </details>

      <footer>
        <button
          data-testid="startup-update-later"
          class="btn-secondary"
          @click="emit('dismiss')"
        >{{ t("startupUpdate.later") }}</button>
        <button
          v-if="state?.ready"
          data-testid="startup-update-install"
          class="btn-primary"
          @click="onInstall"
        >{{ t("startupUpdate.installRestart") }}</button>
        <button
          v-else-if="state?.downloading"
          data-testid="startup-update-cancel"
          class="btn-primary danger"
          :disabled="cancelling"
          @click="onCancel"
        >{{ cancelling ? t("startupUpdate.cancelling") : t("startupUpdate.cancel", { pct: state?.download_pct ?? 0 }) }}</button>
        <button
          v-else
          data-testid="startup-update-download"
          class="btn-primary"
          :disabled="clickInFlight"
          @click="onDownload"
        >{{ t("startupUpdate.downloadInstall") }}</button>
      </footer>
    </div>
  </div>
</template>

<style scoped>
.startup-update-backdrop {
  position: fixed; inset: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex; align-items: center; justify-content: center;
  z-index: 210;
}
.startup-update-dialog {
  background: var(--bg, #0d1117);
  color: var(--fg, #d1d5db);
  border-radius: 8px;
  min-width: 420px; max-width: 640px; max-height: 80vh;
  overflow: hidden; display: flex; flex-direction: column;
  padding: 16px 20px;
  gap: 12px;
}
.startup-update-dialog header { display: flex; flex-direction: column; gap: 4px; }
.startup-update-dialog h2 { font-size: 1.1rem; margin: 0; }
.subtitle { font-size: 0.85rem; opacity: 0.7; margin: 0; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
.status { font-size: 0.85rem; margin: 0; }
.notes { font-size: 12px; }
.notes summary { opacity: 0.7; cursor: pointer; }
.notes pre {
  background: var(--bg); border: 1px solid var(--border, #444);
  padding: 8px; border-radius: 6px; white-space: pre-wrap; word-break: break-word;
  max-height: 160px; overflow-y: auto; font-size: 11px; margin: 6px 0 0;
}
footer { display: flex; justify-content: flex-end; gap: 8px; }
.btn-primary {
  background: var(--accent, #2563eb); color: #0d1117; border: 0;
  padding: 6px 12px; border-radius: 4px; cursor: pointer; font-weight: 600;
}
.btn-primary.danger { background: var(--bad, #ef4444); }
.btn-primary:disabled { opacity: 0.5; cursor: default; }
.btn-secondary {
  background: transparent; color: inherit; border: 1px solid var(--border, #444);
  padding: 6px 12px; border-radius: 4px; cursor: pointer;
}
</style>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd desktop/frontend && npm run test -- src/components/StartupUpdateDialog.test.ts`
Expected: PASS (5 tests).

- [ ] **Step 5: Typecheck the component**

Run: `cd desktop/frontend && npx vue-tsc --noEmit -p tsconfig.json`
Expected: no errors referencing `StartupUpdateDialog.vue`. (If the repo has no `vue-tsc` script, skip — the `?raw` test plus Task 3's mount coverage is sufficient.)

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/components/StartupUpdateDialog.vue desktop/frontend/src/components/StartupUpdateDialog.test.ts
git commit -m "feat(desktop): add StartupUpdateDialog component"
```

---

### Task 3: Boot gate + wiring in `App.vue`

**Files:**
- Modify: `desktop/frontend/src/App.vue`
  - imports (~line 40–68): add `checkUpdate`, `getAutoCheckUpdates`
  - component import (~line 16): add `StartupUpdateDialog`
  - component-scope state/functions (near the `useRecoveryRestore` block ~line 457)
  - boot auto-start block (~line 1714–1736)
  - template (after `<RecoveryDialog …/>` ~line 1935)
- Test: `desktop/frontend/src/App.test.ts` (add a `describe` block)

**Interfaces:**
- Consumes: `StartupUpdateDialog` (Task 2, emits `dismiss`); `checkUpdate`, `getAutoCheckUpdates`, `getUpdateState` from `./lib/api`; existing `caps`, `recoveryDialogState` (from `useRecoveryRestore`), `startNewTab`, `webTabs`.
- Produces: nothing consumed by later tasks (terminal task).

- [ ] **Step 1: Write the failing test**

Append to `desktop/frontend/src/App.test.ts` (uses the existing `source` import = `./App.vue?raw`):

```ts
describe("startup update gate", () => {
  test("gate is desktop-only and respects the auto-check preference", () => {
    expect(source).toContain("caps.autoUpdate");
    expect(source).toContain("getAutoCheckUpdates()");
    expect(source).toContain("checkForUpdateBounded");
    expect(source).toContain("BOOT_UPDATE_CHECK_TIMEOUT_MS");
  });

  test("bounded check races checkUpdate against a timeout", () => {
    expect(source).toContain("Promise.race");
    expect(source).toContain("checkUpdate()");
  });

  test("recovery/auto-start is extracted into resumeBoot and deferred by the dialog", () => {
    expect(source).toContain("function resumeBoot()");
    expect(source).toContain("function onStartupUpdateDismiss()");
    expect(source).toMatch(/onStartupUpdateDismiss[\s\S]{0,80}resumeBoot\(\)/);
  });

  test("INVARIANT: the dialog is opened in exactly one place (the boot gate)", () => {
    const opens = source.match(/startupUpdateOpen\.value\s*=\s*true/g) ?? [];
    expect(opens.length).toBe(1);
  });

  test("INVARIANT: the 5s update poll only toggles the badge, never the dialog", () => {
    // The poll interval body sets updateBadge and must not reference the dialog flag.
    const poll = source.match(/updatePollHandle\s*=\s*window\.setInterval\(([\s\S]*?)\},\s*5000\)/)?.[1] ?? "";
    expect(poll).toContain("updateBadge");
    expect(poll).not.toContain("startupUpdateOpen");
  });

  test("template renders StartupUpdateDialog gated on startupUpdateOpen", () => {
    expect(source).toContain("<StartupUpdateDialog");
    expect(source).toContain('v-if="startupUpdateOpen"');
    expect(source).toContain('@dismiss="onStartupUpdateDismiss"');
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd desktop/frontend && npm run test -- src/App.test.ts -t "startup update gate"`
Expected: FAIL — `checkForUpdateBounded` / `resumeBoot` / `<StartupUpdateDialog` not present.

- [ ] **Step 3: Add imports**

In `App.vue`, add the component import near line 16 (after the `RecoveryDialog` import):

```ts
import StartupUpdateDialog from "./components/StartupUpdateDialog.vue";
```

In the `./lib/api` import block (lines 40–68), add `checkUpdate,` and `getAutoCheckUpdates,` alongside the existing `getUpdateState,`:

```ts
  getUpdateState,
  checkUpdate,
  getAutoCheckUpdates,
```

- [ ] **Step 4: Add component-scope state and functions**

In `App.vue`, right after the `useRecoveryRestore({ … })` block (~line 457, where `recoveryDialogState` is destructured), add:

```ts
// Startup update gate (see docs/superpowers/specs/2026-09-15-startup-update-prompt-design.md).
// The dialog opens EXACTLY ONCE from the boot auto-start block below — never from
// the 5s update poll or the 24h background check (those only feed the ⚙ badge).
const startupUpdateOpen = ref(false);
const bootRecoverySnap = ref<RecoverySnapshot | null>(null);
const bootRecoveryEnabled = ref(true);
const BOOT_UPDATE_CHECK_TIMEOUT_MS = 3000;

// resumeBoot performs the original recovery-or-startNewTab decision. It is called
// either immediately (no update) or after the user dismisses the update dialog.
function resumeBoot() {
  const snap = bootRecoverySnap.value;
  const hasRecovery = bootRecoveryEnabled.value && (snap?.tabs?.length ?? 0) > 0;
  if (hasRecovery && snap) {
    recoveryDialogState.value = { open: true, snapshot: snap };
  } else if (caps.localPty) {
    startNewTab();
  }
  // else: no recovery snapshot and no local PTY (web build) — render empty state.
}

function onStartupUpdateDismiss() {
  startupUpdateOpen.value = false;
  resumeBoot();
}

// checkForUpdateBounded forces a check but never blocks boot longer than `ms`.
// Returns true only when state.available is set within the budget; any
// timeout/error returns false so boot proceeds and the ⚙ badge covers the rest.
async function checkForUpdateBounded(ms: number): Promise<boolean> {
  try {
    const timeout = new Promise<void>((resolve) => window.setTimeout(resolve, ms));
    await Promise.race([checkUpdate().catch(() => {}), timeout]);
    const st = await getUpdateState();
    return !!st.available;
  } catch {
    return false;
  }
}
```

Note: `caps`, `recoveryDialogState`, `startNewTab`, `ref`, and `RecoverySnapshot` are already in scope in `App.vue`.

- [ ] **Step 5: Rewire the boot auto-start block**

In `App.vue`, replace the inner body of the `if (!webTabs.tryRestoreOnBoot()) { … }` branch (currently ~lines 1722–1735, the `const hasRecovery … recoveryDialogState.value = … else if (caps.localPty) startNewTab()` block) with:

```ts
      if (!webTabs.tryRestoreOnBoot()) {
        // Snapshot the boot-local recovery decision so resumeBoot() (and the
        // dialog's dismiss handler) can reach it after an async gate.
        bootRecoverySnap.value = recoverySnap;
        bootRecoveryEnabled.value = recoveryEnabled;

        // Startup update gate (Wails only, and only if auto-check is on). If an
        // update is available within the time budget, show the dialog and defer
        // resumeBoot() until dismiss. Otherwise fall straight through.
        if (caps.wailsBindings && caps.autoUpdate && (await getAutoCheckUpdates())) {
          if (await checkForUpdateBounded(BOOT_UPDATE_CHECK_TIMEOUT_MS)) {
            startupUpdateOpen.value = true;
            return; // onStartupUpdateDismiss() → resumeBoot()
          }
        }
        resumeBoot();
      }
```

(The surrounding `if (!autoStarted && tabs.value.length === 0) { autoStarted = true; … }` guard and the enclosing `try { … } catch` stay exactly as they are. The `return` exits the `onMounted` callback cleanly.)

- [ ] **Step 6: Add the dialog to the template**

In `App.vue`, immediately after the closing `</RecoveryDialog>`/`/>` (~line 1935), add:

```html
    <StartupUpdateDialog
      v-if="startupUpdateOpen"
      @dismiss="onStartupUpdateDismiss"
    />
```

- [ ] **Step 7: Run the gate tests**

Run: `cd desktop/frontend && npm run test -- src/App.test.ts -t "startup update gate"`
Expected: PASS (6 tests), including both INVARIANT tests.

- [ ] **Step 8: Run the full frontend suite**

Run: `cd desktop/frontend && npm run test`
Expected: PASS — existing `App.test.ts`, `SettingsUpdates.test.ts`, `RecoveryDialog.test.ts`, and i18n tests all green.

- [ ] **Step 9: Commit**

```bash
git add desktop/frontend/src/App.vue desktop/frontend/src/App.test.ts
git commit -m "feat(desktop): startup update gate before entering session"
```

---

## Manual verification (after all tasks)

> Note: per repo memory, this project lives on a `/Volumes` mount where wails/vite dev file-watching is unreliable — fully restart `wails dev` and confirm you are running the new code before verifying.

1. Build/run a **non-dev** desktop build (dev builds short-circuit the updater: `current == "dev"`). Point the updater at a release where a newer version exists.
2. Launch the app:
   - **Update available:** the `StartupUpdateDialog` appears before any session/tab is created. "Download & install" shows progress, then "Install & restart"; "Later" dismisses and the app proceeds to recovery/new-tab, with the ⚙ badge dot still lit.
   - **No update / offline:** no dialog; the app enters a session as before (within ~3s at most if the network hangs).
3. **Invariant check:** with the app already running in a session, leave it idle past an update-state refresh — confirm the dialog never pops; only the ⚙ badge reflects availability.
4. Toggle Settings → Updates → "automatically check for updates" OFF, relaunch: confirm the startup dialog does not appear.

## Self-Review

- **Spec coverage:** new dialog (Task 2) ✓; boot gate before session (Task 3 Step 5) ✓; desktop-only gating `caps.wailsBindings && caps.autoUpdate` (Task 3 Step 5) ✓; bounded 3s check (Task 3 Step 4) ✓; ready-does-not-auto-install (Task 2 template: install only via button) ✓; update dialog precedes recovery (Task 3: `return` before `resumeBoot`, dismiss → `resumeBoot` → recovery) ✓; i18n en+zh (Task 1) ✓; hard invariant enforced by test (Task 3 Step 1 INVARIANT tests) ✓; respect auto-check preference (Task 3 Step 5 `getAutoCheckUpdates()`) ✓; no Go changes ✓.
- **Placeholder scan:** none — all steps carry real code/commands.
- **Type consistency:** `dismiss` emit, `startupUpdateOpen`, `resumeBoot`, `onStartupUpdateDismiss`, `checkForUpdateBounded`, `BOOT_UPDATE_CHECK_TIMEOUT_MS`, `bootRecoverySnap`, `bootRecoveryEnabled` used identically across component and App tasks; bindings (`checkUpdate`, `getUpdateState`, `getAutoCheckUpdates`, `startDownload`, `cancelDownload`, `installUpdate`) match `lib/api/updates.ts` exactly.

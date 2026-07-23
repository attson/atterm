# Pinned Session Recovery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** After a desktop-app restart, sessions that were pinned to the top of the sidebar stay pinned — currently the pin is lost for local (respawned) panes because they get a new `session_id`.

**Architecture:** Route `session_id` from the previous run into `useRecoverySnapshot`'s output for local panes too (currently only remote panes carry it), then in `executeRestore` migrate the pin set atomically: when a spawned local pane's old sid was pinned, replace it with the newly returned sid. Remote panes are unaffected — their sids survive the restart naturally.

**Tech Stack:** Vue 3 + TypeScript + Vitest. Wails Go bindings unchanged.

## Global Constraints

- Language: user-facing prose 中文; code, commits, i18n keys stay English (memory `feedback_reply_language.md`).
- No backward-compat shims for recovery.json (memory `feedback_no_backward_compat.md`): schema fields are already `omitempty`, so old snapshots load fine — no migration needed.
- No new frontend dependencies (AGENTS.md §style).
- Pin key remains `session_id` (spec §2 non-goals — do NOT switch to (tab, slot) or content signatures).
- `useSessionPins` state is module-scoped and reactive via `pinnedIds.value = new Set(...)`; NEVER mutate the Set in place — Vue's shallow-ref detection misses same-instance mutation (see existing comment in `useSessionPins.ts`).
- Tests run from `desktop/frontend/`; command is `npm test`. Type-check + build is `npm run build`.
- Commit style: lowercase verb start, ≤72 char subject, one logical change per commit (AGENTS.md §style).

Spec: [`docs/superpowers/specs/2026-07-23-pinned-session-recovery-design.md`](../specs/2026-07-23-pinned-session-recovery-design.md)

## File Overview

- Modify: `desktop/frontend/src/composables/useSessionPins.ts` — add `rename(old, new)` and `flushNow()`, expose in `UseSessionPins` interface.
- Modify: `desktop/frontend/src/composables/useSessionPins.test.ts` — cover the new methods + rapid-rename debounce.
- Modify: `desktop/frontend/src/composables/useRecoverySnapshot.ts` — write `session_id` for every pane that has one (drop the `persistAsRemote` gate on that specific field).
- Modify: `desktop/frontend/src/composables/useRecoverySnapshot.test.ts` — replace the "omits session_id for local panes" regression test with the new expected behavior, keep the remote regression test.
- Modify: `desktop/frontend/src/App.vue::executeRestore` — capture `snap.session_id` before spawn, migrate the pin set after spawn, `await pins.flushNow()` before returning.

Go side (`desktop/config.go`, `desktop/app.go::Get/SetPinnedSessionIds`, `desktop/recovery_types.go`) is **untouched** — the `session_id` field is already declared with `omitempty` in `PaneSnapshot` (`desktop/recovery_types.go:47`), so writing it more often is purely additive on the wire.

---

### Task 1: Extend `useSessionPins` with `rename` + `flushNow`

**Files:**
- Modify: `desktop/frontend/src/composables/useSessionPins.ts` — add methods, extend interface
- Modify: `desktop/frontend/src/composables/useSessionPins.test.ts` — new tests

**Interfaces:**
- Consumes: existing `pinnedIds: Ref<Set<string>>`, `schedulePersist()`, `setPinnedSessionIds()` from `../lib/api`
- Produces:
  - `rename(oldId: string, newId: string): void` — replaces `oldId` with `newId` in the pin set; no-op if oldId isn't pinned, if either id is empty, or if they're equal. Schedules debounced persist.
  - `flushNow(): Promise<void>` — cancels the pending debounce and awaits `setPinnedSessionIds(Array.from(pinnedIds.value))` immediately. Called by `executeRestore` in Task 3 so the sidebar's first repaint after recovery sees the new pin ids already persisted.

- [ ] **Step 1: Write failing tests for `rename`**

Append to `desktop/frontend/src/composables/useSessionPins.test.ts` (inside the existing `describe("useSessionPins", ...)` block, before the closing `});`):

```ts
  test("rename replaces old id with new id and schedules one persist", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a", "b"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("a", "a2");
    expect(pins.isPinned("a")).toBe(false);
    expect(pins.isPinned("a2")).toBe(true);
    expect(pins.isPinned("b")).toBe(true);
    expect(setSpy).not.toHaveBeenCalled();

    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(new Set(setSpy.mock.calls[0][0])).toEqual(new Set(["a2", "b"]));
  });

  test("rename is a no-op when old id is not pinned", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("x", "y");
    expect(pins.isPinned("a")).toBe(true);
    expect(pins.isPinned("y")).toBe(false);
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).not.toHaveBeenCalled();
  });

  test("rename(x, x) is a no-op", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("a", "a");
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).not.toHaveBeenCalled();
    expect(pins.isPinned("a")).toBe(true);
  });

  test("rename with empty ids is a no-op", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("", "b");
    pins.rename("a", "");
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).not.toHaveBeenCalled();
    expect(pins.isPinned("a")).toBe(true);
  });

  test("rename onto an already-pinned id is idempotent (Set dedup)", async () => {
    vi.spyOn(api, "getPinnedSessionIds").mockResolvedValue(["a", "b"]);
    const setSpy = vi
      .spyOn(api, "setPinnedSessionIds")
      .mockResolvedValue(undefined);
    let pins!: ReturnType<typeof useSessionPins>;
    scope.run(() => {
      pins = useSessionPins();
    });
    await flushPromises();

    pins.rename("a", "b");
    expect(pins.isPinned("a")).toBe(false);
    expect(pins.isPinned("b")).toBe(true);
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(setSpy.mock.calls[0][0]).toEqual(["b"]);
  });

  test("flushNow cancels the debounce and persists immediately", async () => {
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
    pins.rename("a", "b");
    expect(setSpy).not.toHaveBeenCalled();
    await pins.flushNow();
    expect(setSpy).toHaveBeenCalledTimes(1);
    expect(setSpy.mock.calls[0][0]).toEqual(["b"]);

    // No stale timer fires after flushNow (would double-persist).
    vi.advanceTimersByTime(300);
    await flushPromises();
    expect(setSpy).toHaveBeenCalledTimes(1);
  });
```

- [ ] **Step 2: Run tests to verify they fail**

Run:
```bash
cd desktop/frontend && npm test -- useSessionPins
```
Expected: fail with `pins.rename is not a function` / `pins.flushNow is not a function`.

- [ ] **Step 3: Implement `rename` and `flushNow`**

Edit `desktop/frontend/src/composables/useSessionPins.ts`:

Add `renameFn` and `flushNowFn` after `unpinFn` (around line 55):

```ts
function renameFn(oldId: string, newId: string): void {
  if (!oldId || !newId || oldId === newId) return;
  if (!pinnedIds.value.has(oldId)) return;
  const next = new Set(pinnedIds.value);
  next.delete(oldId);
  next.add(newId);
  pinnedIds.value = next;
  schedulePersist();
}

async function flushNowFn(): Promise<void> {
  if (flushHandle) {
    clearTimeout(flushHandle);
    flushHandle = null;
  }
  try {
    await setPinnedSessionIds(Array.from(pinnedIds.value));
  } catch {
    /* best-effort — same policy as schedulePersist */
  }
}
```

Extend the `UseSessionPins` interface:

```ts
export interface UseSessionPins {
  pinnedIds: Readonly<Ref<ReadonlySet<string>>>;
  isPinned: (id: string) => boolean;
  pin: (id: string) => void;
  unpin: (id: string) => void;
  toggle: (id: string) => void;
  rename: (oldId: string, newId: string) => void;
  flushNow: () => Promise<void>;
}
```

Wire them into the returned object in `useSessionPins()`:

```ts
export function useSessionPins(): UseSessionPins {
  void loadOnce();
  return {
    pinnedIds,
    isPinned: (id) => pinnedIds.value.has(id),
    pin: pinFn,
    unpin: unpinFn,
    toggle: (id) => (pinnedIds.value.has(id) ? unpinFn(id) : pinFn(id)),
    rename: renameFn,
    flushNow: flushNowFn,
  };
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd desktop/frontend && npm test -- useSessionPins
```
Expected: all pin tests pass (5 existing + 6 new = 11).

- [ ] **Step 5: Type-check**

Run:
```bash
cd desktop/frontend && npm run build
```
Expected: build succeeds, no TS errors.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/composables/useSessionPins.ts \
        desktop/frontend/src/composables/useSessionPins.test.ts
git commit -m "feat(desktop): add rename+flushNow to useSessionPins for pin migration"
```

---

### Task 2: Persist `session_id` for local panes in recovery snapshot

**Files:**
- Modify: `desktop/frontend/src/composables/useRecoverySnapshot.ts:60-83` — drop the `persistAsRemote` gate on the `session_id` field
- Modify: `desktop/frontend/src/composables/useRecoverySnapshot.test.ts` — flip the "omits session_id for local panes" assertion, add coverage for the new local-with-sid case

**Interfaces:**
- Consumes: existing `RecoveryPaneSnapshot` (Go-side `PaneSnapshot`); the `session_id` field is already declared with `omitempty` at `desktop/recovery_types.go:47`, so the wire shape is backward-compatible.
- Produces (semantic change): `panes[].session_id` is now written for every pane that has a `sessionId`. The `remote` flag still discriminates usage — `remote: true` → sid is authoritative for rebind; `remote: false/undefined` → sid is a previous-generation id, only meaningful to Task 3's pin migration.

- [ ] **Step 1: Update the existing "omits session_id for local panes" test to assert the new behavior**

In `desktop/frontend/src/composables/useRecoverySnapshot.test.ts:344-386`, replace the block:

```ts
  it("omits remote/host_id/session_id for local panes (keeps snapshot lean)", async () => {
    ...
    expect(pane.remote).toBeUndefined();
    expect(pane.session_id).toBeUndefined();
    expect(pane.host_id).toBeUndefined();
    scope.stop();
  });
```

with:

```ts
  it("writes session_id for local panes (needed for pin migration) but keeps remote/host_id undefined", async () => {
    // Regression: pin state is keyed by session_id. On restart the local pane
    // gets a fresh sid from newSession(); executeRestore needs the previous
    // generation's sid to remap the pin set. See
    // docs/superpowers/specs/2026-07-23-pinned-session-recovery-design.md §4.1.
    const tabs = ref<Tab[]>([]);
    const currentTabId = ref<string | null>(null);
    const sessionInfoFor = (sid: string) =>
      sid === "local-sid"
        ? {
            id: sid,
            command: "zsh",
            cwd: "/tmp",
            title: "tmp",
            cols: 80,
            rows: 24,
            started_at: 0,
            host_id: "local-host",
          }
        : undefined;

    const scope = effectScope();
    scope.run(() => {
      useRecoverySnapshot({ tabs, currentTabId, sessionInfoFor, localHostID: ref(""), onEvent: () => () => {} });
    });

    tabs.value.push({
      id: "t1",
      layout: "single",
      panes: [{ sessionId: "local-sid", remote: false }],
      activePaneIdx: 0,
      colRatio: 0.5,
      rowRatio: 0.5,
    });
    await nextTick();
    vi.advanceTimersByTime(600);
    await Promise.resolve();

    const calls = (api.saveRecoverySnapshot as any).mock.calls;
    expect(calls.length).toBeGreaterThan(0);
    const last = calls[calls.length - 1][0];
    const pane = last.tabs[0].panes[0];
    expect(pane.session_id).toBe("local-sid");
    expect(pane.remote).toBeUndefined();
    expect(pane.host_id).toBeUndefined();
    scope.stop();
  });
```

- [ ] **Step 2: Run tests to verify the updated test fails**

Run:
```bash
cd desktop/frontend && npm test -- useRecoverySnapshot
```
Expected: the "writes session_id for local panes" test fails at `expect(pane.session_id).toBe("local-sid")` — value is `undefined` under current implementation. All other tests still pass.

- [ ] **Step 3: Update `buildSnapshot` to always write `session_id` when present**

In `desktop/frontend/src/composables/useRecoverySnapshot.ts`, change the `session_id` assignment (line ~73):

Before:
```ts
          session_id: persistAsRemote && p.sessionId ? p.sessionId : undefined,
```

After:
```ts
          // Written for both remote (authoritative id used to rebind by
          // executeRestore) and local (previous-generation id, used only by
          // executeRestore's pin migration to keep pinned sessions pinned
          // after respawn). See §4.1 of
          // 2026-07-23-pinned-session-recovery-design.md.
          session_id: p.sessionId || undefined,
```

`remote` and `host_id` remain gated by `persistAsRemote` — do not touch those two lines.

- [ ] **Step 4: Run tests to verify they pass**

Run:
```bash
cd desktop/frontend && npm test -- useRecoverySnapshot
```
Expected: all tests pass — the flipped local-sid regression test, plus every remote/other regression (the "captures remote=true panes with session_id + host_id" test and the sidebar-viewer look-through test must still pass unchanged).

- [ ] **Step 5: Type-check**

Run:
```bash
cd desktop/frontend && npm run build
```
Expected: build succeeds.

- [ ] **Step 6: Commit**

```bash
git add desktop/frontend/src/composables/useRecoverySnapshot.ts \
        desktop/frontend/src/composables/useRecoverySnapshot.test.ts
git commit -m "feat(desktop): persist session_id for local panes in recovery snapshot"
```

---

### Task 3: Migrate pin ids in `executeRestore`

**Files:**
- Modify: `desktop/frontend/src/App.vue` — import `useSessionPins`, capture old sid, rename after spawn, flush at the end

**Interfaces:**
- Consumes: `useSessionPins()` from Task 1 (`rename`, `flushNow`, `isPinned`); `snap.session_id` from Task 2 for local panes.
- Produces: no new exports. Behavior change only.

- [ ] **Step 1: Add the `useSessionPins` import at the top of `App.vue`**

In `desktop/frontend/src/App.vue`, find the block around line 62-63:

```ts
import { useSessions } from "./composables/useSessions";
import { useRecoverySnapshot } from "./composables/useRecoverySnapshot";
```

Add the pins composable import right after it:

```ts
import { useSessions } from "./composables/useSessions";
import { useRecoverySnapshot } from "./composables/useRecoverySnapshot";
import { useSessionPins } from "./composables/useSessionPins";
```

- [ ] **Step 2: Instantiate `pins` inside `executeRestore`**

In `desktop/frontend/src/App.vue::executeRestore` (starts at line ~834), immediately after the opening line `const newIds: string[] = [];` add:

```ts
async function executeRestore(picks: RecoveryTabSnapshot[], savedActiveTabId: string) {
  const newIds: string[] = [];
  // Pin migration: local panes get a fresh session_id on respawn, which
  // would strand the previous generation's pin ids in config. Capture the
  // old sid from the snapshot and hand it to pins.rename after each spawn.
  // Remote panes keep their sid across restarts, so remote entries in the
  // pin set are already correct — no rename needed on that branch.
  // See 2026-07-23-pinned-session-recovery-design.md §4.3.
  const pins = useSessionPins();
  let savedActiveIdx = -1;
```

- [ ] **Step 3: Capture `oldSid` and migrate the pin after spawn**

Inside the `try { ... } catch` block for local panes (line ~873 onwards), rewrite so that `oldSid` is captured before `newSession` runs and `pins.rename` is called after `resp.session_id` is known:

Before:
```ts
      try {
        const dims = predictCellDims(tab.layout);
        const req = buildRestoreSessionReq(snap, dims.cols, dims.rows, defaultShell);
        const resp = await newSession(req);
        t.panes[i] = { sessionId: resp.session_id, remote: false };
        // Seed localList immediately (mirrors spawnLocalShell) ...
        pendingLocalIds.add(resp.session_id);
        localList.value = [
          ...localList.value,
          {
            id: resp.session_id,
            command: req.command,
            cwd: req.cwd || "",
            title: snap.title || req.command,
            type: snap.session_type || "",
            cols: dims.cols,
            rows: dims.rows,
            started_at: Math.floor(Date.now() / 1000),
            host_id: localHostID.value,
          },
        ];
        // Resume injection is handled Go-side on the shell's first prompt
        // (see relay_host SetOnFirstPrompt) — reliable, no task-state poll.
      } catch (e) {
        console.warn("[recovery] pane spawn failed", e);
        t.panes[i] = { sessionId: null, remote: false };
      }
```

After:
```ts
      try {
        // oldSid: previous generation's session_id, saved by useRecoverySnapshot
        // for local panes (Task 2). Empty when the snapshot pre-dates that
        // change — skip pin migration in that case, matches old behavior.
        const oldSid = snap.session_id || "";
        const dims = predictCellDims(tab.layout);
        const req = buildRestoreSessionReq(snap, dims.cols, dims.rows, defaultShell);
        const resp = await newSession(req);
        t.panes[i] = { sessionId: resp.session_id, remote: false };
        // Seed localList immediately (mirrors spawnLocalShell) so the recovery
        // snapshot can resolve this pane's SessionInfo right away. Without this,
        // the window before the relay's session-list push arrives would persist
        // shell:"" / cwd:"" — corrupting recovery.json and making the NEXT
        // restore fall back to /bin/sh (sh-3.2$). pendingLocalIds protects the
        // seed from being dropped by the FIRST (still-stale) LIST_RESP frame
        // that arrives after a multi-spawn restore.
        pendingLocalIds.add(resp.session_id);
        localList.value = [
          ...localList.value,
          {
            id: resp.session_id,
            command: req.command,
            cwd: req.cwd || "",
            title: snap.title || req.command,
            type: snap.session_type || "",
            cols: dims.cols,
            rows: dims.rows,
            started_at: Math.floor(Date.now() / 1000),
            host_id: localHostID.value,
          },
        ];
        // Resume injection is handled Go-side on the shell's first prompt
        // (see relay_host SetOnFirstPrompt) — reliable, no task-state poll.
        if (oldSid && pins.isPinned(oldSid)) {
          pins.rename(oldSid, resp.session_id);
        }
      } catch (e) {
        console.warn("[recovery] pane spawn failed", e);
        t.panes[i] = { sessionId: null, remote: false };
      }
```

- [ ] **Step 4: Flush pins before returning from `executeRestore`**

At the end of `executeRestore`, after the final `if (savedActiveIdx >= 0 ...) { gotoTab(...) } else if (newIds.length > 0) { gotoTab(...) }` block (line ~914), add:

Before:
```ts
  if (savedActiveIdx >= 0 && newIds[savedActiveIdx]) {
    gotoTab(newIds[savedActiveIdx]);
  } else if (newIds.length > 0) {
    gotoTab(newIds[0]);
  }
}
```

After:
```ts
  if (savedActiveIdx >= 0 && newIds[savedActiveIdx]) {
    gotoTab(newIds[savedActiveIdx]);
  } else if (newIds.length > 0) {
    gotoTab(newIds[0]);
  }
  // Persist the migrated pin set now (rather than waiting for the 300ms
  // debounce). The sidebar's first repaint after recovery reads pinnedIds
  // from the reactive Set (already up to date), but a fast-follow force
  // quit before the debounce fires would strand the new sids out of
  // config. Idempotent no-op when no rename happened.
  await pins.flushNow();
}
```

- [ ] **Step 5: Type-check + build**

Run:
```bash
cd desktop/frontend && npm run build
```
Expected: build succeeds. If TS complains about `snap.session_id` possibly being undefined, that's the intended defensive `oldSid = snap.session_id || ""` fallback.

- [ ] **Step 6: Run the whole frontend test suite**

Run:
```bash
cd desktop/frontend && npm test
```
Expected: all tests pass. In particular the pin + recovery-snapshot suites from Tasks 1-2 continue passing, and the existing `App.test.ts` / `App.theme.test.ts` don't regress (they don't currently mock `useSessionPins`; verify the import in Task 3 Step 1 doesn't break their setup — module-scoped `loadOnce()` fails silently on `getPinnedSessionIds` rejection, which is fine).

If `App.test.ts` fails because `useSessionPins` calls `getPinnedSessionIds` on the fake Wails bridge and the test harness doesn't stub it: add a `vi.mock("../lib/api", ...)` guard for `getPinnedSessionIds` / `setPinnedSessionIds` returning `[]` / `undefined` at the top of that file. Do not weaken `useSessionPins`'s own error handling.

- [ ] **Step 7: Go vet + go test (regression only)**

Run:
```bash
cd /Users/attson/code/github.com.attson/atterm && go vet -tags webkit2_41 ./...
cd /Users/attson/code/github.com.attson/atterm && go test -tags webkit2_41 -timeout 60s ./desktop/
```
Expected: pass. This plan doesn't touch Go, but AGENTS.md commits require vet + tests to be clean.

- [ ] **Step 8: Commit**

```bash
git add desktop/frontend/src/App.vue
git commit -m "feat(desktop): migrate pinned session ids across recovery respawn"
```

---

### Task 4: Manual verification

**Files:** none (verification only).

- [ ] **Step 1: Build the app**

Run:
```bash
cd /Users/attson/code/github.com.attson/atterm/desktop && wails build -tags webkit2_41
```
Expected: `desktop/build/bin/AT Term` produced.

- [ ] **Step 2: Local-pane pin survives restart**

Launch `AT Term`. Open a fresh tab with a shell session. Right-click the row in the left sidebar and click **置顶** — verify it moves into the 📌 置顶 group. Quit the app (⌘Q, choose "yes recover" if prompted).

Relaunch `AT Term`. Accept the Recovery dialog. Expected: the same session is back in the pane, still in the 📌 置顶 group.

If it's NOT still pinned:
- Check `~/.config/atterm/recovery.json` — the pane entry should have a `session_id` field.
- Check `~/.config/atterm/config.json` — `pinned_session_ids` should contain the NEW session id, not the pre-restart one.
- If either is missing / stale, the Task 2 or Task 3 patch didn't land — re-check the diffs.

- [ ] **Step 3: Remote-pane pin regression**

If you have access to another host (or the embedded relay + a second desktop instance):
- Attach a session on the remote host.
- Pin it via right-click.
- Restart desktop.
- Recovery dialog restores it.
- Expected: still pinned (session_id unchanged — no rename needed).

- [ ] **Step 4: Discard branch**

Pin a local session, quit, relaunch, then click **Discard** on the Recovery dialog. Expected:
- No panes restored (as before).
- `~/.config/atterm/config.json` still contains the old pin id (orphan — this is the spec §2 & §4.5 documented behavior).
- New pins work normally (pin something new after the fact).

- [ ] **Step 5: Multi-pin single-flush**

Pin 3 local sessions. Quit + relaunch + recover-all. Observe:
- All three appear in 📌 置顶.
- `pinned_session_ids` in config contains all three NEW sids, none of the OLD sids (unless discard was chosen).
- Only one persistence write happens at the end of `executeRestore` (open dev-tools console beforehand — no rapid `[api] setPinnedSessionIds` chatter).

- [ ] **Step 6: Old-snapshot compatibility**

Delete or edit `~/.config/atterm/recovery.json` and remove the `session_id` field from a local pane entry (simulate pre-Task-2 snapshot). Relaunch, recover. Expected: no crash; the restored pane is NOT auto-pinned even if the pane's OLD sid was pinned in config (skipped by the `oldSid || ""` fallback — matches spec §4.5 "旧 recovery.json" row).

Also handles a snapshot written by the new build but the pane's old sid isn't in the pin set: nothing pinned, as expected.

- [ ] **Step 7: Update memory (only if a non-obvious hazard surfaces)**

If Task 3 Step 6 required an `App.test.ts` stub, record a one-line feedback memory that `useSessionPins`'s module-scoped `loadOnce()` needs `getPinnedSessionIds` mocked in Wails-fake test harnesses. Otherwise skip — the design's existing patterns already cover this.

- [ ] **Step 8: Done — no commit for verification step**

Verification only. If verification uncovers a defect, cycle back to Task 1/2/3.

---

## Rollout

- No relay / protocol / mobile / web changes.
- `recovery.json` schema is additive (`omitempty` field, wider write coverage).
- `config.json` schema unchanged.
- Ships as a fix on v0.3-dev; next tag is `v0.3.(Z+1)` per AGENTS.md red line #29. Do NOT tag on `main` (that would land on v0.2.x).

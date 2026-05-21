# Title-bar dblclick-maximize unification Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drop the macOS-specific guard in `TitleBar.vue`'s `onTitleDblClick` so double-clicking the title bar maximizes/restores on all three platforms via the same code path.

**Architecture:** Single 1-line deletion in the component handler + one test reversal. No new files, no new dependencies.

**Tech Stack:** Vue 3.4 + Vitest 1.6 + @vue/test-utils 2.4. Wails runtime `WindowToggleMaximise()`.

**Spec:** `docs/superpowers/specs/2026-05-20-titlebar-dblclick-maximize-design.md`

---

## File Structure

**Modified files**

- `desktop/frontend/src/components/TitleBar.vue` — delete the `if (os.value === "darwin") return;` guard inside `onTitleDblClick`.
- `desktop/frontend/src/components/TitleBar.test.ts` — reverse the "on darwin, double-click on root does NOT call WindowToggleMaximise" assertion to expect it IS called once.

---

## Task 1: Reverse the darwin dblclick test, then drop the guard

**Files:**
- Modify: `desktop/frontend/src/components/TitleBar.test.ts`
- Modify: `desktop/frontend/src/components/TitleBar.vue`

- [ ] **Step 1: Update the failing test**

In `desktop/frontend/src/components/TitleBar.test.ts`, find the test currently reading:

```ts
  it("on darwin, double-click on root does NOT call WindowToggleMaximise (system handles zoom)", async () => {
    const w = await mountForPlatform("darwin");
    await w.get('[data-testid="titlebar-root"]').trigger("dblclick");
    expect(WindowToggleMaximise).not.toHaveBeenCalled();
  });
```

Replace it with:

```ts
  it("on darwin, double-click on root calls WindowToggleMaximise (system zoom doesn't fire under TitleBarHiddenInset)", async () => {
    const w = await mountForPlatform("darwin");
    await w.get('[data-testid="titlebar-root"]').trigger("dblclick");
    expect(WindowToggleMaximise).toHaveBeenCalledTimes(1);
  });
```

- [ ] **Step 2: Run test to verify it fails**

Run from `desktop/frontend/`: `npm test -- src/components/TitleBar.test.ts`

Expected: the reversed darwin test FAILS. All other 15 tests still pass.

- [ ] **Step 3: Drop the darwin guard in the component**

In `desktop/frontend/src/components/TitleBar.vue`, find:

```ts
function onTitleDblClick() {
  // macOS handles zoom natively in the TitleBarHiddenInset toolbar area;
  // calling WindowToggleMaximise there would double-fire and interfere.
  if (os.value === "darwin") return;
  try {
    WindowToggleMaximise();
    setMaximized(!isMaximized.value);
  } catch (e) {
    console.warn("[TitleBar] WindowToggleMaximise failed", e);
  }
}
```

Replace with:

```ts
function onTitleDblClick() {
  // macOS' system zoom-on-dblclick fires off NSWindow events that the
  // WebKit view eats under TitleBarHiddenInset, so we drive maximize
  // ourselves on all three platforms.
  try {
    WindowToggleMaximise();
    setMaximized(!isMaximized.value);
  } catch (e) {
    console.warn("[TitleBar] WindowToggleMaximise failed", e);
  }
}
```

- [ ] **Step 4: Run tests to verify all pass**

Run from `desktop/frontend/`: `npm test -- src/components/TitleBar.test.ts`

Expected: all 16 tests PASS.

- [ ] **Step 5: Run the full suite to confirm no regression**

Run from `desktop/frontend/`: `npm test`

Expected: all 363 tests PASS.

- [ ] **Step 6: Type-check**

Run from `desktop/frontend/`: `npm run build`

Expected: succeeds.

- [ ] **Step 7: Commit**

```bash
git add desktop/frontend/src/components/TitleBar.vue desktop/frontend/src/components/TitleBar.test.ts
git commit -m "feat(desktop): unify dblclick-maximize across mac/win/linux"
```

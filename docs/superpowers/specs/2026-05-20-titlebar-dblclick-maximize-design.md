# Title-bar double-click maximize: unify across platforms

## Problem

After the unified-titlebar shipped (PR #64), double-clicking the empty area of the merged title bar on macOS does nothing. On Windows and Linux it correctly toggles maximize via `WindowToggleMaximise()`. The macOS no-op is intentional in T4's code — the original assumption was that macOS's `TitleBarHiddenInset` would let the system handle dblclick → zoom natively. In practice, the WebKit view eats the event before the NSWindow sees it, so the system zoom never fires.

## Goal

Make double-click on the title bar empty area toggle maximize on macOS too, via the same code path Windows/Linux use. No platform-specific branching.

## Approach

Delete the `if (os.value === "darwin") return;` guard in `desktop/frontend/src/components/TitleBar.vue`'s `onTitleDblClick`. After the change, all three platforms run:

```ts
function onTitleDblClick() {
  try {
    WindowToggleMaximise();
    setMaximized(!isMaximized.value);
  } catch (e) {
    console.warn("[TitleBar] WindowToggleMaximise failed", e);
  }
}
```

`@dblclick.self` on the root header (already in place) keeps child clicks out, and traffic lights are a native overlay outside the DOM so they continue to behave normally.

## Side-effect on the 8 px maximize inset

`useWindowMaximized` will now flip to `true` on macOS too. `App.vue:showMaximizedInset` already gates the inset on `platform !== "darwin"`, so the existing 8 px Windows/Linux inset stays Win/Linux-only. No additional changes needed there.

## Testing

Update `desktop/frontend/src/components/TitleBar.test.ts`. The current test "on darwin, double-click on root does NOT call WindowToggleMaximise (system handles zoom)" must be reversed: darwin now should call it.

Rewrite that test as:

```ts
it("on darwin, double-click on root calls WindowToggleMaximise (system zoom doesn't fire under TitleBarHiddenInset)", async () => {
  const w = await mountForPlatform("darwin");
  await w.get('[data-testid="titlebar-root"]').trigger("dblclick");
  expect(WindowToggleMaximise).toHaveBeenCalledTimes(1);
});
```

The existing "on windows" dblclick test stays. Total: 16 tests in `TitleBar.test.ts` (count unchanged).

Manual verification: launch `wails dev` on macOS, double-click empty area of title bar, confirm window maximizes/restores.

## Out of scope

- Honoring macOS `AppleActionOnDoubleClick` user preference. Would require cgo to read NSUserDefaults; not worth it for one preference.
- Smooth-resize animation matching macOS native zoom. Wails-provided `WindowToggleMaximise()` is fine for now.

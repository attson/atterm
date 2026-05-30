# Terminal right-click "Send" — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a "发送 / Send" item to the desktop terminal's right-click menu that sends the current xterm selection to the current pane's PTY with a trailing `\r`, executing it immediately.

**Architecture:** Two new tiny pure helpers (`canSendSelection`, `prepareSendPayload`) live in `desktop/frontend/src/lib/terminalContextMenu.ts` and own all enablement + payload-shaping logic. `TerminalView.vue` calls them from a new `onMenuSend` handler that writes through the existing `SessionConnection.sendInput` path — same byte stream that `term.onData` uses for typed input, deliberately bypassing `term.paste()` (which would wrap the payload in bracketed paste sequences and suppress execute-on-Enter).

**Tech Stack:** Vue 3 SFC + xterm.js 5 + Vitest + TypeScript (desktop frontend, Wails).

**Spec:** `docs/superpowers/specs/2026-05-30-terminal-right-click-send-design.md`

---

## File Structure

| File | Role |
|---|---|
| `desktop/frontend/src/lib/terminalContextMenu.ts` | host two new pure helpers: `canSendSelection`, `prepareSendPayload`. Existing `isPasteAllowed` / `clampContextMenuPosition` stay. |
| `desktop/frontend/src/lib/terminalContextMenu.test.ts` | add Vitest cases for the two helpers. Existing tests stay. |
| `desktop/frontend/src/i18n/messages/en.ts` | add `terminal.sendSelection = "send"`. |
| `desktop/frontend/src/i18n/messages/zh-CN.ts` | add `terminal.sendSelection = "发送"`. |
| `desktop/frontend/src/components/TerminalView.vue` | new `onMenuSend` handler + new `<button>` in the menu between Paste and Clear, bound via a `menuCanSend` computed. |
| `desktop/frontend/src/components/TerminalView.test.ts` | add source-string assertions matching the existing `describe("TerminalView right-click menu")` pattern. |

No backend / relay / protocol changes.

---

## Task 1: `canSendSelection` predicate

**Files:**
- Modify: `desktop/frontend/src/lib/terminalContextMenu.ts`
- Test: `desktop/frontend/src/lib/terminalContextMenu.test.ts`

- [ ] **Step 1.1: Write the failing tests**

Append to `desktop/frontend/src/lib/terminalContextMenu.test.ts` (inside the existing top-level `describe("terminal context menu helpers", ...)`):

```ts
it("allows send only when selection + writeable + driver", () => {
  expect(
    canSendSelection({ hasSelection: true, status: "attached", permission: "full", isDriver: true }),
  ).toBe(true);
});

it("blocks send with no selection", () => {
  expect(
    canSendSelection({ hasSelection: false, status: "attached", permission: "full", isDriver: true }),
  ).toBe(false);
});

it("blocks send for read-only or detached sessions", () => {
  expect(
    canSendSelection({ hasSelection: true, status: "attached", permission: "view", isDriver: true }),
  ).toBe(false);
  expect(
    canSendSelection({ hasSelection: true, status: "connecting", permission: "full", isDriver: true }),
  ).toBe(false);
});

it("blocks send for non-driver clients even when permission allows writes", () => {
  expect(
    canSendSelection({ hasSelection: true, status: "attached", permission: "control", isDriver: false }),
  ).toBe(false);
});
```

Update the existing import at the top of the file:

```ts
import {
  canSendSelection,
  clampContextMenuPosition,
  effectiveRemotePermission,
  imagePasteBlockedReason,
  isPasteAllowed,
} from "./terminalContextMenu";
```

- [ ] **Step 1.2: Run the tests to verify they fail**

Run from the desktop frontend dir:

```bash
cd desktop/frontend && npx vitest run src/lib/terminalContextMenu.test.ts
```

Expected: 4 new tests fail with `canSendSelection is not defined` (TypeScript will refuse to compile — that's still the "red" we want).

- [ ] **Step 1.3: Implement the predicate**

Append to `desktop/frontend/src/lib/terminalContextMenu.ts` after the existing `isPasteAllowed` export:

```ts
export interface CanSendSelectionInput {
  hasSelection: boolean;
  status: Status;
  permission?: string;
  isDriver: boolean;
}

export function canSendSelection(input: CanSendSelectionInput): boolean {
  if (!input.hasSelection) return false;
  if (!input.isDriver) return false;
  return isPasteAllowed(input.status, input.permission);
}
```

- [ ] **Step 1.4: Run the tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalContextMenu.test.ts
```

Expected: all tests in the file pass (the 4 new ones + the 4 pre-existing ones).

- [ ] **Step 1.5: Commit**

```bash
git add desktop/frontend/src/lib/terminalContextMenu.ts desktop/frontend/src/lib/terminalContextMenu.test.ts
git commit -m "feat(terminal): canSendSelection predicate"
```

---

## Task 2: `prepareSendPayload` helper

Owns C1 stripping + newline normalization so `TerminalView.vue` stays a thin wrapper.

**Files:**
- Modify: `desktop/frontend/src/lib/terminalContextMenu.ts`
- Test: `desktop/frontend/src/lib/terminalContextMenu.test.ts`

- [ ] **Step 2.1: Write the failing tests**

Append to `desktop/frontend/src/lib/terminalContextMenu.test.ts` inside the same top-level `describe(...)`:

```ts
it("appends a single CR to a one-line selection", () => {
  expect(prepareSendPayload("ls -la")).toBe("ls -la\r");
});

it("converts internal LF and CRLF newlines to CR", () => {
  expect(prepareSendPayload("a\nb\r\nc")).toBe("a\rb\rc\r");
});

it("collapses trailing newlines to a single CR", () => {
  expect(prepareSendPayload("ls -la\n")).toBe("ls -la\r");
  expect(prepareSendPayload("ls -la\r\n\n")).toBe("ls -la\r");
});

it("strips C1 controls before normalizing", () => {
  // U+0093 = Ctrl-S | 0x80 — see stripC1Controls.ts.
  expect(prepareSendPayload("ls -la")).toBe("ls -la\r");
});

it("returns null for empty or whitespace-only-after-strip input", () => {
  expect(prepareSendPayload("")).toBeNull();
  expect(prepareSendPayload("\n\n")).toBeNull();
  expect(prepareSendPayload("")).toBeNull();
});
```

Extend the import block at the top:

```ts
import {
  canSendSelection,
  clampContextMenuPosition,
  effectiveRemotePermission,
  imagePasteBlockedReason,
  isPasteAllowed,
  prepareSendPayload,
} from "./terminalContextMenu";
```

- [ ] **Step 2.2: Run the tests to verify they fail**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalContextMenu.test.ts
```

Expected: 5 new tests fail (compile error: `prepareSendPayload is not defined`).

- [ ] **Step 2.3: Implement the helper**

Add the import at the top of `desktop/frontend/src/lib/terminalContextMenu.ts`:

```ts
import { stripC1Controls } from "./stripC1Controls";
```

Append the helper at the bottom of the file:

```ts
// prepareSendPayload shapes a raw xterm selection into bytes ready for
// SessionConnection.sendInput: strips C1 controls (same as typed input),
// normalizes every newline shape to \r (PTY input semantics), collapses
// any run of trailing \r down to one. Returns null if nothing meaningful
// remains — callers should treat null as "no-op".
export function prepareSendPayload(selection: string): string | null {
  const { cleaned } = stripC1Controls(selection);
  if (!cleaned) return null;
  const normalized = cleaned.replace(/\r\n?|\n/g, "\r").replace(/\r+$/, "");
  if (!normalized) return null;
  return normalized + "\r";
}
```

- [ ] **Step 2.4: Run the tests to verify they pass**

```bash
cd desktop/frontend && npx vitest run src/lib/terminalContextMenu.test.ts
```

Expected: all 9 tests in the file pass.

- [ ] **Step 2.5: Commit**

```bash
git add desktop/frontend/src/lib/terminalContextMenu.ts desktop/frontend/src/lib/terminalContextMenu.test.ts
git commit -m "feat(terminal): prepareSendPayload helper for right-click send"
```

---

## Task 3: i18n key

**Files:**
- Modify: `desktop/frontend/src/i18n/messages/en.ts`
- Modify: `desktop/frontend/src/i18n/messages/zh-CN.ts`

- [ ] **Step 3.1: Add the English key**

In `desktop/frontend/src/i18n/messages/en.ts`, locate the `terminal: { ... }` block. Right after the `clearBuffer: "clear buffer",` line (currently around line 63), insert:

```ts
    sendSelection: "send",
```

- [ ] **Step 3.2: Add the Chinese key**

In `desktop/frontend/src/i18n/messages/zh-CN.ts`, right after the `clearBuffer: "清空缓冲区",` line (currently around line 65), insert:

```ts
    sendSelection: "发送",
```

- [ ] **Step 3.3: Verify both locales typecheck and stay in sync**

```bash
cd desktop/frontend && npx vitest run src/i18n
```

Expected: existing i18n tests still pass (the cross-locale parity check, if any, accepts the new key in both).

If the i18n test suite has a "both locales have the same keys" check and it fires, that's the intended signal — both edits land together so the check stays green.

- [ ] **Step 3.4: Commit**

```bash
git add desktop/frontend/src/i18n/messages/en.ts desktop/frontend/src/i18n/messages/zh-CN.ts
git commit -m "i18n(terminal): add sendSelection label for zh-CN and en"
```

---

## Task 4: Wire the menu item into `TerminalView.vue`

**Files:**
- Modify: `desktop/frontend/src/components/TerminalView.vue`
- Test: `desktop/frontend/src/components/TerminalView.test.ts`

- [ ] **Step 4.1: Write the failing tests**

Append a new `describe(...)` block at the bottom of `desktop/frontend/src/components/TerminalView.test.ts` (the file already imports `source` from `TerminalView.vue?raw` — we keep the source-string assertion style the rest of the file uses):

```ts
describe("TerminalView right-click send", () => {
  test("imports the send predicate and payload helper", () => {
    expect(source).toMatch(/canSendSelection[\s\S]*from\s+["']\.\.\/lib\/terminalContextMenu["']/);
    expect(source).toMatch(/prepareSendPayload[\s\S]*from\s+["']\.\.\/lib\/terminalContextMenu["']/);
  });

  test("renders a send menu item between paste and clear", () => {
    const pasteIdx = source.indexOf('t("common.paste")');
    const sendIdx = source.indexOf('t("terminal.sendSelection")');
    const clearIdx = source.indexOf('t("terminal.clearBuffer")');
    expect(pasteIdx).toBeGreaterThan(-1);
    expect(sendIdx).toBeGreaterThan(pasteIdx);
    expect(clearIdx).toBeGreaterThan(sendIdx);
  });

  test("binds the send button's disabled state to menuCanSend", () => {
    expect(source).toContain('@click="onMenuSend"');
    expect(source).toContain(':disabled="!menuCanSend"');
    expect(source).toMatch(/const\s+menuCanSend\s*=\s*computed/);
  });

  test("menuCanSend feeds canSendSelection with selection + status + permission + isDriver", () => {
    expect(source).toMatch(
      /canSendSelection\(\s*\{[^}]*hasSelection[^}]*status[^}]*permission[^}]*isDriver[^}]*\}\s*\)/,
    );
  });

  test("onMenuSend writes through SessionConnection.sendInput, not term.paste", () => {
    expect(source).toMatch(/function\s+onMenuSend\s*\(\s*\)/);
    // The helper produces the final payload — onMenuSend just forwards it.
    expect(source).toMatch(/prepareSendPayload\s*\(/);
    expect(source).toMatch(/conn\??\.sendInput\s*\(/);
    // term.paste must NOT appear inside onMenuSend's body — bracketed paste
    // would suppress execute-on-Enter in many shells. We pin this by
    // checking the existing term.paste call site stays inside pasteFromClipboard.
    const sendBody = source.match(/function\s+onMenuSend\s*\([^)]*\)\s*\{[\s\S]*?\n\}/);
    expect(sendBody).not.toBeNull();
    expect(sendBody![0]).not.toMatch(/term\.paste\b/);
  });
});
```

- [ ] **Step 4.2: Run the tests to verify they fail**

```bash
cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts
```

Expected: the 5 new tests fail. Existing tests in the file still pass.

- [ ] **Step 4.3: Update the import in TerminalView.vue**

Replace the existing import line in `desktop/frontend/src/components/TerminalView.vue` (currently around line 17):

```ts
import { clampContextMenuPosition, isPasteAllowed } from "../lib/terminalContextMenu";
```

with:

```ts
import {
  canSendSelection,
  clampContextMenuPosition,
  isPasteAllowed,
  prepareSendPayload,
} from "../lib/terminalContextMenu";
```

- [ ] **Step 4.4: Add the `menuCanSend` computed**

In `desktop/frontend/src/components/TerminalView.vue`, locate the existing `menuCanPaste` computed (currently around line 104). Add directly after it:

```ts
const menuCanSend = computed(() =>
  canSendSelection({
    hasSelection: menuHasSelection.value,
    status: status.value,
    permission: props.remotePermission,
    isDriver: isDriver.value,
  }),
);
```

- [ ] **Step 4.5: Add the `onMenuSend` handler**

In the same file, add a new function next to `onMenuClear` (currently around line 250). Place it between `onMenuPaste` and `onMenuClear` to mirror the menu order:

```ts
function onMenuSend() {
  closeContextMenu();
  if (!term || !conn) return;
  const payload = prepareSendPayload(term.getSelection());
  if (payload === null) return;
  conn.sendInput(payload);
}
```

- [ ] **Step 4.6: Add the `<button>` in the template**

In the `<template>` block of `desktop/frontend/src/components/TerminalView.vue`, locate the existing menu (currently around lines 555-557):

```vue
        <button class="term-context-item" :disabled="!menuHasSelection" @click="onMenuCopy">{{ t("common.copy") }}</button>
        <button class="term-context-item" :disabled="!menuCanPaste || pasteBusy" @click="onMenuPaste">{{ t("common.paste") }}</button>
        <button class="term-context-item" @click="onMenuClear">{{ t("terminal.clearBuffer") }}</button>
```

Replace those three lines with four:

```vue
        <button class="term-context-item" :disabled="!menuHasSelection" @click="onMenuCopy">{{ t("common.copy") }}</button>
        <button class="term-context-item" :disabled="!menuCanPaste || pasteBusy" @click="onMenuPaste">{{ t("common.paste") }}</button>
        <button class="term-context-item" :disabled="!menuCanSend" @click="onMenuSend">{{ t("terminal.sendSelection") }}</button>
        <button class="term-context-item" @click="onMenuClear">{{ t("terminal.clearBuffer") }}</button>
```

- [ ] **Step 4.7: Bump `MENU_HEIGHT` for the new row**

The existing constant `const MENU_HEIGHT = 110;` (around line 102) was sized for 3 rows. With a 4th row, the clamp budget needs to grow proportionally — each row is ~30px, so:

```ts
const MENU_HEIGHT = 140;
```

This keeps `clampContextMenuPosition` honest about how much vertical room the menu actually needs, so a right-click near the bottom of the viewport still places the menu fully inside.

The existing test `expect(source).toMatch(/const\s+MENU_HEIGHT\s*=\s*110/)` (around line 79 of `TerminalView.test.ts`) is now wrong — update it in the same step:

```ts
    expect(source).toMatch(/const\s+MENU_HEIGHT\s*=\s*140/);
```

- [ ] **Step 4.8: Run the tests to verify they all pass**

```bash
cd desktop/frontend && npx vitest run src/components/TerminalView.test.ts
```

Expected: all tests in `TerminalView.test.ts` pass, including the 5 new ones and the updated `MENU_HEIGHT` assertion.

- [ ] **Step 4.9: Full desktop-frontend check**

```bash
cd desktop/frontend && npm run build && npx vitest run
```

Expected: TypeScript compiles cleanly; all Vitest suites in the desktop frontend pass.

- [ ] **Step 4.10: Commit**

```bash
git add desktop/frontend/src/components/TerminalView.vue desktop/frontend/src/components/TerminalView.test.ts
git commit -m "feat(terminal): right-click send pushes selection through PTY"
```

---

## Task 5: Manual smoke

Not committed, but the spec calls these out and they catch what unit tests can't.

- [ ] **Step 5.1: Run the dev app**

```bash
cd desktop && wails dev
```

(On Linux, append `-tags webkit2_41` per repo conventions.)

- [ ] **Step 5.2: Single-line send**

1. In a fresh pane, run `echo hello`.
2. Select the literal text `echo hello` from the scrollback.
3. Right-click → 发送.
4. Expected: `echo hello` runs again, prints `hello`. Menu closes.

- [ ] **Step 5.3: Multi-line send**

1. Select two consecutive command lines from history (e.g. `pwd` and `whoami`).
2. Right-click → 发送.
3. Expected: both commands execute in order, one prompt each, no spurious empty trailing line.

- [ ] **Step 5.4: Disabled states**

1. Right-click with **no selection** → 发送 is greyed.
2. Connect a second client as a **viewer** (or set `remote_permission = view`) → 发送 is greyed.
3. Become viewer (let another client claim driver, confirm overlay) → 发送 is greyed.

- [ ] **Step 5.5: i18n smoke**

Switch UI language between zh-CN and en. Menu label flips between `发送` and `send`. No layout breakage.

---

## Self-Review

Spec coverage:
- Menu placement (between Paste and Clear) → Task 4.6 + 4.7. ✓
- Enablement (selection + writeable + driver) → Task 1 (`canSendSelection`) + Task 4.4 (`menuCanSend`). ✓
- `conn.sendInput` not `term.paste` → Task 4.5 + Task 4.1 (test pins it). ✓
- Newline normalization (`\n`, `\r\n` → `\r`; trim trailing) → Task 2 + tests in Step 2.1. ✓
- C1 strip → Task 2 + test in Step 2.1. ✓
- i18n keys for zh-CN and en → Task 3. ✓
- Tests #1–9 from the spec's test plan → Task 1 covers #2–5, Task 2 covers #6/7/8, Task 4 covers #1 (sendInput call) and #9 (menu closes via `closeContextMenu()` call in `onMenuSend`). ✓

Placeholder scan: no TBD / TODO / "similar to" / unimplemented references. All code is present in-step. ✓

Type consistency: `canSendSelection({ hasSelection, status, permission, isDriver })` interface used identically in Tasks 1, 4.4, and 4.1 test. `prepareSendPayload(selection: string): string | null` used identically in Tasks 2 and 4.5. `Status` type imported from `./connection` is already in scope in `terminalContextMenu.ts`. ✓

One spec line worth re-pinning here: the spec says we explicitly do NOT also fix the latent Paste-in-viewer-mode bug (out of scope). This plan touches `menuCanPaste` zero times — confirmed. ✓

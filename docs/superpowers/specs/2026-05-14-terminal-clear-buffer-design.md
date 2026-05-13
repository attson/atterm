# Terminal Right-Click "Clear Buffer" Design

**Date:** 2026-05-14
**Status:** Approved

## Goal

Add a `clear buffer` item to the terminal right-click context menu in the desktop app. The item wipes the local xterm viewport and scrollback for the focused pane without touching the remote PTY.

## Motivation

The right-click menu currently exposes `copy` and `paste` (shipped 2026-05-13, see `docs/superpowers/plans/2026-05-13-terminal-right-click-clipboard.md`). Users often want a discoverable way to wipe a noisy terminal — equivalent to iTerm's Cmd+K — without remembering a keyboard shortcut or typing `clear` (which only scrolls past output instead of removing scrollback).

## Non-Goals

- Sending `clear`, `reset`, or Ctrl+L to the remote shell.
- A separate "clear scrollback only" variant.
- A keyboard shortcut (Cmd+K or similar).
- Affecting other clients attached to the same session.

## Behavior

1. The third menu item, labeled `clear buffer`, appears below `paste`.
2. Clicking calls `term.clear()` from xterm.js, which wipes the visible viewport and scrollback while keeping the current prompt line at row 0.
3. The menu closes after the click.
4. The item is always enabled while the menu is open. The menu only opens when `term` exists, so the handler can rely on a non-null terminal.
5. No permission gating. The action is purely local-display: it never reaches the PTY, doesn't require `control` or `full` permission, and is unaffected by `view`-only sessions.
6. Other attachers see no effect — their xterm scrollback is independent.

## Architecture

Single-file change in `TerminalView.vue`, plus a source-level test assertion.

### Components Touched

- **`desktop/frontend/src/components/TerminalView.vue`**
  - Add `onMenuClear()` handler: calls `term.clear()` then `closeContextMenu()`.
  - Add a third `<button class="term-context-item">clear buffer</button>` to the teleported menu, between `paste` and the closing `</div>`.
  - Bump `MENU_HEIGHT` constant from `76` to `110` so `clampContextMenuPosition` keeps the larger menu inside the viewport.

- **`desktop/frontend/src/components/TerminalView.test.ts`**
  - Extend the existing "renders copy/paste buttons" source-test block to also assert `>clear buffer<` appears in the template source.

No backend changes. No new helper module. No changes to `terminalContextMenu.ts`, `terminalPaste.ts`, `PaneGrid.vue`, or `App.vue`.

### Error Handling

`term.clear()` is a synchronous, no-op-safe call in xterm.js — it has no failure mode worth surfacing. No try/catch, no toast emission.

## Testing

- Source-level test in `TerminalView.test.ts` asserts the new button text is in the rendered template (matches the pattern already used for `copy` / `paste`).
- Manual verification: right-click → `clear buffer` while a session has scrollback; viewport and scrollback are empty; the prompt remains usable; remote shell state is unchanged; switching to another attached client confirms its view is untouched.

## Risks

Low. The change is additive, scoped to one component, and the underlying `term.clear()` is well-established xterm.js API. The only behavioral subtlety is that `term.clear()` keeps the current prompt line — which matches user intent (don't lose the prompt you're typing into) and matches iTerm's Cmd+K behavior.

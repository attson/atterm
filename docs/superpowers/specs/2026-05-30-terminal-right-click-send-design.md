# Terminal right-click "Send" for selected text

## Problem

The terminal context menu (复制 / 粘贴 / 清屏 + plugin items) lets users
get text out of the buffer but not back in. A common workflow today is:

1. select a command from the scrollback,
2. ⌘C to copy,
3. ⌘V (or right-click → 粘贴) to paste,
4. press Enter to run it.

For "I see a command in the output, run it again" this is four steps with
two distinct gestures. Add a one-click "Send" that fires the selection
straight to the current pane's PTY and executes it.

## Goals

- Right-click on a terminal with text selected → menu item that sends the
  selection to the **current pane** as input and executes it immediately.
- Same enablement rules as 粘贴 (writeable session) plus needing a
  non-empty selection.
- Behave correctly under driver/viewer split — only the driver can write
  PTY input, so non-driver clients must not be able to click Send.
- Localized in zh-CN / en (same coverage as the existing menu items).

## Non-goals

- Cross-pane send ("send selection from pane A to pane B"). Out of scope;
  if needed later, add as a submenu.
- Editing the text before sending (preview/confirm dialog).
- A "send without Enter" variant. The whole point is one-click execute;
  users who want paste-without-Enter already have 粘贴.
- Web / mobile clients. The PWA/iOS context menu surface is different
  (long-press, browser native) and would need its own design. This spec
  is desktop-only.
- A new plugin slot. Send is a core terminal action like Copy/Paste, not
  a domain plugin like Translate.

## Design

### Menu placement

In `desktop/frontend/src/components/TerminalView.vue`, add one entry
between 粘贴 and 清屏:

```
复制
粘贴
发送          ← new
清屏
[plugin items]
```

### Enablement

The button is enabled iff **all** of:

1. There is a non-empty selection (`menuHasSelection.value === true`,
   already tracked).
2. The session is writeable in the paste sense
   (`isPasteAllowed(status, remotePermission) === true`).
3. This client is the driver (`isDriver.value === true`).

(3) is stricter than the existing Paste path, which doesn't check
`isDriver`. That's a latent UX bug for Paste in viewer mode (`term.paste()`
runs but `disableStdin` swallows the resulting `onData`). Send fixes it
for its own menu item by checking `isDriver` directly; we don't widen the
fix to Paste in this change (out of scope, separate decision).

Disabled state uses the same grey styling as Copy/Paste/Plugin items.

### Send semantics

```ts
async function onMenuSend() {
  closeContextMenu();
  if (!term || !conn) return;
  const selection = term.getSelection();
  if (!selection) return;
  const { cleaned } = stripC1Controls(selection);
  if (!cleaned) return;
  const normalized = cleaned.replace(/\r\n?|\n/g, "\r").replace(/\r+$/, "");
  conn.sendInput(normalized + "\r");
}
```

Key choices:

- **`conn.sendInput`, not `term.paste`.** `term.paste()` wraps the payload
  in bracketed-paste sequences when the host shell has bracketed paste
  enabled, which in many shells suppresses execute-on-Enter inside the
  pasted block. We want fire-and-execute; `sendInput` writes raw bytes
  through the same path `term.onData` already uses.
- **Newline normalization.** Selections come back with `\n` or `\r\n`
  for line breaks (xterm's selection joiner). PTY input expects `\r`
  for "press Enter". Normalize every newline shape to `\r`, then trim
  trailing `\r`s, then append exactly one `\r`. Result: a single-line
  selection gets one Enter; a 3-line selection gets two interior Enters
  plus one trailing Enter, so the shell runs three commands cleanly
  with no spurious empty trailing prompt.
- **`stripC1Controls` reuse.** The existing `term.onData` handler strips
  C1 controls from typed input to keep stray DEL/CAN bytes from
  smuggling into the PTY; selection from the buffer can contain these
  too (e.g. text echoed from a TUI). Re-using the same helper keeps the
  policy consistent.
- **Multi-line selections execute line-by-line.** Each `\r` in the
  normalized payload is a discrete "press Enter" — the shell runs each
  line as soon as it sees its terminating `\r`. That's the natural
  reading of "直接发送"; users who want paste-without-execute already
  have 粘贴. Surfacing a multi-line confirmation is not a goal of this
  spec.
- **No clipboard mutation.** Send doesn't write to the system clipboard
  (that's what Copy is for).

### i18n

Add one key per locale:

```ts
// en.ts
terminal: {
  ...
  sendSelection: "send",
}

// zh-CN.ts
terminal: {
  ...
  sendSelection: "发送",
}
```

The label is the verb only, matching the brevity of "复制"/"粘贴"/"清空缓冲区"
in the same menu. Long form like "发送选中" reads worse next to single-word
siblings.

### Failure handling

The menu currently emits a toast on copy/paste failure. Send has fewer
failure modes because it's synchronous and local to the websocket buffer:

- No selection / empty after C1 strip → no-op silently (item should have
  been disabled in the first place; this is defense in depth, not an
  error path the user sees).
- `conn` is null → no-op silently. The menu would also be effectively
  disabled because `status !== "attached"` already greys it.

No toast is needed. If we later see "I clicked Send and nothing happened"
reports, we can add one then.

## Test plan

Unit (Vitest, against `TerminalView.vue` and any extracted helper):

1. With a non-empty selection, clicking Send calls `conn.sendInput` with
   `selection + "\r"`.
2. With an empty selection, the Send item is disabled.
3. With `status !== "attached"`, Send is disabled.
4. With `remotePermission === "view"`, Send is disabled.
5. With `isDriver === false`, Send is disabled.
6. C1 controls in the selection are stripped before send (mirror the
   existing `stripC1Controls` test pattern).
7. A multi-line selection (`"a\nb\nc"`) becomes `"a\rb\rc\r"` over the
   wire — three discrete `\r`s, none doubled, no trailing empty line.
8. A selection with trailing newline (`"ls -la\n"`) becomes `"ls -la\r"`
   — one Enter, not two.
9. The menu closes after a successful Send.

Manual (during implementation):

- Run `wails dev`, select a previous command from scrollback, right-click
  → Send, confirm the command re-executes.
- Multi-pane: select in pane A, Send sends to pane A (not B).
- Remote viewer: attach a second client as viewer, confirm Send is
  greyed.

i18n smoke: switch UI language between zh-CN / en, confirm the menu
label updates.

## File touch list

- `desktop/frontend/src/components/TerminalView.vue`
  - new `onMenuSend` handler
  - new `<button>` between Paste and Clear, disabled-when binding
- `desktop/frontend/src/i18n/messages/en.ts` — add `terminal.sendSelection`
- `desktop/frontend/src/i18n/messages/zh-CN.ts` — add `terminal.sendSelection`
- `desktop/frontend/src/components/TerminalView.test.ts` (or sibling)
  — tests above. Extract the predicate into a tiny pure function
  (`canSendSelection({ hasSelection, status, permission, isDriver })`)
  in `desktop/frontend/src/lib/terminalContextMenu.ts` so it's
  unit-testable without mounting the SFC.

No backend, no protocol, no relay change.

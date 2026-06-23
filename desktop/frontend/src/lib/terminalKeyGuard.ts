import type { Terminal } from "xterm";

/**
 * True when the event is a keydown of a *bare* modifier key — Ctrl, ⌘ (Meta),
 * Alt, or Shift pressed on its own with no character. These never produce
 * terminal input.
 */
export function isBareModifierKeydown(e: KeyboardEvent): boolean {
  if (e.type !== "keydown") return false;
  return (
    e.key === "Control" ||
    e.key === "Meta" ||
    e.key === "Alt" ||
    e.key === "Shift"
  );
}

/**
 * Stop xterm from yanking the viewport to the bottom when the user merely
 * presses a modifier key (e.g. holding Ctrl/⌘ to mod-click a link sitting in
 * the scrollback).
 *
 * Why this is needed: xterm 5.3's `_keyDown` runs `scrollOnUserInput &&
 * scrollToBottom()` whenever its IME-composition path reports the key as
 * "unhandled", and that path treats *every* keydown carrying `keyCode === 229`
 * as composition. CJK input methods (Pinyin, etc.) deliver keydowns — including
 * bare modifier presses — with `keyCode === 229`, so with such an IME active a
 * plain Ctrl/⌘ press scrolls the terminal to the prompt before the user can
 * complete a mod-click on a link further up the buffer. (Latin keyboards report
 * the real keyCode 17/91/…, never hit this path, which is why it only bites
 * IME users.)
 *
 * Bare modifier keydowns carry no terminal payload, so swallowing them via the
 * custom key handler suppresses the unwanted scroll while leaving real
 * keystrokes — and IME composition of actual characters — untouched. Returning
 * `false` skips xterm's handling without `preventDefault`, so the modifier
 * stays held for the subsequent mouse click.
 */
export function installModifierScrollGuard(
  term: Pick<Terminal, "attachCustomKeyEventHandler">,
): void {
  term.attachCustomKeyEventHandler((e) => !isBareModifierKeydown(e));
}

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

export interface TerminalKeyHandlerOptions {
  ime229Takeover: boolean;
  /** Return false when the caller consumed the keydown itself. */
  onKeydown?: (event: KeyboardEvent) => boolean | void;
  /** Runs only for keydowns that will continue into xterm. */
  onAcceptedKeydown?: (event: KeyboardEvent) => void;
}

export interface DesktopImeInputContext {
  /** True while the browser is still inside a composition session. */
  compositionActive?: boolean;
  /** Monotonic timestamp recorded from the most recent compositionend. */
  compositionEndedAt: number | null;
  now: number;
}

const POST_COMPOSITION_INPUT_GRACE_MS = 100;

/**
 * Installs xterm's one custom-key handler. xterm replaces the previous handler
 * on every attachCustomKeyEventHandler call, so all keydown interception must
 * be composed here.
 *
 * The bare-modifier guard stops xterm 5.3 from running `scrollOnUserInput &&
 * scrollToBottom()` whenever its IME-composition path reports the key as
 * "unhandled", and that path treats *every* keydown carrying `keyCode === 229`
 * as composition. CJK input methods (Pinyin, etc.) deliver keydowns — including
 * bare modifier presses — with `keyCode === 229`, so with such an IME active a
 * plain Ctrl/⌘ press scrolls the terminal to the prompt before the user can
 * complete a mod-click on a link further up the buffer. (Latin keyboards report
 * the real keyCode 17/91/…, never hit this path, which is why it only bites
 * IME users.)
 *
 * With desktop IME takeover enabled, all remaining keyCode-229 keydowns are
 * also stopped. xterm otherwise recovers their text by asynchronously diffing
 * its hidden textarea, which races keyup clearing and can merge, duplicate, or
 * drop fast keystrokes. The caller delivers those keys from capture-phase
 * `input` events instead.
 */
export function installTerminalKeyHandler(
  term: Pick<Terminal, "attachCustomKeyEventHandler">,
  options: TerminalKeyHandlerOptions,
): void {
  term.attachCustomKeyEventHandler((event) => {
    if (event.type !== "keydown") return true;
    if (options.onKeydown?.(event) === false) {
      // xterm does not cancel the DOM event when a custom handler returns
      // false. Cancel it here so a caller-owned key (for example accepting a
      // prompt suggestion with Enter) cannot produce a later textarea input
      // event and leak through the desktop IME sender.
      event.preventDefault();
      event.stopPropagation();
      return false;
    }
    if (isBareModifierKeydown(event)) return false;
    if (options.ime229Takeover && event.keyCode === 229) return false;
    options.onAcceptedKeydown?.(event);
    return true;
  });
}

/**
 * Maps a desktop IME-owned input event to the exact bytes sent to the PTY.
 * Composition events stay with xterm; taking those over would double-send
 * pinyin-to-Hanzi commits.
 */
export function desktopImeInputPayload(
  event: Pick<InputEvent, "inputType" | "data" | "isComposing">,
  context?: DesktopImeInputContext,
): string | null {
  if (event.isComposing) return null;
  if (event.inputType === "insertText" && context?.compositionActive) return null;
  // WebKit may label the final input after compositionend as a plain
  // `insertText` with isComposing=false. xterm deliberately finishes that
  // composition on a timer; taking over the trailing event here would send
  // the committed Hanzi once from each path.
  if (
    event.inputType === "insertText" &&
    context?.compositionEndedAt !== null &&
    context?.compositionEndedAt !== undefined
  ) {
    const elapsed = context.now - context.compositionEndedAt;
    if (elapsed >= 0 && elapsed <= POST_COMPOSITION_INPUT_GRACE_MS) return null;
  }
  switch (event.inputType) {
    case "insertText":
      return event.data || null;
    case "deleteContentBackward":
      return "\x7f";
    case "insertLineBreak":
    case "insertParagraph":
      return "\r";
    default:
      return null;
  }
}

/**
 * Returns true when an input event is the browser-side echo of a physical
 * keydown. WebKit can dispatch that input before or after keydown, so the
 * terminal keeps the event briefly pending and uses this predicate to decide
 * whether xterm already sent the byte through its keydown path.
 */
export function desktopImeInputMatchesKeydown(
  payload: string,
  inputType: string,
  key: string,
): boolean {
  if (inputType === "deleteContentBackward") return key === "Backspace";
  if (inputType === "insertLineBreak" || inputType === "insertParagraph") {
    return key === "Enter" || key === "Return";
  }
  if (inputType !== "insertText") return false;
  if (key === "Spacebar") return payload === " ";
  return key.length === 1 && payload === key;
}

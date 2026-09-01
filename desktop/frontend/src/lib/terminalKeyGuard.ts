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

export interface TerminalKeyHandlerHooks {
  // Fired for every keydown xterm is allowed to process.
  onRegularKeydown?: () => void;
}

/**
 * The single attachCustomKeyEventHandler call site. xterm keeps only the LAST
 * handler passed to attachCustomKeyEventHandler — a second call anywhere else
 * silently disables this one, so every custom-keydown concern must live here.
 *
 * Concern 1 — bare-modifier scroll guard. xterm 5.3's `_keyDown` runs
 * `scrollOnUserInput && scrollToBottom()` whenever its IME-composition path
 * reports the key as "unhandled", and that path treats *every* keydown
 * carrying `keyCode === 229` as composition. CJK input methods (Pinyin, etc.)
 * deliver keydowns — including bare modifier presses — with `keyCode === 229`,
 * so with such an IME active a plain Ctrl/⌘ press scrolls the terminal to the
 * prompt before the user can complete a mod-click on a link further up the
 * buffer. Swallowing bare modifier keydowns (return false, no preventDefault)
 * suppresses the scroll while the modifier stays held for the mouse click.
 *
 * Concern 2 — keyCode 229 takeover (desktop only, `ime229Takeover`). For a
 * 229 keydown xterm cannot read the character from the event; its
 * CompositionHelper._handleAnyTextareaChanges instead snapshots the hidden
 * textarea's value and diffs it against the new value in a setTimeout(0).
 * Those timers race xterm's own keyup handler (which CLEARS the textarea) and
 * each other, so fast consecutive keystrokes get merged ("cd" in one chunk),
 * duplicated, or dropped — the "type cd fast, only c arrives" bug. Returning
 * false for 229 keydowns keeps xterm's diff path from ever being scheduled;
 * the caller delivers those characters deterministically from `input` events
 * instead (see ime229Payload / TerminalView.onDesktopImeInput).
 * Real composition (pinyin → Hanzi) is unaffected: compositionstart/update/end
 * listeners are registered by xterm independently of the keydown path.
 * Mobile/web keep xterm's stock behavior — their soft-keyboard delivery
 * already leans on the existing unconditional insertText takeover.
 */
export function installTerminalKeyHandler(
  term: Pick<Terminal, "attachCustomKeyEventHandler">,
  opts: { ime229Takeover: boolean; hooks?: TerminalKeyHandlerHooks },
): void {
  term.attachCustomKeyEventHandler((e) => {
    if (e.type !== "keydown") return true;
    if (isBareModifierKeydown(e)) return false;
    if (opts.ime229Takeover && e.keyCode === 229) return false;
    opts.hooks?.onRegularKeydown?.();
    return true;
  });
}

/**
 * Decides what an `input` event on xterm's hidden textarea should send to the
 * PTY under the desktop takeover. Reads the event's own data — never the
 * textarea value — so it is immune to the clear/diff races described on
 * installTerminalKeyHandler.
 *
 * Single-sender invariant: the caller listens in the CAPTURE phase on an
 * ANCESTOR of the textarea, so it runs before every listener xterm attached
 * to the textarea itself and stops propagation after sending. IME event
 * order varies (WKWebView can fire the insertText `input` BEFORE the
 * keystroke's own 229 keydown, which lights up xterm's `_inputEvent` via its
 * `!_keyDownSeen` branch), so gating on keydown state doubles or drops —
 * intercepting ahead of xterm is what makes exactly one sender regardless of
 * order. Keys xterm handles on keydown (plain keyboards) are preventDefaulted
 * by xterm and never produce an `input` event, so they cannot double here.
 *
 * Returns null when the event is not ours to handle: an IME composition in
 * progress or committing (CompositionHelper owns insertCompositionText /
 * insertFromComposition), or an inputType we do not map.
 */
export function ime229Payload(
  ev: Pick<InputEvent, "inputType" | "data" | "isComposing">,
): string | null {
  if (ev.isComposing) return null;
  switch (ev.inputType) {
    case "insertText":
      return ev.data || null;
    // With the 229 keydown path disabled these no longer reach the PTY via
    // xterm's textarea diff, so map the ones an IME can route as 229 here.
    case "deleteContentBackward":
      return "\x7f";
    case "insertLineBreak":
    case "insertParagraph":
      return "\r";
    default:
      return null;
  }
}

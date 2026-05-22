// useLongPressModifier — detect a "pure long-press" of the platform mod key
// (Meta on mac, Control elsewhere) and emit show/hide callbacks. "Pure" means
// no other modifier is co-held and no non-modifier key is pressed during the
// hold window. Used to drive the shortcut-hints overlay.
//
// Listeners run in capture phase so they observe events even when other
// capture listeners (e.g. useTerminalShortcuts) stopPropagation()-ate normal
// chords — capture-phase handlers all fire before bubble-phase ones; calling
// stopPropagation on a different capture listener does not affect ours.

import { onScopeDispose } from "vue";
import type { Mod } from "../lib/shortcutBindings";

export interface LongPressOptions {
  mod: Mod;
  thresholdMs?: number;
  onShow: () => void;
  onHide: () => void;
}

export function useLongPressModifier(opts: LongPressOptions): void {
  const threshold = opts.thresholdMs ?? 3000;
  const modKeyName: "Meta" | "Control" = opts.mod;

  let timer: number | null = null;
  let showing = false;

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  function hideIfShowing() {
    if (showing) {
      showing = false;
      opts.onHide();
    }
  }

  function fireShow() {
    timer = null;
    showing = true;
    opts.onShow();
  }

  // Returns true if this keydown event represents the platform mod key being
  // pressed without any other modifier already held.
  function isPureModKeydown(e: KeyboardEvent): boolean {
    if (e.key !== modKeyName) return false;
    // The mod-down keydown reports its own flag as true (e.ctrlKey for
    // Control, e.metaKey for Meta). The other modifiers must all be false —
    // otherwise the user is building a chord like Ctrl+Alt.
    if (e.altKey) return false;
    if (e.shiftKey) return false;
    // wrong-platform modifier held (e.g. Meta on a Control platform) also
    // disqualifies — the user is doing something layered.
    const wrongModifier = modKeyName === "Meta" ? e.ctrlKey : e.metaKey;
    if (wrongModifier) return false;
    return true;
  }

  function onKeydown(e: KeyboardEvent) {
    if (isPureModKeydown(e)) {
      if (e.repeat) return;        // OS auto-repeat: keep state as-is
      if (timer !== null) return;  // already counting
      if (showing) return;         // already shown (shouldn't happen since
                                   // keydown of the mod that's already held
                                   // arrives with repeat=true, but defensive)
      timer = setTimeout(fireShow, threshold) as unknown as number;
      return;
    }
    // Any other keydown — cancel pending timer, hide if showing.
    clearTimer();
    hideIfShowing();
  }

  function onKeyup(e: KeyboardEvent) {
    if (e.key !== modKeyName) return;
    clearTimer();
    hideIfShowing();
  }

  function onBlur() {
    clearTimer();
    hideIfShowing();
  }

  document.addEventListener("keydown", onKeydown, { capture: true });
  document.addEventListener("keyup", onKeyup, { capture: true });
  window.addEventListener("blur", onBlur);

  onScopeDispose(() => {
    clearTimer();
    document.removeEventListener("keydown", onKeydown, { capture: true } as EventListenerOptions);
    document.removeEventListener("keyup", onKeyup, { capture: true } as EventListenerOptions);
    window.removeEventListener("blur", onBlur);
  });
}

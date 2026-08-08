import { logDebug } from "./log";
// Touch-scroll debug instrumentation.
//
// Default OFF; opt-in per user via localStorage:
//   localStorage.setItem('atterm.debugTouch', '1')  → enable
//   localStorage.setItem('atterm.debugTouch', '0')  → disable
//
// When enabled, logTouch() appends one entry to a bounded in-memory ring
// (touchDebugRing) AND mirrors it to console with a `[tsc]` prefix so a
// `grep tsc` in Safari Web Inspector / Xcode logs picks it up. The ring
// is a fallback for when console history has scrolled off before the user
// dumps it — installTouchDebugDump() exposes it as
// `window.__attermTouchDebug()` for on-device inspection.
//
// Kept separate from TerminalView.vue so the mega-SFC only ships call
// sites (logTouch(...)), not the ring/toggle plumbing.

export type TouchDebugEntry = { t: number; tag: string; data: unknown };

const TOUCH_DEBUG_MAX = 300;
const touchDebugRing: TouchDebugEntry[] = [];
let touchDebugEnabled = false;

/** Read the atterm.debugTouch localStorage toggle. Wrapped in try/catch
 *  so a private-mode / disabled-storage browser silently falls through
 *  to "not enabled". */
export function readTouchDebugFlag(): boolean {
  try {
    const raw = window.localStorage.getItem("atterm.debugTouch");
    // Explicit opt-in only — the per-touchmove console.log serializations
    // are expensive enough on iOS Safari (especially with Web Inspector
    // attached) to noticeably drop touch events, which was masquerading
    // as a "swipe does nothing" scroll bug. Set atterm.debugTouch=1 to
    // re-enable when diagnosing.
    return raw === "1" || raw === "true";
  } catch {
    return false;
  }
}

/** Latch touch-scroll debug on/off. Call once with the return of
 *  readTouchDebugFlag() at component mount. Every logTouch() call after
 *  this either writes or no-ops based on this flag. */
export function setTouchDebugEnabled(enabled: boolean): void {
  touchDebugEnabled = enabled;
}

/** True when touch-scroll debug is currently enabled. Exposed for
 *  callers that want to skip an expensive "build the data object"
 *  step before invoking logTouch — logTouch itself already no-ops
 *  when disabled, but the caller may want to short-circuit its own
 *  reactive reads. */
export function isTouchDebugEnabled(): boolean {
  return touchDebugEnabled;
}

/** Append one entry to the ring (bounded to TOUCH_DEBUG_MAX) + mirror
 *  to console with a `[tsc]` prefix. No-op when disabled. */
export function logTouch(tag: string, data: unknown = {}): void {
  if (!touchDebugEnabled) return;
  const entry = { t: Date.now(), tag, data };
  touchDebugRing.push(entry);
  if (touchDebugRing.length > TOUCH_DEBUG_MAX) touchDebugRing.shift();
  try {
    logDebug("touch-scroll", tag, { data: JSON.stringify(data) });
  } catch {
    /* swallow */
  }
}

/** Expose `window.__attermTouchDebug()` so a Safari Web Inspector
 *  session on a real device can dump the ring even after console
 *  history has scrolled off. Idempotent — safe to call more than once
 *  (last call wins the property). */
export function installTouchDebugDump(): void {
  try {
    (window as unknown as { __attermTouchDebug?: () => TouchDebugEntry[] }).__attermTouchDebug =
      () => touchDebugRing.slice();
  } catch {
    /* swallow */
  }
}

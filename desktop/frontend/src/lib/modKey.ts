// Cross-platform mod-key detection: five call sites previously each rolled
// their own `navigator.platform?.toLowerCase().includes("mac")` — the copies
// silently drifted (some checked `Mac/i.test`, some `platform.toLowerCase`).
// A single source of truth here means a future WebView / user-agent quirk
// only needs patching in one place.

/** True when running on a macOS host. Falls back to false server-side. */
export function isMac(): boolean {
  if (typeof navigator === "undefined") return false;
  return navigator.platform?.toLowerCase().includes("mac") ?? false;
}

/**
 * The DOM KeyboardEvent modifier key that behaves as the "primary" chord
 * key on this platform: `Meta` on macOS (⌘), `Control` elsewhere.
 * Consumed by the terminal shortcut listener and by the Settings hint UI.
 */
export function modKey(): "Meta" | "Control" {
  return isMac() ? "Meta" : "Control";
}

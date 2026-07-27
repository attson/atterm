// jsdom (24.x) exposes a `crypto` global with `getRandomValues` but no
// `subtle`. The OPAQUE login/register flow (@cloudflare/opaque-ts) needs
// `crypto.subtle.digest`, so without this shim those tests throw
// "Cannot read properties of undefined (reading 'digest')" before any
// network call. Real runtimes (browser, Capacitor WebView, Node) all ship
// full WebCrypto — this just brings the test env in line with them.
import { webcrypto } from "node:crypto";
import { afterEach } from "vitest";
import { enableAutoUnmount } from "@vue/test-utils";

// Component tests across the suite mount App (and other components) without
// always unmounting afterward. Vue-level effects usually don't leak across
// tests, but document-level listeners registered via composables (e.g.
// useTerminalShortcuts' capture-phase keydown handler) survive an un-unmounted
// component for the rest of the file, so a later test's synthetic keydown can
// be picked up by every earlier still-mounted App instance too. Auto-unmount
// after each test closes those listeners down deterministically.
enableAutoUnmount(afterEach);

// App.vue's web-only tabs snapshot (lib/webTabsSnapshot) reads/writes real
// jsdom localStorage/sessionStorage keyed by a per-session window id. Since
// every test file shares one jsdom global, a mounted App instance whose
// tabs change can debounce-save a snapshot (300ms real setTimeout) that
// outlives the test and leaks into a later mount's loadSnapshot() call —
// same class of cross-test bleed as the listener leak above, just via
// Storage instead of `document`. Clear both after every test so no test's
// window id / snapshot survives into the next.
afterEach(() => {
  localStorage.clear();
  sessionStorage.clear();
});

if (!globalThis.crypto?.subtle) {
  Object.defineProperty(globalThis, "crypto", {
    value: webcrypto,
    configurable: true,
  });
}

// jsdom does not implement URL.createObjectURL / revokeObjectURL. Components
// that preview pasted images (PasteImagePreviewHost) call these for blob URLs;
// real browsers and the Capacitor WebView ship both. Provide no-op defaults so
// component tests can `vi.spyOn(URL, "createObjectURL")` without first having
// to define the method.
if (typeof URL.createObjectURL !== "function") {
  // @ts-expect-error - augmenting jsdom's URL constructor for parity with browsers
  URL.createObjectURL = () => "blob:jsdom/stub";
}
if (typeof URL.revokeObjectURL !== "function") {
  // @ts-expect-error - augmenting jsdom's URL constructor for parity with browsers
  URL.revokeObjectURL = () => {};
}

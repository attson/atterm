// jsdom (24.x) exposes a `crypto` global with `getRandomValues` but no
// `subtle`. The OPAQUE login/register flow (@cloudflare/opaque-ts) needs
// `crypto.subtle.digest`, so without this shim those tests throw
// "Cannot read properties of undefined (reading 'digest')" before any
// network call. Real runtimes (browser, Capacitor WebView, Node) all ship
// full WebCrypto — this just brings the test env in line with them.
import { webcrypto } from "node:crypto";

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

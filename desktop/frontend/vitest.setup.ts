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

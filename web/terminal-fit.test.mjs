import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const app = await readFile(new URL("./app.js", import.meta.url), "utf8");
const css = await readFile(new URL("./style.css", import.meta.url), "utf8");
const sw = await readFile(new URL("./sw.js", import.meta.url), "utf8");

function styleBlockFor(selector) {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  return match?.[1] ?? "";
}

test("web terminal retries fit after layout settles", () => {
  assert.match(app, /function fitTerminal\(\)/);
  assert.match(app, /proposeDimensions/);
  assert.match(app, /function retryFitAfterLayout\(\)/);
  assert.match(app, /requestAnimationFrame/);
  assert.match(app, /ResizeObserver/);
});

test("web terminal padding is on xterm so FitAddon accounts for it", () => {
  assert.doesNotMatch(styleBlockFor("#term"), /padding\s*:/);
  const xtermStyle = styleBlockFor("#term .xterm");
  assert.match(xtermStyle, /padding\s*:[^;]*\b6px\b/);
  assert.match(xtermStyle, /padding\s*:[^;]*\b8px\b/);
  assert.match(xtermStyle, /padding\s*:[^;]*env\(safe-area-inset-left\)/);
  assert.match(xtermStyle, /padding\s*:[^;]*env\(safe-area-inset-right\)/);
});

test("web terminal uses a route class for full-height layout", () => {
  assert.match(app, /classList\.toggle\("terminal-active", Boolean\(sessionId\)\)/);
  assert.match(css, /body\.terminal-active\s*\{[^}]*height\s*:\s*var\(--app-height\)[^}]*overflow\s*:\s*hidden/s);
  assert.match(styleBlockFor("body.terminal-active #app-shell"), /height\s*:\s*var\(--app-height\)/);
  assert.match(styleBlockFor("body.terminal-active #main"), /display\s*:\s*flex/);
  assert.match(styleBlockFor("body.terminal-active #main"), /min-height\s*:\s*0/);
  assert.match(styleBlockFor("body.terminal-active #term-view"), /flex\s*:\s*1 1 auto/);
});

test("service worker cache is bumped for web shell asset changes", () => {
  assert.match(sw, /at-term-web-v7/);
});

test("service worker precaches login/signup/settings js so split-out auth-page modules stay consistent", () => {
  // login.js / signup.js / settings.js are now the bootstraps for the
  // auth pages. If sw caches a stale app-core.js without also pre-caching
  // these, the page imports a fetchVersionLabel that doesn't exist and
  // submit handlers never bind — exactly the v0.1.66 outage. Keep all of
  // them in the cache list so a CACHE bump refreshes them atomically.
  assert.match(sw, /\.\/login\.js/);
  assert.match(sw, /\.\/signup\.js/);
  assert.match(sw, /\.\/settings\.js/);
});

import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const app = await readFile(new URL("./app.js", import.meta.url), "utf8");
const css = await readFile(new URL("./style.css", import.meta.url), "utf8");

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

// CACHE-name freshness is now enforced by sw-cache-bump.test.mjs: it
// hashes ASSETS contents and asserts CACHE encodes the hash, so the old
// "is the literal version string still vN" check would be redundant and
// fight that test on every legitimate asset change.


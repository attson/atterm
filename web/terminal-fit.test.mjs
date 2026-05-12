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
  assert.match(styleBlockFor("#term .xterm"), /padding\s*:\s*6px 8px/);
});

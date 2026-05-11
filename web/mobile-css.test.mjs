import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const css = await readFile(new URL("./style.css", import.meta.url), "utf8");

test("css includes safe-area and mobile terminal controls", () => {
  assert.match(css, /env\(safe-area-inset-top\)/);
  assert.match(css, /env\(safe-area-inset-bottom\)/);
  assert.match(css, /#shortcut-bar/);
  assert.match(css, /#paste-fallback/);
  assert.match(css, /visual|100dvh|--app-height/);
});

test("css keeps desktop responsive session grid", () => {
  assert.match(css, /@media \(min-width: 720px\)/);
  assert.match(css, /grid-template-columns: repeat\(auto-fill, minmax\(320px, 1fr\)\)/);
});

test("css hides mobile terminal shortcuts on desktop web", () => {
  assert.match(css, /body\.desktop-web #shortcut-bar/);
  assert.match(css, /display: none/);
});

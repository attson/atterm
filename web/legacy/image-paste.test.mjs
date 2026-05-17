import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const css = await readFile(new URL("./style.css", import.meta.url), "utf8");
const app = await readFile(new URL("./app.js", import.meta.url), "utf8");

test("paste fallback has a file picker for image upload", () => {
  assert.match(html, /id="paste-image-file"[^>]*type="file"/);
  assert.match(html, /accept="image\/\*"/);
  assert.match(html, /for="paste-image-file"/);
});

test("pick-image label is styled like a paste-action button", () => {
  const style = css.match(/\.paste-image-pick\s*\{([^}]*)\}/)?.[1] ?? "";
  assert.match(style, /border-radius\s*:\s*999px/);
  assert.match(style, /cursor\s*:\s*pointer/);
  assert.match(style, /touch-action\s*:\s*manipulation/);
});

test("Paste click opens the modal on mobile instead of trying the clipboard", () => {
  // mobile-web short-circuits to the fallback so the file picker is always reachable
  assert.match(app, /currentClientMode\(\)\s*===\s*"mobile-web"[^]*?openPasteFallback/);
});

test("file input change handler streams image bytes via sendImagePaste", () => {
  assert.match(app, /pasteImageFile\.addEventListener\("change"/);
  assert.match(app, /sendImagePaste\(file,\s*file\.name/);
});

test("non-image file selection surfaces an error instead of pasting", () => {
  assert.match(app, /file\.type\.startsWith\("image\/"\)/);
  assert.match(app, /setStatus\("not an image",\s*"err"\)/);
});

import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const manifest = JSON.parse(await readFile(new URL("./manifest.webmanifest", import.meta.url), "utf8"));

test("index contains PWA and iOS metadata", () => {
  assert.match(html, /<link rel="manifest" href="manifest\.webmanifest"/);
  assert.match(html, /<meta name="apple-mobile-web-app-capable" content="yes"/);
  assert.match(html, /<meta name="apple-mobile-web-app-title" content="atterm"/);
  assert.match(html, /<link rel="apple-touch-icon" href="icon\.svg"/);
  assert.match(html, /<script type="module" src="app\.js"><\/script>/);
});

test("manifest is installable and scoped to relay root", () => {
  assert.equal(manifest.name, "atterm");
  assert.equal(manifest.short_name, "atterm");
  assert.equal(manifest.start_url, ".");
  assert.equal(manifest.scope, ".");
  assert.equal(manifest.display, "standalone");
  assert.equal(manifest.icons[0].src, "icon.svg");
});

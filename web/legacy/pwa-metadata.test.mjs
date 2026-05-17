import test from "node:test";
import assert from "node:assert/strict";
import { readFile, stat } from "node:fs/promises";

const html = await readFile(new URL("./index.html", import.meta.url), "utf8");
const manifest = JSON.parse(await readFile(new URL("./manifest.webmanifest", import.meta.url), "utf8"));
const sw = await readFile(new URL("./sw.js", import.meta.url), "utf8");

test("index contains PWA and iOS metadata", () => {
  assert.match(html, /<link rel="manifest" href="manifest\.webmanifest"/);
  assert.match(html, /<meta name="apple-mobile-web-app-capable" content="yes"/);
  assert.match(html, /<meta name="apple-mobile-web-app-title" content="AT Term"/);
  assert.match(html, /<link rel="apple-touch-icon" href="icon\.png"/);
  assert.match(html, /<title>AT Term<\/title>/);
  assert.match(html, /<section id="install-hint"/);
  assert.match(html, /Add to Home Screen/);
  assert.match(html, /<script type="module" src="app\.js"><\/script>/);
  // Token panel and insecure-mode input removed; cookie auth is used instead.
  assert.doesNotMatch(html, /id="token-panel"/);
  assert.doesNotMatch(html, /id="token-toggle"/);
  // Version div and sign-out button are now rendered by layout.js; asserts moved to nav.test.mjs.
});

test("index uses vendored terminal assets only", () => {
  assert.doesNotMatch(html, /cdn\.jsdelivr\.net/);
  assert.match(html, /<link rel="stylesheet" href="vendor\/xterm\/xterm\.css"/);
  assert.match(html, /<script src="vendor\/xterm\/xterm\.js"><\/script>/);
  assert.match(html, /<script src="vendor\/xterm-addon-fit\/xterm-addon-fit\.js"><\/script>/);
});

test("manifest is installable and scoped to relay root", () => {
  assert.equal(manifest.name, "AT Term");
  assert.equal(manifest.short_name, "AT Term");
  assert.equal(manifest.start_url, ".");
  assert.equal(manifest.scope, ".");
  assert.equal(manifest.display, "standalone");
  assert.equal(manifest.icons[0].src, "icon.png");
  assert.equal(manifest.icons[0].type, "image/png");
});

test("PWA PNG icon exists for iOS install surfaces", async () => {
  const icon = await stat(new URL("./icon.png", import.meta.url));
  assert.ok(icon.isFile());
  assert.ok(icon.size > 0);
});

test("service worker is present for secure-context PWA installs", async () => {
  assert.match(sw, /self\.addEventListener\("install"/);
  assert.match(sw, /self\.addEventListener\("fetch"/);
});

test("service worker caches vendored terminal assets", () => {
  assert.match(sw, /"\.\/vendor\/xterm\/xterm\.css"/);
  assert.match(sw, /"\.\/vendor\/xterm\/xterm\.js"/);
  assert.match(sw, /"\.\/vendor\/xterm-addon-fit\/xterm-addon-fit\.js"/);
});

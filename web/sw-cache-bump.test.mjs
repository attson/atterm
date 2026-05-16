// PR #34 outage: app-core.js gained a new export but web/sw.js still used
// the same CACHE name, so installed clients kept serving the old module
// and login broke. This test fails whenever any precached asset changes
// without a corresponding CACHE bump, and tells you exactly which hex
// suffix to paste in.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createHash } from "node:crypto";
import path from "node:path";

const sw = readFileSync("web/sw.js", "utf8");

const cacheMatch = sw.match(/const CACHE = "([^"]+)"/);
assert.ok(cacheMatch, "web/sw.js must declare `const CACHE = \"...\"`");
const cacheName = cacheMatch[1];

const assetsMatch = sw.match(/const ASSETS = \[([\s\S]*?)\];/);
assert.ok(assetsMatch, "web/sw.js must declare `const ASSETS = [...]`");
const assets = [...assetsMatch[1].matchAll(/"([^"]+)"/g)].map((m) => m[1]);

// sw.js precaches "./" as a synonym for the root navigation document.
// Hash the file the browser actually receives for that key (index.html).
function resolveAssetPath(asset) {
    return asset === "./" ? "./index.html" : asset;
}

const hasher = createHash("sha256");
for (const asset of assets) {
    const rel = resolveAssetPath(asset);
    const filePath = path.join("web", rel);
    hasher.update(asset);
    hasher.update("\0");
    hasher.update(readFileSync(filePath));
    hasher.update("\0");
}
const expectedHash = hasher.digest("hex").slice(0, 8);

test("sw.js CACHE name embeds the current ASSETS content hash", () => {
    assert.ok(
        cacheName.includes(expectedHash),
        `web/sw.js CACHE = "${cacheName}" is stale. Replace it with ` +
            `"at-term-web-${expectedHash}" (or any string containing ` +
            `"${expectedHash}"). The CACHE name must change whenever any ` +
            `precached asset changes — otherwise installed PWA clients keep ` +
            `serving the old version of that asset (see PR #34).`,
    );
});

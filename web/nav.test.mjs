// PR B moved the topnav from each page's hand-written HTML into
// web/layout.js. The static HTML now contains a meta page identifier,
// an empty <header id="topbar"> placeholder, and a layout.js import.
// Runtime rendering correctness is covered by web/layout.test.mjs;
// this file just asserts the placeholder contract on each authenticated
// page so the layout shell wires up correctly.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const PAGES = [
    { path: "web/index.html", page: "home" },
    { path: "web/settings.html", page: "settings" },
];

for (const { path, page } of PAGES) {
    test(`${path} declares <meta name="page" content="${page}">`, () => {
        const src = readFileSync(path, "utf8");
        const re = new RegExp(`<meta\\s+name="page"\\s+content="${page}"`);
        assert.match(src, re);
    });

    test(`${path} has empty #topbar placeholder for layout.js`, () => {
        const src = readFileSync(path, "utf8");
        assert.match(src, /<header\s+id="topbar"\s*>\s*<\/header>/);
        // Must NOT contain a hard-coded topnav anymore.
        assert.doesNotMatch(src, /<nav\s+class="topnav"/);
    });

    test(`${path} imports layout.js as an ES module`, () => {
        const src = readFileSync(path, "utf8");
        assert.match(src, /<script\s+type="module"\s+src="\.?\/?layout\.js"/);
    });
}

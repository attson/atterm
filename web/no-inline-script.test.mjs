// The relay sets `Content-Security-Policy: script-src 'self'` (no
// 'unsafe-inline'). Any inline <script>…</script> in a served HTML file is
// silently dropped by the browser, breaking auth and other client logic.
// This test scans every .html file in web/ and fails if it finds a <script>
// tag without a `src` attribute.

import { test } from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";

const SCRIPT_TAG = /<script\b([^>]*)>/gi;

function isInlineScript(attrs) {
    return !/\bsrc\s*=/.test(attrs);
}

test("no web/*.html contains an inline <script> block (CSP script-src 'self')", () => {
    const offenders = [];
    for (const name of readdirSync("web")) {
        if (!name.endsWith(".html")) continue;
        const src = readFileSync(path.join("web", name), "utf8");
        for (const m of src.matchAll(SCRIPT_TAG)) {
            if (isInlineScript(m[1])) {
                offenders.push(`${name}: <script${m[1]}>`);
            }
        }
    }
    assert.deepEqual(offenders, [],
        "Inline <script> blocks are blocked by CSP. Move them into an external .js " +
        "file and reference it via <script type=\"module\" src=\"./file.js\"></script>.");
});

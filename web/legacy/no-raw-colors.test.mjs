// PR E established a design-token system. Going forward, raw #rgb /
// rgba() literals in style.css and the inline <style> blocks of the
// authenticated pages are a smell — they break the "one source of
// truth" invariant the tokens give us. Allow a narrow set of
// intentional exceptions (gradient stops, shadows, one-off visual
// treatments documented in the PR E plan).

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";

const HEX_RGBA = /#[0-9a-fA-F]{3,8}\b|rgba?\([^)]*\)/g;

// File → patterns it's allowed to keep raw. Matchers are substrings the
// problematic value contains; if a hit matches any of these, it's OK.
const ALLOWED = {
    "web/legacy/style.css": [
        // Token declarations themselves.
        "--",
        // Body / page background gradients use one-off accent dark stops.
        "#172554",
        // Install-hint banner — one-shot visual treatment, see plan task 2.
        "install-hint",
        // .check-row status badge colors — out of scope desktop-centric UI.
        "rgba(251, 191, 36, 0.28)",
        "rgba(120, 53, 15, 0.18)",
        // Install dismiss button and install-hint accents — one-off treatment.
        "rgba(125, 211, 252, 0.28)",
        "rgba(219, 234, 254, 0.36)",
        "rgba(2, 6, 23, 0.24)",
        // Tile grid/modal overlays — one-off non-token backgrounds.
        "rgba(15, 23, 42, 0.96)",
        "rgba(14, 116, 144, 0.92)",
        "rgba(15, 23, 42, 0.98)",
        "rgba(17, 24, 39, 0.92)",
        "rgba(148, 163, 184, 0.2)",
        "#000",
        "rgba(148, 163, 184, 0.28)",
        "rgba(2, 6, 23, 0.88)",
        "rgba(148, 163, 184, 0.24)",
        "rgba(2, 6, 23, 0.96)",
        "#334155",
        "#0f172a",
        "#020617",
        // .ghost-btn etc. translucent button family (kept raw deliberately).
        "rgba(15, 23, 42, 0.9)",
        // Scrollbar rgb overlays.
        "scrollbar",
        // Drop-shadows and box-shadow values are one-off intentional rgbas.
        "box-shadow",
        // Replay progress / vendor xterm overlays — out of scope for PR E.
        "replay-progress",
        "xterm",
    ],
    "web/legacy/admin/index.html": [
        "--",
        "#1e1b4b",                    // admin gradient violet stop
        "box-shadow",
        "rgba(0, 0, 0, 0.3)",         // code box backdrop
    ],
};

function leakLines(path) {
    const src = readFileSync(path, "utf8");
    const lines = src.split("\n");
    const allowed = ALLOWED[path] ?? [];
    const leaks = [];
    for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        if (!HEX_RGBA.test(line)) { HEX_RGBA.lastIndex = 0; continue; }
        HEX_RGBA.lastIndex = 0;
        if (allowed.some((tag) => line.includes(tag))) continue;
        leaks.push(`${path}:${i + 1}: ${line.trim()}`);
    }
    return leaks;
}

test("web/legacy/style.css has no raw #hex / rgba() outside the allow-list", () => {
    const leaks = leakLines("web/legacy/style.css");
    assert.equal(leaks.length, 0, "raw color literals found:\n" + leaks.join("\n"));
});

test("web/legacy/admin/index.html inline <style> has no raw colors outside the allow-list", () => {
    const leaks = leakLines("web/legacy/admin/index.html");
    assert.equal(leaks.length, 0, "raw color literals found:\n" + leaks.join("\n"));
});

